package server

import (
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/syncengine"
)

// The sync job event stream, story GIT-US-0074 (docs/07-cli-and-api.md §5.6).
//
// internal/syncengine never learns what a WebSocket is: it calls Options.OnJob
// with a copy of the job after every state change, off its own lock, and this
// file is the only thing that turns those calls into hub topics. That is what
// keeps the engine unit-testable against a fake handler, and it is why nothing
// in internal/syncengine imports internal/server.
//
// Five topics are published:
//
//	sync.job.queued    a job entered the queue for the first time
//	sync.job.started   a worker picked it up
//	sync.job.progress  a coalesced count of how far a batch has got
//	sync.job.done      it succeeded, or was cancelled (`state` says which)
//	sync.job.failed    it exhausted its attempts or hit a terminal error
//
// Back-pressure is the hub's: a client holds a 256-event buffer and is
// disconnected with `stream.overflow` when it fills up (hub.go). A long import
// must therefore not narrate itself frame by frame, so `sync.job.progress` is
// throttled per coalescing group to syncProgressInterval and carries the
// running counts instead. A terminal event is never throttled: the last thing a
// client hears about a job is always the truth about it.

// syncProgressInterval is the smallest gap between two `sync.job.progress`
// events for one coalescing group. Two a second is fast enough for a progress
// bar and slow enough that a thousand-job import cannot flood a client.
const syncProgressInterval = 500 * time.Millisecond

// syncJobEventData is the payload of every `sync.job.*` event.
//
// It carries bookkeeping only — ids, kinds, counts, a redacted message — for
// the same reason the journal does: everything here reaches every connected
// browser, and a payload is the easiest place to leak a credential from. The
// job's payload is deliberately absent.
type syncJobEventData struct {
	// ID is the engine's job id, stable across a restart.
	ID string `json:"id"`
	// Kind is the job family, which is what selects a handler.
	Kind string `json:"kind"`
	// Key is the coalescing key: jobs sharing a kind and a key run together.
	Key string `json:"key,omitempty"`
	// State is the engine state the job is in now.
	State string `json:"state"`
	// Attempt is how many handler calls this job has taken part in.
	Attempt int `json:"attempt"`
	// Processed and Total count the coalescing group this job belongs to: how
	// many of its jobs have finished, and how many there are. They are what a
	// progress bar renders.
	Processed int `json:"processed"`
	Total     int `json:"total"`
	// Error is the redacted message of the last failure, and ErrorClass how it
	// was classified. Both are empty unless the job has failed.
	Error      string `json:"error,omitempty"`
	ErrorClass string `json:"errorClass,omitempty"`
}

// The `sync.job.*` topics. They are one namespace so that a client can
// subscribe to the five of them by name in one frame.
const (
	eventSyncJobQueued   = "sync.job.queued"
	eventSyncJobStarted  = "sync.job.started"
	eventSyncJobProgress = "sync.job.progress"
	eventSyncJobDone     = "sync.job.done"
	eventSyncJobFailed   = "sync.job.failed"
)

// syncJobTopics lists the five topics, for the documentation and the tests.
func syncJobTopics() []string {
	return []string{
		eventSyncJobQueued, eventSyncJobStarted, eventSyncJobProgress,
		eventSyncJobDone, eventSyncJobFailed,
	}
}

// syncGroup is how far one coalescing group — one kind and one key, which is
// what the engine hands to a handler as a batch — has got.
type syncGroup struct {
	// seen and finished are job ids rather than counters so that the tally
	// survives a job being reported twice, which a retry does by design.
	seen     map[string]bool
	finished map[string]bool
	// lastProgress is when this group last published, for the throttle.
	lastProgress time.Time
}

// syncObserver turns engine state changes into hub events.
type syncObserver struct {
	hub *Hub
	now func() time.Time
	// interval is the progress throttle; tests shorten it.
	interval time.Duration

	mu     sync.Mutex
	groups map[string]*syncGroup
}

// newSyncObserver builds the observer the engine's OnJob points at.
func newSyncObserver(hub *Hub, now func() time.Time) *syncObserver {
	if now == nil {
		now = time.Now
	}
	return &syncObserver{hub: hub, now: now, interval: syncProgressInterval, groups: map[string]*syncGroup{}}
}

// onJob is [syncengine.Options.OnJob]. The engine calls it off its own lock,
// after every state change, with a copy of the job, one call at a time and in
// the order the changes happened — which is why it must never call back into
// the engine.
func (o *syncObserver) onJob(job syncengine.Job) {
	if o == nil || o.hub == nil {
		return
	}
	data, topic, progress, complete := o.classify(job)
	if topic != "" {
		o.hub.Publish(topic, data)
	}
	if progress {
		// A group's last progress event is published whatever the throttle
		// says: a bar that stops at 19 of 20 is worse than one extra frame.
		payload := data
		payload.State = string(job.State)
		o.hub.Publish(eventSyncJobProgress, payload)
	}
	if complete {
		o.forget(job)
	}
}

// classify folds one transition into the event to publish, the running counts
// of its group, and whether a progress event is due.
//
// It is one method because the tally and the throttle must be decided under the
// same lock: two transitions arriving from two workers at the same instant must
// not both conclude that they are the one allowed to publish.
func (o *syncObserver) classify(job syncengine.Job) (data syncJobEventData, topic string, progress, complete bool) {
	key := syncGroupKey(job)
	now := o.now()

	o.mu.Lock()
	group := o.groups[key]
	if group == nil {
		group = &syncGroup{seen: map[string]bool{}, finished: map[string]bool{}}
		o.groups[key] = group
	}
	group.seen[job.ID] = true
	terminal := isTerminalJobState(job.State)
	if terminal {
		group.finished[job.ID] = true
	}
	processed, total := len(group.finished), len(group.seen)
	complete = processed == total

	switch {
	case terminal:
		// A terminal transition always publishes its own topic; a progress
		// event goes with it while the group still has work left to do, so that
		// a batch narrates itself without every job doing so.
		progress = !complete && now.Sub(group.lastProgress) >= o.interval
	case job.State == syncengine.StateQueued && job.Attempts > 0:
		// A re-queue is a retry, which is progress rather than a new job.
		progress = now.Sub(group.lastProgress) >= o.interval
	}
	if progress || (complete && terminal) {
		group.lastProgress = now
	}
	o.mu.Unlock()

	data = syncJobEventData{
		ID: job.ID, Kind: string(job.Kind), Key: job.Key,
		State: string(job.State), Attempt: job.Attempts,
		Processed: processed, Total: total,
	}
	if job.LastError != nil {
		// Already redacted by the engine: internal/syncengine strips every
		// credential it recognizes before it records an error, so this is the
		// one place that must resist the temptation to add %v of the cause.
		data.Error = job.LastError.Message
		data.ErrorClass = string(job.LastError.Class)
	}
	topic = topicForJob(job)
	return data, topic, progress, complete && terminal
}

// forget drops a finished group's tally, so that a companion running for a week
// does not remember every import it ever ran.
func (o *syncObserver) forget(job syncengine.Job) {
	key := syncGroupKey(job)
	o.mu.Lock()
	delete(o.groups, key)
	o.mu.Unlock()
}

// syncGroupKey is the coalescing identity of a job: the pair the engine batches
// by. The separator cannot occur in either half.
func syncGroupKey(job syncengine.Job) string { return string(job.Kind) + "\x00" + job.Key }

// isTerminalJobState reports whether a job has stopped moving.
func isTerminalJobState(state syncengine.State) bool {
	switch state {
	case syncengine.StateDone, syncengine.StateFailed, syncengine.StateCancelled:
		return true
	case syncengine.StateQueued, syncengine.StateRunning:
		return false
	}
	return false
}

// topicForJob maps a state onto the topic that announces it.
//
// A cancelled job is announced on `sync.job.done`: it is a job that has stopped
// without failing, and a client watching a queue drain cares that it is over,
// not which of the two ways it ended. `state` in the payload says which.
func topicForJob(job syncengine.Job) string {
	switch job.State {
	case syncengine.StateQueued:
		if job.Attempts > 0 {
			// The retry itself is reported as progress, not as a new job.
			return ""
		}
		return eventSyncJobQueued
	case syncengine.StateRunning:
		return eventSyncJobStarted
	case syncengine.StateDone, syncengine.StateCancelled:
		return eventSyncJobDone
	case syncengine.StateFailed:
		return eventSyncJobFailed
	}
	return ""
}
