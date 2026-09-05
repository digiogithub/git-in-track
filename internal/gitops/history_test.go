package gitops

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The history walk of GIT-US-0028, against real repositories and against both
// backends. The fixture writes a known sequence of item-file revisions, so the
// assertions are about the reconstructed series, not about git.

// itemFile renders a minimal item file with the given status.
func itemFile(id, status string) string {
	return "---\nid: " + id + "\ntype: story\ntitle: " + id +
		"\nstatus: " + status + "\nestimate: 3\n---\n\nBody.\n"
}

// commitAt writes a file and commits it with an explicit author date, so that
// the reconstructed instants are the fixture's and not the clock's.
func commitAt(t *testing.T, dir, rel, text, subject, date string) {
	t.Helper()
	write(t, dir, rel, text)
	g := gitRunner(t, dir)
	g("add", rel)
	g("commit", "-m", subject, "--date="+date)
}

// historyFixture builds a repository holding two item files with a known
// transition history.
func historyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	g := gitRunner(t, dir)
	g("init", "--initial-branch=main")
	configure(t, dir)
	write(t, dir, "README.md", "# fixture\n")
	g("add", "README.md")
	g("commit", "-m", "chore: seed", "--date=2026-02-20T09:00:00Z")

	one := "docs/.pmngr/stories/DEMO-US-0001.md"
	two := "docs/.pmngr/stories/DEMO-US-0002.md"
	commitAt(t, dir, one, itemFile("DEMO-US-0001", "todo"), "feat: add one", "2026-02-25T09:00:00Z")
	commitAt(t, dir, two, itemFile("DEMO-US-0002", "todo"), "feat: add two", "2026-02-26T09:00:00Z")
	commitAt(t, dir, one, itemFile("DEMO-US-0001", "in_progress"), "chore: start one", "2026-03-02T10:00:00Z")
	commitAt(t, dir, one, itemFile("DEMO-US-0001", "done"), "chore: finish one", "2026-03-03T16:00:00Z")
	// A commit that touches neither item, to prove the walk ignores it.
	commitAt(t, dir, "docs/notes.md", "notes\n", "docs: unrelated", "2026-03-04T09:00:00Z")
	return dir
}

func TestBackendHistory(t *testing.T) {
	one := "docs/.pmngr/stories/DEMO-US-0001.md"
	two := "docs/.pmngr/stories/DEMO-US-0002.md"

	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			dir := historyFixture(t)
			backend := open(t, dir, kind)

			t.Run("every revision of the requested paths is reconstructed", func(t *testing.T) {
				history, err := backend.History(t.Context(), HistoryRequest{Paths: []string{one, two}})
				if err != nil {
					t.Fatalf("History: %v", err)
				}
				want := []struct {
					path   string
					when   string
					status string
				}{
					{one, "2026-02-25T09:00:00Z", "todo"},
					{two, "2026-02-26T09:00:00Z", "todo"},
					{one, "2026-03-02T10:00:00Z", "in_progress"},
					{one, "2026-03-03T16:00:00Z", "done"},
				}
				if len(history.Revisions) != len(want) {
					for _, rev := range history.Revisions {
						t.Logf("revision %s %s deleted=%v", rev.Path, rev.When.Format(time.RFC3339), rev.Deleted)
					}
					t.Fatalf("revisions = %d, want %d", len(history.Revisions), len(want))
				}
				for i, expected := range want {
					rev := history.Revisions[i]
					if rev.Path != expected.path {
						t.Errorf("revision %d path = %s, want %s", i, rev.Path, expected.path)
					}
					if got := rev.When.UTC().Format(time.RFC3339); got != expected.when {
						t.Errorf("revision %d when = %s, want %s", i, got, expected.when)
					}
					if !strings.Contains(string(rev.Data), "status: "+expected.status) {
						t.Errorf("revision %d does not carry status %s", i, expected.status)
					}
				}
				if history.Truncated {
					t.Error("a five-commit repository must not report a truncated history")
				}
				if history.Head == "" {
					t.Error("a history must name the commit it was read at")
				}
			})

			t.Run("a deletion ends the history of a path", func(t *testing.T) {
				local := historyFixture(t)
				g := gitRunner(t, local)
				if err := os.Remove(filepath.Join(local, filepath.FromSlash(two))); err != nil {
					t.Fatalf("remove: %v", err)
				}
				g("add", "-A")
				g("commit", "-m", "chore: drop two", "--date=2026-03-05T09:00:00Z")

				history, err := open(t, local, kind).History(t.Context(), HistoryRequest{Paths: []string{two}})
				if err != nil {
					t.Fatalf("History: %v", err)
				}
				if len(history.Revisions) != 2 {
					t.Fatalf("revisions = %d, want 2", len(history.Revisions))
				}
				if history.Revisions[0].Deleted {
					t.Error("the first revision is the creation, not a deletion")
				}
				if !history.Revisions[1].Deleted {
					t.Error("the last revision must be the deletion")
				}
			})

			t.Run("an unknown path yields nothing rather than failing", func(t *testing.T) {
				history, err := backend.History(t.Context(),
					HistoryRequest{Paths: []string{"docs/.pmngr/stories/DEMO-US-9999.md"}})
				if err != nil {
					t.Fatalf("History: %v", err)
				}
				if len(history.Revisions) != 0 {
					t.Errorf("revisions = %d, want 0", len(history.Revisions))
				}
			})

			t.Run("no path reads nothing", func(t *testing.T) {
				history, err := backend.History(t.Context(), HistoryRequest{})
				if err != nil {
					t.Fatalf("History: %v", err)
				}
				if len(history.Revisions) != 0 {
					t.Errorf("revisions = %d, want 0", len(history.Revisions))
				}
			})
		})
	}
}

func TestHistoryCacheInvalidatesOnANewCommit(t *testing.T) {
	t.Parallel()

	cache := NewHistoryCache()
	req := HistoryRequest{Paths: []string{"a.md"}}
	calls := 0
	read := func() (FileHistory, error) {
		calls++
		return FileHistory{Head: "one", Commits: calls}, nil
	}

	for _, tc := range []struct {
		name      string
		head      string
		wantCalls int
	}{
		{"the first read misses", "one", 1},
		{"the same head hits", "one", 1},
		{"a new commit invalidates", "two", 2},
		{"the new head then hits", "two", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := cache.Do(tc.head, req, read); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if calls != tc.wantCalls {
				t.Errorf("reads = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
	if cache.Hits != 2 || cache.Misses != 2 {
		t.Errorf("hits/misses = %d/%d, want 2/2", cache.Hits, cache.Misses)
	}
}

// TestHistoryWalkIsFastOnALongHistory is the performance criterion of
// GIT-US-0028: reconstructing the history of a sprint's item files must not
// depend on how long the repository's history is, because git filters by path
// before it walks. The fixture is deliberately built out of commits that touch
// nothing the walk asks for.
func TestHistoryWalkIsFastOnALongHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("building the long-history fixture takes a few seconds")
	}
	dir := t.TempDir()
	g := gitRunner(t, dir)
	g("init", "--initial-branch=main")
	configure(t, dir)
	one := "docs/.pmngr/stories/DEMO-US-0001.md"
	commitAt(t, dir, one, itemFile("DEMO-US-0001", "todo"), "feat: add one", "2026-02-25T09:00:00Z")
	for i := 0; i < 2000; i++ {
		g("commit", "--allow-empty", "-q", "-m", "chore: noise "+strconv.Itoa(i))
	}
	commitAt(t, dir, one, itemFile("DEMO-US-0001", "done"), "chore: finish one", "2026-03-03T16:00:00Z")

	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			backend := open(t, dir, kind)
			started := time.Now()
			history, err := backend.History(t.Context(), HistoryRequest{Paths: []string{one}})
			if err != nil {
				t.Fatalf("History: %v", err)
			}
			elapsed := time.Since(started)
			if len(history.Revisions) != 2 {
				t.Fatalf("revisions = %d, want 2", len(history.Revisions))
			}
			if elapsed > 5*time.Second {
				t.Errorf("the walk took %s over 2002 commits, want under 5s", elapsed)
			}
			t.Logf("%s walked 2002 commits in %s", kind, elapsed)
		})
	}
}
