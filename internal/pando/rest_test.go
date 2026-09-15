package pando

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReindexKBNotConfigured(t *testing.T) {
	t.Parallel()
	c, err := New(Options{MCPURL: "http://127.0.0.1:9777/mcp"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.ReindexKB(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ReindexKB() error = %v, want ErrNotConfigured", err)
	}
}

func TestReindexKBSuccess(t *testing.T) {
	t.Parallel()
	var gotPath, gotMethod, gotToken, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotToken = r.Header.Get("X-Pando-Token")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"scanned":12,"added":3,"updated":2,"unchanged":7,"deleted":1,"links_indexed":9}`))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Options{RESTURL: srv.URL + "/", RESTToken: "rest-token", Token: "mcp-token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	stats, err := c.ReindexKB(context.Background())
	if err != nil {
		t.Fatalf("ReindexKB() error = %v", err)
	}
	want := ReindexStats{Scanned: 12, Added: 3, Updated: 2, Unchanged: 7, Deleted: 1, LinksIndexed: 9}
	if stats != want {
		t.Fatalf("stats = %+v, want %+v", stats, want)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/remembrances/kb/reindex" {
		t.Fatalf("called %s %s", gotMethod, gotPath)
	}
	if gotToken != "rest-token" {
		t.Errorf("X-Pando-Token = %q", gotToken)
	}
	// The MCP bearer token must never reach the REST surface: they are
	// separate credentials and the REST one is the only one this call needs.
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want the MCP token to stay on the MCP transport", gotAuth)
	}
}

func TestReindexKBStatusMapping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusConflict, "a KB reindex is already running", ErrReindexRunning},
		{http.StatusServiceUnavailable, "KB filesystem mirror not configured", ErrNotConfigured},
		{http.StatusUnauthorized, "unauthorized", ErrUnauthorized},
		{http.StatusInternalServerError, "kb reindex failed: disk full", ErrUnreachable},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, tc.body, tc.status)
			}))
			t.Cleanup(srv.Close)
			c, err := New(Options{RESTURL: srv.URL})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = c.ReindexKB(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("ReindexKB() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReindexKBTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c, err := New(Options{RESTURL: srv.URL, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.ReindexKB(context.Background()); !errors.Is(err, ErrTimeout) {
		t.Fatalf("ReindexKB() error = %v, want ErrTimeout", err)
	}
}

func TestReindexKBMalformedBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Options{RESTURL: srv.URL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.ReindexKB(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("ReindexKB() error = %v, want ErrUnreachable", err)
	}
}
