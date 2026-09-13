package server

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/syncengine"
)

// The REST surface of the background job engine, story GIT-US-0078
// (docs/07-cli-and-api.md section 5.5).
//
// Six endpoints, all inside the authenticated group and all thin: the engine
// already holds one consistent view of its queue, so every handler here reads
// [syncengine.Engine.Snapshot] or calls one of its mutators and renders the
// result. There is deliberately no endpoint that enqueues a job: kinds are
// owned by the feature that creates them, and a generic enqueue would be a
// shape-level way to drive this companion's outbound HTTP from a page.

// defaultSyncJobsPerPage is the page GET /sync/jobs serves when the caller asks
// for none. The cap is maxItemsPerPage, as everywhere else.
const defaultSyncJobsPerPage = 100

// syncJobView is one job as the API renders it.
//
// It is [syncengine.Job] without the payload: the payload is the handler's
// business, it is the one field of a job whose content this package cannot
// vouch for, and nothing in the UI reads it.
type syncJobView struct {
	ID          string                  `json:"id"`
	Kind        string                  `json:"kind"`
	Key         string                  `json:"key,omitempty"`
	State       string                  `json:"state"`
	Attempts    int                     `json:"attempts"`
	CreatedAt   time.Time               `json:"createdAt"`
	UpdatedAt   time.Time               `json:"updatedAt"`
	NextAttempt *time.Time              `json:"nextAttempt,omitempty"`
	LastError   *syncengine.ErrorRecord `json:"lastError,omitempty"`
	DeadLetter  bool                    `json:"deadLetter,omitempty"`
}

// syncJobsPage is the answer of GET /api/v1/sync/jobs.
type syncJobsPage struct {
	Jobs []syncJobView `json:"jobs"`
	// NextCursor is the id to resume from; empty on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
	// Total is how many jobs matched the filter, before paging.
	Total int `json:"total"`
	// Counts summarizes the whole queue by state, not the page, so a badge
	// never needs a second call.
	Counts syncengine.Counts `json:"counts"`
	// Running is how many batches are inside a handler right now, and
	// DeadLetter how many jobs gave up and are waiting to be retried or
	// cleared.
	Running    int `json:"running"`
	DeadLetter int `json:"deadLetter"`
	// Engine reports whether the pool is up. A companion serving repositories
	// with no integration configured answers with an empty queue and
	// `engine: true`: an idle engine, not a missing one.
	Engine bool `json:"engine"`
}

// syncEngineView is the engine half of GET|PATCH /api/v1/sync/settings.
type syncEngineView struct {
	Workers     int     `json:"workers"`
	BatchSize   int     `json:"batchSize"`
	Rate        float64 `json:"rate"`
	MaxAttempts int     `json:"maxAttempts"`
	// RetentionHours is how long a finished job is kept, and DrainSeconds how
	// long a shutdown drains before it journals the rest. Both are rendered in
	// whole units because that is how they are configured.
	RetentionHours float64 `json:"retentionHours"`
	DrainSeconds   float64 `json:"drainSeconds"`
	// Running reports whether the pool is up.
	Running bool `json:"running"`
}

// syncEnginePatch is the engine half of the PATCH body. Every field is a
// pointer so that an absent one is left alone.
type syncEnginePatch struct {
	Workers     *int     `json:"workers,omitempty"`
	BatchSize   *int     `json:"batchSize,omitempty"`
	Rate        *float64 `json:"rate,omitempty"`
	MaxAttempts *int     `json:"maxAttempts,omitempty"`
}

// touched reports whether the patch addresses the engine at all.
func (p syncEnginePatch) touched() bool {
	return p.Workers != nil || p.BatchSize != nil || p.Rate != nil || p.MaxAttempts != nil
}

// engineView renders the settings in force.
func (s *Server) engineView() syncEngineView {
	current := s.sync.current()
	return syncEngineView{
		Workers:        current.Workers,
		BatchSize:      current.BatchSize,
		Rate:           current.Rate,
		MaxAttempts:    current.MaxAttempts,
		RetentionHours: current.Retention.Hours(),
		DrainSeconds:   current.DrainTimeout.Seconds(),
		Running:        s.sync.running(),
	}
}

// jobView renders one job.
func jobView(job syncengine.Job, dead bool) syncJobView {
	out := syncJobView{
		ID: job.ID, Kind: string(job.Kind), Key: job.Key,
		State: string(job.State), Attempts: job.Attempts,
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
		LastError: job.LastError, DeadLetter: dead,
	}
	if !job.NextAttempt.IsZero() {
		at := job.NextAttempt
		out.NextAttempt = &at
	}
	return out
}

// -------------------------------------------------------------- handlers ---

// mountSyncJobs registers the job routes under the existing /sync subtree.
func (s *Server) mountSyncJobs(r chi.Router) {
	r.Get("/jobs", s.handleSyncJobList)
	r.Get("/jobs/{id}", s.handleSyncJobGet)
	r.Post("/jobs/{id}/retry", s.handleSyncJobRetry)
	r.Post("/jobs/{id}/cancel", s.handleSyncJobCancel)
}

// handleSyncJobList serves GET /api/v1/sync/jobs?state=&kind=&limit=&cursor=.
//
// `state` and `kind` are repeatable and OR within a field, exactly as the item
// filter works. The page is bounded and the cursor is the id of the first job
// of the next page, which is stable because the engine hands its jobs back in
// creation order.
func (s *Server) handleSyncJobList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	states, ok := parseJobStates(w, r, q["state"])
	if !ok {
		return
	}
	kinds := q["kind"]
	limit := defaultSyncJobsPerPage
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			failProblem(w, r, codeInvalidRequest, "limit must be a positive whole number.")
			return
		}
		limit = min(n, maxItemsPerPage)
	}

	snapshot := s.sync.engine.Snapshot()
	dead := make(map[string]bool, len(snapshot.DeadLetter))
	for _, job := range snapshot.DeadLetter {
		dead[job.ID] = true
	}

	matched := make([]syncengine.Job, 0, len(snapshot.Jobs))
	for _, job := range snapshot.Jobs {
		if matchesJobFilter(job, states, kinds) {
			matched = append(matched, job)
		}
	}
	page := syncJobsPage{
		Jobs: make([]syncJobView, 0, limit), Total: len(matched), Counts: snapshot.Counts,
		Running: snapshot.Running, DeadLetter: len(snapshot.DeadLetter), Engine: s.sync.running(),
	}
	start := 0
	if cursor := q.Get("cursor"); cursor != "" {
		start = -1
		for i, job := range matched {
			if job.ID == cursor {
				start = i
				break
			}
		}
		if start < 0 {
			failProblem(w, r, codeInvalidRequest, "The cursor does not name a job of this queue; start the walk again.")
			return
		}
	}
	for i := start; i < len(matched) && len(page.Jobs) < limit; i++ {
		page.Jobs = append(page.Jobs, jobView(matched[i], dead[matched[i].ID]))
	}
	if next := start + len(page.Jobs); next < len(matched) {
		page.NextCursor = matched[next].ID
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(page.Total))
	writeJSON(w, r, http.StatusOK, page)
}

// parseJobStates validates the `state` filter, refusing a value the engine has
// no such state for rather than silently matching nothing.
func parseJobStates(w http.ResponseWriter, r *http.Request, raw []string) ([]syncengine.State, bool) {
	out := make([]syncengine.State, 0, len(raw))
	for _, value := range raw {
		state := syncengine.State(value)
		switch state {
		case syncengine.StateQueued, syncengine.StateRunning, syncengine.StateDone,
			syncengine.StateFailed, syncengine.StateCancelled:
			out = append(out, state)
		default:
			failProblem(w, r, codeInvalidRequest,
				"Unknown job state "+value+": use queued, running, done, failed or cancelled.")
			return nil, false
		}
	}
	return out, true
}

// matchesJobFilter reports whether a job passes the state and kind filters.
func matchesJobFilter(job syncengine.Job, states []syncengine.State, kinds []string) bool {
	if len(states) > 0 {
		found := false
		for _, state := range states {
			if job.State == state {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if len(kinds) == 0 {
		return true
	}
	for _, kind := range kinds {
		if string(job.Kind) == kind {
			return true
		}
	}
	return false
}

// handleSyncJobGet serves GET /api/v1/sync/jobs/{id}.
func (s *Server) handleSyncJobGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, ok := s.sync.engine.Job(id)
	if !ok {
		failProblem(w, r, codeSyncJobNotFound, "No job of this queue is called "+id+".")
		return
	}
	writeJSON(w, r, http.StatusOK, jobView(job, s.deadLettered(id)))
}

// deadLettered reports whether a job is in the dead-letter list.
func (s *Server) deadLettered(id string) bool {
	for _, job := range s.sync.engine.DeadLetter() {
		if job.ID == id {
			return true
		}
	}
	return false
}

// handleSyncJobRetry serves POST /api/v1/sync/jobs/{id}/retry.
//
// A failed job goes back to the queue with a fresh attempt budget and keeps its
// id, so a client watching it sees the same row come back to life. A cancelled
// job cannot: the engine's state machine has no edge out of `cancelled`, by
// design, so it is re-queued as a new job with the same kind, key and payload
// and the answer names the new id.
func (s *Server) handleSyncJobRetry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, ok := s.sync.engine.Job(id)
	if !ok {
		failProblem(w, r, codeSyncJobNotFound, "No job of this queue is called "+id+".")
		return
	}
	switch job.State {
	case syncengine.StateFailed:
		if err := s.sync.engine.RetryDeadLetter(id); err != nil {
			s.failSyncEngine(w, r, job, err)
			return
		}
	case syncengine.StateCancelled:
		fresh, err := s.sync.engine.Enqueue(r.Context(), syncengine.Request{
			Kind: job.Kind, Key: job.Key, Payload: job.Payload,
		})
		if err != nil {
			s.failSyncEngine(w, r, job, err)
			return
		}
		writeJSON(w, r, http.StatusOK, jobView(fresh, false))
		return
	default:
		failProblem(w, r, codeSyncJobNotRetryable,
			"Job "+id+" is "+string(job.State)+": only a failed or cancelled job can be retried.")
		return
	}
	retried, _ := s.sync.engine.Job(id)
	writeJSON(w, r, http.StatusOK, jobView(retried, false))
}

// handleSyncJobCancel serves POST /api/v1/sync/jobs/{id}/cancel. A running job
// has the context of its batch cancelled; a queued one simply leaves the queue.
func (s *Server) handleSyncJobCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job, ok := s.sync.engine.Job(id)
	if !ok {
		failProblem(w, r, codeSyncJobNotFound, "No job of this queue is called "+id+".")
		return
	}
	if job.State != syncengine.StateQueued && job.State != syncengine.StateRunning {
		failProblem(w, r, codeSyncJobNotRetryable,
			"Job "+id+" is "+string(job.State)+": only a queued or running job can be cancelled.")
		return
	}
	if !s.sync.engine.Cancel(id) {
		failProblem(w, r, codeSyncJobNotRetryable,
			"Job "+id+" finished before it could be cancelled.")
		return
	}
	cancelled, _ := s.sync.engine.Job(id)
	writeJSON(w, r, http.StatusOK, jobView(cancelled, false))
}

// failSyncEngine maps an engine error onto the problem catalog. The engine
// redacts credentials before it records anything, so its messages are safe to
// render; a cause this layer does not recognize is still reported as an
// internal failure rather than echoed.
func (s *Server) failSyncEngine(w http.ResponseWriter, r *http.Request, job syncengine.Job, err error) {
	switch {
	case errors.Is(err, syncengine.ErrClosed), errors.Is(err, syncengine.ErrNotStarted):
		failProblem(w, r, codeSyncEngineNotRunning,
			"The background job engine is not running on this companion.")
	case errors.Is(err, syncengine.ErrUnknownJob):
		failProblem(w, r, codeSyncJobNotFound, "No job of this queue is called "+job.ID+".")
	case errors.Is(err, syncengine.ErrNoHandler):
		failProblem(w, r, codeSyncJobNotRetryable,
			"Nothing in this build handles "+string(job.Kind)+" jobs, so it cannot be run again.")
	case errors.Is(err, syncengine.ErrNotRetryable):
		failProblem(w, r, codeSyncJobNotRetryable,
			"Job "+job.ID+" is "+string(job.State)+" and cannot be retried.")
	default:
		s.log.Warn("sync engine request failed", "job", job.ID, "error", err)
		failProblem(w, r, codeInternal, "The background job engine refused the request.")
	}
}
