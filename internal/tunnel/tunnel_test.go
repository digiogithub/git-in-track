package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/prometheus/client_golang/prometheus"
)

// discardLogger keeps cloudflared's and the manager's log lines out of the test
// output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeBroker serves the trycloudflare.com provisioning endpoint, handing out a
// different hostname on every call so a test can tell one round from the next.
func fakeBroker(t *testing.T) *httptest.Server {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tunnel" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success":true,"result":{
			"id":"3f2504e0-4f89-11d3-9a0c-0305e82c330%d",
			"name":"round-%d",
			"hostname":"round-%d.trycloudflare.com",
			"account_tag":"acct",
			"secret":"c2VjcmV0"
		}}`, n, n, n)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// recorder collects the statuses delivered to Manager.OnChange.
type recorder struct {
	mu   sync.Mutex
	seen []Status
}

func (r *recorder) record(s Status) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, s)
}

func (r *recorder) states() []State {
	r.mu.Lock()
	defer r.mu.Unlock()
	states := make([]State, len(r.seen))
	for i, s := range r.seen {
		states[i] = s.State
	}
	return states
}

// newTestManager returns a manager wired to a fake broker and a tunnel runner
// that never touches the network: it blocks until its context is cancelled and
// reports every connection event the test asks for through started.
func newTestManager(t *testing.T) (*Manager, chan struct{}) {
	t.Helper()
	broker := fakeBroker(t)
	m := NewManager(Options{
		Origin:   "127.0.0.1:7317",
		Version:  "0.0.0-test",
		Logger:   discardLogger(),
		endpoint: broker.URL,
	})
	running := make(chan struct{}, 8)
	m.runTunnel = func(ctx context.Context, _ connection.Credentials, _ string) error {
		running <- struct{}{}
		<-ctx.Done()
		return nil
	}
	return m, running
}

// emit feeds an event into the state machine the way the observer sink does,
// stamping it with whatever generation is current.
func (m *Manager) emit(event connection.Event) {
	m.applyEvent(m.generation.Load(), event)
}

func TestManagerStartReturnsTheURLBeforeConnectivity(t *testing.T) {
	t.Parallel()

	m, running := newTestManager(t)
	rec := &recorder{}
	m.OnChange(rec.record)

	status, err := m.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-running

	if status.State != StateStarting {
		t.Fatalf("state = %q, want %q", status.State, StateStarting)
	}
	if status.URL != "https://round-1.trycloudflare.com" {
		t.Fatalf("url = %q, want the provisioned hostname", status.URL)
	}
	if status.Connections != 0 {
		t.Fatalf("connections = %d, want 0 before the edge confirms", status.Connections)
	}
	if status.Since.IsZero() {
		t.Fatal("since is zero, want the moment starting was entered")
	}
	if got := rec.states(); len(got) < 1 || got[0] != StateStarting {
		t.Fatalf("first callback state = %v, want starting first", got)
	}

	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestManagerStartStopStart(t *testing.T) {
	t.Parallel()

	m, running := newTestManager(t)
	rec := &recorder{}
	m.OnChange(rec.record)

	for round := 1; round <= 3; round++ {
		status, err := m.Start(t.Context())
		if err != nil {
			t.Fatalf("round %d: Start: %v", round, err)
		}
		<-running

		wantURL := fmt.Sprintf("https://round-%d.trycloudflare.com", round)
		if status.URL != wantURL {
			t.Fatalf("round %d: url = %q, want %q", round, status.URL, wantURL)
		}

		m.emit(connection.Event{Index: 0, EventType: connection.Connected})
		if got := m.Status(); got.State != StateConnected || got.Connections != 1 {
			t.Fatalf("round %d: status = %+v, want connected with 1 connection", round, got)
		}

		if err := m.Stop(t.Context()); err != nil {
			t.Fatalf("round %d: Stop: %v", round, err)
		}
		if got := m.Status(); got != (Status{State: StateOff}) {
			t.Fatalf("round %d: status after stop = %+v, want the off status", round, got)
		}
	}
}

func TestManagerStartAndStopAreIdempotent(t *testing.T) {
	t.Parallel()

	m, running := newTestManager(t)

	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop on a stopped manager: %v", err)
	}

	first, err := m.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-running

	second, err := m.Start(t.Context())
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if second.URL != first.URL {
		t.Fatalf("second Start url = %q, want the running tunnel's %q", second.URL, first.URL)
	}
	select {
	case <-running:
		t.Fatal("second Start launched a second tunnel")
	case <-time.After(50 * time.Millisecond):
	}

	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestManagerIgnoresEventsFromAPreviousRound(t *testing.T) {
	t.Parallel()

	m, running := newTestManager(t)
	if _, err := m.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-running

	stale := m.generation.Load()
	m.emit(connection.Event{Index: 0, EventType: connection.Connected})
	if got := m.Status(); got.State != StateConnected {
		t.Fatalf("state = %q, want connected", got.State)
	}

	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := m.Start(t.Context()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	<-running

	// A Disconnected from round one, delivered late, must not touch round two.
	m.applyEvent(stale, connection.Event{Index: 0, EventType: connection.Disconnected})
	if got := m.Status(); got.State != StateStarting {
		t.Fatalf("state = %q, want the fresh round to stay starting", got.State)
	}

	// The same event while the manager is stopped is dropped as well.
	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	m.emit(connection.Event{Index: 0, EventType: connection.Connected})
	if got := m.Status(); got != (Status{State: StateOff}) {
		t.Fatalf("status = %+v, want the off status while stopped", got)
	}
}

func TestManagerAppliesConnectionEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		events          []connection.Event
		wantState       State
		wantConnections int
	}{
		{
			name:            "one connection makes the tunnel connected",
			events:          []connection.Event{{Index: 0, EventType: connection.Connected}},
			wantState:       StateConnected,
			wantConnections: 1,
		},
		{
			name: "two connections are counted once each",
			events: []connection.Event{
				{Index: 0, EventType: connection.Connected},
				{Index: 0, EventType: connection.Connected},
				{Index: 1, EventType: connection.Connected},
			},
			wantState:       StateConnected,
			wantConnections: 2,
		},
		{
			name: "losing one of two connections stays connected",
			events: []connection.Event{
				{Index: 0, EventType: connection.Connected},
				{Index: 1, EventType: connection.Connected},
				{Index: 1, EventType: connection.Disconnected},
			},
			wantState:       StateConnected,
			wantConnections: 1,
		},
		{
			name: "losing the last connection moves to reconnecting",
			events: []connection.Event{
				{Index: 0, EventType: connection.Connected},
				{Index: 0, EventType: connection.Disconnected},
			},
			wantState:       StateReconnecting,
			wantConnections: 0,
		},
		{
			name: "unregistering the last connection moves to reconnecting",
			events: []connection.Event{
				{Index: 0, EventType: connection.Connected},
				{Index: 0, EventType: connection.Unregistering},
			},
			wantState:       StateReconnecting,
			wantConnections: 0,
		},
		{
			name:            "reconnecting before any connection leaves starting behind",
			events:          []connection.Event{{Index: 0, EventType: connection.Reconnecting}},
			wantState:       StateReconnecting,
			wantConnections: 0,
		},
		{
			name:            "registering is not yet a connection",
			events:          []connection.Event{{Index: 0, EventType: connection.RegisteringTunnel}},
			wantState:       StateStarting,
			wantConnections: 0,
		},
		{
			name: "reconnecting after a drop recovers on the next connect",
			events: []connection.Event{
				{Index: 0, EventType: connection.Connected},
				{Index: 0, EventType: connection.Disconnected},
				{Index: 0, EventType: connection.Connected},
			},
			wantState:       StateConnected,
			wantConnections: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m, running := newTestManager(t)
			if _, err := m.Start(t.Context()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			<-running

			for _, event := range tt.events {
				m.emit(event)
			}

			got := m.Status()
			if got.State != tt.wantState {
				t.Errorf("state = %q, want %q", got.State, tt.wantState)
			}
			if got.Connections != tt.wantConnections {
				t.Errorf("connections = %d, want %d", got.Connections, tt.wantConnections)
			}
			if err := m.Stop(t.Context()); err != nil {
				t.Fatalf("Stop: %v", err)
			}
		})
	}
}

func TestManagerOnChangeDeliversEveryTransition(t *testing.T) {
	t.Parallel()

	m, running := newTestManager(t)
	rec := &recorder{}
	m.OnChange(rec.record)

	if _, err := m.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-running
	m.emit(connection.Event{Index: 0, EventType: connection.Connected})
	m.emit(connection.Event{Index: 0, EventType: connection.Disconnected})
	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	want := []State{StateStarting, StateStarting, StateConnected, StateReconnecting, StateOff}
	got := rec.states()
	if len(got) != len(want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("states = %v, want %v", got, want)
		}
	}
}

func TestManagerReportsAFailedStart(t *testing.T) {
	t.Parallel()

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no capacity", http.StatusServiceUnavailable)
	}))
	t.Cleanup(broken.Close)

	m := NewManager(Options{
		Origin:   "127.0.0.1:7317",
		Version:  "0.0.0-test",
		Logger:   discardLogger(),
		endpoint: broken.URL,
	})
	rec := &recorder{}
	m.OnChange(rec.record)

	if _, err := m.Start(t.Context()); err == nil {
		t.Fatal("Start: want an error when the broker refuses")
	}

	got := m.Status()
	if got.State != StateError {
		t.Fatalf("state = %q, want %q", got.State, StateError)
	}
	if !strings.Contains(got.Err, "503") {
		t.Fatalf("err = %q, want the broker status in it", got.Err)
	}

	// A failed start leaves the manager stoppable and restartable.
	if err := m.Stop(t.Context()); err != nil {
		t.Fatalf("Stop after a failed start: %v", err)
	}
}

func TestManagerRunnerFailureMovesToError(t *testing.T) {
	t.Parallel()

	broker := fakeBroker(t)
	m := NewManager(Options{
		Origin:   "127.0.0.1:7317",
		Version:  "0.0.0-test",
		Logger:   discardLogger(),
		endpoint: broker.URL,
	})
	failed := make(chan struct{})
	m.runTunnel = func(_ context.Context, _ connection.Credentials, _ string) error {
		defer close(failed)
		return errTunnelExited
	}

	if _, err := m.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-failed

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status().State == StateError {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state = %q, want %q after the supervisor died", m.Status().State, StateError)
}

func TestManagerRejectsAnEmptyOrigin(t *testing.T) {
	t.Parallel()

	m := NewManager(Options{Version: "0.0.0-test", Logger: discardLogger()})
	if _, err := m.Start(t.Context()); err == nil {
		t.Fatal("Start: want ErrNoOrigin for an empty origin")
	}
}

func TestManagerStopHonorsItsContext(t *testing.T) {
	t.Parallel()

	broker := fakeBroker(t)
	m := NewManager(Options{
		Origin:   "127.0.0.1:7317",
		Version:  "0.0.0-test",
		Logger:   discardLogger(),
		endpoint: broker.URL,
	})
	release := make(chan struct{})
	m.runTunnel = func(_ context.Context, _ connection.Credentials, _ string) error {
		<-release
		return nil
	}
	t.Cleanup(func() { close(release) })

	if _, err := m.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := m.Stop(ctx); err == nil {
		t.Fatal("Stop: want an error when the tunnel outlives the context")
	}
}

func TestWithFreshDefaultRegistererSurvivesADuplicateCollector(t *testing.T) {
	// Not parallel: it swaps the process-wide prometheus.DefaultRegisterer.
	original := prometheus.DefaultRegisterer

	newCollector := func() prometheus.Collector {
		return prometheus.NewCounter(prometheus.CounterOpts{
			Name: "gintrack_tunnel_test_total",
			Help: "A collector registered once per simulated tunnel.",
		})
	}

	// This is the exact shape of supervisor.NewSupervisor's
	// v3.NewMetrics(prometheus.DefaultRegisterer): MustRegister on the package
	// global, which panics the second time unless the global is swapped.
	for round := 1; round <= 3; round++ {
		withFreshDefaultRegisterer(func() {
			prometheus.MustRegister(newCollector())
		})
		if prometheus.DefaultRegisterer != original {
			t.Fatalf("round %d: the default registerer was not restored", round)
		}
	}
}

func TestProvision(t *testing.T) {
	t.Parallel()

	const validID = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"

	tests := []struct {
		name         string
		status       int
		body         string
		wantHostname string
		wantErr      string
	}{
		{
			name:         "a successful response yields credentials and a hostname",
			status:       http.StatusOK,
			body:         `{"success":true,"result":{"id":"` + validID + `","hostname":"witty-fox.trycloudflare.com","account_tag":"acct","secret":"c2VjcmV0"}}`,
			wantHostname: "witty-fox.trycloudflare.com",
		},
		{
			name:    "a non-2xx status is an error",
			status:  http.StatusServiceUnavailable,
			body:    "no capacity",
			wantErr: "status 503",
		},
		{
			name:    "an errors payload is an error",
			status:  http.StatusOK,
			body:    `{"success":false,"errors":[{"code":1015,"message":"rate limited"}]}`,
			wantErr: "rate limited",
		},
		{
			name:    "success false without errors is an error",
			status:  http.StatusOK,
			body:    `{"success":false}`,
			wantErr: "broker reported failure",
		},
		{
			name:    "a malformed body is an error",
			status:  http.StatusOK,
			body:    "not json at all",
			wantErr: "decode quick tunnel response",
		},
		{
			name:    "a missing hostname is an error",
			status:  http.StatusOK,
			body:    `{"success":true,"result":{"id":"` + validID + `"}}`,
			wantErr: "broker returned no hostname",
		},
		{
			name:    "an unparseable tunnel id is an error",
			status:  http.StatusOK,
			body:    `{"success":true,"result":{"id":"not-a-uuid","hostname":"x.trycloudflare.com"}}`,
			wantErr: "parse quick tunnel id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath, gotAgent string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotAgent = r.Method, r.URL.Path, r.Header.Get("User-Agent")
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			creds, hostname, err := provision(t.Context(), srv.URL, "cloudflared/0.0.0-test")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("provision: %v", err)
			}
			if hostname != tt.wantHostname {
				t.Errorf("hostname = %q, want %q", hostname, tt.wantHostname)
			}
			if creds.TunnelID.String() != validID {
				t.Errorf("tunnel id = %q, want %q", creds.TunnelID, validID)
			}
			if creds.AccountTag != "acct" {
				t.Errorf("account tag = %q, want %q", creds.AccountTag, "acct")
			}
			if gotMethod != http.MethodPost || gotPath != "/tunnel" {
				t.Errorf("request = %s %s, want POST /tunnel", gotMethod, gotPath)
			}
			if gotAgent != "cloudflared/0.0.0-test" {
				t.Errorf("user agent = %q, want the cloudflared version string", gotAgent)
			}
		})
	}
}

func TestProvisionHonorsItsContext(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	defer func() { close(block); srv.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := provision(ctx, srv.URL, "cloudflared/0.0.0-test"); err == nil {
		t.Fatal("provision: want an error when the context expires")
	}
}

func TestSlogWriterBridgesZerologEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		quiet     bool
		event     map[string]any
		wantLevel slog.Level
	}{
		{
			name:      "an info event stays info",
			event:     map[string]any{"level": "info", "message": "Registered tunnel connection"},
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "a warning stays a warning",
			event:     map[string]any{"level": "warn", "message": "Retrying"},
			wantLevel: slog.LevelWarn,
		},
		{
			name:      "a real error stays an error",
			event:     map[string]any{"level": "error", "message": "Failed to dial origin"},
			wantLevel: slog.LevelError,
		},
		{
			name:      "teardown noise stays an error while the tunnel is up",
			event:     map[string]any{"level": "error", "message": "Serve tunnel error"},
			wantLevel: slog.LevelError,
		},
		{
			name:      "teardown noise is demoted while stopping",
			quiet:     true,
			event:     map[string]any{"level": "error", "message": "Serve tunnel error"},
			wantLevel: slog.LevelDebug,
		},
		{
			name:      "an unrelated error is not demoted while stopping",
			quiet:     true,
			event:     map[string]any{"level": "error", "message": "Failed to dial origin"},
			wantLevel: slog.LevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got slog.Level
			var seen bool
			handler := &capturingHandler{fn: func(r slog.Record) { got, seen = r.Level, true }}
			var quiet atomic.Bool
			quiet.Store(tt.quiet)

			payload, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			writer := slogWriter{log: slog.New(handler), quiet: &quiet}
			if _, err := writer.Write(payload); err != nil {
				t.Fatalf("write: %v", err)
			}
			if !seen {
				t.Fatal("no record reached the handler")
			}
			if got != tt.wantLevel {
				t.Fatalf("level = %v, want %v", got, tt.wantLevel)
			}
		})
	}
}

// capturingHandler records every slog.Record at every level.
type capturingHandler struct {
	fn func(slog.Record)
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.fn(r)
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *capturingHandler) WithGroup(string) slog.Handler { return h }
