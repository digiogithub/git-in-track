package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/tunnel"
)

// fakeTunnel is the test seam: a TunnelDriver that records the toggles and
// reports whatever status the test wants, so that no test ever dials
// Cloudflare. It mimics the parts of internal/tunnel.Manager the server relies
// on: Start reports StateStarting with a URL, Stop is idempotent, and both fire
// the change callback outside any lock the server holds.
type fakeTunnel struct {
	url      string
	startErr error

	mu       sync.Mutex
	status   tunnel.Status
	onChange func(tunnel.Status)
	starts   int
	stops    int
}

// newFakeTunnel builds a driver that is off and has never been started.
func newFakeTunnel(url string) *fakeTunnel {
	return &fakeTunnel{url: url, status: tunnel.Status{State: tunnel.StateOff}}
}

func (f *fakeTunnel) Start(_ context.Context) (tunnel.Status, error) {
	f.mu.Lock()
	f.starts++
	if f.startErr != nil {
		err := f.startErr
		f.mu.Unlock()
		return tunnel.Status{State: tunnel.StateError, Err: err.Error()}, err
	}
	f.status = tunnel.Status{
		State: tunnel.StateStarting,
		URL:   f.url,
		Since: time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC),
	}
	f.mu.Unlock()
	f.emit()
	return f.Status(), nil
}

func (f *fakeTunnel) Stop(_ context.Context) error {
	f.mu.Lock()
	f.stops++
	f.status = tunnel.Status{State: tunnel.StateOff}
	f.mu.Unlock()
	f.emit()
	return nil
}

func (f *fakeTunnel) Status() tunnel.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeTunnel) OnChange(fn func(tunnel.Status)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onChange = fn
}

// connect moves the driver to "connected", the way a real edge connection
// would, so that a test can watch the broadcast that follows.
func (f *fakeTunnel) connect() {
	f.mu.Lock()
	f.status.State = tunnel.StateConnected
	f.status.Connections = 1
	f.mu.Unlock()
	f.emit()
}

// emit fires the callback outside the driver's own lock, which is the contract
// internal/tunnel.Manager keeps.
func (f *fakeTunnel) emit() {
	f.mu.Lock()
	fn, status := f.onChange, f.status
	f.mu.Unlock()
	if fn != nil {
		fn(status)
	}
}

// counts reports how often the driver was started and stopped.
func (f *fakeTunnel) counts() (starts, stops int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.stops
}

// newTunnelServer builds a server whose tunnel is the given fake. An empty
// token means a companion started with `--token none`.
func newTunnelServer(t *testing.T, token string, driver *fakeTunnel) *Server {
	t.Helper()

	built := 0
	s, err := New(Options{
		Token:     token,
		Version:   "0.0.1-test",
		Workspace: "test",
		Now:       func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
		NewTunnel: func(opts tunnel.Options) (TunnelDriver, error) {
			// The origin must be the address the server listens on, otherwise a
			// real tunnel would forward to nothing.
			if opts.Origin == "" {
				t.Errorf("the tunnel was built without an origin")
			}
			built++
			if built > 1 {
				t.Errorf("the driver was built %d times; one Manager is process-scoped", built)
			}
			return driver, nil
		},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

// tunnelRequest runs one call against the tunnel routes, presenting the token
// only when the server has one.
func tunnelRequest(t *testing.T, s *Server, method, token string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/tunnel", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	return rec
}

// TestTunnelStatusOnAFreshServer pins the frozen response body of docs/07: the
// web UI is written against these exact field names, and `since` is null rather
// than absent or an empty string.
func TestTunnelStatusOnAFreshServer(t *testing.T) {
	t.Parallel()

	s := newTunnelServer(t, "test-token", newFakeTunnel("https://demo.trycloudflare.com"))
	rec := tunnelRequest(t, s, http.MethodGet, "test-token")

	var body map[string]any
	decode(t, rec, http.StatusOK, &body)

	want := map[string]any{
		"supported":       true,
		"provider":        "cloudflare",
		"state":           "off",
		"url":             "",
		"connections":     float64(0),
		"since":           nil,
		"error":           "",
		"tokenConfigured": true,
	}
	if len(body) != len(want) {
		t.Fatalf("body has %d fields, want %d: %v", len(body), len(want), body)
	}
	for field, expected := range want {
		got, ok := body[field]
		if !ok {
			t.Errorf("the response has no %q field", field)
			continue
		}
		if got != expected {
			t.Errorf("%s = %v, want %v", field, got, expected)
		}
	}
}

// TestTunnelRequiresAToken is the security test: a companion started without
// authentication must refuse to publish itself, whatever the UI asks for.
func TestTunnelRequiresAToken(t *testing.T) {
	t.Parallel()

	driver := newFakeTunnel("https://demo.trycloudflare.com")
	s := newTunnelServer(t, "", driver)

	rec := tunnelRequest(t, s, http.MethodPost, "")
	var doc problem
	decode(t, rec, http.StatusConflict, &doc)

	if doc.Code != codeTunnelRequiresToken {
		t.Errorf("code = %q, want %q", doc.Code, codeTunnelRequiresToken)
	}
	if want := problemBase + "tunnel-requires-token"; doc.Type != want {
		t.Errorf("type = %q, want %q", doc.Type, want)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json; charset=utf-8" {
		t.Errorf("content type = %q", ct)
	}
	if starts, _ := driver.counts(); starts != 0 {
		t.Errorf("the tunnel was started %d times despite the refusal", starts)
	}

	// The status route still answers, and says why the toggle is refused.
	var body tunnelBody
	decode(t, tunnelRequest(t, s, http.MethodGet, ""), http.StatusOK, &body)
	if body.TokenConfigured {
		t.Error("tokenConfigured = true on a server with no token")
	}
	if body.State != string(tunnel.StateOff) {
		t.Errorf("state = %q, want off", body.State)
	}
}

// TestTunnelToggle drives the whole lifecycle through the API with the fake
// driver: enable, read back, disable, and disable again.
func TestTunnelToggle(t *testing.T) {
	t.Parallel()

	const publicURL = "https://demo.trycloudflare.com"
	driver := newFakeTunnel(publicURL)
	s := newTunnelServer(t, "test-token", driver)

	tests := []struct {
		name       string
		method     string
		want       int
		wantState  string
		wantURL    string
		wantStarts int
		wantStops  int
	}{
		{
			name:       "enabling answers 202 with the public URL",
			method:     http.MethodPost,
			want:       http.StatusAccepted,
			wantState:  string(tunnel.StateStarting),
			wantURL:    publicURL,
			wantStarts: 1,
		},
		{
			name:       "reading it back reports the same tunnel",
			method:     http.MethodGet,
			want:       http.StatusOK,
			wantState:  string(tunnel.StateStarting),
			wantURL:    publicURL,
			wantStarts: 1,
		},
		{
			name:       "disabling answers 200 and closes it",
			method:     http.MethodDelete,
			want:       http.StatusOK,
			wantState:  string(tunnel.StateOff),
			wantStarts: 1,
			wantStops:  1,
		},
		{
			name:       "disabling a closed tunnel is not an error",
			method:     http.MethodDelete,
			want:       http.StatusOK,
			wantState:  string(tunnel.StateOff),
			wantStarts: 1,
			// Stop is not called again: the server already knows it is off.
			wantStops: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body tunnelBody
			decode(t, tunnelRequest(t, s, tc.method, "test-token"), tc.want, &body)

			if body.State != tc.wantState {
				t.Errorf("state = %q, want %q", body.State, tc.wantState)
			}
			if body.URL != tc.wantURL {
				t.Errorf("url = %q, want %q", body.URL, tc.wantURL)
			}
			if !body.Supported || !body.TokenConfigured {
				t.Errorf("supported = %v, tokenConfigured = %v", body.Supported, body.TokenConfigured)
			}
			starts, stops := driver.counts()
			if starts != tc.wantStarts || stops != tc.wantStops {
				t.Errorf("starts = %d, stops = %d; want %d and %d", starts, stops, tc.wantStarts, tc.wantStops)
			}
		})
	}
}

// TestTunnelEnableReportsAFailedProvider checks that a provider that cannot be
// reached is reported as a problem document rather than a half-open tunnel.
func TestTunnelEnableReportsAFailedProvider(t *testing.T) {
	t.Parallel()

	driver := newFakeTunnel("https://demo.trycloudflare.com")
	driver.startErr = errors.New("provision quick tunnel: broker reported failure")
	s := newTunnelServer(t, "test-token", driver)

	var doc problem
	decode(t, tunnelRequest(t, s, http.MethodPost, "test-token"), http.StatusInternalServerError, &doc)
	if doc.Code != codeTunnelFailed {
		t.Errorf("code = %q, want %q", doc.Code, codeTunnelFailed)
	}

	// The server is back to "off", so the UI offers the toggle again.
	var body tunnelBody
	decode(t, tunnelRequest(t, s, http.MethodGet, "test-token"), http.StatusOK, &body)
	if body.State != string(tunnel.StateOff) {
		t.Errorf("state = %q, want off", body.State)
	}
}

// TestTunnelChangeIsBroadcast checks that a status change reaches a subscriber
// as a `tunnel.changed` event carrying the same document the REST route serves.
func TestTunnelChangeIsBroadcast(t *testing.T) {
	t.Parallel()

	const publicURL = "https://demo.trycloudflare.com"
	driver := newFakeTunnel(publicURL)
	s := newTunnelServer(t, "test-token", driver)

	client := newHubClient()
	s.hub.register(client)
	t.Cleanup(func() { s.hub.unregister(client) })

	decode(t, tunnelRequest(t, s, http.MethodPost, "test-token"), http.StatusAccepted, nil)
	// The edge confirms a connection after the URL is known, exactly as the
	// real manager reports it.
	driver.connect()

	states := make([]string, 0, 2)
	for range 2 {
		select {
		case ev := <-client.events:
			if ev.Type != eventTunnelChanged {
				t.Fatalf("event type = %q, want %q", ev.Type, eventTunnelChanged)
			}
			body, ok := ev.Data.(tunnelBody)
			if !ok {
				t.Fatalf("event data is %T, want tunnelBody", ev.Data)
			}
			if body.URL != publicURL {
				t.Errorf("url = %q, want %q", body.URL, publicURL)
			}
			// The payload is the public document; the token is never in it.
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("encode the payload: %v", err)
			}
			if strings.Contains(string(raw), "test-token") {
				t.Fatalf("the event payload carries the bearer token: %s", raw)
			}
			states = append(states, body.State)
		case <-time.After(2 * time.Second):
			t.Fatal("no tunnel.changed event arrived")
		}
	}

	if states[0] != string(tunnel.StateStarting) || states[1] != string(tunnel.StateConnected) {
		t.Errorf("broadcast states = %v, want [starting connected]", states)
	}
}

// TestTunnelAutostartIsRefusedWithoutAToken covers the configuration-driven and
// `--tunnel` paths: the refusal happens at startup, before anything listens.
func TestTunnelAutostartIsRefusedWithoutAToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{
			name:    "a tunnel without a token is refused",
			opts:    Options{Tunnel: config.Tunnel{Enabled: true, Provider: config.DefaultTunnelProvider}},
			wantErr: true,
		},
		{
			name: "a tunnel with a token starts",
			opts: Options{Token: "test-token", Tunnel: config.Tunnel{Enabled: true, Provider: config.DefaultTunnelProvider}},
		},
		{
			name: "no tunnel needs no token",
			opts: Options{},
		},
		{
			name:    "an unknown provider is refused rather than served by Cloudflare",
			opts:    Options{Token: "test-token", Tunnel: config.Tunnel{Enabled: true, Provider: "ngrok"}},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.NewTunnel = func(tunnel.Options) (TunnelDriver, error) {
				return newFakeTunnel("https://demo.trycloudflare.com"), nil
			}
			_, err := New(opts)
			if tc.wantErr != (err != nil) {
				t.Fatalf("New() error = %v, want error: %v", err, tc.wantErr)
			}
		})
	}
}

// TestTunnelCapability checks that the feature is advertised where the web app
// looks for it.
func TestTunnelCapability(t *testing.T) {
	t.Parallel()

	s := newTunnelServer(t, "test-token", newFakeTunnel("https://demo.trycloudflare.com"))

	var body struct {
		Features map[string]any `json:"features"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/capabilities"}), http.StatusOK, &body)
	if body.Features["tunnel"] != true {
		t.Errorf("features.tunnel = %v, want true", body.Features["tunnel"])
	}
}
