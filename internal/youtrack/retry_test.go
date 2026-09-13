package youtrack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		header string
		want   time.Duration
		ok     bool
	}{
		{"absent", "", 0, false},
		{"seconds", "2", 2 * time.Second, true},
		{"seconds padded", "  30 ", 30 * time.Second, true},
		{"negative seconds clamp to zero", "-5", 0, true},
		{"http date in the future", now.Add(90 * time.Second).UTC().Format(http.TimeFormat), 90 * time.Second, true},
		{"http date in the past clamps to zero", now.Add(-time.Hour).UTC().Format(http.TimeFormat), 0, true},
		{"nonsense", "soon", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.header, now)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got (%v, %v), want (%v, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestRetryHonoursRetryAfter covers both header forms end to end: the client
// must wait exactly what the server asked for, and then succeed.
func TestRetryHonoursRetryAfter(t *testing.T) {
	clockNow := newFakeClock().Now()
	tests := []struct {
		name   string
		header func() string
		want   time.Duration
	}{
		{"delta seconds", func() string { return "2" }, 2 * time.Second},
		{"http date", func() string { return clockNow.Add(3 * time.Second).Format(http.TimeFormat) }, 3 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Retry-After", tc.header())
					// A body on the 429 that must be drained, not leaked.
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, "slow down")
					return
				}
				writeJSON(t, w, "me.json")
			})
			clock := newFakeClock()
			client := newTestClient(t, srv.URL, clock, nil)

			user, err := client.Me(context.Background())
			if err != nil {
				t.Fatalf("Me: %v", err)
			}
			if user.Login != "jose" {
				t.Fatalf("login = %q", user.Login)
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("%d attempts, want 2", got)
			}
			if got := clock.slept(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("waits = %v, want one wait of %v", got, tc.want)
			}
		})
	}
}

// TestRetryBackoffWithoutRetryAfter pins the exponential schedule: the base
// doubles per attempt, and the jitter seam is pinned to zero here.
func TestRetryBackoffWithoutRetryAfter(t *testing.T) {
	var calls atomic.Int32
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusBadGateway)
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, func(o *Options) { o.MaxRetries = 3 })

	_, err := client.Me(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway {
		t.Fatalf("want a 502 APIError, got %v", err)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("%d attempts, want 4 (the first plus three retries)", got)
	}
	want := []time.Duration{DefaultRetryBase, 2 * DefaultRetryBase, 4 * DefaultRetryBase}
	got := clock.slept()
	if len(got) != len(want) {
		t.Fatalf("waits = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wait %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestJitterIsAdded proves the jitter seam is really wired into the backoff.
func TestJitterIsAdded(t *testing.T) {
	var calls atomic.Int32
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "boom", http.StatusServiceUnavailable)
			return
		}
		writeJSON(t, w, "me.json")
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, func(o *Options) { o.Jitter = func() float64 { return 0.5 } })

	if _, err := client.Me(context.Background()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	want := DefaultRetryBase + DefaultRetryBase/2
	if got := clock.slept(); len(got) != 1 || got[0] != want {
		t.Fatalf("waits = %v, want one wait of %v", got, want)
	}
}

// TestNonRetryableStatusFailsImmediately pins that a 400 is a caller mistake:
// one attempt, no waiting.
func TestNonRetryableStatusFailsImmediately(t *testing.T) {
	var calls atomic.Int32
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad query", http.StatusBadRequest)
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, nil)

	_, err := client.Me(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("want a 400 APIError, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("%d attempts, want exactly 1", got)
	}
	if got := clock.totalSlept(); got != 0 {
		t.Fatalf("waited %v, want no wait", got)
	}
}

// TestRateLimitedAfterBudgetIsTyped proves that a 429 that outlives the retry
// budget still reaches the caller as ErrRateLimited rather than as a bare
// APIError, because the settings UI words that case differently.
func TestRateLimitedAfterBudgetIsTyped(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many", http.StatusTooManyRequests)
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, func(o *Options) { o.MaxRetries = 2 })

	_, err := client.Me(context.Background())
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
	if rec.count() != 3 {
		t.Fatalf("%d attempts, want 3", rec.count())
	}
}

// TestTransportErrorsAreRetried covers the case where no response arrives at
// all. The failing transport is injected, so nothing touches the network.
func TestTransportErrorsAreRetried(t *testing.T) {
	var calls atomic.Int32
	failing := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("dial tcp: connection refused while sending %s", r.Header.Get("Authorization"))
	})}
	clock := newFakeClock()
	client := newTestClient(t, "https://yt.example.com/youtrack", clock, func(o *Options) {
		o.HTTPClient = failing
		o.MaxRetries = 2
	})

	_, err := client.Me(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("%d attempts, want 3", got)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("the transport error leaked the token: %v", err)
	}
	if len(clock.slept()) != 2 {
		t.Fatalf("waits = %v, want 2", clock.slept())
	}
}

// TestRetryDrainsBodies asserts that a retried response body is read to
// completion and closed, which is what lets the underlying connection be
// reused: the server counts the TCP connections it accepts, and a leaked body
// would force a new one per attempt.
func TestRetryDrainsBodies(t *testing.T) {
	var calls atomic.Int32
	var conns atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusInternalServerError)
			// Deliberately large: a missing drain would leave this unread.
			for i := 0; i < 1000; i++ {
				_, _ = io.WriteString(w, "padding padding padding\n")
			}
			return
		}
		writeJSON(t, w, "me.json")
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, nil)

	if _, err := client.Me(context.Background()); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("%d attempts, want 3", got)
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("%d connections opened for 3 attempts, want 1 reused connection", got)
	}
}

// TestContextCancellationStopsRetrying proves a cancelled context aborts the
// loop instead of burning the whole budget.
func TestContextCancellationStopsRetrying(t *testing.T) {
	var calls atomic.Int32
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "boom", http.StatusServiceUnavailable)
	})
	ctx, cancel := context.WithCancel(context.Background())
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, func(o *Options) {
		o.Sleep = func(context.Context, time.Duration) error {
			cancel()
			return ctxErr(context.Canceled)
		}
	})

	_, err := client.Me(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("%d attempts, want 1", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
