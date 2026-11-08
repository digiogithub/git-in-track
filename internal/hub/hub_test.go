package hub

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/digiogithub/git-in-track/internal/gitops"
)

// countingTarget records how many syncs ran.
type countingTarget struct{ n atomic.Int32 }

func (c *countingTarget) SyncNow(context.Context) []gitops.SyncResult {
	c.n.Add(1)
	return nil
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSyncLoop(t *testing.T) {
	t.Parallel()
	waitFor := func(t *testing.T, c *countingTarget, want int32) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for c.n.Load() < want {
			if time.Now().After(deadline) {
				t.Fatalf("syncs = %d, want at least %d", c.n.Load(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	t.Run("syncs at start and on every tick", func(t *testing.T) {
		t.Parallel()
		target := &countingTarget{}
		loop := newSyncLoop(20*time.Millisecond, time.Hour, quietLog())
		loop.target = target
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go loop.run(ctx)
		waitFor(t, target, 3)
	})

	t.Run("a burst of triggers coalesces into one sync after the delay", func(t *testing.T) {
		t.Parallel()
		target := &countingTarget{}
		loop := newSyncLoop(time.Hour, 50*time.Millisecond, quietLog())
		loop.target = target
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go loop.run(ctx)
		waitFor(t, target, 1) // the start-up sync
		for range 5 {
			loop.trigger()
		}
		waitFor(t, target, 2)
		time.Sleep(150 * time.Millisecond)
		if got := target.n.Load(); got != 2 {
			t.Fatalf("syncs = %d after a burst, want 2", got)
		}
	})
}

const projectYAML = `schema: 2
key: ACME
name: Acme
timezone: UTC
docs:
  path: docs
workflow:
  initial: backlog
  statuses:
    - { id: backlog, name: Backlog, category: todo }
    - { id: done,    name: Done,    category: done, terminal: true }
`

// TestHubPushesAWriteMadeThroughTheAPI writes a knowledge-base page through
// the REST API and waits for it on the forge: commit-on-save commits it,
// OnCommit wakes the loop, and the loop pushes it, with nobody asking.
func TestHubPushesAWriteMadeThroughTheAPI(t *testing.T) {
	t.Parallel()
	bare := bareRemote(t, map[string]string{"docs/.pmngr/project.yaml": projectYAML})
	cfg := &Config{
		DataDir: t.TempDir(), Bind: "127.0.0.1", Token: "test-token",
		PullInterval: Duration(time.Hour),
		Git:          GitIdentity{AuthorName: "hub", AuthorEmail: "hub@example.invalid"},
		Organization: Organization{ID: "acme", Projects: []RepoSpec{{ID: "web", URL: bare, Branch: "main"}}},
	}
	h, err := New(t.Context(), cfg, Deps{UI: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, Logger: quietLog(), Git: gitBin(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.syncer.delay = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go h.syncer.run(ctx)

	body := strings.NewReader(`{"path":"intro.md","content":"# Intro\n\nWritten through the hub.\n"}`)
	req := httptest.NewRequestWithContext(ctx, http.MethodPut, "/api/v1/projects/ACME/kb/page", body)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Server().Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT page = %d: %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		files := git(t, filepath.Dir(bare), "--git-dir", bare, "ls-tree", "-r", "--name-only", "main")
		if strings.Contains(files, "docs/intro.md") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the page never reached the forge; main holds:\n%s", files)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestHubSyncsWithItsForge clones a local bare repository, then checks both
// directions of the sync the loop runs: a commit made in the hub's clone
// reaches the forge, and a commit pushed to the forge by someone else reaches
// the clone.
func TestHubSyncsWithItsForge(t *testing.T) {
	t.Parallel()
	bare := bareRemote(t, map[string]string{"README.md": "hello\n"})
	cfg := &Config{
		DataDir: t.TempDir(), Bind: "127.0.0.1", Port: 0,
		PullInterval: Duration(time.Hour),
		Git:          GitIdentity{AuthorName: "hub", AuthorEmail: "hub@example.invalid"},
		Organization: Organization{ID: "acme", Projects: []RepoSpec{{ID: "web", URL: bare, Branch: "main"}}},
	}
	h, err := New(t.Context(), cfg, Deps{UI: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, Logger: quietLog(), Git: gitBin(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	clone := cfg.RepoDir("web")

	// Outgoing: a commit in the clone is pushed by the next sync.
	if err := os.WriteFile(filepath.Join(clone, "notes.md"), []byte("from the hub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "add", "notes.md")
	git(t, clone, "commit", "--quiet", "-m", "hub write")
	for _, res := range h.Server().SyncNow(t.Context()) {
		if !res.OK() {
			t.Fatalf("sync %s failed: %s %s", res.Repo, res.Code, res.Message)
		}
	}
	if got := git(t, filepath.Dir(bare), "--git-dir", bare, "log", "-1", "--format=%s", "main"); got != "hub write" {
		t.Fatalf("forge head = %q, want the hub's commit", got)
	}

	// Incoming: a commit someone else pushed is pulled by the next sync.
	other := filepath.Join(t.TempDir(), "other")
	git(t, filepath.Dir(other), "clone", "--quiet", bare, other)
	if err := os.WriteFile(filepath.Join(other, "theirs.md"), []byte("from a technician\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, other, "add", "theirs.md")
	git(t, other, "commit", "--quiet", "-m", "technician write")
	git(t, other, "push", "--quiet", "origin", "HEAD:main")
	for _, res := range h.Server().SyncNow(t.Context()) {
		if !res.OK() {
			t.Fatalf("sync %s failed: %s %s", res.Repo, res.Code, res.Message)
		}
	}
	if _, err := os.Stat(filepath.Join(clone, "theirs.md")); err != nil {
		t.Fatalf("the pulled file is missing from the clone: %v", err)
	}
}
