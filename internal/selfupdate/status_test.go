package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// statusGitHub serves a release list and counts the lookups.
func statusGitHub(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

const releasesBody = `[{"tag_name":"v2.3.0","html_url":"https://example.test/r/v2.3.0"},{"tag_name":"v2.2.0"}]`

func newTestChecker(t *testing.T, srv *httptest.Server, current string, now *time.Time) *Checker {
	t.Helper()
	return &Checker{
		Dir:     t.TempDir(),
		Current: current,
		Client:  New(Options{BaseURL: srv.URL, HTTPClient: srv.Client()}),
		Now:     func() time.Time { return *now },
	}
}

func TestCheckerCacheTTLs(t *testing.T) {
	t.Parallel()

	t.Run("success is cached for six hours", func(t *testing.T) {
		t.Parallel()
		srv, hits := statusGitHub(t, http.StatusOK, releasesBody)
		now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
		c := newTestChecker(t, srv, "v2.2.0", &now)

		st := c.Check(context.Background())
		if !st.UpdateAvailable || st.Latest != "2.3.0" || st.URL == "" {
			t.Fatalf("first check = %+v", st)
		}
		now = now.Add(SuccessTTL - time.Minute)
		if st = c.Check(context.Background()); !st.UpdateAvailable || hits.Load() != 1 {
			t.Fatalf("within TTL: %+v, %d lookups", st, hits.Load())
		}
		now = now.Add(2 * time.Minute)
		c.Check(context.Background())
		if hits.Load() != 2 {
			t.Fatalf("after TTL lookups = %d, want 2", hits.Load())
		}
	})

	t.Run("failure is cached for fifteen minutes", func(t *testing.T) {
		t.Parallel()
		srv, hits := statusGitHub(t, http.StatusInternalServerError, `{}`)
		now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
		c := newTestChecker(t, srv, "2.2.0", &now)

		st := c.Check(context.Background())
		if st.Err == "" || st.UpdateAvailable {
			t.Fatalf("failed check = %+v", st)
		}
		now = now.Add(FailureTTL - time.Minute)
		if st = c.Check(context.Background()); st.Err == "" || hits.Load() != 1 {
			t.Fatalf("within failure TTL: %+v, %d lookups", st, hits.Load())
		}
		now = now.Add(2 * time.Minute)
		c.Check(context.Background())
		if hits.Load() != 2 {
			t.Fatalf("after failure TTL lookups = %d, want 2", hits.Load())
		}
	})

	t.Run("up to date", func(t *testing.T) {
		t.Parallel()
		srv, _ := statusGitHub(t, http.StatusOK, releasesBody)
		now := time.Now()
		c := newTestChecker(t, srv, "2.3.0", &now)
		if st := c.Check(context.Background()); st.UpdateAvailable || st.Latest != "2.3.0" {
			t.Fatalf("status = %+v", st)
		}
	})

	t.Run("corrupt cache file is ignored and replaced", func(t *testing.T) {
		t.Parallel()
		srv, hits := statusGitHub(t, http.StatusOK, releasesBody)
		now := time.Now()
		c := newTestChecker(t, srv, "2.2.0", &now)
		if err := os.WriteFile(filepath.Join(c.Dir, CacheFileName), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, fresh := c.Cached(); fresh {
			t.Fatal("a corrupt file must not be fresh")
		}
		if st := c.Check(context.Background()); !st.UpdateAvailable || hits.Load() != 1 {
			t.Fatalf("status = %+v, %d lookups", st, hits.Load())
		}
		if st, fresh := c.Cached(); !fresh || !st.UpdateAvailable {
			t.Fatalf("cached after rewrite = %+v fresh=%v", st, fresh)
		}
	})

	t.Run("cached never touches the network", func(t *testing.T) {
		t.Parallel()
		srv, hits := statusGitHub(t, http.StatusOK, releasesBody)
		now := time.Now()
		c := newTestChecker(t, srv, "2.2.0", &now)
		if st, fresh := c.Cached(); fresh || st.UpdateAvailable || hits.Load() != 0 {
			t.Fatalf("empty cache: %+v fresh=%v hits=%d", st, fresh, hits.Load())
		}
	})
}

func TestCheckerNotApplicable(t *testing.T) {
	t.Parallel()

	srv, hits := statusGitHub(t, http.StatusOK, releasesBody)
	now := time.Now()
	for _, tc := range []struct {
		name    string
		current string
		seam    func() string
	}{
		{name: "dev build", current: "dev"},
		{name: "unparseable", current: "test"},
		{name: "seam says no", current: "2.2.0", seam: func() string { return "installed by Homebrew" }},
	} {
		c := newTestChecker(t, srv, tc.current, &now)
		c.NotApplicable = tc.seam
		if st := c.Check(context.Background()); st.NotApplicable == "" || st.UpdateAvailable {
			t.Errorf("%s: status = %+v", tc.name, st)
		}
	}
	if hits.Load() != 0 {
		t.Fatalf("not-applicable installs made %d lookups", hits.Load())
	}
}
