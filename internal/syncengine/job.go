package syncengine

import (
	"encoding/json"
	"fmt"
	"time"
)

// Kind names a family of work. It is the key of the handler registry, so the
// caller — not the engine — decides what kinds exist.
type Kind string

// State is where a job is in its life.
type State string

// The states a job can be in. A job is created queued, a worker moves it to
// running, and it ends done, failed or cancelled.
const (
	// StateQueued means the job is waiting: in a coalescing batch, in the ready
	// queue, or scheduled for a future retry (see [Job.NextAttempt]).
	StateQueued State = "queued"
	// StateRunning means a worker is inside the handler with it.
	StateRunning State = "running"
	// StateDone means the handler reported success.
	StateDone State = "done"
	// StateFailed means the job exhausted its attempts or hit a terminal error.
	// A failed job is kept, in the dead-letter list, for inspection.
	StateFailed State = "failed"
	// StateCancelled means a caller withdrew the job.
	StateCancelled State = "cancelled"
)

// transitions is the whole state machine, in one place.
//
// Besides the main path (queued → running → done|failed|cancelled) exactly two
// re-queue edges exist, both deliberate: running → queued when a retryable
// error schedules another attempt or a batch is abandoned before it produced a
// result, and failed → queued when a user retries a dead-lettered job. Anything
// else is refused by [transition].
var transitions = map[State][]State{
	StateQueued:    {StateRunning, StateCancelled},
	StateRunning:   {StateDone, StateFailed, StateCancelled, StateQueued},
	StateDone:      nil,
	StateFailed:    {StateQueued},
	StateCancelled: nil,
}

// canTransition reports whether from → to is a legal move.
func canTransition(from, to State) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Job is one unit of background work, and the record the REST layer, the queue
// table and the journal all read.
//
// Payload is opaque to the engine: it is handed back to the handler untouched.
// Keep it small and free of content — ids, keys and options, never the body of
// an item or a page — because it is written to the journal. Credentials must
// never appear in it; the journal redacts what it recognizes, but a handler
// that needs a token reads it from the vault, not from a payload.
type Job struct {
	// ID is unique for the life of the engine, and stable across a restart.
	ID string `json:"id"`
	// Kind selects the handler.
	Kind Kind `json:"kind"`
	// Key is the coalescing key: jobs sharing a kind and a key batch together.
	// An empty key coalesces with every other empty key of the same kind.
	Key string `json:"key,omitempty"`
	// Payload is handed to the handler verbatim.
	Payload json.RawMessage `json:"payload,omitempty"`
	// State is where the job is; see the State constants.
	State State `json:"state"`
	// Attempts counts the handler calls this job has taken part in.
	Attempts int `json:"attempts"`
	// CreatedAt is when the job was enqueued, on the engine's clock.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when its state last changed.
	UpdatedAt time.Time `json:"updatedAt"`
	// NextAttempt is when a retry is due. Zero unless a retry is scheduled.
	NextAttempt time.Time `json:"nextAttempt,omitzero"`
	// LastError is the most recent failure, redacted. Nil while the job has
	// never failed.
	LastError *ErrorRecord `json:"lastError,omitempty"`
}

// ErrorRecord is why a job failed once, in a form safe to show and to store.
type ErrorRecord struct {
	// Attempt is the attempt number this error came from, 1-based.
	Attempt int `json:"attempt"`
	// Class is how the error was classified.
	Class ErrorClass `json:"class"`
	// Message is the error text with every credential it recognized redacted.
	Message string `json:"message"`
	// At is when the failure was recorded.
	At time.Time `json:"at"`
	// RetryAfter is the delay the error itself asked for, zero when it asked
	// for none.
	RetryAfter time.Duration `json:"retryAfter,omitempty"`
}

// Counts reports how many jobs the engine is holding in each state. It is what
// a status bar renders.
type Counts struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Done      int `json:"done"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
}

// Total is the number of jobs the engine currently remembers.
func (c Counts) Total() int {
	return c.Queued + c.Running + c.Done + c.Failed + c.Cancelled
}

// Snapshot is the engine's whole observable state at one instant: what the REST
// layer of a later story serves and what a WebSocket event carries.
type Snapshot struct {
	// Jobs is every job the engine remembers, oldest first.
	Jobs []Job `json:"jobs"`
	// DeadLetter is the jobs that gave up, oldest first. They are also present
	// in Jobs.
	DeadLetter []Job `json:"deadLetter"`
	// Counts summarizes Jobs by state.
	Counts Counts `json:"counts"`
	// Workers, BatchSize and Rate are the settings in force right now.
	Workers   int     `json:"workers"`
	BatchSize int     `json:"batchSize"`
	Rate      float64 `json:"rate"`
	// DeadLetterEvicted counts dead-letter entries dropped because the list was
	// full. It only ever grows.
	DeadLetterEvicted int `json:"deadLetterEvicted"`
	// Running is the number of batches inside a handler right now.
	Running int `json:"running"`
	// At is when the snapshot was taken.
	At time.Time `json:"at"`
}

// jobRecord is the engine's private bookkeeping around a Job.
type jobRecord struct {
	job Job
	// gen stamps the job for result fencing. Canceling or re-queueing a job
	// bumps it, so the worker that is still inside the handler with the old
	// stamp finds its result superseded and drops it instead of overwriting a
	// newer decision — the idiom internal/tunnel/tunnel.go uses for a
	// supervised worker.
	gen uint64
	// cancel signals the context of the batch the job is running in. Nil unless
	// the job is running.
	cancel func()
	// retry is the armed timer that will return the job to the queue, nil
	// unless a retry is scheduled.
	retry Timer
	// batchKey is the coalescing key of the batch the job is waiting in, empty
	// once it has left the pending map.
	batchKey string
	// dead records that the job is in the dead-letter list.
	dead bool
}

// transition moves a job to a new state, refusing anything the state machine
// does not allow. Every state change in the engine goes through it.
func transition(r *jobRecord, to State, now time.Time) error {
	if r.job.State == to {
		return nil
	}
	if !canTransition(r.job.State, to) {
		return fmt.Errorf("%w: %s -> %s", ErrBadTransition, r.job.State, to)
	}
	r.job.State = to
	r.job.UpdatedAt = now
	if to != StateQueued {
		r.job.NextAttempt = time.Time{}
	}
	return nil
}

// clone copies a job so a caller can never reach the engine's own record.
func (j Job) clone() Job {
	out := j
	if j.Payload != nil {
		out.Payload = append(json.RawMessage(nil), j.Payload...)
	}
	if j.LastError != nil {
		rec := *j.LastError
		out.LastError = &rec
	}
	return out
}
