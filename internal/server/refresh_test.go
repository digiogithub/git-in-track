package server

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestItemReadRefreshesAFileEditedOnDisk pins read-time freshness: with no
// watcher running at all, opening a task edited on disk serves the edit and
// announces it, so the lists other people have open follow.
func TestItemReadRefreshesAFileEditedOnDisk(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), streamTimeout)
	defer cancel()

	// The watcher is never started: only the read can notice the edit.
	s, httpSrv, root := watchingServer(t, nil)
	conn := dialEvents(ctx, t, s, httpSrv, "")

	rel := "docs/.pmngr/tasks/DEMO-T-0001-add-address-validation.md"
	onDisk := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read the fixture task: %v", err)
	}
	edited := strings.Replace(string(data), "title: Add address validation", "title: Add address validation on read", 1)
	if err := os.WriteFile(onDisk, []byte(edited), 0o644); err != nil {
		t.Fatalf("write the task: %v", err)
	}

	var item struct {
		Title string `json:"title"`
	}
	decode(t, send(t, s, request{method: "GET", target: "/api/v1/items/DEMO-T-0001"}), 200, &item)
	if item.Title != "Add address validation on read" {
		t.Errorf("title = %q, want the edit made on disk", item.Title)
	}
	awaitFrame(ctx, t, conn, eventItemChanged)
}

// TestWatchScopesOfARootProjectSkipTheSourceTree pins the other half of the
// watch-budget fix: a project whose documentation folder is the repository
// root used to watch every directory of the repository.
func TestWatchScopesOfARootProjectSkipTheSourceTree(t *testing.T) {
	t.Parallel()

	// The fixture's documentation folder, promoted to a repository root, next
	// to a source tree the index never reads.
	root := copyTree(t, filepath.Join(fixtureRoot, "docs"))
	src := filepath.Join(root, "src", "pkg", "deep")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatalf("make the source tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package deep\n"), 0o644); err != nil {
		t.Fatalf("write a source file: %v", err)
	}

	reg := newRegistry([]Repo{{ID: testRepoID, Path: root, Role: "project"}}, time.Now)
	m := reg.all()[0]
	if !m.ready() {
		t.Fatalf("the root project did not mount: %v", m.err)
	}

	got := watchScopes(m)
	if len(got) == 0 {
		t.Fatal("watchScopes() is empty, which watches the whole repository")
	}
	for _, want := range []string{".pmngr", "architecture"} {
		if !slices.Contains(got, want) {
			t.Errorf("watchScopes() = %v, want it to cover %q", got, want)
		}
	}
	for _, scope := range got {
		if scope == "src" || strings.HasPrefix(scope, "src/") {
			t.Errorf("watchScopes() = %v, the source tree must not be watched", got)
		}
	}
}
