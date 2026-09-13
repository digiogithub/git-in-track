package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/digiogithub/git-in-track/internal/syncengine"
)

// The companion without a listener, for the one-shot commands of
// `gintrack youtrack` (GIT-US-0062, GIT-US-0079, GIT-US-0094).
//
// A command that imports issues, pushes a comment or synchronizes a
// knowledge-base page needs exactly what the companion already assembles: the
// mounted vaults, a YouTrack client per project built from the machine-local
// token, the two seams the vault reaches a tracker through, and the background
// engine that runs the queued work. Rebuilding any of that in `cmd/gintrack`
// would be a second wiring to drift out of step with the first.
//
// So the CLI builds the same [Server] and simply never binds a port. Nothing
// here duplicates a decision: it is [Server.Start] minus the listener, the
// watcher and the tunnel, plus the two accessors a command needs — dispatch one
// core method, and follow one job to its end.

// Headless is a companion process with no HTTP listener.
type Headless struct {
	srv     *Server
	stop    func(context.Context)
	started bool
}

// NewHeadless builds the companion a one-shot command drives.
//
// The options are the ordinary ones, with two differences the caller does not
// have to remember: there is no token because nothing is served, and there is
// no UI because nothing renders. A tunnel is refused outright — publishing a
// workspace is not something a batch command may do as a side effect.
func NewHeadless(opts Options) (*Headless, error) {
	opts.Token = ""
	opts.UI = nil
	opts.OpenBrowser = false
	opts.Watch = false
	opts.MCPHTTP = false
	opts.Tunnel.Enabled = false
	srv, err := New(opts)
	if err != nil {
		return nil, err
	}
	return &Headless{srv: srv}, nil
}

// Start brings up the background job engine, replaying whatever the journal
// holds, so that a queued job actually runs. It is separate from NewHeadless
// because a command that only previews never needs it.
func (h *Headless) Start(ctx context.Context) {
	if h.started {
		return
	}
	h.srv.startSyncEngine(ctx)
	h.started = true
	h.stop = func(stopCtx context.Context) { h.srv.stopSyncEngine(stopCtx) }
}

// Close drains the engine and flushes commit-on-save.
//
// The context is detached by the caller for the same reason [Server.Start]
// detaches it: a job halfway through a call to a tracker must be allowed to
// finish, and the very shutdown waiting for it must not be what cancels it.
func (h *Headless) Close(ctx context.Context) {
	if h.stop != nil {
		h.stop(ctx)
		h.stop = nil
	}
	h.srv.git.close(ctx)
}

// Projects lists the project keys this companion serves.
func (h *Headless) Projects() []string { return h.srv.youtrack.projectKeys() }

// ErrNoProject is returned when a command names a project no mounted
// repository exposes.
var ErrNoProject = errYouTrackNoProject

// Dispatch runs one core method against the repository that serves a project
// and decodes the answer into out, which may be nil.
//
// It goes through the same [vault.Vault.Dispatch] the REST API and the MCP
// server go through, so a command cannot grow a rule the other surfaces do not
// have.
func (h *Headless) Dispatch(ctx context.Context, project, method string, params, out any) error {
	m, found := h.srv.repos.forProject(project)
	if !found {
		return fmt.Errorf("%w: no mounted repository exposes project %s", ErrNoProject, project)
	}
	if !m.ready() {
		return fmt.Errorf("repository %s is not indexed: %w", m.id, m.err)
	}
	return dispatchVault(ctx, m, method, params, out)
}

// Job reports one background job as the engine currently holds it.
func (h *Headless) Job(id string) (syncengine.Job, bool) { return h.srv.sync.engine.Job(id) }

// jobPollInterval is how often WaitForJob looks again. The engine has no
// blocking wait, and a tenth of a second is far below the latency of the
// network calls a job makes, so polling costs nothing and keeps the waiting
// code a loop anybody can read.
const jobPollInterval = 100 * time.Millisecond

// WaitForJob follows a job until it is done, failed or cancelled, and returns
// it in that final state.
//
// A cancelled context stops the wait and is reported as such: the job itself
// keeps running, which is what interrupting `--wait` should mean — stop
// watching, not stop working.
func (h *Headless) WaitForJob(ctx context.Context, id string) (syncengine.Job, error) {
	ticker := time.NewTicker(jobPollInterval)
	defer ticker.Stop()
	for {
		job, ok := h.srv.sync.engine.Job(id)
		if !ok {
			return syncengine.Job{}, fmt.Errorf("job %s is not in the queue", id)
		}
		switch job.State {
		case syncengine.StateDone, syncengine.StateFailed, syncengine.StateCancelled:
			return job, nil
		case syncengine.StateQueued, syncengine.StateRunning:
		}
		select {
		case <-ctx.Done():
			return job, fmt.Errorf("stopped watching job %s: %w", id, ctx.Err())
		case <-ticker.C:
		}
	}
}

// JobFailure renders why a job ended badly, empty when it did not.
func JobFailure(job syncengine.Job) string {
	if job.State == syncengine.StateCancelled {
		return "the job was cancelled"
	}
	if job.State != syncengine.StateFailed {
		return ""
	}
	if job.LastError != nil {
		return job.LastError.Message
	}
	return "the job failed without recording a reason"
}

// EncodeJobPayload is what a caller uses to read a job's own parameters back,
// which is how a command prints what the job it waited for was asked to do.
func EncodeJobPayload(job syncengine.Job, out any) error {
	if len(job.Payload) == 0 {
		return nil
	}
	var envelope youtrackJobPayload
	if err := json.Unmarshal(job.Payload, &envelope); err != nil {
		return fmt.Errorf("decode the payload of job %s: %w", job.ID, err)
	}
	if len(envelope.Params) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(envelope.Params, out); err != nil {
		return fmt.Errorf("decode the parameters of job %s: %w", job.ID, err)
	}
	return nil
}
