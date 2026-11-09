package rag

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/digiogithub/git-in-track/internal/core"
)

func TestMemoryStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fsys := fstest.MapFS{
		"docs/a.md": {Data: []byte("# Alpha\n\nInvoicing rules.\n")},
		"docs/.pmngr/stories/X-US-0001-invoicing-story.md": {Data: []byte("---\nid: X-US-0001\ntype: story\ntitle: Invoicing story\nstatus: backlog\n---\n")},
	}
	s := NewMemoryStore()
	st, err := s.ApplyChanges(ctx, fsys, []Change{{Op: OpAdd, Path: "docs/a.md", Blob: "b1"}, {Op: OpAdd, Path: "docs/.pmngr/stories/X-US-0001-invoicing-story.md"}}, "c1")
	if err != nil || st.Added != 2 {
		t.Fatalf("ApplyChanges = %+v, %v", st, err)
	}
	if again, _ := s.ApplyChanges(ctx, fsys, []Change{{Op: OpDelete, Path: "docs/a.md"}}, "c1"); again.Deleted != 0 {
		t.Error("a watermark already applied was applied again")
	}

	t.Run("statuses default to active, which a page without status is", func(t *testing.T) {
		hits, _ := s.Search(ctx, "invoicing", Query{})
		if len(hits) != 1 || hits[0].Path != "docs/a.md" {
			t.Fatalf("hits = %+v", hits)
		}
		all, _ := s.Search(ctx, "invoicing", Query{Statuses: []string{AnyStatus}})
		if len(all) != 2 {
			t.Errorf("AnyStatus hits = %+v", all)
		}
	})
	t.Run("filters apply before ranking", func(t *testing.T) {
		if hits, _ := s.Search(ctx, "invoicing", Query{Statuses: []string{AnyStatus}, Kinds: []string{"story"}}); len(hits) != 1 || hits[0].Kind != "story" {
			t.Errorf("kind filter = %+v", hits)
		}
		if hits, _ := s.Search(ctx, "invoicing", Query{Statuses: []string{AnyStatus}, ExcludePrefixes: []string{"docs/.pmngr"}}); len(hits) != 1 {
			t.Errorf("exclude filter = %+v", hits)
		}
	})
	t.Run("a rename of an unchanged blob reads nothing", func(t *testing.T) {
		before := s.Reads()
		if _, err := s.ApplyChanges(ctx, fstest.MapFS{}, []Change{{Op: OpRename, OldPath: "docs/a.md", Path: "docs/b.md", Blob: "b1"}}, "c2"); err != nil {
			t.Fatal(err)
		}
		if s.Reads() != before {
			t.Error("the rename re-read the document")
		}
		if hits, _ := s.Search(ctx, "invoicing", Query{}); len(hits) != 1 || hits[0].Path != "docs/b.md" {
			t.Errorf("after rename = %+v", hits)
		}
	})
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@x.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@x.invalid", "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, rel, text string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestIndexerFollowsCommits(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	dir := t.TempDir()
	gitIn(t, dir, "init", "--quiet", "--initial-branch=main")
	write(t, dir, "docs/a.md", "# Alpha\n\nInvoicing.\n")
	write(t, dir, "src/x.go", "package x\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "-m", "one")

	store := NewMemoryStore()
	var applied [][]core.FileEvent
	ix := &Indexer{
		Open: func(Repo) (Store, error) { return store, nil },
		Apply: func(_ context.Context, _ string, ev []core.FileEvent) error {
			applied = append(applied, ev)
			return nil
		},
	}
	repos := []Repo{{ID: "r", Dir: dir}}
	if err := ix.Sync(t.Context(), repos); err != nil {
		t.Fatal(err)
	}
	if mark, _ := store.Watermark(t.Context()); mark != gitIn(t, dir, "rev-parse", "HEAD") {
		t.Fatalf("watermark = %q", mark)
	}
	if store.Reads() != 1 {
		t.Errorf("the first index read %d documents, want the one Markdown file", store.Reads())
	}
	if len(applied) != 0 {
		t.Error("the first sync replayed the tree into the vault, which indexed it at mount")
	}

	gitIn(t, dir, "mv", "docs/a.md", "docs/b.md")
	write(t, dir, "docs/c.md", "# Charlie\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "--quiet", "-m", "two")
	if err := ix.Sync(t.Context(), repos); err != nil {
		t.Fatal(err)
	}
	if store.Reads() != 2 {
		t.Errorf("reads = %d, want one more for docs/c.md and none for the rename", store.Reads())
	}
	if len(applied) != 1 || len(applied[0]) != 3 {
		t.Errorf("vault events = %+v, want remove a, create b, create c", applied)
	}

	t.Run("a restart resumes from the watermark", func(t *testing.T) {
		fresh := &Indexer{Open: func(Repo) (Store, error) { return store, nil }}
		reads := store.Reads()
		if err := fresh.Sync(t.Context(), repos); err != nil {
			t.Fatal(err)
		}
		if store.Reads() != reads {
			t.Error("a restart at the same HEAD re-read documents")
		}
	})
}
