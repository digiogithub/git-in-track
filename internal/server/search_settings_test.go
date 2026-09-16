package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// searchServerOptions are the knobs the search tests vary on top of the fixture
// server newAPIServer builds.
type searchServerOptions struct {
	// configPath, when set, is the configuration file a settings PATCH is
	// persisted to. Empty is a companion started with `serve --repo`.
	configPath string
	// port makes the server startable. Zero leaves it unstarted.
	port int
}

// newPandoSearchServer mounts the fixture with a search configuration.
func newPandoSearchServer(t *testing.T, opts searchServerOptions) (*Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Bind:       "127.0.0.1",
		Port:       opts.port,
		Token:      "test-token",
		Version:    "0.0.1-test",
		Workspace:  "test",
		Repos:      []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		ConfigPath: opts.configPath,
		Now:        func() time.Time { return time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s, root
}

// settingsView is GET|PATCH /api/v1/search/settings on the wire.
type settingsView struct {
	Backend     string `json:"backend"`
	Configured  bool   `json:"configured"`
	MCPURL      string `json:"mcpUrl"`
	RESTURL     string `json:"restUrl"`
	ProjectID   string `json:"projectId"`
	AllowRemote bool   `json:"allowRemote"`
	Reachable   *bool  `json:"reachable"`
	Indexed     []struct {
		Repo     string   `json:"repo"`
		Root     string   `json:"root"`
		Docs     []string `json:"docs"`
		Items    int      `json:"items"`
		Pages    int      `json:"pages"`
		Comments int      `json:"comments"`
		Code     *struct {
			Project string `json:"project"`
			Status  string `json:"status"`
			Job     string `json:"job"`
			Note    string `json:"note"`
		} `json:"code"`
	} `json:"indexed"`
	Reindex   *reindexWire `json:"reindex"`
	Persisted bool         `json:"persisted"`
}

type reindexWire struct {
	JobID  string `json:"jobId"`
	Phase  string `json:"phase"`
	KBNote string `json:"kbNote"`
	Repos  []struct {
		Repo      string `json:"repo"`
		CodeJob   string `json:"codeJob"`
		CodeError string `json:"codeError"`
	} `json:"repos"`
}

// TestSearchSettingsReportsWhatPandoIndexes replaces the corpus report the
// card used to read: with Pando indexing the repository itself there is no
// second tree to date, so the honest answer is where Pando was pointed and
// what this companion's own index holds there (GIT-US-0095).
func TestSearchSettingsReportsWhatPandoIndexes(t *testing.T) {
	t.Parallel()

	s, root := newPandoSearchServer(t, searchServerOptions{})

	var view settingsView
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/search/settings"}),
		http.StatusOK, &view)

	if view.Backend != "core" || view.Configured {
		t.Errorf("an unconfigured companion reports backend %q configured %v", view.Backend, view.Configured)
	}
	if view.Reachable != nil {
		t.Error("there is nothing to probe, so reachability must be null")
	}
	if len(view.Indexed) != 1 || view.Indexed[0].Repo != testRepoID {
		t.Fatalf("indexed = %+v, want one entry per mounted repository", view.Indexed)
	}
	row := view.Indexed[0]
	if row.Root != root {
		t.Errorf("root = %q, want the working tree %q", row.Root, root)
	}
	if len(row.Docs) != 1 || row.Docs[0] != "docs" {
		t.Errorf("docs = %v, want the documentation directory Pando's KBPath points at", row.Docs)
	}
	if row.Items == 0 || row.Pages == 0 {
		t.Errorf("the row reports %d items and %d pages; an empty row cannot diagnose a bad KBPath", row.Items, row.Pages)
	}

	// With a Pando installed the probe runs and the backend flips.
	installPando(t, s, &fakePando{})
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/search/settings"}),
		http.StatusOK, &view)
	if view.Backend != "pando" || !view.Configured {
		t.Errorf("backend = %q configured = %v, want pando", view.Backend, view.Configured)
	}
	if view.Reachable == nil || !*view.Reachable {
		t.Errorf("reachable = %v, want a successful probe", view.Reachable)
	}
}

// TestSearchSettingsPatchIgnoresTheRetiredCorpusDir pins that the field is
// gone from the surface rather than merely unused: an old client still sending
// it changes nothing, and nothing about a corpus comes back.
func TestSearchSettingsPatchIgnoresTheRetiredCorpusDir(t *testing.T) {
	t.Parallel()

	s, _ := newPandoSearchServer(t, searchServerOptions{})
	rec := send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/search/settings",
		body:   map[string]any{"corpusDir": "/tmp/pando-kb"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "corpusDir") || strings.Contains(body, "corpora") {
		t.Errorf("the settings payload still mentions the corpus: %s", body)
	}
}

func TestSearchSettingsPatchPersists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	s, _ := newPandoSearchServer(t, searchServerOptions{configPath: configPath})

	var view settingsView
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/search/settings",
		body: map[string]any{
			"mcpUrl":    "http://127.0.0.1:9777/mcp",
			"projectId": "git-in-track",
		},
	}), http.StatusOK, &view)

	if !view.Persisted {
		t.Error("a companion with a configuration file must persist the change")
	}
	if view.MCPURL != "http://127.0.0.1:9777/mcp" || view.ProjectID != "git-in-track" {
		t.Errorf("the response does not carry the new settings: %+v", view)
	}
	if view.Backend != "pando" {
		t.Errorf("backend = %q, want pando once an endpoint is configured", view.Backend)
	}

	saved, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("reload the configuration: %v", err)
	}
	if saved.Search.Pando.MCPURL != "http://127.0.0.1:9777/mcp" {
		t.Errorf("the file holds mcpUrl %q", saved.Search.Pando.MCPURL)
	}
	if saved.Search.Pando.ProjectID != "git-in-track" {
		t.Errorf("the file holds projectId %q", saved.Search.Pando.ProjectID)
	}

	// The running process took it too: the semantic backend is installed.
	if s.search.semantic() == nil {
		t.Error("the running process did not adopt the new endpoint")
	}
}

func TestSearchSettingsPatchWithoutAConfigFile(t *testing.T) {
	t.Parallel()

	s, _ := newPandoSearchServer(t, searchServerOptions{})

	var view settingsView
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/search/settings",
		body:   map[string]any{"mcpUrl": "http://127.0.0.1:9777/mcp"},
	}), http.StatusOK, &view)

	if view.Persisted {
		t.Error("with no configuration path the change must not claim to be persisted")
	}
	if view.MCPURL != "http://127.0.0.1:9777/mcp" {
		t.Errorf("the process did not take the change: %+v", view)
	}
}

func TestSearchSettingsPatchRejectsBadValues(t *testing.T) {
	t.Parallel()

	s, _ := newPandoSearchServer(t, searchServerOptions{})

	cases := map[string]map[string]any{
		"a non-loopback endpoint without allowRemote": {"mcpUrl": "http://pando.example.com/mcp"},
		"a URL that is not one":                       {"mcpUrl": "://nonsense"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := send(t, s, request{
				method: http.MethodPatch, target: "/api/v1/search/settings", body: body,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if code := recorderProblemCode(t, rec); code != codeInvalidRequest {
				t.Errorf("problem code = %q, want %q", code, codeInvalidRequest)
			}
		})
	}
	// Nothing was adopted.
	if s.search.semantic() != nil {
		t.Error("a refused patch changed the running configuration")
	}
}

// TestSearchReindexIndexesTheCodeAndTheKnowledgeBase pins what is left of the
// job now that there is no export half: the source tree goes to Pando's code
// index, and the knowledge base to its REST reindex (GIT-US-0095).
func TestSearchReindexIndexesTheCodeAndTheKnowledgeBase(t *testing.T) {
	t.Parallel()

	s, root := newPandoSearchServer(t, searchServerOptions{})
	fake := &fakePando{reindex: pando.ReindexStats{Scanned: 7, Added: 7}}
	installPando(t, s, fake)

	events := subscribeHub(t, s, eventSearchProgress)

	var job reindexWire
	decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/search/reindex"}),
		http.StatusAccepted, &job)
	if job.JobID == "" {
		t.Fatal("the reindex answered no job id")
	}

	view := waitForReindex(t, s, job.JobID)
	if view.Phase != searchPhaseDone {
		t.Fatalf("phase = %q (%+v)", view.Phase, view)
	}
	if len(view.Repos) != 1 || view.Repos[0].CodeJob != "job-1" {
		t.Fatalf("the source tree was not indexed: %+v", view.Repos)
	}
	fake.mu.Lock()
	indexed := append([]string(nil), fake.indexed...)
	fake.mu.Unlock()
	if len(indexed) != 1 || indexed[0] != root {
		t.Errorf("code index ran over %v, want the repository working tree %s", indexed, root)
	}
	if view.KBNote != "Reindexed." {
		t.Errorf("kbNote = %q, want the REST reindex outcome", view.KBNote)
	}

	if !waitForEvent(t, events, func(ev Event) bool {
		data, ok := ev.Data.(map[string]any)
		return ok && data["operationId"] == job.JobID
	}) {
		t.Error("the reindex published no progress event")
	}
}

func TestSearchReindexRefusesASecondRun(t *testing.T) {
	t.Parallel()

	s, _ := newPandoSearchServer(t, searchServerOptions{})
	gate := make(chan struct{})
	installPando(t, s, &fakePando{indexGate: gate})

	rec := send(t, s, request{method: http.MethodPost, target: "/api/v1/search/reindex"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}

	// The first job is parked inside IndexProject, so the slot is taken.
	second := send(t, s, request{method: http.MethodPost, target: "/api/v1/search/reindex"})
	if second.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", second.Code)
	}
	if code := recorderProblemCode(t, second); code != codeSearchReindexRunning {
		t.Errorf("problem code = %q, want %q", code, codeSearchReindexRunning)
	}

	close(gate)
	// The running job is unaffected by the refusal: it finishes normally.
	var job reindexWire
	decode(t, rec, http.StatusAccepted, &job)
	if view := waitForReindex(t, s, job.JobID); view.Phase != searchPhaseDone {
		t.Errorf("the running job ended in %q", view.Phase)
	}
}

func TestSearchReindexReportsAPartialFailure(t *testing.T) {
	t.Parallel()

	s, _ := newPandoSearchServer(t, searchServerOptions{})
	installPando(t, s, &fakePando{
		indexErr:   pando.ErrUnreachable,
		reindexErr: pando.ErrNotConfigured,
	})

	var job reindexWire
	decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/search/reindex"}),
		http.StatusAccepted, &job)

	view := waitForReindex(t, s, job.JobID)
	if view.Phase != searchPhaseFailed {
		t.Errorf("phase = %q, want the job to report the failure", view.Phase)
	}
	if len(view.Repos) != 1 {
		t.Fatalf("repos = %+v", view.Repos)
	}
	if view.Repos[0].CodeError == "" {
		t.Error("the code-index failure was not reported")
	}
	// Without Pando's REST reindex route the KB half says so rather than
	// claiming an index operation that never ran.
	if view.KBNote == "" || view.KBNote == "Reindexed." {
		t.Errorf("kbNote = %q, want the not-reindexed wording", view.KBNote)
	}
}

func TestSearchReindexWithoutAnythingConfigured(t *testing.T) {
	t.Parallel()

	s, _ := newPandoSearchServer(t, searchServerOptions{})
	rec := send(t, s, request{method: http.MethodPost, target: "/api/v1/search/reindex"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if code := recorderProblemCode(t, rec); code != codeSearchNotConfigured {
		t.Errorf("problem code = %q, want %q", code, codeSearchNotConfigured)
	}
}

// waitForReindex polls the settings until the job has finished.
func waitForReindex(t *testing.T, s *Server, jobID string) reindexWire {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var view settingsView
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/search/settings"}),
			http.StatusOK, &view)
		if view.Reindex != nil && view.Reindex.JobID == jobID &&
			(view.Reindex.Phase == searchPhaseDone || view.Reindex.Phase == searchPhaseFailed) {
			return *view.Reindex
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the reindex %s never finished", jobID)
	return reindexWire{}
}

// subscribeHub attaches an in-process subscriber to the hub, the way the event
// stream does, and returns its queue.
func subscribeHub(t *testing.T, s *Server, topics ...string) chan Event {
	t.Helper()

	client := newHubClient()
	client.subscribe(topics)
	s.hub.register(client)
	t.Cleanup(func() { s.hub.unregister(client) })
	return client.events
}

// waitForEvent drains the queue until match says yes or the deadline passes.
func waitForEvent(t *testing.T, events chan Event, match func(Event) bool) bool {
	t.Helper()

	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if match(ev) {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

// recorderProblemCode reads the `code` of a problem+json recorder.
func recorderProblemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode the problem document %s: %v", rec.Body.Bytes(), err)
	}
	return problem.Code
}

// TestSearchProjectIDDefaultsToTheSanitizedMountPath covers the promise docs/07
// section 4 and config.SearchPando make: an empty `search.pando.projectId` is
// the id Pando itself derives from the repository path, not "no project"
// (GIT-T-0142).
func TestSearchProjectIDDefaultsToTheSanitizedMountPath(t *testing.T) {
	t.Parallel()

	s, root := newPandoSearchServer(t, searchServerOptions{})
	want := pando.SanitizeProjectID(root)
	if want == "" {
		t.Fatalf("the fixture path %q sanitizes to nothing", root)
	}

	var view settingsView
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/search/settings"}),
		http.StatusOK, &view)
	if view.ProjectID != want {
		t.Errorf("projectId = %q, want the derived %q", view.ProjectID, want)
	}

	// Configuring an endpoint builds the client SearchCode runs against, and
	// that client carries the derived id as its default project: SearchCode
	// with an empty project id falls back to exactly this value.
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/search/settings",
		body:   map[string]any{"mcpUrl": "http://127.0.0.1:9777/mcp"},
	}), http.StatusOK, &view)
	if view.ProjectID != want {
		t.Errorf("projectId after the patch = %q, want the derived %q", view.ProjectID, want)
	}
	client, ok := s.search.pando().(*pando.Client)
	if !ok {
		t.Fatalf("the configured endpoint built no Pando client: %T", s.search.pando())
	}
	if client.ProjectID() != want {
		t.Errorf("the client searches code in project %q, want %q", client.ProjectID(), want)
	}

	// An explicit id wins, and it is the one persisted — the derived default
	// must never be frozen into the file behind the operator's back.
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/search/settings",
		body:   map[string]any{"projectId": "chosen_by_hand"},
	}), http.StatusOK, &view)
	if view.ProjectID != "chosen_by_hand" {
		t.Errorf("projectId = %q, want the configured value to win", view.ProjectID)
	}
	client, ok = s.search.pando().(*pando.Client)
	if !ok {
		t.Fatalf("the configured endpoint built no Pando client: %T", s.search.pando())
	}
	if client.ProjectID() != "chosen_by_hand" {
		t.Errorf("the client searches code in project %q, want the configured one", client.ProjectID())
	}
}

// TestDerivedProjectIDMatchesPandoSanitisation pins the derivation itself over
// the sample paths the task names.
func TestDerivedProjectIDMatchesPandoSanitisation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want string
	}{
		{"/www/git-in-track", "www_git-in-track"},
		{"/home/jose/src/acme.api", "home_jose_src_acme_api"},
		{"/srv/repos/My Project", "srv_repos_My_Project"},
		{"relative/path", "relative_path"},
	}
	for _, tc := range cases {
		repos := &registry{mounts: []*mount{{id: "r", path: tc.path, vlt: &vault.Vault{}}}}
		if got := derivedProjectID(repos); got != tc.want {
			t.Errorf("derivedProjectID(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
	if got := derivedProjectID(&registry{}); got != "" {
		t.Errorf("a workspace with no ready mount derived %q, want the empty string", got)
	}
}
