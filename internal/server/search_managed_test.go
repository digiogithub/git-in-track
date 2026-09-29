package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// fakeInstance is a ManagedInstance that spawns nothing: the supervisor's own
// tests cover the process handling, and these cover what the server does with
// the states it reports.
type fakeInstance struct {
	mu       sync.Mutex
	st       supervisor.Status
	url      string
	startErr error
	starts   int
	stops    int
	restarts int
}

func (f *fakeInstance) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return f.startErr
}

func (f *fakeInstance) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}

func (f *fakeInstance) Restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restarts++
}

func (f *fakeInstance) Status() supervisor.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st
}

func (f *fakeInstance) Endpoint() (string, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.st.State != supervisor.StateReady {
		return "", "", false
	}
	return f.url, "instance-token", true
}

// set moves the instance to a state.
func (f *fakeInstance) set(state supervisor.State, pid int, lastErr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st.State, f.st.PID, f.st.LastError = state, pid, lastErr
	f.st.Version = "pando v1.1.1"
	if pid > 0 {
		f.st.Port = 40000 + pid
	}
}

func (f *fakeInstance) counts() (starts, stops, restarts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.stops, f.restarts
}

// graphPando is a fakePando whose client also reads the code graph.
type graphPando struct {
	*fakePando
	mu       sync.Mutex
	analyzed []string
}

func (g *graphPando) ImpactAnalysis(_ context.Context, projectID string, _ []string, _ pando.ImpactOptions) (pando.ImpactResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.analyzed = append(g.analyzed, projectID)
	return pando.ImpactResult{}, nil
}

// managedRig is a server in managed mode over several repositories with a fake
// instance and a fake Pando behind each.
type managedRig struct {
	s     *Server
	roots map[string]string
	inst  map[string]*fakeInstance
	pando map[string]*graphPando
	opts  map[string]supervisor.Options
	mu    sync.Mutex
}

type managedRigOptions struct {
	// repos are the mount ids, in order; optIn names the ones that opted in.
	repos []string
	optIn []string
	// max is search.pando.managed.maxInstances, 0 for the default.
	max int
	// binary is what the PATH lookup answers; empty means not found.
	binary string
	// mode overrides search.pando.mode.
	mode string
	// mcpURL sets an external endpoint.
	mcpURL string
}

func newManagedRig(t *testing.T, o managedRigOptions) *managedRig {
	t.Helper()

	rig := &managedRig{
		roots: map[string]string{}, inst: map[string]*fakeInstance{},
		pando: map[string]*graphPando{}, opts: map[string]supervisor.Options{},
	}
	var repos []Repo
	for _, id := range o.repos {
		root := copyTree(t, fixtureRoot)
		rig.roots[id] = root
		repos = append(repos, Repo{
			ID: id, Path: root, Role: "project", DocsFolder: "docs",
			SemanticSearch: contains(o.optIn, id),
		})
		rig.inst[id] = &fakeInstance{url: "fake://" + id}
		rig.pando[id] = &graphPando{fakePando: &fakePando{}}
	}
	byURL := func(url string) *graphPando {
		for id, inst := range rig.inst {
			if inst.url == url {
				return rig.pando[id]
			}
		}
		return nil
	}
	mode := o.mode
	if mode == "" {
		mode = config.PandoModeAuto
	}
	s, err := New(Options{
		Token: "test-token", Workspace: "test", Repos: repos, CacheDir: t.TempDir(),
		Now: func() time.Time { return time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC) },
		Search: config.Search{Pando: config.SearchPando{
			Mode: mode, MCPURL: o.mcpURL, Managed: config.PandoManaged{MaxInstances: o.max},
		}},
		pandoLookPath: func(string) (string, error) {
			if o.binary == "" {
				return "", errors.New("not found")
			}
			return o.binary, nil
		},
		pandoNewInstance: func(so supervisor.Options) (ManagedInstance, error) {
			rig.mu.Lock()
			defer rig.mu.Unlock()
			for id, root := range rig.roots {
				if root == so.RepoRoot {
					rig.opts[id] = so
					return rig.inst[id], nil
				}
			}
			return nil, errors.New("unknown repository " + so.RepoRoot)
		},
		pandoNewClient: func(url, token, project string) (pandoAPI, error) {
			if token != "instance-token" {
				return nil, errors.New("the client was built without the instance token")
			}
			g := byURL(url)
			if g == nil {
				return nil, errors.New("no fake behind " + url)
			}
			return g, nil
		},
		pandoTick: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	rig.s = s
	return rig
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// begin starts the managed instances as Server.Start does, and stops them when
// the test ends.
func (r *managedRig) begin(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	r.s.search.startManaged(ctx)
	t.Cleanup(func() {
		cancel()
		r.s.search.stopManaged(context.Background())
	})
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// managedSettings is the part of /api/v1/search/settings this story adds.
type managedSettings struct {
	Backend      string `json:"backend"`
	Configured   bool   `json:"configured"`
	Reachable    *bool  `json:"reachable"`
	Mode         string `json:"mode"`
	ModeRule     int    `json:"modeRule"`
	ModeReason   string `json:"modeReason"`
	Binary       string `json:"binary"`
	MaxInstances int    `json:"maxInstances"`
	Indexed      []struct {
		Repo string `json:"repo"`
		Code *struct {
			Project string `json:"project"`
			Status  string `json:"status"`
		} `json:"code"`
		Managed *struct {
			OptedIn bool   `json:"optedIn"`
			State   string `json:"state"`
			PID     int    `json:"pid"`
			Port    int    `json:"port"`
			Version string `json:"version"`
			Error   string `json:"error"`
		} `json:"managed"`
	} `json:"indexed"`
	Reindex *reindexWire `json:"reindex"`
}

func (r *managedRig) settings(t *testing.T) managedSettings {
	t.Helper()

	var view managedSettings
	decode(t, send(t, r.s, request{method: http.MethodGet, target: "/api/v1/search/settings"}),
		http.StatusOK, &view)
	return view
}

func (v managedSettings) row(t *testing.T, repo string) managedRowView {
	t.Helper()

	for _, row := range v.Indexed {
		if row.Repo == repo {
			if row.Managed == nil {
				t.Fatalf("row %s carries no managed state", repo)
			}
			return managedRowView{
				OptedIn: row.Managed.OptedIn, State: row.Managed.State, PID: row.Managed.PID,
				Port: row.Managed.Port, Version: row.Managed.Version, Error: row.Managed.Error,
				CodeStatus: codeStatus(row.Code),
			}
		}
	}
	t.Fatalf("no row for %s in %+v", repo, v.Indexed)
	return managedRowView{}
}

type managedRowView struct {
	OptedIn                 bool
	State, Version, Error   string
	PID, Port               int
	CodeStatus, CodeProject string
}

func codeStatus(c *struct {
	Project string `json:"project"`
	Status  string `json:"status"`
},
) string {
	if c == nil {
		return ""
	}
	return c.Status
}

// TestManagedStartsOneInstancePerOptedInRepository covers the start: one
// supervisor per opted-in repository, capped by maxInstances, the rest
// reported as skipped with the reason, the code project registered under
// SanitizeProjectID(root) once the instance is ready.
func TestManagedStartsOneInstancePerOptedInRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        managedRigOptions
		wantStarts  map[string]int
		wantSkipped []string
		wantState   map[string]string
	}{
		{
			name:       "every opted-in repository gets an instance",
			opts:       managedRigOptions{repos: []string{"a", "b", "c"}, optIn: []string{"a", "c"}, binary: "/bin/pando"},
			wantStarts: map[string]int{"a": 1, "b": 0, "c": 1},
			wantState:  map[string]string{"a": "starting", "b": "disabled", "c": "starting"},
		},
		{
			name:        "the cap skips the extra repositories",
			opts:        managedRigOptions{repos: []string{"a", "b", "c"}, optIn: []string{"a", "b", "c"}, max: 2, binary: "/bin/pando"},
			wantStarts:  map[string]int{"a": 1, "b": 1, "c": 0},
			wantSkipped: []string{"c"},
			wantState:   map[string]string{"a": "starting", "b": "starting", "c": "skipped"},
		},
		{
			name:        "no binary skips every opted-in repository",
			opts:        managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, mode: config.PandoModeManaged},
			wantStarts:  map[string]int{"a": 0},
			wantSkipped: []string{"a"},
			wantState:   map[string]string{"a": "skipped"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rig := newManagedRig(t, tc.opts)
			for _, inst := range rig.inst {
				inst.set(supervisor.StateStarting, 0, "")
			}
			rig.begin(t)

			eventually(t, "the instances to be started", func() bool {
				for id, want := range tc.wantStarts {
					if got, _, _ := rig.inst[id].counts(); got != want {
						return false
					}
				}
				return true
			})
			view := rig.settings(t)
			if view.Mode != "managed" {
				t.Errorf("mode = %q, want managed", view.Mode)
			}
			for id, want := range tc.wantState {
				if got := view.row(t, id).State; got != want {
					t.Errorf("state of %s = %q, want %q", id, got, want)
				}
			}
			for _, id := range tc.wantSkipped {
				if row := view.row(t, id); row.Error == "" {
					t.Errorf("skipped repository %s carries no reason", id)
				}
			}
		})
	}

	t.Run("the supervisor gets the repository, the binary and the cache directory", func(t *testing.T) {
		t.Parallel()

		rig := newManagedRig(t, managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/opt/pando"})
		rig.begin(t)
		eventually(t, "the instance to be built", func() bool {
			rig.mu.Lock()
			defer rig.mu.Unlock()
			_, ok := rig.opts["a"]
			return ok
		})
		rig.mu.Lock()
		got := rig.opts["a"]
		rig.mu.Unlock()
		if got.Binary != "/opt/pando" || got.RepoRoot != rig.roots["a"] || got.DocsFolder != "docs" || got.CacheDir == "" {
			t.Errorf("supervisor options = %+v", got)
		}
	})
}

// TestManagedRegistersTheCodeProject covers the registration under
// SanitizeProjectID(root) once the instance is ready, and only then.
func TestManagedRegistersTheCodeProject(t *testing.T) {
	t.Parallel()

	rig := newManagedRig(t, managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando"})
	rig.inst["a"].set(supervisor.StateStarting, 0, "")
	rig.begin(t)

	time.Sleep(30 * time.Millisecond)
	if got := rig.pando["a"].indexedProjects(); len(got) != 0 {
		t.Fatalf("a project was registered before the instance was ready: %v", got)
	}

	rig.inst["a"].set(supervisor.StateReady, 101, "")
	eventually(t, "the code project to be registered", func() bool {
		return len(rig.pando["a"].indexedProjects()) == 1
	})
	if got := rig.pando["a"].indexedProjects()[0]; got != rig.roots["a"] {
		t.Errorf("indexed %q, want the repository root %q", got, rig.roots["a"])
	}
	view := rig.settings(t)
	row := view.row(t, "a")
	if row.State != "ready" || row.PID != 101 || row.Port != 40101 || row.Version == "" {
		t.Errorf("managed row = %+v", row)
	}
	if row.CodeStatus != codeIndexStatusIndexing {
		t.Errorf("code status = %q, want %q", row.CodeStatus, codeIndexStatusIndexing)
	}
	want := pando.SanitizeProjectID(rig.roots["a"])
	for _, r := range view.Indexed {
		if r.Code == nil || r.Code.Project != want {
			t.Errorf("code project = %+v, want %q", r.Code, want)
		}
	}
}

// TestManagedSettingsReport covers the mode, the rule and the per-repository
// state, including a failed instance and its error.
func TestManagedSettingsReport(t *testing.T) {
	t.Parallel()

	rig := newManagedRig(t, managedRigOptions{repos: []string{"a", "b"}, optIn: []string{"a", "b"}, max: 3, binary: "/bin/pando"})
	rig.inst["a"].set(supervisor.StateReady, 7, "")
	rig.inst["b"].set(supervisor.StateFailed, 0, "the pando binary is older than 1.1.0")
	rig.begin(t)
	eventually(t, "both instances to be built", func() bool {
		s1, _, _ := rig.inst["a"].counts()
		s2, _, _ := rig.inst["b"].counts()
		return s1 == 1 && s2 == 1
	})

	view := rig.settings(t)
	if view.Mode != "managed" || view.ModeRule != 5 || view.ModeReason == "" || view.Binary != "/bin/pando" || view.MaxInstances != 3 {
		t.Errorf("mode fields = %+v", view)
	}
	if view.Reachable == nil || !*view.Reachable {
		t.Errorf("reachable = %v, want true while one instance is ready", view.Reachable)
	}
	if got := view.row(t, "b"); got.State != "failed" || !strings.Contains(got.Error, "older than") {
		t.Errorf("failed row = %+v", got)
	}
	if strings.Contains(send(t, rig.s, request{method: http.MethodGet, target: "/api/v1/search/settings"}).Body.String(), "instance-token") {
		t.Error("the settings response carries an instance token")
	}
}

// TestManagedSearchFansOut covers the parallel search over every ready
// instance, the merge, and the honest failure when none can answer.
func TestManagedSearchFansOut(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, states map[string]supervisor.State) *managedRig {
		t.Helper()
		rig := newManagedRig(t, managedRigOptions{
			repos: []string{"a", "b", "c"}, optIn: []string{"a", "b", "c"}, max: 3, binary: "/bin/pando",
		})
		rig.pando["a"].hits = []pando.KBHit{{
			FilePath: ".pmngr/stories/DEMO-US-0002-save-payment-methods.md", Chunk: "saved cards", Score: 0.03,
		}}
		rig.pando["b"].codeHits = map[string][]pando.CodeHit{
			pando.SanitizeProjectID(rig.roots["b"]): {{FilePath: "docs/index.md", Name: "Index", Score: 0.9}},
		}
		pid := 10
		for id, st := range states {
			pid++
			rig.inst[id].set(st, pid, "not answering")
		}
		rig.begin(t)
		eventually(t, "the searchers to see the states", func() bool { return rig.s.search.managed != nil })
		return rig
	}

	t.Run("every ready instance is asked and the answers are merged", func(t *testing.T) {
		t.Parallel()

		rig := setup(t, map[string]supervisor.State{"a": supervisor.StateReady, "b": supervisor.StateReady, "c": supervisor.StateReady})
		got := searchFor(t, rig.s, "/api/v1/search?q=cards&limit=20")
		indexes := map[string]bool{}
		for _, h := range got.Hits {
			indexes[h.Index] = true
		}
		if got.Degraded {
			t.Error("the answer is degraded although every instance answered")
		}
		for id, p := range rig.pando {
			if p.searches != 1 {
				t.Errorf("instance %s was asked %d times, want 1", id, p.searches)
			}
		}
		// a answers from its knowledge base and b from its own code project.
		if !indexes["kb"] || !indexes["code"] {
			t.Errorf("the merged hits come from %v, want a kb hit and a code hit: %+v", indexes, got.Hits)
		}
		if searched := rig.pando["b"].codeSearched; len(searched) != 1 ||
			searched[0] != pando.SanitizeProjectID(rig.roots["b"]) {
			t.Errorf("instance b searched code projects %v, want only its own", searched)
		}
	})

	t.Run("an instance that is not ready costs only its own hits", func(t *testing.T) {
		t.Parallel()

		rig := setup(t, map[string]supervisor.State{"a": supervisor.StateReady, "b": supervisor.StateFailed, "c": supervisor.StateStarting})
		got := searchFor(t, rig.s, "/api/v1/search?q=cards&limit=20")
		if got.Degraded || len(got.Hits) == 0 {
			t.Errorf("the ready instance's hits were lost: degraded=%v hits=%d", got.Degraded, len(got.Hits))
		}
		if rig.pando["b"].searches != 0 || rig.pando["c"].searches != 0 {
			t.Error("an instance that is not ready was searched")
		}
	})

	t.Run("no ready instance is unavailable, never an empty success", func(t *testing.T) {
		t.Parallel()

		rig := setup(t, map[string]supervisor.State{"a": supervisor.StateFailed, "b": supervisor.StateStarting, "c": supervisor.StateRestarting})
		got := searchFor(t, rig.s, "/api/v1/search?q=cards&limit=20")
		if !got.Degraded {
			t.Error("the answer is not marked degraded although no instance could answer")
		}
		_, err := rig.s.repos.workspace().SearchSemantic(context.Background(), vaultQuery("cards"))
		if err == nil || !pando.IsUnavailable(err) {
			t.Fatalf("SearchSemantic() error = %v, want an unavailable error", err)
		}
		if !strings.Contains(err.Error(), "repository a") || !strings.Contains(err.Error(), "not answering") {
			t.Errorf("the error does not say why, per repository: %v", err)
		}
	})

	t.Run("a search within the budget when one instance hangs", func(t *testing.T) {
		t.Parallel()

		rig := setup(t, map[string]supervisor.State{"a": supervisor.StateReady, "b": supervisor.StateReady})
		rig.pando["b"].block = make(chan struct{})
		t.Cleanup(func() { close(rig.pando["b"].block) })
		start := time.Now()
		got := searchFor(t, rig.s, "/api/v1/search?q=cards&limit=20")
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("the search took %v, want it inside the budget", elapsed)
		}
		if len(got.Hits) == 0 {
			t.Error("the healthy instance's hits were lost")
		}
	})
}

// TestManagedImpactUsesTheInstanceOfTheRepository covers tiers 2 and 3.
func TestManagedImpactUsesTheInstanceOfTheRepository(t *testing.T) {
	t.Parallel()

	rig := newManagedRig(t, managedRigOptions{repos: []string{"a", "b", "c"}, optIn: []string{"a", "b"}, binary: "/bin/pando"})
	rig.inst["a"].set(supervisor.StateReady, 21, "")
	rig.inst["b"].set(supervisor.StateFailed, 0, "crashed 5 times")
	rig.begin(t)
	eventually(t, "the instances to be built", func() bool {
		s1, _, _ := rig.inst["a"].counts()
		return s1 == 1
	})

	graph := rig.s.impactCallGraph("a")
	if graph == nil {
		t.Fatal("the ready repository has no code graph")
	}
	if _, err := graph.ImpactAnalysis(context.Background(), "p", []string{"X"}, pando.ImpactOptions{}); err != nil {
		t.Fatalf("ImpactAnalysis() error = %v", err)
	}
	if got := rig.pando["a"].analyzed; len(got) != 1 {
		t.Errorf("instance a analyzed %v, want one call", got)
	}
	if got := rig.pando["b"].analyzed; len(got) != 0 {
		t.Errorf("instance b was asked to analyze repository a: %v", got)
	}

	failed := rig.s.impactCallGraph("b")
	if failed == nil {
		t.Fatal("a failed instance gave no graph, so the tier could not say why")
	}
	if _, err := failed.ImpactAnalysis(context.Background(), "p", nil, pando.ImpactOptions{}); !pando.IsUnavailable(err) {
		t.Errorf("the failed instance's graph error = %v, want unavailable", err)
	}
	if _, err := rig.s.impactSemantic("b").SearchSemantic(context.Background(), vaultQuery("x")); !pando.IsUnavailable(err) {
		t.Errorf("the failed instance's searcher error = %v, want unavailable", err)
	}
	if rig.s.impactCallGraph("c") != nil || rig.s.impactSemantic("c") != nil {
		t.Error("a repository that did not opt in got an instance")
	}
	if rig.s.impactSemantic("a") == nil {
		t.Error("the ready repository has no semantic searcher")
	}
}

// TestManagedReindexRestartsTheInstance covers the KB half of a reindex.
func TestManagedReindexRestartsTheInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		body         any
		wantRestarts map[string]int
		wantNote     string
		wantStatus   int
	}{
		{"the whole workspace", nil, map[string]int{"a": 1, "b": 1}, "Restarted 2 managed Pando instance(s)", http.StatusAccepted},
		{"one repository", map[string]string{"repo": "a"}, map[string]int{"a": 1, "b": 0}, "Restarted 1 managed Pando instance(s)", http.StatusAccepted},
		{"a repository that did not opt in", map[string]string{"repo": "c"}, map[string]int{"a": 0, "b": 0}, "", http.StatusConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rig := newManagedRig(t, managedRigOptions{repos: []string{"a", "b", "c"}, optIn: []string{"a", "b"}, binary: "/bin/pando"})
			rig.inst["a"].set(supervisor.StateReady, 31, "")
			rig.inst["b"].set(supervisor.StateReady, 32, "")
			rig.begin(t)
			eventually(t, "the code projects to be registered", func() bool {
				return len(rig.pando["a"].indexedProjects()) == 1 && len(rig.pando["b"].indexedProjects()) == 1
			})

			rec := send(t, rig.s, request{method: http.MethodPost, target: "/api/v1/search/reindex", body: tc.body})
			if tc.wantStatus != http.StatusAccepted {
				if rec.Code == http.StatusAccepted {
					t.Fatalf("status = %d, want a refusal", rec.Code)
				}
				return
			}
			var job reindexWire
			decode(t, rec, http.StatusAccepted, &job)
			done := waitForReindex(t, rig.s, job.JobID)
			if !strings.Contains(done.KBNote, tc.wantNote) {
				t.Errorf("kbNote = %q, want it to say %q", done.KBNote, tc.wantNote)
			}
			for id, want := range tc.wantRestarts {
				if _, _, got := rig.inst[id].counts(); got != want {
					t.Errorf("restarts of %s = %d, want %d", id, got, want)
				}
			}
			// The restart interrupts the running index job, so the fresh child
			// is handed the source tree again.
			if tc.wantRestarts["a"] == 1 {
				rig.inst["a"].set(supervisor.StateReady, 33, "")
				eventually(t, "the source tree to be handed over again", func() bool {
					return len(rig.pando["a"].indexedProjects()) >= 3
				})
			}
		})
	}
}

// TestManagedStopsWithTheServer covers the graceful shutdown path.
func TestManagedStopsWithTheServer(t *testing.T) {
	t.Parallel()

	rig := newManagedRig(t, managedRigOptions{repos: []string{"a", "b"}, optIn: []string{"a", "b"}, binary: "/bin/pando"})
	rig.inst["a"].set(supervisor.StateReady, 41, "")
	rig.inst["b"].set(supervisor.StateReady, 42, "")
	// Port 0: the operating system picks one, so a parallel test cannot hold it.
	rig.s.addr = "127.0.0.1:0"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rig.s.Start(ctx) }()
	eventually(t, "both instances to be started", func() bool {
		a, _, _ := rig.inst["a"].counts()
		b, _, _ := rig.inst["b"].counts()
		return a == 1 && b == 1
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Start() did not return")
	}
	for id, inst := range rig.inst {
		if _, stops, _ := inst.counts(); stops != 1 {
			t.Errorf("instance %s stopped %d times, want 1", id, stops)
		}
	}
}

// TestManagedInstanceTakenByAnotherServe covers ErrLocked: the instance is read
// from its state file instead of being started twice.
func TestManagedInstanceTakenByAnotherServe(t *testing.T) {
	t.Parallel()

	rig := newManagedRig(t, managedRigOptions{repos: []string{"a"}, optIn: []string{"a"}, binary: "/bin/pando"})
	rig.inst["a"].startErr = supervisor.ErrLocked
	rig.begin(t)
	eventually(t, "the slot to switch to the foreign instance", func() bool {
		slot := rig.s.search.managed.slot("a")
		slot.mu.Lock()
		defer slot.mu.Unlock()
		_, foreign := slot.sup.(foreignInstance)
		return foreign
	})
	// Nothing was written for it, so the foreign instance is stopped, with a
	// reason, and a search says so.
	if got := rig.settings(t).row(t, "a"); got.State != "stopped" || got.Error == "" {
		t.Errorf("row = %+v, want stopped with a reason", got)
	}
	if _, err := rig.s.repos.workspace().SearchSemantic(context.Background(), vaultQuery("x")); !pando.IsUnavailable(err) {
		t.Errorf("SearchSemantic() error = %v, want unavailable", err)
	}
}

// TestPandoModesOtherThanManagedAreUnchanged covers external and off: no
// instance is ever built, the settings say which mode is in force, and the
// external client is the one it always was.
func TestPandoModesOtherThanManagedAreUnchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     managedRigOptions
		wantMode string
		wantRule int
		wantCfg  bool
	}{
		{"an explicit endpoint beats the binary", managedRigOptions{mcpURL: "http://127.0.0.1:9777/mcp", binary: "/bin/pando"}, "external", 4, true},
		{"mode external", managedRigOptions{mode: config.PandoModeExternal, binary: "/bin/pando"}, "external", 2, false},
		{"mode off", managedRigOptions{mode: config.PandoModeOff, binary: "/bin/pando"}, "off", 1, false},
		{"mode off beats an mcpUrl", managedRigOptions{mode: config.PandoModeOff, mcpURL: "http://127.0.0.1:9777/mcp", binary: "/bin/pando"}, "off", 1, false},
		{"no binary in auto", managedRigOptions{}, "off", 6, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.opts.repos, tc.opts.optIn = []string{"a"}, []string{"a"}
			rig := newManagedRig(t, tc.opts)
			if rig.s.search.managed != nil || rig.s.search.managedSearch != nil {
				t.Fatal("a managed state exists outside managed mode")
			}
			rig.begin(t)
			time.Sleep(20 * time.Millisecond)
			if starts, _, _ := rig.inst["a"].counts(); starts != 0 {
				t.Error("an instance was started outside managed mode")
			}
			view := rig.settings(t)
			if view.Mode != tc.wantMode || view.ModeRule != tc.wantRule || view.Configured != tc.wantCfg {
				t.Errorf("settings = mode %q rule %d configured %v", view.Mode, view.ModeRule, view.Configured)
			}
			for _, row := range view.Indexed {
				if row.Managed != nil {
					t.Errorf("row %s carries managed state outside managed mode", row.Repo)
				}
			}
			if tc.wantRule == 1 {
				if rig.s.search.pando() != nil || view.Backend != "core" {
					t.Errorf("mode off built a client or reports backend %q", view.Backend)
				}
				_, err := rig.s.repos.workspace().SearchSemantic(context.Background(), vaultQuery("x"))
				if err == nil || !pando.IsUnavailable(err) || !strings.Contains(err.Error(), "search.pando.mode: off") {
					t.Errorf("SearchSemantic() error = %v, want unavailable naming search.pando.mode: off", err)
				}
				if rig.s.impactCallGraph("a") != nil {
					t.Error("mode off gave the impact tier a code graph")
				}
				if _, err := rig.s.impactSemantic("a").SearchSemantic(context.Background(), vaultQuery("x")); !pando.IsUnavailable(err) {
					t.Errorf("tier 3 searcher error = %v, want unavailable", err)
				}
			}
			if tc.wantCfg && rig.s.search.pando() == nil {
				t.Error("the configured external endpoint built no client")
			}
		})
	}

	t.Run("the endpoint cannot be patched in while managed", func(t *testing.T) {
		t.Parallel()

		rig := newManagedRig(t, managedRigOptions{repos: []string{"a"}, binary: "/bin/pando"})
		rec := send(t, rig.s, request{
			method: http.MethodPatch, target: "/api/v1/search/settings",
			body: map[string]string{"mcpUrl": "http://127.0.0.1:9/mcp"},
		})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "mcpUrl") {
			t.Errorf("status = %d body = %s, want a refusal naming mcpUrl", rec.Code, rec.Body.String())
		}
	})
}

// TestManagedEnableAndDisable covers starting and stopping one instance from a
// request, which the web story builds on.
func TestManagedEnableAndDisable(t *testing.T) {
	t.Parallel()

	rig := newManagedRig(t, managedRigOptions{repos: []string{"a", "b"}, binary: "/bin/pando"})
	rig.begin(t)
	if got := rig.settings(t).row(t, "a").State; got != "disabled" {
		t.Fatalf("state before Enable = %q, want disabled", got)
	}
	view := rig.s.search.managed.Enable("a")
	if !view.OptedIn || view.State == "skipped" {
		t.Fatalf("Enable() = %+v", view)
	}
	eventually(t, "the instance to start", func() bool {
		starts, _, _ := rig.inst["a"].counts()
		return starts == 1
	})
	if err := rig.s.search.managed.Disable(context.Background(), "a"); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if _, stops, _ := rig.inst["a"].counts(); stops != 1 {
		t.Errorf("stops = %d, want 1", stops)
	}
	if got := rig.settings(t).row(t, "a").State; got != "disabled" {
		t.Errorf("state after Disable = %q, want disabled", got)
	}
}

func vaultQuery(q string) vault.SemanticQuery { return vault.SemanticQuery{Q: q, Limit: 10} }
