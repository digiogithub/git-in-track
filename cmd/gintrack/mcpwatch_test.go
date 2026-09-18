package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/watcher"
)

// freshnessFixture mounts a copy of the fixture the way `gintrack mcp` does
// and returns the keeper, the repository root and a probe that reports whether
// the index holds an item.
func freshnessFixture(t *testing.T) (*mcpFreshness, string, func(id string) bool) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "acme-api")
	copyTree(t, filepath.Join("..", "..", "testdata", "fixtures", fixtureName), repo)
	_, mounts, err := mountWorkspace([]config.Repo{{ID: "acme-api", Path: repo, Role: config.RoleProject}}, "test")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	fresh := newMCPFreshness(mounts, slog.New(slog.NewTextHandler(io.Discard, nil)))
	has := func(id string) bool {
		t.Helper()
		out, err := mounts[0].vlt.Dispatch(context.Background(), "item.list", []byte(`{}`))
		if err != nil {
			t.Fatalf("item.list: %v", err)
		}
		data, err := json.Marshal(out)
		if err != nil {
			t.Fatalf("encode item.list: %v", err)
		}
		return strings.Contains(string(data), `"`+id+`"`)
	}
	return fresh, repo, has
}

// writeTriagedTask writes, behind the server's back, the task an inbox triage
// in the web UI would have written.
func writeTriagedTask(t *testing.T, repo string) {
	t.Helper()
	dir := filepath.Join(repo, "docs", ".pmngr", "tasks")
	data, err := os.ReadFile(filepath.Join(dir, "DEMO-T-0001-add-address-validation.md"))
	if err != nil {
		t.Fatalf("read the fixture task: %v", err)
	}
	task := strings.Replace(string(data), "id: DEMO-T-0001", "id: DEMO-T-0002", 1)
	task = strings.Replace(task, "title: Add address validation", "title: Triaged from the inbox", 1)
	if err := os.WriteFile(filepath.Join(dir, "DEMO-T-0002-triaged-from-the-inbox.md"), []byte(task), 0o644); err != nil {
		t.Fatalf("write the triaged task: %v", err)
	}
}

// TestMCPFreshnessWatches pins the regression behind this keeper: a task
// written after the stdio server indexed the workspace reaches its index.
func TestMCPFreshnessWatches(t *testing.T) {
	fresh, repo, has := freshnessFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := fresh.start(ctx)
	defer stop()

	writeTriagedTask(t, repo)
	deadline := time.Now().Add(5 * time.Second)
	for !has("DEMO-T-0002") {
		if time.Now().After(deadline) {
			t.Fatal("the watcher never folded the new task into the index")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(fresh.polled) != 0 {
		t.Errorf("polled = %d repositories, want none while the watcher runs", len(fresh.polled))
	}
}

// TestMCPFreshnessFallsBackToRescan pins the fallback: with no watcher, a tool
// call rescans first, at most once per interval.
func TestMCPFreshnessFallsBackToRescan(t *testing.T) {
	fresh, repo, has := freshnessFixture(t)
	fresh.newWatcher = func(watcher.Options) (mcpWatcher, error) {
		return nil, errors.New("inotify budget exhausted")
	}
	clock := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	fresh.now = func() time.Time { return clock }
	ctx := context.Background()
	fresh.start(ctx)()

	fresh.beforeCall(ctx)
	writeTriagedTask(t, repo)
	fresh.beforeCall(ctx)
	if has("DEMO-T-0002") {
		t.Error("a second call inside the interval rescanned again")
	}
	clock = clock.Add(mcpRescanInterval)
	fresh.beforeCall(ctx)
	if !has("DEMO-T-0002") {
		t.Error("a call after the interval did not rescan: the new task is still missing")
	}
}
