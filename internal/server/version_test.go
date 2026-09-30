package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/selfupdate"
)

// TestVersionEndpoint covers GET /api/v1/version (GIT-US-0195).
func TestVersionEndpoint(t *testing.T) {
	t.Parallel()

	auth := map[string]string{"Authorization": "Bearer test-token"}
	decode := func(t *testing.T, s *Server) map[string]any {
		t.Helper()
		resp := do(t, s, http.MethodGet, "/api/v1/version", auth)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	newServer := func(t *testing.T, version string, up UpdateChecker) *Server {
		t.Helper()
		s, err := New(Options{Token: "test-token", Version: version, Update: up})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	github := func(t *testing.T, status int, body string) *selfupdate.Client {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return selfupdate.New(selfupdate.Options{BaseURL: srv.URL, HTTPClient: srv.Client()})
	}

	t.Run("requires the token", func(t *testing.T) {
		t.Parallel()
		s := newServer(t, "2.2.0", nil)
		resp := do(t, s, http.MethodGet, "/api/v1/version", nil)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("update available", func(t *testing.T) {
		t.Parallel()
		checker := &selfupdate.Checker{
			Dir: t.TempDir(), Current: "2.2.0",
			Client: github(t, http.StatusOK, `[{"tag_name":"v2.3.0","html_url":"https://example.test/v2.3.0"}]`),
		}
		body := decode(t, newServer(t, "2.2.0", checker))
		if body["current"] != "2.2.0" || body["latest"] != "2.3.0" || body["updateAvailable"] != true ||
			body["url"] != "https://example.test/v2.3.0" || body["checkedAt"] == nil {
			t.Fatalf("body = %v", body)
		}
	})

	t.Run("lookup failure is not an error", func(t *testing.T) {
		t.Parallel()
		checker := &selfupdate.Checker{
			Dir: t.TempDir(), Current: "2.2.0", Client: github(t, http.StatusBadGateway, `{}`),
		}
		body := decode(t, newServer(t, "2.2.0", checker))
		if body["updateAvailable"] != false || body["latest"] != "" {
			t.Fatalf("body = %v", body)
		}
	})

	t.Run("development build", func(t *testing.T) {
		t.Parallel()
		checker := &selfupdate.Checker{Dir: t.TempDir(), Current: "dev"}
		body := decode(t, newServer(t, "dev", checker))
		if body["updateAvailable"] != false || body["checkedAt"] != nil {
			t.Fatalf("body = %v", body)
		}
	})

	t.Run("no checker", func(t *testing.T) {
		t.Parallel()
		body := decode(t, newServer(t, "2.2.0", nil))
		if body["current"] != "2.2.0" || body["updateAvailable"] != false {
			t.Fatalf("body = %v", body)
		}
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), "token") {
			t.Fatalf("response mentions a token: %s", raw)
		}
	})
}
