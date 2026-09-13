package youtrack

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// TestLimiterSpreadsConcurrentCallers proves the limiter is shared: N
// goroutines reserving a slot at once are spread over N slots, one interval
// apart, whatever order they arrive in. The clock is frozen, so the total delay
// the callers wait is 0 + i + 2i + … + (N-1)i regardless of the interleaving.
func TestLimiterSpreadsConcurrentCallers(t *testing.T) {
	const (
		rate    = 5.0
		callers = 10
	)
	clock := newFakeClock()
	limiter := newLimiter(rate, clock.Now, clock.Sleep)

	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.wait(context.Background()); err != nil {
				t.Errorf("wait: %v", err)
			}
		}()
	}
	wg.Wait()

	interval := time.Duration(float64(time.Second) / rate)
	var want time.Duration
	for i := 0; i < callers; i++ {
		want += time.Duration(i) * interval
	}
	if got := clock.totalSlept(); got != want {
		t.Fatalf("total wait = %v, want %v", got, want)
	}
	if got := len(clock.slept()); got != callers-1 {
		t.Fatalf("%d callers waited, want %d (the first slot is free)", got, callers-1)
	}
}

func TestLimiterDisabledWhenRateNotPositive(t *testing.T) {
	for _, rate := range []float64{0, -1} {
		clock := newFakeClock()
		limiter := newLimiter(rate, clock.Now, clock.Sleep)
		for i := 0; i < 5; i++ {
			if err := limiter.wait(context.Background()); err != nil {
				t.Fatalf("wait: %v", err)
			}
		}
		if got := clock.totalSlept(); got != 0 {
			t.Fatalf("rate %g waited %v, want no wait", rate, got)
		}
	}
}

func TestLimiterHonoursContextCancellation(t *testing.T) {
	clock := newFakeClock()
	limiter := newLimiter(1, clock.Now, clock.Sleep)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := limiter.wait(ctx); err == nil {
		t.Fatal("want the context error")
	}
}

// TestClientThrottlesRequests wires the limiter through the client and checks
// that the default rate applies when Options leaves Rate unset.
func TestClientThrottlesRequests(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "me.json")
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, func(o *Options) { o.Rate = 0 })

	for i := 0; i < 3; i++ {
		if _, err := client.Me(context.Background()); err != nil {
			t.Fatalf("Me: %v", err)
		}
	}
	if rec.count() != 3 {
		t.Fatalf("got %d requests, want 3", rec.count())
	}
	interval := time.Duration(float64(time.Second) / DefaultRate)
	if got, want := clock.totalSlept(), interval+2*interval; got != want {
		t.Fatalf("total wait = %v, want %v", got, want)
	}
}
