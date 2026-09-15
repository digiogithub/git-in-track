package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
)

// The credentials of the fixture. The upstream one must never appear in a
// response, a header a browser can read, or a log line; the companion one is
// what the browser presents instead.
const (
	upstreamToken  = "pando-secret-token-value"
	companionToken = "companion-test-token"
)

// fakeAGUI is an httptest stand-in for `pando agui-serve`. It records what the
// proxy sent it, so a test can assert on the hop the browser cannot see.
type fakeAGUI struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	requests []recordedRequest

	// run is called for POST {path}/{agent}. Nil streams two frames and ends.
	run http.HandlerFunc
	// infoStatus, when non-zero, is the status /info answers with instead of a
	// document.
	infoStatus int
}

// recordedRequest is what the fake upstream saw.
type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Origin string
	Cookie string
	Body   string
}

// aguiPath is the AG-UI mount point the fixture serves, which is Pando's own
// default.
const aguiPath = "/api/v1/agui"

// newFakeAGUI starts the stand-in adapter.
func newFakeAGUI(t *testing.T) *fakeAGUI {
	t.Helper()
	f := &fakeAGUI{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+aguiPath+"/healthz", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		writeRaw(w, http.StatusOK, `{"status":"ok"}`)
	})
	mux.HandleFunc("GET "+aguiPath+"/info", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if f.infoStatus != 0 {
			writeRaw(w, f.infoStatus, `{"error":"invalid or missing token"}`)
			return
		}
		if !f.authorized(w, r) {
			return
		}
		base := f.server.URL
		writeRaw(w, http.StatusOK, fmt.Sprintf(`{
			"protocol": "ag-ui",
			"version": "0.705.1",
			"path": %q,
			"capabilities": {"humanInTheLoop": true, "sharedState": true},
			"agents": [
				{"name": "backlog-assistant", "description": "Pando agent", "url": %q,
				 "model": {"provider": "anthropic"}},
				{"name": "coder", "description": "Pando agent", "url": %q}
			],
			"docs": %q
		}`, aguiPath, base+aguiPath+"/backlog-assistant", base+aguiPath+"/coder", base+"/docs"))
	})
	mux.HandleFunc("GET "+aguiPath+"/threads", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !f.authorized(w, r) {
			return
		}
		writeRaw(w, http.StatusOK, `{"threads":[]}`)
	})
	mux.HandleFunc("GET "+aguiPath+"/threads/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !f.authorized(w, r) {
			return
		}
		writeRaw(w, http.StatusOK, `{"messages":[]}`)
	})
	mux.HandleFunc("DELETE "+aguiPath+"/threads/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !f.authorized(w, r) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST "+aguiPath+"/runs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !f.authorized(w, r) {
			return
		}
		writeRaw(w, http.StatusOK, `{"cancelled":true}`)
	})
	mux.HandleFunc("POST "+aguiPath+"/{agent}", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		if !f.authorized(w, r) {
			return
		}
		if f.run != nil {
			f.run(w, r)
			return
		}
		sseFrame(w, `{"type":"RUN_STARTED"}`)
		sseFrame(w, `{"type":"RUN_FINISHED"}`)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

// authorized enforces the bearer token the real adapter requires.
func (f *fakeAGUI) authorized(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+upstreamToken {
		writeRaw(w, http.StatusUnauthorized, `{"error":"invalid or missing token"}`)
		return false
	}
	return true
}

// record stores one request, body included.
func (f *fakeAGUI) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Auth:   r.Header.Get("Authorization"),
		Origin: r.Header.Get("Origin"),
		Cookie: r.Header.Get("Cookie"),
		Body:   string(body),
	})
}

// last returns the most recent recorded request.
func (f *fakeAGUI) last() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		f.t.Fatal("the upstream saw no request")
	}
	return f.requests[len(f.requests)-1]
}

// writeRaw writes a JSON body with an explicit status.
func writeRaw(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	// The real adapter answers a browser with CORS headers; the proxy must
	// strip them rather than forward a policy that is not the companion's.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// sseFrame writes one AG-UI frame and flushes it.
func sseFrame(w http.ResponseWriter, payload string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "data: "+payload+"\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// agentFixture is a companion wired to a fake adapter, plus the log it wrote.
type agentFixture struct {
	server   *Server
	upstream *fakeAGUI
	logs     *bytes.Buffer
}

// newAgentFixture builds a companion with the agent proxy switched on.
func newAgentFixture(t *testing.T, mutate func(*config.Config, *Options)) *agentFixture {
	t.Helper()
	upstream := newFakeAGUI(t)

	cfg := config.Default()
	cfg.Agent.Enabled = true
	cfg.Agent.Pando.URL = upstream.server.URL
	cfg.Agent.Pando.Path = aguiPath
	cfg.Agent.Pando.Token = upstreamToken
	cfg.Agent.Pando.Agent = "backlog-assistant"

	logs := &bytes.Buffer{}
	opts := Options{
		Bind:   "127.0.0.1",
		Port:   freeLoopbackPort(t),
		Token:  companionToken,
		Agent:  true,
		Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	if mutate != nil {
		mutate(cfg, &opts)
	}
	opts.Pando = cfg.PandoTargets()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the fixture configuration is invalid: %v", err)
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return &agentFixture{server: s, upstream: upstream, logs: logs}
}

// do runs one request through the companion router.
func (f *agentFixture) do(t *testing.T, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, path, body)
	r.Header.Set("Authorization", "Bearer "+companionToken)
	// A browser always sends these; both must die at the proxy.
	r.Header.Set("Origin", "http://127.0.0.1:7317")
	r.Header.Set("Cookie", "session=browser-cookie")
	rec := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(rec, r)
	return rec
}

func TestAgentInfoRewritesEveryUpstreamURL(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	rec := f.do(t, http.MethodGet, agentPath+"/info", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	agents, ok := doc["agents"].([]any)
	if !ok || len(agents) != 2 {
		t.Fatalf("agents = %v", doc["agents"])
	}
	for _, entry := range agents {
		descriptor := entry.(map[string]any)
		if descriptor["url"] != agentRunPath {
			t.Fatalf("agent %v keeps the url %v, want %s", descriptor["name"], descriptor["url"], agentRunPath)
		}
	}
	if doc["path"] != agentPath {
		t.Fatalf("path = %v, want the companion mount point", doc["path"])
	}
	// `docs` was an absolute URL into the Pando origin, so it is gone rather
	// than rewritten.
	if _, present := doc["docs"]; present {
		t.Fatalf("an absolute upstream URL survived: %v", doc["docs"])
	}
	// Nothing that would tell a browser where Pando listens is left anywhere.
	origin := strings.TrimPrefix(f.upstream.server.URL, "http://")
	if strings.Contains(rec.Body.String(), origin) {
		t.Fatalf("the response names the Pando origin %s: %s", origin, rec.Body)
	}
	// The upstream capabilities the client actually needs survive.
	if _, present := doc["capabilities"]; !present {
		t.Fatal("the discovery document lost its capabilities")
	}
}

func TestAgentInjectsTokenAndStripsOrigin(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	rec := f.do(t, http.MethodPost, agentPath+"/run?token=leak&cursor=abc", strings.NewReader(`{"threadId":"t1"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	sent := f.upstream.last()
	switch {
	case sent.Auth != "Bearer "+upstreamToken:
		t.Fatalf("the upstream token was not injected: %q", sent.Auth)
	case sent.Origin != "":
		t.Fatalf("the browser Origin reached the adapter: %q", sent.Origin)
	case sent.Cookie != "":
		t.Fatalf("the browser cookie reached the adapter: %q", sent.Cookie)
	case sent.Path != aguiPath+"/backlog-assistant":
		t.Fatalf("upstream path = %q", sent.Path)
	case strings.Contains(sent.Query, "token") || strings.Contains(sent.Query, "repo"):
		t.Fatalf("the proxy forwarded a routing or credential parameter: %q", sent.Query)
	case !strings.Contains(sent.Query, "cursor=abc"):
		t.Fatalf("an ordinary query parameter was dropped: %q", sent.Query)
	case sent.Body != `{"threadId":"t1"}`:
		t.Fatalf("the body was altered: %q", sent.Body)
	}
	assertNoTokenLeak(t, f, rec)
}

func TestAgentRunStreamsFrameByFrame(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	f := newAgentFixture(t, nil)
	f.upstream.run = func(w http.ResponseWriter, r *http.Request) {
		sseFrame(w, `{"type":"RUN_STARTED"}`)
		<-release
		sseFrame(w, `{"type":"RUN_FINISHED"}`)
	}

	ts := httptest.NewServer(f.server.Handler())
	defer ts.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+agentPath+"/run", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+companionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got == "*" {
		t.Fatal("the upstream CORS policy was forwarded to the browser")
	}

	// The first frame has to arrive while the upstream is still holding the
	// second one: that is what "unbuffered" means on this hop.
	buf := make([]byte, 128)
	n, err := resp.Body.Read(buf)
	if err != nil {
		t.Fatalf("read the first frame: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "RUN_STARTED") {
		t.Fatalf("first read = %q", buf[:n])
	}
	close(release)

	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the rest: %v", err)
	}
	if !strings.Contains(string(rest), "RUN_FINISHED") {
		t.Fatalf("the terminal frame never arrived: %q", rest)
	}
}

func TestAgentRunCancelsUpstreamOnDisconnect(t *testing.T) {
	t.Parallel()

	cancelled := make(chan struct{})
	f := newAgentFixture(t, nil)
	f.upstream.run = func(w http.ResponseWriter, r *http.Request) {
		sseFrame(w, `{"type":"RUN_STARTED"}`)
		<-r.Context().Done()
		close(cancelled)
	}

	ts := httptest.NewServer(f.server.Handler())
	defer ts.Close()

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+agentPath+"/run", strings.NewReader("{}"))
	if err != nil {
		cancel()
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+companionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("do: %v", err)
	}
	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil {
		cancel()
		t.Fatalf("read the first frame: %v", err)
	}
	// The browser goes away mid-run.
	cancel()
	_ = resp.Body.Close()

	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the upstream run was not cancelled when the client disconnected")
	}
}

func TestAgentRunCapRefusesWithRetryAfter(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	started := make(chan struct{})
	f := newAgentFixture(t, func(cfg *config.Config, _ *Options) {
		cfg.Agent.Pando.MaxRuns = 1
	})
	f.upstream.run = func(w http.ResponseWriter, r *http.Request) {
		sseFrame(w, `{"type":"RUN_STARTED"}`)
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	}

	ts := httptest.NewServer(f.server.Handler())
	defer ts.Close()
	defer close(release)

	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.URL+agentPath+"/run", strings.NewReader("{}"))
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+companionToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		buf := make([]byte, 64)
		_, _ = resp.Body.Read(buf)
		<-release
		_ = resp.Body.Close()
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first run never reached the upstream")
	}

	rec := f.do(t, http.MethodPost, agentPath+"/run", strings.NewReader("{}"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Retry-After"); got != agentRetryAfter {
		t.Fatalf("Retry-After = %q, want %q", got, agentRetryAfter)
	}
	var doc problem
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode the problem: %v", err)
	}
	if doc.Code != codeAgentBusy {
		t.Fatalf("code = %q, want %q", doc.Code, codeAgentBusy)
	}
}

func TestAgentRequiresCompanionToken(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	for _, path := range []string{agentPath + "/info", agentPath + "/run", agentPath + "/health"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/run") {
			method = http.MethodPost
		}
		r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(rec, r)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a token = %d, want 401", method, path, rec.Code)
		}
	}
}

func TestAgentDisabledReportsNotImplemented(t *testing.T) {
	t.Parallel()

	// Switched off by the flag even though an upstream is configured.
	off := newAgentFixture(t, func(_ *config.Config, opts *Options) { opts.Agent = false })
	rec := off.do(t, http.MethodGet, agentPath+"/info", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body %s", rec.Code, rec.Body)
	}
	if off.server.agent.available() {
		t.Fatal("features.agent must be false without the flag")
	}

	// Switched on with no upstream: the same refusal, for the other reason.
	none := newAgentFixture(t, func(cfg *config.Config, _ *Options) { cfg.Agent.Enabled = false; cfg.Agent.Pando.URL = "" })
	if none.server.agent.available() {
		t.Fatal("features.agent must be false without a URL")
	}
	rec = none.do(t, http.MethodPost, agentPath+"/run", strings.NewReader("{}"))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body %s", rec.Code, rec.Body)
	}
}

func TestAgentCapabilityFlag(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	rec := f.do(t, http.MethodGet, apiPrefix+"/capabilities", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var doc struct {
		Features map[string]any `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Features["agent"] != true {
		t.Fatalf("features.agent = %v, want true", doc.Features["agent"])
	}
	assertNoTokenLeak(t, f, rec)
}

func TestAgentUnknownRepoIsA404(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	rec := f.do(t, http.MethodPost, agentPath+"/run?repo=not-mounted", strings.NewReader("{}"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body %s", rec.Code, rec.Body)
	}
	var doc problem
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode the problem: %v", err)
	}
	if doc.Code != codeAgentRepoUnknown {
		t.Fatalf("code = %q, want %q", doc.Code, codeAgentRepoUnknown)
	}
}

func TestAgentRefusesAnOversizedBody(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	body := strings.NewReader(strings.Repeat("x", maxRequestBody+1))
	rec := f.do(t, http.MethodPost, agentPath+"/run", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body)
	}
	var doc problem
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode the problem: %v", err)
	}
	if doc.Code != codeInvalidRequest {
		t.Fatalf("code = %q, want %q", doc.Code, codeInvalidRequest)
	}
	if strings.Contains(rec.Body.String(), "data:") {
		t.Fatal("an oversized body produced a stream instead of a problem document")
	}
}

func TestAgentUpstreamFailureIsAProblem(t *testing.T) {
	t.Parallel()

	f := newAgentFixture(t, nil)
	f.upstream.infoStatus = http.StatusUnauthorized

	rec := f.do(t, http.MethodGet, agentPath+"/info", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body %s", rec.Code, rec.Body)
	}
	var doc problem
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode the problem: %v", err)
	}
	if doc.Code != codeAgentUpstream {
		t.Fatalf("code = %q, want %q", doc.Code, codeAgentUpstream)
	}
	assertNoTokenLeak(t, f, rec)

	// A dead upstream is the same shape of refusal, never a half stream.
	dead := newAgentFixture(t, nil)
	dead.upstream.server.Close()
	rec = dead.do(t, http.MethodPost, agentPath+"/run", strings.NewReader("{}"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body %s", rec.Code, rec.Body)
	}
	assertNoTokenLeak(t, dead, rec)
}

func TestAgentHealthNeedsNoUpstreamToken(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	rec := f.do(t, http.MethodGet, agentPath+"/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	sent := f.upstream.last()
	if sent.Path != aguiPath+"/healthz" {
		t.Fatalf("upstream path = %q, want the healthz probe", sent.Path)
	}
	if sent.Auth != "" {
		t.Fatalf("the probe carried a credential it does not need: %q", sent.Auth)
	}
}

func TestAgentThreadRoutesRelayOneToOne(t *testing.T) {
	t.Parallel()
	f := newAgentFixture(t, nil)

	tests := []struct {
		method string
		path   string
		want   string
		status int
	}{
		{http.MethodGet, agentPath + "/threads", aguiPath + "/threads", http.StatusOK},
		{http.MethodGet, agentPath + "/threads/t1/messages", aguiPath + "/threads/t1/messages", http.StatusOK},
		{http.MethodDelete, agentPath + "/threads/t1", aguiPath + "/threads/t1", http.StatusNoContent},
		{http.MethodPost, agentPath + "/runs/r1/cancel", aguiPath + "/runs/r1/cancel", http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := f.do(t, tc.method, tc.path, strings.NewReader("{}"))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body)
			}
			sent := f.upstream.last()
			if sent.Path != tc.want {
				t.Fatalf("upstream path = %q, want %q", sent.Path, tc.want)
			}
			if sent.Auth != "Bearer "+upstreamToken {
				t.Fatalf("the upstream token was not injected: %q", sent.Auth)
			}
			if sent.Origin != "" {
				t.Fatalf("the browser Origin reached the adapter: %q", sent.Origin)
			}
		})
	}
}

func TestAgentRoutesAreExemptFromTheRequestTimeout(t *testing.T) {
	t.Parallel()

	s, err := New(Options{Bind: "127.0.0.1", Port: freeLoopbackPort(t), Token: companionToken})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	var bounded bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, bounded = r.Context().Deadline()
	})
	handler := s.timeoutExceptStream(next)

	for _, path := range []string{agentPath + "/run", agentPath + "/threads/t1/stream"} {
		bounded = false
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, http.NoBody))
		if bounded {
			t.Fatalf("%s carries the %s request deadline", path, requestTimeout)
		}
	}
	// Everything else still does.
	bounded = false
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPrefix+"/items", http.NoBody))
	if !bounded {
		t.Fatal("an ordinary route lost its request deadline")
	}
}

func TestAgentRunRoutingTablePerRepo(t *testing.T) {
	t.Parallel()

	other := newFakeAGUI(t)
	f := newAgentFixture(t, func(cfg *config.Config, _ *Options) {
		cfg.Agent.Pando.Repos = []config.PandoRepo{{
			Repo:  "git-in-track",
			URL:   other.server.URL,
			Token: upstreamToken,
			Agent: "coder",
		}}
	})

	rec := f.do(t, http.MethodPost, agentPath+"/run?repo=git-in-track", strings.NewReader("{}"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	sent := other.last()
	if sent.Path != aguiPath+"/coder" {
		t.Fatalf("the declared row was not used: %q", sent.Path)
	}
	f.upstream.mu.Lock()
	seen := len(f.upstream.requests)
	f.upstream.mu.Unlock()
	if seen != 0 {
		t.Fatal("the request reached the default upstream instead of the repository's own")
	}
}

// assertNoTokenLeak checks the two surfaces the upstream credential must never
// reach: the response a browser reads, and the log an operator pastes into an
// issue.
func assertNoTokenLeak(t *testing.T, f *agentFixture, rec *httptest.ResponseRecorder) {
	t.Helper()
	if strings.Contains(rec.Body.String(), upstreamToken) {
		t.Fatalf("the response carries the upstream token: %s", rec.Body)
	}
	for key, values := range rec.Header() {
		for _, value := range values {
			if strings.Contains(value, upstreamToken) {
				t.Fatalf("the header %s carries the upstream token", key)
			}
		}
	}
	if strings.Contains(f.logs.String(), upstreamToken) {
		t.Fatalf("a log line carries the upstream token: %s", f.logs)
	}
}
