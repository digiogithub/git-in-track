package server

import (
	"context"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// The sync surface, story GIT-US-0021 (docs/06-git-sync.md section 4,
// docs/07-cli-and-api.md section 5.5).
//
// A run is PREFLIGHT -> FETCH -> INTEGRATE -> PUSH per repository. The
// preflight step this layer owns is committing what commit-on-save batched:
// the pipeline refuses to touch a tree with uncommitted work, so a sync
// flushes the committer before it fetches.
//
// The run is synchronous and streams `sync.progress` while it works. Canceling
// the HTTP request cancels the run through its context, and every phase is
// non-destructive on failure, so an interrupted sync leaves a tree the user can
// carry on from.

// syncRepoStatus is one repository in GET /api/v1/sync/status.
type syncRepoStatus struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	// Git is false when the folder is not a git working tree; Reason says so.
	Git     bool   `json:"git"`
	Reason  string `json:"reason,omitempty"`
	Backend string `json:"backend,omitempty"`
	// Status is nil only when reading it failed, which Reason then explains.
	Status *gitops.SyncStatus `json:"status,omitempty"`
	// Pending is how many edits commit-on-save has batched but not committed;
	// a sync commits them first.
	Pending int `json:"pending"`
}

// syncRunRequest is the body of POST /api/v1/sync/run.
type syncRunRequest struct {
	// Repos limits the run; empty means every registered repository.
	Repos []string `json:"repos,omitempty"`
	// DryRun previews without integrating or pushing.
	DryRun bool `json:"dryRun,omitempty"`
	// Push overrides `git.pushOnSync` for this run.
	Push *bool `json:"push,omitempty"`
	// Strategy overrides `git.pullStrategy` for this run.
	Strategy string `json:"strategy,omitempty"`
}

// syncRunResponse is what a run reports back.
type syncRunResponse struct {
	OperationID string              `json:"operationId"`
	StartedAt   string              `json:"startedAt"`
	DryRun      bool                `json:"dryRun"`
	Results     []gitops.SyncResult `json:"results"`
	// Commits is what the preflight committed before fetching.
	Commits []commitEventData `json:"commits,omitempty"`
}

// conflictEventData is the payload of a `conflict.detected` event.
type conflictEventData struct {
	OperationID string             `json:"operationId"`
	Repo        string             `json:"repo"`
	Paths       []string           `json:"paths"`
	Kind        string             `json:"kind"`
	Conflicts   []gitops.Conflict  `json:"conflicts"`
	Operation   string             `json:"operation,omitempty"`
	Strategy    gitops.Strategy    `json:"strategy,omitempty"`
	Resolvable  string             `json:"resolvable"`
	Status      *gitops.SyncStatus `json:"status,omitempty"`
}

// syncCounter numbers the operations of this process, so every run has an id
// the progress events can be correlated by.
var syncCounter atomic.Uint64

// mountSync composes the /sync subtree.
func (s *Server) mountSync(r chi.Router) {
	r.Get("/status", s.handleSyncStatus)
	r.Post("/run", s.handleSyncRun)
	r.Post("/abort", s.handleSyncAbort)
	r.Get("/conflicts", s.handleSyncConflicts)
	r.Patch("/settings", s.handleSyncSettingsPatch)
	r.Post("/conflicts/resolve", s.notImplemented(
		"Resolving a conflict from the API arrives with GIT-US-0022; "+
			"until then resolve the files and continue the rebase, or POST /api/v1/sync/abort."))
}

// handleSyncStatus serves GET /api/v1/sync/status.
func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	wanted := r.URL.Query().Get("repo")
	out := make([]syncRepoStatus, 0, len(s.repos.all()))
	for _, m := range s.repos.all() {
		if wanted != "" && m.id != wanted {
			continue
		}
		out = append(out, s.syncStatusOf(r.Context(), m))
	}
	if wanted != "" && len(out) == 0 {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+wanted+".")
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"repos":    out,
		"settings": s.git.syncView(),
	})
}

// syncStatusOf reads one repository's sync state.
func (s *Server) syncStatusOf(ctx context.Context, m *mount) syncRepoStatus {
	out := syncRepoStatus{Repo: m.id, Path: m.path, Pending: s.git.pending()}
	backend, ok := s.git.backendFor(m.id)
	if !ok {
		out.Reason = s.git.reasonFor(m.id)
		return out
	}
	out.Git = true
	out.Backend = backend.Name()
	st, err := backend.SyncStatus(ctx)
	if err != nil {
		out.Reason = err.Error()
		return out
	}
	out.Status = &st
	return out
}

// handleSyncRun serves POST /api/v1/sync/run.
func (s *Server) handleSyncRun(w http.ResponseWriter, r *http.Request) {
	var body syncRunRequest
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	settings, _ := s.git.current()
	strategy := gitops.Strategy(settings.PullStrategy)
	if body.Strategy != "" {
		strategy = gitops.Strategy(body.Strategy)
	}
	if strategy == "" {
		strategy = gitops.StrategyRebase
	}
	if !strategy.Valid() {
		failProblem(w, r, codeInvalidRequest,
			"Unknown integration strategy "+body.Strategy+": use rebase or merge.")
		return
	}
	mounts, ok := s.syncTargets(w, r, body.Repos)
	if !ok {
		return
	}

	op := "sync-" + strconv.FormatUint(syncCounter.Add(1), 10)
	resp := syncRunResponse{
		OperationID: op,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
		DryRun:      body.DryRun,
		Results:     make([]gitops.SyncResult, 0, len(mounts)),
	}

	// PREFLIGHT: commit what commit-on-save batched. A dry run previews, so it
	// must not commit anything either.
	if !body.DryRun {
		resp.Commits = renderOutcomes(s.git.flush(r.Context()))
	}

	push := settings.PushOnSync
	if body.Push != nil {
		push = *body.Push
	}
	for _, m := range mounts {
		resp.Results = append(resp.Results, s.syncOne(r.Context(), op, m, gitops.SyncOptions{
			Repo:           m.id,
			Strategy:       strategy,
			Push:           push,
			DryRun:         body.DryRun,
			MaxPushRetries: settings.MaxPushRetries,
		}))
	}
	if !body.DryRun {
		s.refreshSnapshotsAfterSync(r.Context(), resp.Results)
	}
	writeJSON(w, r, http.StatusOK, resp)
}

// syncTargets resolves the repositories a run acts on.
func (s *Server) syncTargets(w http.ResponseWriter, r *http.Request, ids []string) ([]*mount, bool) {
	if len(ids) == 0 {
		out := make([]*mount, 0, len(s.repos.all()))
		for _, m := range s.repos.all() {
			if _, ok := s.git.backendFor(m.id); ok {
				out = append(out, m)
			}
		}
		return out, true
	}
	out := make([]*mount, 0, len(ids))
	for _, id := range ids {
		m, ok := s.repos.lookup(id)
		if !ok {
			failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+id+".")
			return nil, false
		}
		if _, ok := s.git.backendFor(m.id); !ok {
			failProblem(w, r, codeInvalidRequest,
				"Repository "+m.id+" is not a git working tree: "+s.git.reasonFor(m.id))
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}

// syncOne runs the pipeline for one repository and publishes its progress.
func (s *Server) syncOne(ctx context.Context, op string, m *mount, opts gitops.SyncOptions) gitops.SyncResult {
	backend, ok := s.git.backendFor(m.id)
	if !ok {
		return gitops.SyncResult{
			Repo: m.id, Phase: gitops.PhaseFailed, Code: gitops.CodeNotARepository,
			Message: s.git.reasonFor(m.id),
		}
	}
	opts.Progress = func(p gitops.Progress) {
		s.hub.Publish(eventSyncProgress, map[string]any{
			"operationId": op,
			"repo":        p.Repo,
			"phase":       string(p.Phase),
			"percent":     p.Percent,
			"message":     p.Message,
			"ahead":       p.Ahead,
			"behind":      p.Behind,
		})
	}
	res, err := gitops.Sync(ctx, backend, opts)
	if err != nil && len(res.Conflicts) > 0 {
		s.publishConflict(ctx, op, backend, res)
	}
	if err != nil {
		s.log.Warn("sync failed", "repo", m.id, "code", res.Code, "error", err)
	}
	return res
}

// publishConflict announces a stopped integration, naming every path so that
// the UI can offer the resolver (GIT-US-0022) instead of a generic failure.
func (s *Server) publishConflict(ctx context.Context, op string, backend gitops.Backend, res gitops.SyncResult) {
	paths := make([]string, 0, len(res.Conflicts))
	kind := gitops.ConflictContent
	for _, c := range res.Conflicts {
		paths = append(paths, c.Path)
		if c.Kind != gitops.ConflictContent {
			kind = c.Kind
		}
	}
	data := conflictEventData{
		OperationID: op, Repo: res.Repo, Paths: paths, Kind: kind,
		Conflicts: res.Conflicts, Strategy: res.Strategy, Resolvable: "manual",
	}
	if st, err := backend.SyncStatus(ctx); err == nil {
		data.Status, data.Operation = &st, st.Operation
	}
	s.hub.Publish(eventConflictDetected, data)
}

// refreshSnapshotsAfterSync regenerates the committed index snapshots once
// incoming work has landed, which is rule R-SNAP-6(a) of docs/04 section 6.
// A failure is logged, never fatal: the sync itself already succeeded.
func (s *Server) refreshSnapshotsAfterSync(ctx context.Context, results []gitops.SyncResult) {
	moved := false
	for _, res := range results {
		if res.OK() && res.Pulled > 0 {
			moved = true
		}
	}
	if !moved {
		return
	}
	if _, err := s.repos.workspace().Dispatch(ctx, "snapshot.refresh", mustJSON(map[string]any{})); err != nil {
		s.log.Warn("could not refresh the index snapshots after the sync", "error", err)
	}
}

// handleSyncAbort serves POST /api/v1/sync/abort: undo a half-finished rebase
// or merge, which is the "get me back where I was" escape hatch of docs/06
// section 12, failures 6 and 8.
func (s *Server) handleSyncAbort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Repo string `json:"repo"`
	}
	if r.ContentLength > 0 && !decodeBody(w, r, &body) {
		return
	}
	m, ok := s.repos.lookup(body.Repo)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+body.Repo+".")
		return
	}
	backend, ok := s.git.backendFor(m.id)
	if !ok {
		failProblem(w, r, codeInvalidRequest,
			"Repository "+m.id+" is not a git working tree: "+s.git.reasonFor(m.id))
		return
	}
	if err := backend.Abort(r.Context()); err != nil {
		writeGitError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, s.syncStatusOf(r.Context(), m))
}

// handleSyncConflicts serves GET /api/v1/sync/conflicts: the conflicted paths
// of every repository whose integration stopped. Reading the three versions of
// a conflicted file and writing a resolution is GIT-US-0022.
func (s *Server) handleSyncConflicts(w http.ResponseWriter, r *http.Request) {
	wanted := r.URL.Query().Get("repo")
	out := make([]conflictEventData, 0, len(s.repos.all()))
	for _, m := range s.repos.all() {
		if wanted != "" && m.id != wanted {
			continue
		}
		backend, ok := s.git.backendFor(m.id)
		if !ok {
			continue
		}
		st, err := backend.SyncStatus(r.Context())
		if err != nil || len(st.Conflicted) == 0 {
			continue
		}
		paths := make([]string, 0, len(st.Conflicted))
		for _, c := range st.Conflicted {
			paths = append(paths, c.Path)
		}
		status := st
		out = append(out, conflictEventData{
			Repo: m.id, Paths: paths, Kind: st.Conflicted[0].Kind, Conflicts: st.Conflicted,
			Operation: st.Operation, Resolvable: "manual", Status: &status,
		})
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"conflicts": out})
}

// syncSettings is the sync half of the git settings, reported next to a status
// listing so the UI does not need a second call to know the strategy.
type syncSettings struct {
	PullStrategy   string `json:"pullStrategy"`
	PushOnSync     bool   `json:"pushOnSync"`
	MaxPushRetries int    `json:"maxPushRetries"`
	// Supported is false when this runtime cannot sync at all, mirroring the
	// browser provider's flag so the UI branches on one field in both modes.
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// syncSettingsPatch is the body of PATCH /api/v1/sync/settings. Every field is
// a pointer so that an absent one is left alone.
type syncSettingsPatch struct {
	PullStrategy   *string `json:"pullStrategy,omitempty"`
	PushOnSync     *bool   `json:"pushOnSync,omitempty"`
	MaxPushRetries *int    `json:"maxPushRetries,omitempty"`
}

// handleSyncSettingsPatch serves PATCH /api/v1/sync/settings.
func (s *Server) handleSyncSettingsPatch(w http.ResponseWriter, r *http.Request) {
	var patch syncSettingsPatch
	if !decodeBody(w, r, &patch) {
		return
	}
	if err := s.git.applySync(patch); err != nil {
		failProblem(w, r, codeInvalidRequest, err.Error())
		return
	}
	if _, err := s.git.persist(); err != nil {
		// The running process already honors the change; only the file did not
		// take it, and the user has to know which of the two happened.
		s.log.Warn("could not persist the sync settings", "error", err)
	}
	writeJSON(w, r, http.StatusOK, s.git.syncView())
}

// applySync validates and swaps the sync half of the git settings.
func (g *gitState) applySync(patch syncSettingsPatch) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	next := g.settings
	if patch.PullStrategy != nil {
		strategy := config.PullStrategy(*patch.PullStrategy)
		if !strategy.Valid() {
			return &settingsError{field: "pullStrategy", message: "unknown strategy: use rebase or merge"}
		}
		next.PullStrategy = strategy
	}
	if patch.PushOnSync != nil {
		next.PushOnSync = *patch.PushOnSync
	}
	if patch.MaxPushRetries != nil {
		if *patch.MaxPushRetries < 0 {
			return &settingsError{field: "maxPushRetries", message: "must not be negative"}
		}
		next.MaxPushRetries = *patch.MaxPushRetries
	}
	g.settings = next
	return nil
}

// syncView renders the sync settings.
func (g *gitState) syncView() syncSettings {
	g.mu.RLock()
	settings := g.settings
	g.mu.RUnlock()
	strategy := settings.PullStrategy
	if strategy == "" {
		strategy = config.PullRebase
	}
	retries := settings.MaxPushRetries
	if retries <= 0 {
		retries = config.DefaultMaxPushRetries
	}
	return syncSettings{
		PullStrategy:   string(strategy),
		PushOnSync:     settings.PushOnSync,
		MaxPushRetries: retries,
		Supported:      true,
	}
}
