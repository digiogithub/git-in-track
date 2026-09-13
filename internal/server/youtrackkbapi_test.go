package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/vault"
)

// The REST surface of the knowledge-base synchronization, task GIT-T-0214.

// linkFixtureToYouTrack appends an `integrations.youtrack` block to the
// project.yaml of a copied fixture, which is what makes the routes of
// youtrackkbapi.go answer at all: they are gated on a linked project.
func linkFixtureToYouTrack(t *testing.T, root string) {
	t.Helper()

	path := filepath.Join(root, "docs", ".pmngr", "project.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // a temporary copy of the fixture
	if err != nil {
		t.Fatalf("read the fixture project.yaml: %v", err)
	}
	block := "\nintegrations:\n  youtrack:\n    url: https://yt.example.com/youtrack\n" +
		"    project: DEMO\n    push_comments: manual\n    kb_sync: manual\n    kb_sync_direction: push\n"
	if err := os.WriteFile(path, append(data, []byte(block)...), 0o644); err != nil { //nolint:gosec // a fixture copy
		t.Fatalf("write the fixture project.yaml: %v", err)
	}
}

// newKBAPIServer builds a companion over a linked copy of the fixture, with the
// job engine running so that a publish or a pull can actually queue.
func newKBAPIServer(t *testing.T) (*Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	linkFixtureToYouTrack(t, root)
	s, _ := newJobServerIn(t, newFakeYouTrack(), root)
	s.startSyncEngine(t.Context())
	t.Cleanup(func() { s.stopSyncEngine(t.Context()) })
	return s, root
}

// TestKBSyncStatusRoute covers the first acceptance criterion: the status route
// answers with one row per page, under every mount, and never consults the
// remote side unless it was asked to.
func TestKBSyncStatusRoute(t *testing.T) {
	t.Parallel()

	s, _ := newKBAPIServer(t)
	for _, target := range []string{
		"/api/v1/youtrack/kb/status?key=DEMO&recursive=true",
		"/api/v1/projects/DEMO/kb/youtrack/status?recursive=true",
		"/api/v1/kb/youtrack/status?project=DEMO&recursive=true",
	} {
		t.Run(target, func(t *testing.T) {
			var got vault.YouTrackKBStatusResult
			decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusOK, &got)
			if got.Project != "DEMO" {
				t.Errorf("project: got %q, want DEMO", got.Project)
			}
			if len(got.Pages) == 0 {
				t.Fatal("the status reported no page at all")
			}
			if got.Remote {
				t.Error("remote must stay opt-in: the route defaulted it on")
			}
			for _, page := range got.Pages {
				if page.State != vault.KBStateUnlinked {
					t.Errorf("%s: got state %q, want unlinked", page.Path, page.State)
				}
			}
		})
	}
}

// TestKBSyncQueueRoutes covers the publish and pull routes: both answer 202
// with a job id and the pages the job selected.
func TestKBSyncQueueRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		target string
	}{
		{name: "publish", target: "/api/v1/youtrack/kb/publish?key=DEMO"},
		{name: "pull", target: "/api/v1/youtrack/kb/pull?key=DEMO"},
		{name: "publish scoped by project", target: "/api/v1/projects/DEMO/kb/youtrack/publish"},
		{name: "pull scoped by project", target: "/api/v1/projects/DEMO/kb/youtrack/pull"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newKBAPIServer(t)
			var got vault.YouTrackKBJobResult
			decode(t, send(t, s, request{
				method: http.MethodPost, target: tc.target,
				body: youtrackKBRequest{Path: "docs", Recursive: true},
			}), http.StatusAccepted, &got)
			if got.JobID == "" {
				t.Error("the answer carries no job id")
			}
			if len(got.Pages) == 0 {
				t.Error("the answer selected no page")
			}
		})
	}
}

// TestKBSyncRoutesRefuseAnUnlinkedProject covers the gating: a project with no
// `integrations.youtrack` block answers 404 on all three routes.
func TestKBSyncRoutesRefuseAnUnlinkedProject(t *testing.T) {
	t.Parallel()

	s, _ := newJobServerIn(t, newFakeYouTrack(), copyTree(t, fixtureRoot))
	for _, tc := range []struct {
		name   string
		method string
		target string
	}{
		{name: "status", method: http.MethodGet, target: "/api/v1/youtrack/kb/status?key=DEMO"},
		{name: "publish", method: http.MethodPost, target: "/api/v1/youtrack/kb/publish?key=DEMO"},
		{name: "pull", method: http.MethodPost, target: "/api/v1/youtrack/kb/pull?key=DEMO"},
		{name: "status scoped by project", method: http.MethodGet, target: "/api/v1/projects/DEMO/kb/youtrack/status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, s, request{method: tc.method, target: tc.target})
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status: got %d, want 404: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "not connected to YouTrack") {
				t.Errorf("the problem does not say why: %s", rec.Body.String())
			}
		})
	}
}

// TestKBSyncRoutesNeedTheToken covers the authentication: the routes sit inside
// the bearer-auth group like every other one.
func TestKBSyncRoutesNeedTheToken(t *testing.T) {
	t.Parallel()

	s, _ := newKBAPIServer(t)
	res := do(t, s, http.MethodGet, "/api/v1/youtrack/kb/status?key=DEMO", nil)
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", res.StatusCode)
	}
}

// TestQueryBool pins the parsing of the optional boolean query parameters, the
// bare `?remote` form included.
func TestQueryBool(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		query string
		want  bool
	}{
		{query: "", want: false},
		{query: "?remote", want: true},
		{query: "?remote=true", want: true},
		{query: "?remote=1", want: true},
		{query: "?remote=false", want: false},
		{query: "?remote=nonsense", want: false},
		{query: "?other=true", want: false},
	} {
		t.Run("query"+tc.query, func(t *testing.T) {
			t.Parallel()

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
				"/api/v1/youtrack/kb/status"+tc.query, nil)
			if got := queryBool(r, "remote"); got != tc.want {
				t.Errorf("queryBool(%q): got %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}
