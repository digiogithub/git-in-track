package youtrack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// testToken is the permanent token every test authenticates with. Its shape
// mirrors a real one so the redaction assertions are meaningful.
const testToken = "perm:am9zZQ==.NDItMQ==.sUp3rS3cr3tV4lu3"

// fakeClock is the clock and sleep seam used by every test: it never blocks.
// Now is frozen, so the reservations a rate limiter makes are deterministic no
// matter how the goroutines interleave, and Sleep only records the delay.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	total  time.Duration
	delays []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return ctxErr(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if d > 0 {
		f.total += d
		f.delays = append(f.delays, d)
	}
	return nil
}

// slept returns the recorded delays.
func (f *fakeClock) slept() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]time.Duration, len(f.delays))
	copy(out, f.delays)
	return out
}

// totalSlept returns the sum of every recorded delay.
func (f *fakeClock) totalSlept() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}

// recorder captures the requests a test server received.
type recorder struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (r *recorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := req.Clone(req.Context())
	r.requests = append(r.requests, clone)
}

func (r *recorder) all() []*http.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*http.Request, len(r.requests))
	copy(out, r.requests)
	return out
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// newTestServer starts an httptest server that records every request and
// delegates to handler.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// newTestClient builds a client wired to srv, with the fake clock, no jitter
// and, unless the test says otherwise, no throttling.
func newTestClient(t *testing.T, baseURL string, clock *fakeClock, mutate func(*Options)) *Client {
	t.Helper()
	opts := Options{
		BaseURL:    baseURL,
		Token:      testToken,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		Rate:       -1,
		Now:        clock.Now,
		Sleep:      clock.Sleep,
		Jitter:     func() float64 { return 0 },
	}
	if mutate != nil {
		mutate(&opts)
	}
	client, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

// writeJSON replies with a fixture file from testdata.
func writeJSON(t *testing.T, w http.ResponseWriter, fixture string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixture, err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}
