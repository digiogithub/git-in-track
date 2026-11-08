package gitops

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// gitShow formats the last commit of dir with a git log --format string.
func gitShow(t *testing.T, dir, format string) string {
	t.Helper()
	bin, _, err := resolveGit("")
	if err != nil {
		t.Skipf("no git binary: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), bin, "log", "-1", "--format="+format)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestCommitAuthorAndCommitter is GIT-US-0208: gintrack-hub commits as its
// service identity on behalf of the authenticated principal, on both git
// backends.
func TestCommitAuthorAndCommitter(t *testing.T) {
	ana := Identity{Name: "Ana García", Email: "ana@acme.example"}
	hub := Identity{Name: "gintrack-hub", Email: "hub@acme.example"}
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("the principal authors and the hub commits", func(t *testing.T) {
				repo := newRepo(t)
				write(t, repo, "notes.md", "x\n")
				_, err := open(t, repo, kind).Commit(context.Background(), CommitRequest{
					Paths: []string{"notes.md"}, Message: Message{Subject: "docs: notes"},
					Author: ana, Committer: hub,
				})
				if err != nil {
					t.Fatalf("Commit: %v", err)
				}
				if got := gitShow(t, repo, "%an <%ae>|%cn <%ce>"); got != ana.String()+"|"+hub.String() {
					t.Fatalf("author|committer = %q", got)
				}
			})
			t.Run("without a committer the author commits too", func(t *testing.T) {
				repo := newRepo(t)
				write(t, repo, "notes.md", "x\n")
				if _, err := open(t, repo, kind).Commit(context.Background(), CommitRequest{
					Paths: []string{"notes.md"}, Message: Message{Subject: "docs: notes"}, Author: ana,
				}); err != nil {
					t.Fatalf("Commit: %v", err)
				}
				if got := gitShow(t, repo, "%cn <%ce>"); got != ana.String() {
					t.Fatalf("committer = %q, want the author", got)
				}
			})
			t.Run("without either the repository identity is used, as before", func(t *testing.T) {
				repo := newRepo(t)
				write(t, repo, "notes.md", "x\n")
				if _, err := open(t, repo, kind).Commit(context.Background(), CommitRequest{
					Paths: []string{"notes.md"}, Message: Message{Subject: "docs: notes"},
				}); err != nil {
					t.Fatalf("Commit: %v", err)
				}
				if got := gitShow(t, repo, "%an <%ae>|%cn <%ce>"); got != "Test User <test@example.com>|Test User <test@example.com>" {
					t.Fatalf("author|committer = %q", got)
				}
			})
		})
	}
}

func TestCommitterAttribution(t *testing.T) {
	ana := Identity{Name: "Ana", Email: "ana@acme.example"}
	bea := Identity{Name: "Bea", Email: "bea@acme.example"}
	hub := Identity{Name: "gintrack-hub", Email: "hub@acme.example"}

	newCommitter := func(t *testing.T, repo string) *Committer {
		t.Helper()
		backend := open(t, repo, KindGoGit)
		c := NewCommitter(CommitterOptions{
			Debounce:  time.Hour,
			Template:  MustParseTemplate("{{action}} {{id}}: {{title}}"),
			Committer: hub,
			Backend:   func(string) (Backend, bool) { return backend, true },
		})
		t.Cleanup(func() { c.Close(context.Background()) })
		return c
	}
	edit := func(t *testing.T, c *Committer, repo, text string, author Identity, fields Fields) {
		t.Helper()
		const path = "docs/.pmngr/stories/ACME-US-0001-story.md"
		write(t, repo, path, text)
		fields.ItemID, fields.Title, fields.Type, fields.Action = "ACME-US-0001", "Story", "story", ActionUpdate
		c.Enqueue(t.Context(), Change{Repo: "repo", Paths: []string{path}, Fields: fields, Author: author})
	}

	t.Run("two authors on one item within the window make two commits", func(t *testing.T) {
		repo := newRepo(t)
		c := newCommitter(t, repo)
		edit(t, c, repo, "ana\n", ana, Fields{})
		// Bea's edit is flushed first so that each commit holds one author's
		// change: the batches are keyed by author, so neither absorbs the other.
		c.Flush(context.Background())
		edit(t, c, repo, "bea\n", bea, Fields{})
		edit(t, c, repo, "ana again\n", ana, Fields{})
		if got := c.Pending(); got != 2 {
			t.Fatalf("Pending() = %d, want one batch per author", got)
		}
		c.Flush(context.Background())
		if got := gitShow(t, repo, "%cn"); got != hub.Name {
			t.Errorf("committer = %q, want the hub", got)
		}
	})

	t.Run("an agent's write carries Agent and On-Behalf-Of", func(t *testing.T) {
		repo := newRepo(t)
		c := newCommitter(t, repo)
		pando := Identity{Name: "Pando (Ana)", Email: "pando-ana@agents.acme.example"}
		edit(t, c, repo, "from pando\n", pando, Fields{Agent: "pando", OnBehalfOf: "ana"})
		c.Flush(context.Background())
		body := gitShow(t, repo, "%B")
		for _, want := range []string{"Agent: pando", "On-Behalf-Of: ana"} {
			if !strings.Contains(body, want) {
				t.Errorf("message lacks %q:\n%s", want, body)
			}
		}
		if got := gitShow(t, repo, "%an|%cn"); got != pando.Name+"|"+hub.Name {
			t.Errorf("author|committer = %q", got)
		}
	})
}
