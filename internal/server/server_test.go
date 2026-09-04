package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// newTestServer builds a server with a fixed token and the given UI bundle.
func newTestServer(t *testing.T, ui map[string]string) *Server {
	t.Helper()

	var fsys fstest.MapFS
	if ui != nil {
		fsys = fstest.MapFS{}
		for name, content := range ui {
			fsys[name] = &fstest.MapFile{Data: []byte(content), ModTime: time.Unix(0, 0)}
		}
	}
	opts := Options{Token: "test-token", Version: "0.0.1-test", Commit: "abc1234"}
	if fsys != nil {
		opts.UI = fsys
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

func do(t *testing.T, s *Server, method, target string, header map[string]string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Result()
}

func TestHealthIsUnauthenticated(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, nil)
	resp := do(t, s, http.MethodGet, "/api/v1/health", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type = %q", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["version"] != "0.0.1-test" {
		t.Errorf("version = %v", body["version"])
	}
	if body["mode"] != "companion" {
		t.Errorf("mode = %v, want companion", body["mode"])
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("security headers are missing")
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Error("no content security policy")
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("no request id echoed back")
	}
}

func TestAuthenticatedRoutes(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, nil)

	tests := []struct {
		name   string
		header map[string]string
		target string
		want   int
	}{
		{name: "no token", target: "/api/v1/capabilities", want: http.StatusUnauthorized},
		{
			name:   "wrong token",
			target: "/api/v1/capabilities",
			header: map[string]string{"Authorization": "Bearer nope"},
			want:   http.StatusUnauthorized,
		},
		{
			name:   "not a bearer scheme",
			target: "/api/v1/capabilities",
			header: map[string]string{"Authorization": "Basic dGVzdC10b2tlbg=="},
			want:   http.StatusUnauthorized,
		},
		{
			name:   "correct token",
			target: "/api/v1/capabilities",
			header: map[string]string{"Authorization": "Bearer test-token"},
			want:   http.StatusOK,
		},
		{name: "token in the query string", target: "/api/v1/capabilities?token=test-token", want: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := do(t, s, http.MethodGet, tt.target, tt.header)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.want {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tt.want, body)
			}
			if tt.want != http.StatusUnauthorized {
				return
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Errorf("content type = %q, want application/problem+json", ct)
			}
			if auth := resp.Header.Get("WWW-Authenticate"); !strings.Contains(auth, "Bearer") {
				t.Errorf("WWW-Authenticate = %q", auth)
			}
			var p problem
			if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if p.Code != "unauthorized" || p.Status != http.StatusUnauthorized || p.Title == "" {
				t.Errorf("problem = %#v", p)
			}
			if !strings.HasPrefix(p.Type, problemBase) {
				t.Errorf("problem type = %q", p.Type)
			}
		})
	}
}

func TestUnknownAPIRouteIsAProblem(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, map[string]string{"index.html": "<!doctype html><title>app</title>"})
	resp := do(t, s, http.MethodGet, "/api/v1/nope", map[string]string{"Authorization": "Bearer test-token"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("content type = %q, want a problem document, not the SPA", ct)
	}
}

func TestSPAFallback(t *testing.T) {
	t.Parallel()

	const index = "<!doctype html><title>gintrack</title><div id=root></div>"
	s := newTestServer(t, map[string]string{
		"index.html":           index,
		"assets/app-abc123.js": "console.log('app')",
		"favicon.svg":          "<svg/>",
	})

	tests := []struct {
		name        string
		target      string
		wantBody    string
		wantType    string
		wantCaching string
	}{
		{name: "root", target: "/", wantBody: index, wantType: "text/html", wantCaching: "no-cache"},
		{name: "client route", target: "/board/ACME", wantBody: index, wantType: "text/html", wantCaching: "no-cache"},
		{name: "deep client route", target: "/items/ACME-US-0042/edit", wantBody: index, wantType: "text/html"},
		{
			name:        "hashed asset",
			target:      "/assets/app-abc123.js",
			wantBody:    "console.log('app')",
			wantCaching: "public, max-age=31536000, immutable",
		},
		{name: "unhashed asset", target: "/favicon.svg", wantBody: "<svg/>", wantCaching: "no-cache"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := do(t, s, http.MethodGet, tt.target, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
			if tt.wantType != "" && !strings.HasPrefix(resp.Header.Get("Content-Type"), tt.wantType) {
				t.Errorf("content type = %q, want %q", resp.Header.Get("Content-Type"), tt.wantType)
			}
			if tt.wantCaching != "" && resp.Header.Get("Cache-Control") != tt.wantCaching {
				t.Errorf("cache control = %q, want %q", resp.Header.Get("Cache-Control"), tt.wantCaching)
			}
		})
	}
}

func TestSPAWithoutABundle(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		ui   map[string]string
	}{
		{name: "no bundle at all", ui: nil},
		{name: "bundle without an index", ui: map[string]string{".gitkeep": ""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newTestServer(t, tt.ui)
			resp := do(t, s, http.MethodGet, "/", nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !strings.Contains(string(body), "make web") {
				t.Errorf("the page must say how to build the UI:\n%s", body)
			}
			// The API keeps working without a UI.
			health := do(t, s, http.MethodGet, "/api/v1/health", nil)
			defer func() { _ = health.Body.Close() }()
			if health.StatusCode != http.StatusOK {
				t.Errorf("health status = %d", health.StatusCode)
			}
		})
	}
}

func TestCapabilitiesReportsUIState(t *testing.T) {
	t.Parallel()

	s := newTestServer(t, map[string]string{"index.html": "<!doctype html>"})
	resp := do(t, s, http.MethodGet, "/api/v1/capabilities", map[string]string{"Authorization": "Bearer test-token"})
	defer func() { _ = resp.Body.Close() }()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ui"] != "embedded" {
		t.Errorf("ui = %v, want embedded", body["ui"])
	}
	if body["commit"] != "abc1234" {
		t.Errorf("commit = %v", body["commit"])
	}
	features, ok := body["features"].(map[string]any)
	if !ok || features["watcher"] != false {
		t.Errorf("features = %#v", body["features"])
	}
}

func TestNewRefusesTokenlessNonLoopbackBind(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{Bind: "0.0.0.0"}); err == nil {
		t.Error("New() without a token on 0.0.0.0 succeeded, want an error")
	}
	if _, err := New(Options{Bind: "127.0.0.1"}); err != nil {
		t.Errorf("New() without a token on loopback: %v", err)
	}
	if _, err := New(Options{Bind: "0.0.0.0", Token: "t"}); err != nil {
		t.Errorf("New() with a token on 0.0.0.0: %v", err)
	}
}

func TestAuthDisabledServesEverything(t *testing.T) {
	t.Parallel()

	s, err := New(Options{Bind: "localhost"})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	resp := do(t, s, http.MethodGet, "/api/v1/capabilities", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 when the token is empty", resp.StatusCode)
	}
}

func TestGenerateToken(t *testing.T) {
	t.Parallel()

	first, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken(): %v", err)
	}
	second, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken(): %v", err)
	}
	if first == second {
		t.Error("two generated tokens are equal")
	}
	if len(first) < 40 {
		t.Errorf("token %q is shorter than 32 random bytes", first)
	}
	if !tokenMatches(first, first) || tokenMatches(first, second) {
		t.Error("tokenMatches is wrong")
	}
}

func TestStartAndShutdown(t *testing.T) {
	t.Parallel()

	s, err := New(Options{Bind: "127.0.0.1", Port: 0, Token: "test-token"})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	var resp *http.Response
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(s.URL() + "/api/v1/health") //nolint:noctx // short-lived probe in a test
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("the server never answered: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start(): %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Start() did not return after the context was cancelled")
	}
}
