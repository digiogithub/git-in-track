package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/pando"
)

// TestServeRegistersTheRepositoryRootWithPando covers the first promise of
// GIT-US-0098: starting the server is what makes a fresh clone searchable, with
// the repository root handed to Pando as a code project under the id the search
// half reads from.
func TestServeRegistersTheRepositoryRootWithPando(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	fake := &fakePando{}
	installPando(t, s, fake)

	s.search.registerCodeProjects(context.Background())

	indexed := fake.indexedProjects()
	if len(indexed) != 1 || indexed[0] != root {
		t.Fatalf("registered %v, want the repository root %q once", indexed, root)
	}
	view := s.search.codeIndexOf(testRepoID)
	if view == nil {
		t.Fatal("the registration reported nothing for the mounted repository")
	}
	if view.Status != codeIndexStatusIndexing || view.Job == "" {
		t.Errorf("registration = %+v, want an indexing job", view)
	}
	if want := pando.SanitizeProjectID(root); view.Project != want {
		t.Errorf("project = %q, want the id the search reads from, %q", view.Project, want)
	}
}

// TestCodeProjectRegistrationIsIdempotent pins the second start: Pando already
// holds the project, so nothing is duplicated and no fresh index is asked for.
func TestCodeProjectRegistrationIsIdempotent(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	fake := &fakePando{projects: []pando.Project{
		{ProjectID: pando.SanitizeProjectID(root), RootPath: root, IndexingStatus: "completed"},
	}}
	installPando(t, s, fake)

	s.search.registerCodeProjects(context.Background())
	s.search.registerCodeProjects(context.Background())

	if indexed := fake.indexedProjects(); len(indexed) != 0 {
		t.Fatalf("a registered project was reindexed: %v", indexed)
	}
	view := s.search.codeIndexOf(testRepoID)
	if view == nil || view.Status != codeIndexStatusRegistered {
		t.Fatalf("registration = %+v, want the project left alone", view)
	}
}

// TestCodeProjectRegistrationIsRedoneForAnotherTree covers the one case that is
// not idempotent: Pando holds the id, but pointed at a different working tree.
func TestCodeProjectRegistrationIsRedoneForAnotherTree(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	fake := &fakePando{projects: []pando.Project{
		{ProjectID: pando.SanitizeProjectID(root), RootPath: "/somewhere/else"},
	}}
	installPando(t, s, fake)

	s.search.registerCodeProjects(context.Background())

	if indexed := fake.indexedProjects(); len(indexed) != 1 || indexed[0] != root {
		t.Fatalf("registered %v, want the mounted tree %q", indexed, root)
	}
}

// TestCodeProjectRegistrationNeverBlocksStartup is the third promise: a Pando
// that is unreachable, or slow enough to still be thinking, leaves the
// companion fully functional — and says why there is no code search.
func TestCodeProjectRegistrationNeverBlocksStartup(t *testing.T) {
	t.Parallel()

	t.Run("a Pando that does not answer", func(t *testing.T) {
		t.Parallel()

		s, _ := newAPIServer(t)
		gate := make(chan struct{})
		fake := &fakePando{indexGate: gate}
		installPando(t, s, fake)

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.search.startRegistration(context.Background())
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("starting the registration blocked on Pando")
		}
		// The companion serves its own index while Pando is still thinking.
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/items?limit=1"})
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /items during registration = %d, want 200", rec.Code)
		}
		close(gate)
	})

	t.Run("a Pando that refuses", func(t *testing.T) {
		t.Parallel()

		s, _ := newAPIServer(t)
		installPando(t, s, &fakePando{indexErr: pando.ErrUnreachable})

		s.search.registerCodeProjects(context.Background())

		view := s.search.codeIndexOf(testRepoID)
		if view == nil || view.Status != codeIndexStatusUnavailable {
			t.Fatalf("registration = %+v, want the failure recorded", view)
		}
		if view.Note == "" {
			t.Error("the settings card is given no reason for the missing code search")
		}
		// And the search still answers, from the core index.
		got := searchFor(t, s, "/api/v1/search?q=checkout&limit=5")
		if got.Engine == "" {
			t.Errorf("search stopped working after a failed registration: %+v", got)
		}
	})
}

// TestSearchSettingsReportsTheCodeIndex pins the reason reaching the card.
func TestSearchSettingsReportsTheCodeIndex(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	installPando(t, s, &fakePando{})
	s.search.registerCodeProjects(context.Background())

	var view settingsView
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/search/settings"}),
		http.StatusOK, &view)
	if len(view.Indexed) != 1 {
		t.Fatalf("indexed = %+v, want one repository", view.Indexed)
	}
	code := view.Indexed[0].Code
	if code == nil {
		t.Fatal("the settings card says nothing about the code index")
	}
	if code.Status != codeIndexStatusIndexing || code.Project != pando.SanitizeProjectID(root) {
		t.Errorf("code = %+v, want the registered project", code)
	}
}

// TestReindexUsesTheRegisteredProject keeps the explicit reindex and the
// registration pointed at one project: a reindex that refreshed a different id
// would leave the search reading a stale index.
func TestReindexUsesTheRegisteredProject(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	fake := &fakePando{}
	installPando(t, s, fake)

	job, err := s.search.startReindex(context.Background())
	if err != nil {
		t.Fatalf("startReindex(): %v", err)
	}
	waitForReindex(t, s, job.ID)

	if indexed := fake.indexedProjects(); len(indexed) != 1 || indexed[0] != root {
		t.Fatalf("reindexed %v, want the mounted tree %q", indexed, root)
	}
	view := s.search.codeIndexOf(testRepoID)
	if view == nil || view.Project != pando.SanitizeProjectID(root) {
		t.Fatalf("the reindex recorded %+v, want the registered project", view)
	}
}
