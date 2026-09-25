package gitops

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

// TestGoGitBackendSerializesConcurrentCalls drives one repository through two
// go-git backends — one opened at the root, one from a subfolder — from many
// goroutines at once. go-git is not safe for concurrent use, so every call must
// run under the one lock of that repository (GIT-US-0146); -race reports any
// overlap inside go-git's object storage.
func TestGoGitBackendSerializesConcurrentCalls(t *testing.T) {
	dir := newRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := open(t, dir, KindGoGit)
	sub := open(t, filepath.Join(dir, "docs"), KindGoGit)

	t.Run("both backends share the repository lock", func(t *testing.T) {
		a, aok := root.(*serialBackend)
		b, bok := sub.(*serialBackend)
		if !aok || !bok {
			t.Fatalf("Open returned %T and %T, want serialized go-git backends", root, sub)
		}
		if a.mu != b.mu {
			t.Error("two backends of one repository hold different locks")
		}
		if other := open(t, newRepo(t), KindGoGit).(*serialBackend); other.mu == a.mu { //nolint:forcetypeassert // checked above
			t.Error("two different repositories share one lock")
		}
	})

	t.Run("concurrent commits and reads", func(t *testing.T) {
		const workers, rounds = 4, 5
		var wg sync.WaitGroup
		for w := range workers {
			wg.Go(func() {
				// Commits go through the root backend, whose paths are
				// relative to the root; half the workers read through the
				// subfolder one.
				b := root
				if w%2 == 1 {
					b = sub
				}
				for i := range rounds {
					rel := "docs/w" + strconv.Itoa(w) + "-" + strconv.Itoa(i) + ".md"
					write(t, dir, rel, "change\n")
					if _, err := root.Commit(t.Context(), CommitRequest{
						Paths:   []string{rel},
						Message: Message{Subject: "test: " + rel},
					}); err != nil {
						t.Errorf("commit %s: %v", rel, err)
					}
					if _, err := b.Status(t.Context()); err != nil {
						t.Errorf("status: %v", err)
					}
					if _, err := b.SyncStatus(t.Context()); err != nil {
						t.Errorf("sync status: %v", err)
					}
					if _, err := b.Commits(t.Context(), LogRequest{To: "HEAD", Limit: 5}); err != nil {
						t.Errorf("commits: %v", err)
					}
				}
			})
		}
		wg.Wait()
		if got, want := len(log(t, dir)), 1+workers*rounds; got != want {
			t.Errorf("%d commits, want %d: one per commit call plus the seed", got, want)
		}
	})
}
