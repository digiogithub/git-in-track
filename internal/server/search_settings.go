package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/pando"
	"github.com/digiogithub/git-in-track/internal/pandosync"
)

// The problem codes of the search surface.
const (
	// codeSearchReindexRunning means a reindex is already walking the corpus.
	// It is a 409: the caller waits for the running job rather than starting a
	// second one that would fight it for the same files.
	codeSearchReindexRunning = "search_reindex_running"
	// codeSearchNotConfigured means no Pando endpoint and no corpus directory
	// are configured, so there is nothing to reindex.
	codeSearchNotConfigured = "search_not_configured"
)

// eventSearchProgress is the hub topic both the startup export and a reindex
// report on. Its payload is shaped like sync.progress (docs/07 section 5.6) so
// that a client already following that stream needs no second renderer.
const eventSearchProgress = "search.progress"

// The phases eventSearchProgress reports, in the order a reindex walks them.
const (
	searchPhaseExport = "export"
	searchPhaseCode   = "code"
	searchPhaseKB     = "kb"
	searchPhaseDone   = "completed"
	searchPhaseFailed = "failed"
)

// searchJobCounter numbers reindex jobs within one process, the way the sync
// pipeline numbers its operations.
var searchJobCounter atomic.Uint64

// searchState owns everything behind /api/v1/search: the Pando client, the
// corpus exporter of every mounted repository, the running `search.pando`
// settings and the single reindex slot.
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
	// corpusBase is the default corpus root, `<index.cacheDir>/pando-kb`. The
	// per-repository directory under it is what `gintrack agent init` writes
	// into Pando's KBPath, so the two must agree: <base>/<repo id>.
	corpusBase string

	mu sync.RWMutex
	// settings is the running `search.pando` section. It carries the resolved
	// tokens and is never marshaled: the view below is what a response sees.
	settings config.SearchPando
	// client is nil when no MCP URL is configured, or when the configured one
	// was refused (a non-loopback host without allowRemote).
	client   pandoAPI
	searcher *pandoSearcher
	// exporters is one corpus exporter per ready mount, keyed by mount id. It
	// is empty when no corpus directory could be resolved.
	exporters map[string]*pandosync.Exporter

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
	// without Pando's REST reindex route the honest answer is "re-exported,
	// awaiting Pando's next import", not "reindexed".
	KBNote string `json:"kbNote,omitempty"`
	Error  string `json:"error,omitempty"`
}

// reindexRepo is the outcome of one repository within a reindex.
type reindexRepo struct {
	Repo string `json:"repo"`
	// Export is the corpus export of this repository.
	Export pandosync.Stats `json:"export"`
	// ExportError is the export failure, empty on success.
	ExportError string `json:"exportError,omitempty"`
	// CodeJob is the Pando job id of the source-tree index, empty when the
	// code half did not run.
	CodeJob string `json:"codeJob,omitempty"`
	// CodeError is the code-index failure. It is reported separately from
	// ExportError on purpose: a Pando that is down must not invalidate a
	// corpus export that worked.
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
		corpusBase: opts.SearchCorpusDir,
		settings:   opts.Search.Pando,
		exporters:  map[string]*pandosync.Exporter{},
	}
	s.rebuild()
	return s
}

// rebuild reconciles the client, the searcher and the exporters with the
// running settings. It is called at construction and after a settings change.
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
			ProjectID:   settings.ProjectID,
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
		searcher = &pandoSearcher{client: client, repos: s.repos, log: s.log}
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
	s.rebuildExporters()
}

// rebuildExporters opens one corpus exporter per ready mount under the
// configured corpus root. A repository whose exporter cannot be opened is
// logged and left without one; the others still export.
func (s *searchState) rebuildExporters() {
	base := s.corpusRoot()
	next := map[string]*pandosync.Exporter{}
	if base != "" {
		for _, m := range s.repos.ready() {
			exp, err := pandosync.New(pandosync.Options{
				Dir:        filepath.Join(base, m.id),
				Source:     m.vlt,
				Logger:     s.log,
				Clock:      s.now,
				OnProgress: s.progressFor(m.id),
			})
			if err != nil {
				s.log.Warn("corpus export is off for this repository", "repo", m.id, "error", err)
				continue
			}
			next[m.id] = exp
		}
	}
	s.mu.Lock()
	s.exporters = next
	s.mu.Unlock()
}

// corpusRoot is the directory the per-repository corpora live under:
// `search.pando.corpusDir` when set, else `<index.cacheDir>/pando-kb`.
func (s *searchState) corpusRoot() string {
	s.mu.RLock()
	dir := strings.TrimSpace(s.settings.CorpusDir)
	s.mu.RUnlock()
	if dir != "" {
		return dir
	}
	return s.corpusBase
}

// progressFor turns the exporter's progress callback into a hub event. The
// startup export reports under the "startup" operation id; a reindex replaces
// it with its own job id through progressJob.
func (s *searchState) progressFor(repo string) func(pandosync.Progress) {
	return func(p pandosync.Progress) {
		s.publishProgress("startup", repo, searchPhaseExport, p.Done, p.Total, "")
	}
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

// exporterFor returns the corpus exporter of one mount.
func (s *searchState) exporterFor(id string) (*pandosync.Exporter, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.exporters[id]
	return exp, ok
}

// allExporters returns the exporters in mount order, so a reindex walks the
// repositories the way the banner lists them.
func (s *searchState) allExporters() []struct {
	repo string
	exp  *pandosync.Exporter
} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]struct {
		repo string
		exp  *pandosync.Exporter
	}, 0, len(s.exporters))
	for _, m := range s.repos.ready() {
		if exp, ok := s.exporters[m.id]; ok {
			out = append(out, struct {
				repo string
				exp  *pandosync.Exporter
			}{m.id, exp})
		}
	}
	return out
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
	Configured bool   `json:"configured"`
	MCPURL     string `json:"mcpUrl,omitempty"`
	RESTURL    string `json:"restUrl,omitempty"`
	ProjectID  string `json:"projectId,omitempty"`
	// CorpusDir is the resolved corpus root, the directory the per-repository
	// corpora are written under.
	CorpusDir   string `json:"corpusDir,omitempty"`
	AllowRemote bool   `json:"allowRemote"`
	// Reachable is the outcome of a live probe of the MCP endpoint. It is null
	// when no endpoint is configured.
	Reachable *bool `json:"reachable"`
	// ReachableError is why the probe failed, empty when it did not.
	ReachableError string `json:"reachableError,omitempty"`
	// Corpora is the last full export of every mounted repository.
	Corpora []searchCorpusView `json:"corpora"`
	// Documents is the exported document count across every repository.
	Documents int `json:"documents"`
	// LastExport is the most recent full export across every repository, zero
	// when none has finished yet.
	LastExport *time.Time `json:"lastExport"`
	// Reindex is the running job, or the last finished one.
	Reindex *reindexJob `json:"reindex,omitempty"`
	// Persisted says whether a PATCH reached the configuration file. It is
	// false on a GET and on a companion started without one.
	Persisted bool `json:"persisted"`
}

// searchCorpusView is one repository's corpus.
type searchCorpusView struct {
	Repo string          `json:"repo"`
	Dir  string          `json:"dir"`
	Last pandosync.Stats `json:"last"`
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
		ProjectID:   settings.ProjectID,
		CorpusDir:   s.corpusRoot(),
		AllowRemote: settings.AllowRemote,
		Corpora:     []searchCorpusView{},
	}
	for _, e := range s.allExporters() {
		last := e.exp.LastExport()
		out.Corpora = append(out.Corpora, searchCorpusView{Repo: e.repo, Dir: e.exp.Dir(), Last: last})
		out.Documents += last.Items + last.Pages
		if !last.At.IsZero() && (out.LastExport == nil || last.At.After(*out.LastExport)) {
			at := last.At
			out.LastExport = &at
		}
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
	CorpusDir   *string `json:"corpusDir,omitempty"`
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
	if patch.CorpusDir != nil {
		next.CorpusDir = strings.TrimSpace(*patch.CorpusDir)
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
	if next.CorpusDir != "" && !filepath.IsAbs(next.CorpusDir) {
		return errors.New("corpusDir: the corpus directory must be an absolute path")
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
	cfg.Search.Pando.CorpusDir = settings.CorpusDir
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
			"A corpus reindex is already running; wait for it to finish rather than starting a second one.")
		return
	case errors.Is(err, errSearchNotConfigured):
		failProblem(w, r, codeSearchNotConfigured,
			"Nothing to reindex: neither a corpus directory nor a Pando endpoint is configured.")
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
	errReindexRunning      = errors.New("a corpus reindex is already running")
	errSearchNotConfigured = errors.New("no corpus directory and no Pando endpoint")
)

// startReindex claims the single reindex slot and runs the job in the
// background, reporting on the hub. It answers as soon as the job has an id: a
// full re-export of a large corpus takes longer than an HTTP request should.
func (s *searchState) startReindex(ctx context.Context) (*reindexJob, error) {
	exporters := s.allExporters()
	client := s.pando()
	if len(exporters) == 0 && client == nil {
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
		Phase:     searchPhaseExport,
		Repos:     []reindexRepo{},
	}
	s.running = job
	s.jobMu.Unlock()

	started := *job
	go s.runReindex(ctx, job)
	return &started, nil
}

// runReindex is the job itself: a full corpus export per repository, then the
// code index of each source tree, then the knowledge-base half.
//
// The three are independent on purpose. A Pando that is down must not
// invalidate an export that worked, so each failure is recorded against its own
// half and the job carries on.
func (s *searchState) runReindex(ctx context.Context, job *reindexJob) {
	defer func() {
		job.EndedAt = s.now().UTC()
		s.jobMu.Lock()
		s.running, s.last = nil, job
		s.jobMu.Unlock()
		s.publishProgress(job.ID, "", job.Phase, 1, 1, job.KBNote)
	}()

	client := s.pando()
	for _, e := range s.allExporters() {
		out := reindexRepo{Repo: e.repo}
		s.publishProgress(job.ID, e.repo, searchPhaseExport, 0, 0, "exporting the corpus")
		stats, err := e.exp.ExportAll(ctx)
		out.Export = stats
		if err != nil {
			out.ExportError = err.Error()
			s.log.Warn("corpus export failed", "repo", e.repo, "error", err)
		}
		if client != nil {
			s.publishProgress(job.ID, e.repo, searchPhaseCode, 0, 0, "indexing the source tree")
			if m, ok := s.repos.lookup(e.repo); ok {
				id, err := client.IndexProject(ctx, m.path, m.id)
				switch {
				case err != nil:
					out.CodeError = err.Error()
					s.log.Warn("code index failed", "repo", e.repo, "error", err)
				default:
					out.CodeJob = id
				}
			}
		}
		job.Repos = append(job.Repos, out)
	}

	// The knowledge-base half. Pando imports the corpus on its own schedule
	// with KBWatch off, so without its REST reindex route the honest report is
	// "re-exported, awaiting the next import" — not "reindexed".
	job.Phase = searchPhaseKB
	s.publishProgress(job.ID, "", searchPhaseKB, 0, 0, "reindexing the knowledge base")
	if client == nil {
		job.KBNote = "Re-exported. No Pando endpoint is configured, so nothing was asked to import it."
	} else {
		stats, err := client.ReindexKB(ctx)
		switch {
		case notConfigured(err):
			job.KBNote = "Re-exported, awaiting Pando's next import pass: no REST URL is configured, " +
				"and with KBWatch off there is no watcher to trigger."
		case err != nil:
			job.KBNote = "Re-exported, but the knowledge-base reindex failed: " + err.Error()
			s.log.Warn("knowledge base reindex failed", "error", err)
		default:
			job.KB = &stats
			job.KBNote = "Reindexed."
		}
	}

	job.Phase = searchPhaseDone
	for _, repo := range job.Repos {
		if repo.ExportError != "" || repo.CodeError != "" {
			job.Phase = searchPhaseFailed
			job.Error = "one or more repositories failed; see repos[]"
			break
		}
	}
}
