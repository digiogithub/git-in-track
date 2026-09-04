package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The structured conflict surface against real repositories (GIT-US-0022).
//
// Every case drives two clones of the same repository into a real conflict and
// then reads the three versions back, resolves them and finishes the rebase —
// the flow the resolver UI performs one call at a time.

// story renders a story file with one status and one body line.
func story(status, body string) string {
	return "---\n" +
		"id: GIT-US-0042\n" +
		"type: story\n" +
		"title: Log in with SSO\n" +
		"status: " + status + "\n" +
		"author: ana\n" +
		"created: 2026-01-01T00:00:00Z\n" +
		"updated: 2026-01-01T00:00:00Z\n" +
		"---\n\n## Description\n\n" + body + "\n"
}

// conflictedPair drives clone a into a stopped rebase on one path and returns
// the backend bound to it.
func conflictedPair(t *testing.T, kind Kind) (Backend, clonePair, string) {
	t.Helper()
	const rel = "docs/.pmngr/stories/GIT-US-0042-log-in-with-sso.md"
	pair := newClonePair(t)
	commitFile(t, pair.b, rel, story("todo", "Theirs."), "docs: theirs")
	gitRunner(t, pair.b)("push", "origin", "main")
	commitFile(t, pair.a, rel, story("in_progress", "Mine."), "docs: mine")

	backend := open(t, pair.a, kind)
	if _, err := Sync(t.Context(), backend, syncOpts(StrategyRebase)); CodeOf(err) != CodeConflict {
		t.Fatalf("want a conflict, got code %q (err %v)", CodeOf(err), err)
	}
	return backend, pair, rel
}

func TestConflictFile(t *testing.T) {
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			if kind == KindGoGit {
				t.Skip("go-git cannot rebase, so it never reaches a conflict to read")
			}
			backend, _, rel := conflictedPair(t, kind)

			t.Run("the three versions come back from the index stages", func(t *testing.T) {
				versions, err := backend.ConflictFile(t.Context(), rel)
				if err != nil {
					t.Fatalf("ConflictFile: %v", err)
				}
				if !versions.HasOurs || !versions.HasTheirs {
					t.Fatalf("a content conflict must carry both sides: %+v", versions)
				}
				// A rebase replays the local commit onto the upstream, so git
				// calls the remote side "ours". The sides are swapped back so
				// that "mine" is the commit this user made.
				if !versions.Rebased {
					t.Errorf("a rebase conflict must report its swapped sides")
				}
				if !strings.Contains(versions.Ours, "Mine.") {
					t.Fatalf("ours is not the local edit: %q", versions.Ours)
				}
				if !strings.Contains(versions.Theirs, "Theirs.") {
					t.Fatalf("theirs is not the remote edit: %q", versions.Theirs)
				}
				if versions.Binary {
					t.Errorf("a Markdown file was classified as binary")
				}
				if !strings.Contains(versions.Working, "<<<<<<<") {
					t.Errorf("the working copy should still carry git's markers: %q", versions.Working)
				}
			})

			t.Run("a path that is not conflicted is refused", func(t *testing.T) {
				if _, err := backend.ConflictFile(t.Context(), "README.md"); CodeOf(err) != CodeNotFound {
					t.Fatalf("code = %q, want %q", CodeOf(err), CodeNotFound)
				}
			})
		})
	}
}

func TestResolvePath(t *testing.T) {
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			if kind == KindGoGit {
				t.Run("go-git refuses to apply a resolution it cannot finish", func(t *testing.T) {
					pair := newClonePair(t)
					backend := open(t, pair.a, kind)
					_, err := backend.ResolvePath(t.Context(), ResolveRequest{Path: "docs/a.md"})
					if CodeOf(err) != CodeUnsupported {
						t.Fatalf("code = %q, want %q", CodeOf(err), CodeUnsupported)
					}
				})
				return
			}

			t.Run("a resolution finishes the rebase and leaves a clean tree", func(t *testing.T) {
				backend, pair, rel := conflictedPair(t, kind)
				resolved := story("in_progress", "Mine and theirs.")
				res, err := backend.ResolvePath(t.Context(), ResolveRequest{
					Path: rel, Content: resolved, Continue: true,
				})
				if err != nil {
					t.Fatalf("ResolvePath: %v", err)
				}
				if !res.Staged || !res.Continued {
					t.Fatalf("want a staged and continued resolution, got %+v", res)
				}
				if len(res.Remaining) != 0 {
					t.Fatalf("remaining conflicts: %+v", res.Remaining)
				}
				if res.Status.State == StateConflicted || res.Status.Operation != "" {
					t.Fatalf("the rebase was not finished: %+v", res.Status)
				}
				raw, err := os.ReadFile(filepath.Join(pair.a, filepath.FromSlash(rel)))
				if err != nil {
					t.Fatalf("read the resolved file: %v", err)
				}
				if string(raw) != resolved {
					t.Fatalf("the resolved file is not what was written:\n%s", raw)
				}
				if strings.Contains(string(raw), "<<<<<<<") {
					t.Fatalf("the resolved file still carries conflict markers")
				}
			})

			t.Run("resolving without continuing leaves the operation in progress", func(t *testing.T) {
				backend, _, rel := conflictedPair(t, kind)
				res, err := backend.ResolvePath(t.Context(), ResolveRequest{
					Path: rel, Content: story("todo", "Something."),
				})
				if err != nil {
					t.Fatalf("ResolvePath: %v", err)
				}
				if res.Continued {
					t.Fatalf("the operation was continued without being asked")
				}
				if res.Status.Operation != OpRebase {
					t.Fatalf("operation = %q, want %q", res.Status.Operation, OpRebase)
				}
				// Abort is still available at this step and restores the tree.
				if err := backend.Abort(t.Context()); err != nil {
					t.Fatalf("Abort after a resolution: %v", err)
				}
				st, err := backend.SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.Operation != "" || st.State == StateConflicted {
					t.Fatalf("the abort left the tree in %+v", st)
				}
			})

			t.Run("resolving a path that is not conflicted is refused", func(t *testing.T) {
				backend, _, _ := conflictedPair(t, kind)
				_, err := backend.ResolvePath(t.Context(), ResolveRequest{Path: "README.md", Content: "x"})
				if CodeOf(err) != CodeNotFound {
					t.Fatalf("code = %q, want %q", CodeOf(err), CodeNotFound)
				}
			})

			t.Run("resolving with no integration in progress is refused", func(t *testing.T) {
				pair := newClonePair(t)
				backend := open(t, pair.a, kind)
				_, err := backend.ResolvePath(t.Context(), ResolveRequest{Path: "README.md", Content: "x"})
				if CodeOf(err) != CodeInProgress {
					t.Fatalf("code = %q, want %q", CodeOf(err), CodeInProgress)
				}
			})
		})
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "text is not binary", content: "# hello\n", want: false},
		{name: "empty is not binary", content: "", want: false},
		{name: "a NUL byte makes it binary", content: "PNG\x00\x01", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBinary(tc.content); got != tc.want {
				t.Errorf("isBinary(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}
