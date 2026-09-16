package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pando"
)

// The problem codes of the search surface.
const (
	// codeSearchReindexRunning means a reindex is already running. It is a
	// 409: the caller waits for the running job rather than starting a second
	// one that would fight it for the same index.
	codeSearchReindexRunning = "search_reindex_running"
	// codeSearchNotConfigured means no Pando endpoint is configured, so there
	// is nothing to reindex.
	codeSearchNotConfigured = "search_not_configured"
)

// eventSearchProgress is the hub topic a reindex reports on. Its payload is
// shaped like sync.progress (docs/07 section 5.6) so that a client already
// following that stream needs no second renderer.
const eventSearchProgress = "search.progress"

// The phases eventSearchProgress reports, in the order a reindex walks them.
const (
	searchPhaseCode   = "code"
	searchPhaseKB     = "kb"
	searchPhaseDone   = "completed"
	searchPhaseFailed = "failed"
)

// searchJobCounter numbers reindex jobs within one process, the way the sync
// pipeline numbers its operations.
var searchJobCounter atomic.Uint64

// searchState owns everything behind /api/v1/search: the Pando client, the
// running `search.pando` settings and the single reindex slot.
//
// It always exists. A companion with no Pando configured holds a state whose
// client is nil, which answers "core" for the backend and `not_configured` for
// a reindex — a different thing from a route that is not there.
type searchState struct {
	log        *slog.Logger
	repos      *registry
	hub        *Hub
	now        func() time.Time
	configPath string

	mu sync.RWMutex
	// settings is the running `search.pando` section. It carries the resolved
	// tokens and is never marshaled: the view below is what a response sees.
	settings config.SearchPando
	// client is nil when no MCP URL is configured, or when the configured one
	// was refused (a non-loopback host without allowRemote).
	client   pandoAPI
	searcher *pandoSearcher
	// codeIndex is what became of each repository's code-project registration,
	// keyed by mount id. It is what the settings card states when there is no
	// code search (GIT-US-0098).
	codeIndex map[string]codeIndexView

	// registering guards the one registration pass this process runs.
	registering atomic.Bool

	// degraded remembers whether the last search fell back, so the log line is
	// written once per state change rather than once per request.
	degraded atomic.Bool

	// jobMu guards the single reindex slot and its last outcome.
	jobMu   sync.Mutex
	running *reindexJob
	last    *reindexJob
}

// reindexJob is one run of POST /api/v1/search/reindex.
type reindexJob struct {
	ID        string              `json:"jobId"`
	StartedAt time.Time           `json:"startedAt"`
	EndedAt   time.Time           `json:"endedAt,omitempty"`
	Phase     string              `json:"phase"`
	Repos     []reindexRepo       `json:"repos"`
	KB        *pando.ReindexStats `json:"kb,omitempty"`
	// KBNote says what happened to the knowledge-base half in words, because
	// without Pando's REST reindex route the honest answer is "nothing was
	// asked to reindex", not "reindexed".
	KBNote string `json:"kbNote,omitempty"`
	Error  string `json:"error,omitempty"`
}

// reindexRepo is the outcome of one repository within a reindex.
type reindexRepo struct {
	Repo string `json:"repo"`
	// CodeJob is the Pando job id of the source-tree index, empty when the
	// code half did not run.
	CodeJob string `json:"codeJob,omitempty"`
	// CodeError is the code-index failure of this repository alone: one
	// repository Pando refused must not be read as the whole job failing.
	CodeError string `json:"codeError,omitempty"`
}

// newSearchState builds the search surface from the resolved options. It never
// fails: a Pando URL that cannot be used is logged and leaves the companion on
// the core index, which is exactly what an operator with a typo in the URL
// needs to keep working.
func newSearchState(opts Options, repos *registry, hub *Hub, log *slog.Logger, now func() time.Time) *searchState {
	s := &searchState{
		log:        log,
		repos:      repos,
		hub:        hub,
		now:        now,
		configPath: opts.ConfigPath,
		settings:   opts.Search.Pando,
	}
	s.rebuild()
	return s
}

// rebuild reconciles the client and the searcher with the running settings. It
// is called at construction and after a settings change.
func (s *searchState) rebuild() {
	s.mu.Lock()
	settings := s.settings
	old := s.client
	s.client = nil
	s.searcher = nil
	s.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}

	var client pandoAPI
	if strings.TrimSpace(settings.MCPURL) != "" {
		c, err := pando.New(pando.Options{
			MCPURL:      settings.MCPURL,
			Token:       settings.MCPToken,
			RESTURL:     settings.RESTURL,
			RESTToken:   settings.RESTToken,
			ProjectID:   s.projectID(),
			AllowRemote: settings.AllowRemote,
		})
		if err != nil {
			// A refused endpoint is a configuration problem, not a request
			// failure: say so once, here, and stay on the core index.
			s.log.Warn("semantic search is off: the Pando endpoint was refused", "error", err)
		} else {
			client = c
		}
	}

	var searcher *pandoSearcher
	if client != nil {
		searcher = &pandoSearcher{client: client, repos: s.repos, log: s.log, projects: s.codeProjects()}
	}
	s.mu.Lock()
	s.client, s.searcher = client, searcher
	s.mu.Unlock()

	// The workspace exposes semantic search as "search.semantic" for every
	// caller of the core contract — the MCP tools included — not only for the
	// REST endpoint (GIT-US-0088).
	if searcher == nil {
		s.repos.workspace().SetSemanticSearcher(nil)
	} else {
		s.repos.workspace().SetSemanticSearcher(searcher)
	}
}

// projectID is the Pando code project the semantic surface talks to.
//
// An empty `search.pando.projectId` is not "no project": docs/07 section 4 and
// config.SearchPando both promise the identifier Pando itself would derive
// from the repository path. There is one Pando per repository, so the default
// is derived here from the mount this state serves, rather than left empty and
// turned into ErrNotConfigured at the first SearchCode call (GIT-T-0142).
func (s *searchState) projectID() string {
	s.mu.RLock()
	configured := strings.TrimSpace(s.settings.ProjectID)
	s.mu.RUnlock()
	if configured != "" {
		return configured
	}
	return derivedProjectID(s.repos)
}

// derivedProjectID is the project id of the repository the workspace is built
// around: the first ready mount, in mount order. It is the id the registration
// registers that repository under, so the search and the registration can never
// disagree about which project is being read (GIT-US-0098). A workspace with no
// ready mount answers the empty string.
func derivedProjectID(repos *registry) string {
	if repos == nil {
		return ""
	}
	for _, m := range repos.ready() {
		if id := codeProjectID(m); id != "" {
			return id
		}
	}
	return ""
}

// publishProgress emits one search.progress frame.
func (s *searchState) publishProgress(job, repo, phase string, done, total int, message string) {
	if s.hub == nil {
		return
	}
	percent := 0
	if total > 0 {
		percent = min(100, done*100/total)
	} else if phase == searchPhaseDone {
		percent = 100
	}
	s.hub.Publish(eventSearchProgress, map[string]any{
		"operationId": job,
		"repo":        repo,
		"phase":       phase,
		"percent":     percent,
		"done":        done,
		"total":       total,
		"message":     message,
	})
}

// pando returns the client, nil when none is configured.
func (s *searchState) pando() pandoAPI {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.client
}

// semantic returns the searcher, nil when semantic search is off.
func (s *searchState) semantic() *pandoSearcher {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.searcher
}

// backend names the search backend currently in force, which is the value of
// the `features.search` capability. It is "pando" only while the accelerator is
// configured and answering; a degraded one reports "core", because that is what
// the next query will actually use.
func (s *searchState) backend() string {
	if s.semantic() == nil || s.degraded.Load() {
		return core.SearchSourceCore
	}
	return core.SearchSourcePando
}

// noteDegraded records the outcome of one search and logs the transition. It
// answers whether the response should carry `degraded`.
func (s *searchState) noteDegraded(err error) bool {
	if err == nil {
		if s.degraded.CompareAndSwap(true, false) {
			s.log.Info("semantic search recovered", "backend", core.SearchSourcePando)
		}
		return false
	}
	if notConfigured(err) {
		// Switched off is not degraded: there is nothing to recover.
		s.degraded.Store(false)
		return false
	}
	if s.degraded.CompareAndSwap(false, true) {
		s.log.Warn("semantic search degraded: answering from the core index", "error", err)
	}
	return true
}

// ------------------------------------------------------------- settings ----

// searchSettingsView is what GET|PATCH /api/v1/search/settings answers. It
// never carries a token: the two Pando tokens are resolved from the
// environment or the configuration file and stay in this process.
type searchSettingsView struct {
	// Backend is the search backend in force: "core" or "pando".
	Backend string `json:"backend"`
	// Configured reports whether a Pando endpoint is set at all.
	Configured  bool   `json:"configured"`
	MCPURL      string `json:"mcpUrl,omitempty"`
	RESTURL     string `json:"restUrl,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	AllowRemote bool   `json:"allowRemote"`
	// Reachable is the outcome of a live probe of the MCP endpoint. It is null
	// when no endpoint is configured.
	Reachable *bool `json:"reachable"`
	// ReachableError is why the probe failed, empty when it did not.
	ReachableError string `json:"reachableError,omitempty"`
	// Indexed says what Pando is pointed at for every mounted repository.
	Indexed []searchIndexedView `json:"indexed"`
	// Reindex is the running job, or the last finished one.
	Reindex *reindexJob `json:"reindex,omitempty"`
	// Persisted says whether a PATCH reached the configuration file. It is
	// false on a GET and on a companion started without one.
	Persisted bool `json:"persisted"`
}

// searchIndexedView is what Pando indexes for one mounted repository.
//
// There is no exported copy to report on any more (GIT-EP-0020), so the honest
// answer to "is my search current?" is where Pando was pointed and how much
// git-in-track's own index found there: a documentation directory holding no
// items is a misconfigured KBPath, and that is what this row makes visible.
type searchIndexedView struct {
	Repo string `json:"repo"`
	// Root is the working tree, the path registered as a Pando code project.
	Root string `json:"root"`
	// Docs are the documentation directories of the repository. Pando's
	// KBPath points at one of them, and the backlog lives under it in
	// `.pmngr/`, so one KB indexation covers both.
	Docs []string `json:"docs"`
	// Items, Pages and Comments are what this companion's own index holds
	// under those directories.
	Items    int `json:"items"`
	Pages    int `json:"pages"`
	Comments int `json:"comments"`
	// Code is the repository's code-project registration: the project it was
	// registered under, or why there is no code search. It is absent until the
	// registration that starts with the server has run.
	Code *codeIndexView `json:"code,omitempty"`
}

// view renders the settings. probe controls whether the reachability check
// actually dials Pando, so that a GET can answer honestly while a PATCH
// response does not pay for a second round trip.
func (s *searchState) view(ctx context.Context, probe bool) searchSettingsView {
	s.mu.RLock()
	settings := s.settings
	s.mu.RUnlock()

	out := searchSettingsView{
		Backend:     s.backend(),
		Configured:  strings.TrimSpace(settings.MCPURL) != "",
		MCPURL:      settings.MCPURL,
		RESTURL:     settings.RESTURL,
		ProjectID:   s.projectID(),
		AllowRemote: settings.AllowRemote,
		Indexed:     []searchIndexedView{},
	}
	for _, m := range s.repos.ready() {
		stats := m.vlt.Stats()
		docs := m.docsFolders
		if len(docs) == 0 && m.docs != "" {
			docs = []string{m.docs}
		}
		out.Indexed = append(out.Indexed, searchIndexedView{
			Repo: m.id, Root: m.path, Docs: docs,
			Items: stats.Items, Pages: stats.Pages, Comments: stats.Comments,
			Code: s.codeIndexOf(m.id),
		})
	}
	if probe && out.Configured {
		reachable := false
		if client := s.pando(); client != nil {
			if err := client.Health(ctx); err != nil {
				out.ReachableError = err.Error()
			} else {
				reachable = true
			}
		} else {
			out.ReachableError = "the configured Pando endpoint was refused by this companion"
		}
		out.Reachable = &reachable
	}
	s.jobMu.Lock()
	if s.running != nil {
		job := *s.running
		out.Reindex = &job
	} else if s.last != nil {
		job := *s.last
		out.Reindex = &job
	}
	s.jobMu.Unlock()
	return out
}

// searchSettingsPatch is the body of PATCH /api/v1/search/settings. Every field
// is optional; an absent one is left alone. Neither token is patchable: a
// credential enters this process from the environment or the configuration
// file and never over the API.
type searchSettingsPatch struct {
	MCPURL      *string `json:"mcpUrl,omitempty"`
	RESTURL     *string `json:"restUrl,omitempty"`
	ProjectID   *string `json:"projectId,omitempty"`
	AllowRemote *bool   `json:"allowRemote,omitempty"`
}

// apply validates the patch, adopts it in this process and rebuilds everything
// that depends on it.
func (s *searchState) apply(patch searchSettingsPatch) error {
	s.mu.RLock()
	next := s.settings
	s.mu.RUnlock()

	if patch.MCPURL != nil {
		next.MCPURL = strings.TrimSpace(*patch.MCPURL)
	}
	if patch.RESTURL != nil {
		next.RESTURL = strings.TrimSpace(*patch.RESTURL)
	}
	if patch.ProjectID != nil {
		next.ProjectID = strings.TrimSpace(*patch.ProjectID)
	}
	if patch.AllowRemote != nil {
		next.AllowRemote = *patch.AllowRemote
	}
	// The endpoints are validated by the client itself, which is the only
	// place that knows what it will accept — a loopback rule included. Doing it
	// here as well would let the two drift apart.
	if next.MCPURL != "" || next.RESTURL != "" {
		probe, err := pando.New(pando.Options{
			MCPURL: next.MCPURL, RESTURL: next.RESTURL, AllowRemote: next.AllowRemote,
		})
		if err != nil {
			return err //nolint:wrapcheck // the client already names the field
		}
		_ = probe.Close()
	}
	s.mu.Lock()
	s.settings = next
	s.mu.Unlock()
	s.degraded.Store(false)
	s.rebuild()
	return nil
}

// persist writes the new `search.pando` section into the configuration file, so
// a change made in the UI survives a restart. A companion with no configuration
// path keeps the change in memory only and says so.
//
// Only the non-secret fields are written: the tokens in the file are left
// exactly as they are, so a token that reached this process from the
// environment can never be copied into a file by a settings change.
func (s *searchState) persist() (bool, error) {
	if s.configPath == "" {
		return false, nil
	}
	s.mu.RLock()
	settings := s.settings
	s.mu.RUnlock()

	cfg, err := config.Load(s.configPath)
	if err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	cfg.Search.Pando.MCPURL = settings.MCPURL
	cfg.Search.Pando.RESTURL = settings.RESTURL
	cfg.Search.Pando.ProjectID = settings.ProjectID
	cfg.Search.Pando.AllowRemote = settings.AllowRemote
	if err := config.Save(s.configPath, cfg); err != nil {
		return false, err //nolint:wrapcheck // config already names the file
	}
	return true, nil
}

// -------------------------------------------------------------- handlers ---

// handleSearchSettings serves GET /api/v1/search/settings.
func (s *Server) handleSearchSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, s.search.view(r.Context(), true))
}

// handleSearchSettingsPatch serves PATCH /api/v1/search/settings.
func (s *Server) handleSearchSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var patch searchSettingsPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	if err := s.search.apply(patch); err != nil {
		failProblem(w, r, codeInvalidRequest, err.Error())
		return
	}
	persisted, err := s.search.persist()
	if err != nil {
		// The running process already honors the change; only the file did
		// not take it, and the user has to know which of the two happened.
		s.log.Warn("could not persist the search settings", "error", err)
	}
	view := s.search.view(r.Context(), true)
	view.Persisted = persisted
	writeJSON(w, r, http.StatusOK, view)
}

// handleSearchReindex serves POST /api/v1/search/reindex.
func (s *Server) handleSearchReindex(w http.ResponseWriter, r *http.Request) {
	job, err := s.search.startReindex(context.WithoutCancel(r.Context()))
	switch {
	case errors.Is(err, errReindexRunning):
		failProblem(w, r, codeSearchReindexRunning,
			"A reindex is already running; wait for it to finish rather than starting a second one.")
		return
	case errors.Is(err, errSearchNotConfigured):
		failProblem(w, r, codeSearchNotConfigured,
			"Nothing to reindex: no Pando endpoint is configured.")
		return
	case err != nil:
		failProblem(w, r, codeInternal, err.Error())
		return
	}
	writeJSON(w, r, http.StatusAccepted, job)
}

// errReindexRunning and errSearchNotConfigured are the two refusals of a
// reindex request.
var (
	errReindexRunning      = errors.New("a reindex is already running")
	errSearchNotConfigured = errors.New("no Pando endpoint")
)

// startReindex claims the single reindex slot and runs the job in the
// background, reporting on the hub. It answers as soon as the job has an id: a
// full reindex of a large repository takes longer than an HTTP request should.
func (s *searchState) startReindex(ctx context.Context) (*reindexJob, error) {
	if s.pando() == nil {
		return nil, errSearchNotConfigured
	}

	s.jobMu.Lock()
	if s.running != nil {
		s.jobMu.Unlock()
		return nil, errReindexRunning
	}
	job := &reindexJob{
		ID:        "reindex-" + strconv.FormatUint(searchJobCounter.Add(1), 10),
		StartedAt: s.now().UTC(),
		Phase:     searchPhaseCode,
		Repos:     []reindexRepo{},
	}
	s.running = job
	s.jobMu.Unlock()

	started := *job
	go s.runReindex(ctx, job)
	return &started, nil
}

// runReindex is the job itself: the code index of each source tree, then the
// knowledge-base half.
//
// The two are independent on purpose. Pando refusing one repository's source
// tree must not stop the knowledge base being reindexed, so each failure is
// recorded against its own half and the job carries on.
func (s *searchState) runReindex(ctx context.Context, job *reindexJob) {
	// job is the same pointer view() copies under jobMu, so every write to it
	// goes through mutate; a settings read racing a running reindex would
	// otherwise observe a half-written record.
	mutate := func(fn func(j *reindexJob)) {
		s.jobMu.Lock()
		fn(job)
		s.jobMu.Unlock()
	}
	defer func() {
		var phase, note string
		s.jobMu.Lock()
		job.EndedAt = s.now().UTC()
		s.running, s.last = nil, job
		phase, note = job.Phase, job.KBNote
		s.jobMu.Unlock()
		s.publishProgress(job.ID, "", phase, 1, 1, note)
	}()

	client := s.pando()
	// The same project list the registration uses, so an explicit reindex asks
	// Pando to refresh the very project the search reads from.
	for _, p := range s.codeProjects() {
		out := reindexRepo{Repo: p.repo}
		s.publishProgress(job.ID, p.repo, searchPhaseCode, 0, 0, "indexing the source tree")
		if client != nil {
			id, err := client.IndexProject(ctx, p.root, p.id)
			switch {
			case err != nil:
				out.CodeError = err.Error()
				s.log.Warn("code index failed", "repo", p.repo, "error", err)
			default:
				out.CodeJob = id
				s.noteCodeIndex(p.repo, codeIndexView{
					Project: p.id, Status: codeIndexStatusIndexing, Job: id,
					Note: "Reindexing the repository root.",
				})
			}
		}
		mutate(func(j *reindexJob) { j.Repos = append(j.Repos, out) })
	}

	// The knowledge-base half. Pando watches the documentation directory
	// itself, so this is a catch-up pass rather than the only way the index
	// ever changes — and without a REST URL there is no route to ask for one.
	mutate(func(j *reindexJob) { j.Phase = searchPhaseKB })
	s.publishProgress(job.ID, "", searchPhaseKB, 0, 0, "reindexing the knowledge base")
	var kbStats *pando.ReindexStats
	var kbNote string
	if client == nil {
		kbNote = "No Pando endpoint is configured, so nothing was reindexed."
	} else {
		stats, err := client.ReindexKB(ctx)
		switch {
		case notConfigured(err):
			kbNote = "Not reindexed: no Pando REST URL is configured, and the reindex route " +
				"is only on the REST surface. Pando's own watcher still follows the " +
				"documentation directory."
		case err != nil:
			kbNote = "The knowledge-base reindex failed: " + err.Error()
			s.log.Warn("knowledge base reindex failed", "error", err)
		default:
			kbStats = &stats
			kbNote = "Reindexed."
		}
	}

	mutate(func(j *reindexJob) {
		j.KB, j.KBNote = kbStats, kbNote
		j.Phase = searchPhaseDone
		for _, repo := range j.Repos {
			if repo.CodeError != "" {
				j.Phase = searchPhaseFailed
				j.Error = "one or more repositories failed; see repos[]"
				break
			}
		}
	})
}
