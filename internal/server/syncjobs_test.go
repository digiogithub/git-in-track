package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/syncengine"
)

// syncJobsBody is the documented shape of GET /api/v1/sync/jobs.
type syncJobsBody struct {
	Jobs []struct {
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		Key         string `json:"key"`
		State       string `json:"state"`
		Attempts    int    `json:"attempts"`
		NextAttempt string `json:"nextAttempt"`
		LastError   *struct {
			Attempt int    `json:"attempt"`
			Class   string `json:"class"`
			Message string `json:"message"`
		} `json:"lastError"`
		DeadLetter bool `json:"deadLetter"`
		// Payload must never appear; the field exists here only so that a
		// regression that adds one fails this test.
		Payload any `json:"payload"`
	} `json:"jobs"`
	NextCursor string `json:"nextCursor"`
	Total      int    `json:"total"`
	Counts     struct {
		Queued    int `json:"queued"`
		Running   int `json:"running"`
		Done      int `json:"done"`
		Failed    int `json:"failed"`
		Cancelled int `json:"cancelled"`
	} `json:"counts"`
	DeadLetter int  `json:"deadLetter"`
	Engine     bool `json:"engine"`
}

// syncSettingsBody is the documented shape of GET|PATCH /api/v1/sync/settings.
type syncSettingsBody struct {
	PullStrategy string `json:"pullStrategy"`
	Engine       struct {
		Workers        int     `json:"workers"`
		BatchSize      int     `json:"batchSize"`
		Rate           float64 `json:"rate"`
		MaxAttempts    int     `json:"maxAttempts"`
		RetentionHours float64 `json:"retentionHours"`
		Running        bool    `json:"running"`
	} `json:"engine"`
	Persisted bool `json:"persisted"`
}

// queuedJobsServer registers a handler but never starts the engine, so every
// job it enqueues stays queued and the listing is deterministic.
func queuedJobsServer(t *testing.T, jobs int) (*Server, []string) {
	t.Helper()

	s := newEngineServer(t, nil)
	if err := s.RegisterSyncHandler(testJobKind, syncengine.HandlerFunc(
		func(context.Context, []syncengine.Job) error { return nil },
	)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := s.RegisterSyncHandler("other.job", syncengine.HandlerFunc(
		func(context.Context, []syncengine.Job) error { return nil },
	)); err != nil {
		t.Fatalf("register: %v", err)
	}
	ids := make([]string, 0, jobs)
	for i := 0; i < jobs; i++ {
		kind := testJobKind
		if i%2 == 1 {
			kind = "other.job"
		}
		job, err := s.sync.engine.Enqueue(t.Context(), syncengine.Request{Kind: kind, Key: fmt.Sprintf("k%d", i)})
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		ids = append(ids, job.ID)
	}
	t.Cleanup(func() { _ = s.sync.close(context.WithoutCancel(t.Context())) })
	return s, ids
}

func TestSyncJobListing(t *testing.T) {
	t.Parallel()

	s, ids := queuedJobsServer(t, 6)

	t.Run("lists the whole queue with its counts", func(t *testing.T) {
		var body syncJobsBody
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs"})
		decode(t, rec, http.StatusOK, &body)
		if body.Total != 6 || len(body.Jobs) != 6 {
			t.Fatalf("jobs = %d, total = %d, want 6", len(body.Jobs), body.Total)
		}
		if body.Counts.Queued != 6 {
			t.Errorf("counts = %+v, want six queued", body.Counts)
		}
		if rec.Header().Get("X-Total-Count") != "6" {
			t.Errorf("X-Total-Count = %q", rec.Header().Get("X-Total-Count"))
		}
		for _, job := range body.Jobs {
			if job.Payload != nil {
				t.Fatalf("a job payload reached the API: %+v", job)
			}
		}
	})

	t.Run("filters by kind and by state", func(t *testing.T) {
		var body syncJobsBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs?kind=other.job"}),
			http.StatusOK, &body)
		if body.Total != 3 {
			t.Fatalf("kind filter = %d jobs, want 3", body.Total)
		}
		var running syncJobsBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs?state=running"}),
			http.StatusOK, &running)
		if running.Total != 0 {
			t.Fatalf("state filter = %d jobs, want none running", running.Total)
		}
	})

	t.Run("an unknown state is refused rather than matching nothing", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs?state=sleeping"}),
			http.StatusBadRequest, &doc)
		if doc.Code != codeInvalidRequest {
			t.Fatalf("code = %q", doc.Code)
		}
	})

	t.Run("pages with a cursor", func(t *testing.T) {
		var first syncJobsBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs?limit=4"}),
			http.StatusOK, &first)
		if len(first.Jobs) != 4 || first.NextCursor == "" {
			t.Fatalf("first page = %d jobs, cursor %q", len(first.Jobs), first.NextCursor)
		}
		var second syncJobsBody
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/sync/jobs?limit=4&cursor=" + first.NextCursor,
		}), http.StatusOK, &second)
		if len(second.Jobs) != 2 || second.NextCursor != "" {
			t.Fatalf("second page = %d jobs, cursor %q", len(second.Jobs), second.NextCursor)
		}
		if second.Jobs[0].ID == first.Jobs[0].ID {
			t.Fatal("the second page repeats the first")
		}
	})

	t.Run("reads one job", func(t *testing.T) {
		var job struct {
			ID    string `json:"id"`
			State string `json:"state"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs/" + ids[0]}),
			http.StatusOK, &job)
		if job.ID != ids[0] || job.State != "queued" {
			t.Fatalf("job = %+v", job)
		}
	})

	t.Run("an unknown id is not found", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs/job_nope"}),
			http.StatusNotFound, &doc)
		if doc.Code != codeSyncJobNotFound {
			t.Fatalf("code = %q, want %q", doc.Code, codeSyncJobNotFound)
		}
	})
}

func TestSyncJobCancel(t *testing.T) {
	t.Parallel()

	s, ids := queuedJobsServer(t, 2)
	client := newHubClient()
	client.subscribe(syncJobTopics())
	s.hub.register(client)

	var cancelled struct {
		State string `json:"state"`
	}
	decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/sync/jobs/" + ids[0] + "/cancel"}),
		http.StatusOK, &cancelled)
	if cancelled.State != "cancelled" {
		t.Fatalf("state = %q, want cancelled", cancelled.State)
	}
	select {
	case ev := <-client.events:
		if ev.Type != eventSyncJobDone {
			t.Fatalf("cancel published %q", ev.Type)
		}
	default:
		t.Fatal("canceling a job published nothing")
	}

	t.Run("a cancelled job cannot be canceled again", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/sync/jobs/" + ids[0] + "/cancel"}),
			http.StatusConflict, &doc)
		if doc.Code != codeSyncJobNotRetryable {
			t.Fatalf("code = %q", doc.Code)
		}
		if !strings.Contains(doc.Detail, "cancelled") {
			t.Fatalf("the problem does not name the current state: %q", doc.Detail)
		}
	})

	t.Run("an unknown job is not found", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/sync/jobs/nope/cancel"}),
			http.StatusNotFound, &doc)
		if doc.Code != codeSyncJobNotFound {
			t.Fatalf("code = %q", doc.Code)
		}
	})

	t.Run("a cancelled job is re-queued as a new one", func(t *testing.T) {
		var fresh struct {
			ID    string `json:"id"`
			State string `json:"state"`
		}
		decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/sync/jobs/" + ids[0] + "/retry"}),
			http.StatusOK, &fresh)
		if fresh.State != "queued" {
			t.Fatalf("state = %q, want queued", fresh.State)
		}
		if fresh.ID == ids[0] {
			t.Fatal("the engine has no edge out of cancelled: the retry must be a new job")
		}
	})
}

func TestSyncJobRetryFromTheDeadLetter(t *testing.T) {
	t.Parallel()

	s := newEngineServer(t, func(o *Options) { o.SyncEngine.MaxAttempts = 1 })
	failed := make(chan struct{}, 4)
	startEngine(t, s, func([]syncengine.Job) error {
		failed <- struct{}{}
		return fmt.Errorf("%w: the remote refused", syncengine.ErrTerminal)
	})
	job, err := s.sync.engine.Enqueue(t.Context(), syncengine.Request{Kind: testJobKind})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitForJobState(t, s, job.ID, syncengine.StateFailed)

	var body syncJobsBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/jobs?state=failed"}),
		http.StatusOK, &body)
	if body.Total != 1 || body.Jobs[0].LastError == nil {
		t.Fatalf("failed listing = %+v", body)
	}
	if body.Jobs[0].LastError.Message == "" || !body.Jobs[0].DeadLetter {
		t.Fatalf("a failed job carries its redacted error and its dead-letter flag: %+v", body.Jobs[0])
	}

	client := newHubClient()
	client.subscribe(syncJobTopics())
	s.hub.register(client)

	var retried struct {
		ID string `json:"id"`
	}
	decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/sync/jobs/" + job.ID + "/retry"}),
		http.StatusOK, &retried)
	if retried.ID != job.ID {
		t.Fatalf("a dead-lettered job keeps its id on retry: %q", retried.ID)
	}
	// The retry re-announces the job as queued with a fresh attempt budget; the
	// engine then runs it again at once, which is why the assertion is on the
	// event rather than on a second read of a moving target.
	select {
	case ev := <-client.events:
		if ev.Type != eventSyncJobQueued {
			t.Fatalf("the retry published %q, want %q", ev.Type, eventSyncJobQueued)
		}
		data, ok := ev.Data.(syncJobEventData)
		if !ok || data.ID != job.ID || data.Attempt != 0 {
			t.Fatalf("payload = %+v, want a fresh budget for %s", ev.Data, job.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the retry published nothing")
	}
}

func TestSyncJobRetryRefusesAJobThatIsNotFinished(t *testing.T) {
	t.Parallel()

	s, ids := queuedJobsServer(t, 1)
	var doc problemBody
	decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/sync/jobs/" + ids[0] + "/retry"}),
		http.StatusConflict, &doc)
	if doc.Code != codeSyncJobNotRetryable || !strings.Contains(doc.Detail, "queued") {
		t.Fatalf("problem = %+v, want a conflict naming the current state", doc)
	}
}

func TestSyncSettingsExposeTheEngine(t *testing.T) {
	t.Parallel()

	s := newEngineServer(t, nil)
	startEngine(t, s, func([]syncengine.Job) error { return nil })

	var read syncSettingsBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sync/settings"}), http.StatusOK, &read)
	if read.Engine.Workers != DefaultSyncWorkers || read.Engine.BatchSize != DefaultSyncBatchSize {
		t.Fatalf("engine = %+v, want the shipped defaults", read.Engine)
	}
	if !read.Engine.Running {
		t.Error("the engine is started but reports itself as stopped")
	}

	t.Run("a patch reaches the running engine", func(t *testing.T) {
		var patched syncSettingsBody
		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/sync/settings",
			body: map[string]any{"workers": 5, "rate": 25.0, "batchSize": 7},
		}), http.StatusOK, &patched)
		if patched.Engine.Workers != 5 || patched.Engine.Rate != 25 || patched.Engine.BatchSize != 7 {
			t.Fatalf("engine = %+v", patched.Engine)
		}
		// The proof that it is not only recorded: the engine's own snapshot,
		// which is what the workers and the limiter actually read, moved too.
		snapshot := s.sync.engine.Snapshot()
		if snapshot.Workers != 5 || snapshot.BatchSize != 7 || snapshot.Rate != 25 {
			t.Fatalf("the running engine reports %d workers, batch %d, rate %g",
				snapshot.Workers, snapshot.BatchSize, snapshot.Rate)
		}
	})

	t.Run("an out-of-range value is refused", func(t *testing.T) {
		for _, body := range []map[string]any{
			{"workers": 0}, {"workers": 10000}, {"batchSize": 0}, {"maxAttempts": 99}, {"rate": 0},
		} {
			var doc problemBody
			decode(t, send(t, s, request{method: http.MethodPatch, target: "/api/v1/sync/settings", body: body}),
				http.StatusBadRequest, &doc)
			if doc.Code != codeInvalidRequest {
				t.Fatalf("%v: code = %q", body, doc.Code)
			}
		}
	})

	t.Run("persisted is false without a configuration file", func(t *testing.T) {
		var patched syncSettingsBody
		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/sync/settings", body: map[string]any{"workers": 3},
		}), http.StatusOK, &patched)
		if patched.Persisted {
			t.Fatal("a server with no configuration path must not claim the change was persisted")
		}
	})
}

// waitForJobState blocks until a job reaches a state, or the test times out.
func waitForJobState(t *testing.T, s *Server, id string, want syncengine.State) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := s.sync.engine.Job(id); ok && job.State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %s", id, want)
}
