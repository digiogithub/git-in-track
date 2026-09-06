package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestQuickTunnelEndToEnd starts a real quick tunnel against trycloudflare.com,
// fetches the public URL and checks the request reaches a local listener.
//
// It is NOT part of the normal test run: CI must never depend on
// trycloudflare.com being reachable or willing to hand out a tunnel. Run it
// deliberately with:
//
//	GINTRACK_TUNNEL_E2E=1 go test ./internal/tunnel/ -run EndToEnd -v
//
// It is skipped under -short and whenever GINTRACK_TUNNEL_E2E is not "1".
func TestQuickTunnelEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("this test reaches trycloudflare.com; skipped under -short")
	}
	if os.Getenv("GINTRACK_TUNNEL_E2E") != "1" {
		t.Skip("set GINTRACK_TUNNEL_E2E=1 to run the live Cloudflare tunnel test")
	}
	if raceEnabled {
		// Not a defect of this package: cloudflared's own DNS resolver races on
		// itself. ingress/origins.(*resolver).peekDial writes r.network and
		// r.address unsynchronized, and Go's resolver dials several nameservers
		// concurrently, so the detector fires inside a dependency we do not own
		// as soon as a real tunnel resolves a name.
		t.Skip("cloudflared's DNS resolver trips the race detector; run this test without -race")
	}

	var listenConfig net.ListenConfig
	listener, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	hosts := make(chan string, 4)
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case hosts <- r.Host:
			default:
			}
			fmt.Fprint(w, "gintrack origin")
		}),
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	m := NewManager(Options{
		Origin:  listener.Addr().String(),
		Version: "0.0.0-e2e",
		Logger:  discardLogger(),
	})

	status, err := m.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Logf("public URL: %s", status.URL)
	t.Cleanup(func() {
		// Not t.Context(): the testing package cancels it just before cleanups
		// run, and Stop would report that cancellation instead of waiting for
		// the tunnel to actually finish shutting down.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.Stop(ctx); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	body := probe(t, status.URL, 90*time.Second)
	if body != "gintrack origin" {
		t.Fatalf("body = %q, want the origin's response", body)
	}

	select {
	case host := <-hosts:
		// The origin sees the public hostname in Host, not the local address.
		t.Logf("origin saw Host=%q", host)
	default:
		t.Fatal("the origin never saw a request")
	}

	// Start, Stop, Start again: the toggle the UI exposes, live.
	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	second, err := m.Start(t.Context())
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if second.URL == status.URL {
		t.Fatalf("second URL = %q, want a freshly provisioned hostname", second.URL)
	}
	if got := probe(t, second.URL, 90*time.Second); got != "gintrack origin" {
		t.Fatalf("second body = %q, want the origin's response", got)
	}
}

// probe polls url until it answers 200 or the budget runs out.
func probe(t *testing.T, url string, budget time.Duration) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(budget)
	var last error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("build probe request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			time.Sleep(2 * time.Second)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil && resp.StatusCode == http.StatusOK {
			return string(body)
		}
		last = fmt.Errorf("status %d", resp.StatusCode)
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("the tunnel never became reachable within %v: %v", budget, last)
	return ""
}
