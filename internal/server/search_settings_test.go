package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando"
)

// searchServerOptions are the knobs the search tests vary on top of the fixture
// server newAPIServer builds.
type searchServerOptions struct {
	// configPath, when set, is the configuration file a settings PATCH is
	// persisted to. Empty is a companion started with `serve --repo`.
	configPath string
	// corpusDir is the corpus root; empty means no corpus is exported.
	corpusDir string
	// port makes the server startable. Zero leaves it unstarted.
	port int
}

// newPandoSearchServer mounts the fixture with a search configuration.
func newPandoSearchServer(t *testing.T, opts searchServerOptions) (*Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Bind:            "127.0.0.1",
		Port:            opts.port,
		Token:           "test-token",
		Version:         "0.0.1-test",
		Workspace:       "test",
		Repos:           []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		ConfigPath:      opts.configPath,
		SearchCorpusDir: opts.corpusDir,
		Now:             func() time.Time { return time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC) },
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
	CorpusDir   string `json:"corpusDir"`
	AllowRemote bool   `json:"allowRemote"`
	Reachable   *bool  `json:"reachable"`
	Corpora     []struct {
		Repo string `json:"repo"`
		Dir  string `json:"dir"`
		Last struct {
			Items int    `json:"items"`
			Pages int    `json:"pages"`
			At    string `json:"at"`
		} `json:"last"`
	} `json:"corpora"`
	Documents  int          `json:"documents"`
	LastExport *string      `json:"lastExport"`
	Reindex    *reindexWire `json:"reindex"`
	Persisted  bool         `json:"persisted"`
}

type reindexWire struct {
	JobID  string `json:"jobId"`
	Phase  string `json:"phase"`
	KBNote string `json:"kbNote"`
	Repos  []struct {
		Repo        string `json:"repo"`
		CodeJob     string `json:"codeJob"`
		CodeError   string `json:"codeError"`
		ExportError string `json:"exportError"`
		Export      struct {
			Items   int `json:"items"`
			Pages   int `json:"pages"`
			Written int `json:"written"`
		} `json:"export"`
	} `json:"repos"`
}

func TestSearchSettingsReportsTheCorpusAndTheBackend(t *testing.T) {
	t.Parallel()

	corpus := t.TempDir()
	s, _ := newPandoSearchServer(t, searchServerOptions{corpusDir: corpus})

	var view settingsView
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/search/settings"}),
		http.StatusOK, &view)

	if view.Backend != "core" || view.Configured {
		t.Errorf("an unconfigured companion reports backend %q configured %v", view.Backend, view.Configured)
	}
	if view.Reachable != nil {
		t.Error("there is nothing to probe, so reachability must be null")
	}
	if view.CorpusDir != corpus {
		t.Errorf("corpusDir = %q, want %q", view.CorpusDir, corpus)
	}
	if len(view.Corpora) != 1 || view.Corpora[0].Repo != testRepoID {
		t.Fatalf("corpora = %+v, want one entry per mounted repository", view.Corpora)
	}
	if want := filepath.Join(corpus, testRepoID); view.Corpora[0].Dir != want {
		t.Errorf("corpus dir = %q, want %q — it must match the KBPath `gintrack agent init` writes",
			view.Corpora[0].Dir, want)
	}
	if view.LastExport != nil {
		t.Error("nothing has been exported yet, so lastExport must be null")
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

func TestSearchSettingsPatchPersists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	corpus := filepath.Join(dir, "corpus")
	s, _ := newPandoSearchServer(t, searchServerOptions{configPath: configPath, corpusDir: corpus})

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

	s, _ := newPandoSearchServer(t, searchServerOptions{corpusDir: t.TempDir()})

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

	s, _ := newPandoSearchServer(t, searchServerOptions{corpusDir: t.TempDir()})

	cases := map[string]map[string]any{
		"a non-loopback endpoint without allowRemote": {"mcpUrl": "http://pando.example.com/mcp"},
		"a relative corpus directory":                 {"corpusDir": "relative/corpus"},
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

func TestSearchReindexExportsAndIndexes(t *testing.T) {
	t.Parallel()

	corpus := t.TempDir()
	s, root := newPandoSearchServer(t, searchServerOptions{corpusDir: corpus})
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
	if len(view.Repos) != 1 || view.Repos[0].Export.Items == 0 {
		t.Fatalf("the corpus was not exported: %+v", view.Repos)
	}
	if view.Repos[0].CodeJob != "job-1" {
		t.Errorf("the source tree was not indexed: %+v", view.Repos[0])
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

	// The corpus is on disk under <corpusDir>/<repo id>/<PROJECT>/…
	item := filepath.Join(corpus, testRepoID, "DEMO", "items", "DEMO-US-0001.md")
	if _, err := os.Stat(item); err != nil {
		t.Errorf("the exported item is missing: %v", err)
	}
	page := filepath.Join(corpus, testRepoID, "DEMO", "kb", "architecture", "overview.md")
	if _, err := os.Stat(page); err != nil {
		t.Errorf("the exported page is missing: %v", err)
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

	s, _ := newPandoSearchServer(t, searchServerOptions{corpusDir: t.TempDir()})
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

	s, _ := newPandoSearchServer(t, searchServerOptions{corpusDir: t.TempDir()})
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
	// The export is intact and only the code half failed, reported separately.
	if view.Repos[0].ExportError != "" || view.Repos[0].Export.Items == 0 {
		t.Errorf("a Pando failure invalidated the corpus export: %+v", view.Repos[0])
	}
	if view.Repos[0].CodeError == "" {
		t.Error("the code-index failure was not reported")
	}
	// Without Pando's REST reindex route the KB half is honest about what it
	// did: it re-exported and is waiting for the next import pass.
	if view.KBNote == "" || view.KBNote == "Reindexed." {
		t.Errorf("kbNote = %q, want the awaiting-import wording", view.KBNote)
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
