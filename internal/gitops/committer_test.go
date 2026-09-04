package gitops

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// committerFor builds a committer over one repository, with the outcomes
// collected for assertion.
func committerFor(t *testing.T, dir string, debounce time.Duration, template string) (*Committer, func() []Outcome) {
	t.Helper()
	backend := open(t, dir, KindGoGit)

	var mu sync.Mutex
	var seen []Outcome
	c := NewCommitter(CommitterOptions{
		Debounce: debounce,
		Template: MustParseTemplate(template),
		Backend: func(repo string) (Backend, bool) {
			if repo != "repo" {
				return nil, false
			}
			return backend, true
		},
		OnResult: func(out Outcome) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, out)
		},
	})
	t.Cleanup(func() { c.Close(context.Background()) })

	return c, func() []Outcome {
		mu.Lock()
		defer mu.Unlock()
		return append([]Outcome(nil), seen...)
	}
}

func TestCommitterCoalescesRapidEdits(t *testing.T) {
	tests := []struct {
		name string
		// edits is the sequence of writes; each one rewrites the same file.
		edits int
		// items is how many distinct items the edits are spread over.
		items int
		want  int
	}{
		{name: "one keystroke burst on one item is one commit", edits: 25, items: 1, want: 1},
		{name: "two items get one commit each", edits: 20, items: 2, want: 2},
		{name: "five items get five commits", edits: 15, items: 5, want: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			// A long window: nothing may fire until Flush, which is what proves
			// the coalescing rather than a lucky timing.
			c, outcomes := committerFor(t, repo, time.Hour, "{{action}} {{id}}: {{title}}")

			for i := range tc.edits {
				item := i % tc.items
				path := "docs/.pmngr/stories/ACME-US-000" + itoa(item) + "-story.md"
				write(t, repo, path, "revision "+itoa(i)+"\n")
				c.Enqueue(t.Context(), Change{
					Repo:  "repo",
					Paths: []string{path},
					Fields: Fields{
						ItemID: "ACME-US-000" + itoa(item),
						Title:  "Story " + itoa(item),
						Type:   "story",
						Action: ActionUpdate,
					},
				})
			}
			if got := c.Pending(); got != tc.items {
				t.Fatalf("Pending() = %d, want one batch per item (%d)", got, tc.items)
			}

			before := len(log(t, repo))
			c.Flush(context.Background())
			after := len(log(t, repo))

			if made := after - before; made != tc.want {
				t.Errorf("%d edits produced %d commits, want %d", tc.edits, made, tc.want)
			}
			for _, out := range outcomes() {
				if out.Err != nil {
					t.Errorf("outcome %+v failed: %v", out, out.Err)
				}
			}
		})
	}
}

func TestCommitterDebounceFires(t *testing.T) {
	repo := newRepo(t)
	c, _ := committerFor(t, repo, 20*time.Millisecond, "")
	before := len(log(t, repo))

	for i := range 5 {
		write(t, repo, "docs/.pmngr/stories/ACME-US-0001-a.md", "revision "+itoa(i)+"\n")
		c.Enqueue(t.Context(), Change{
			Repo:   "repo",
			Paths:  []string{"docs/.pmngr/stories/ACME-US-0001-a.md"},
			Fields: Fields{ItemID: "ACME-US-0001", Title: "A", Type: "story"},
		})
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.Pending() == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c.Pending() != 0 {
		t.Fatal("the debounce window elapsed but nothing was committed")
	}
	// The timer callback may still be finishing; Close waits for it.
	c.Close(context.Background())

	if made := len(log(t, repo)) - before; made != 1 {
		t.Fatalf("%d commits, want exactly one", made)
	}
	if got := log(t, repo)[0]; got != `pmngr: update ACME-US-0001 "A"` {
		t.Errorf("subject = %q", got)
	}
}

func TestCommitterMessages(t *testing.T) {
	tests := []struct {
		name     string
		template string
		change   Change
		want     string
	}{
		{
			name:     "a create keeps its action through later edits",
			template: "{{action}} {{id}}: {{title}}",
			change: Change{
				Repo:   "repo",
				Paths:  []string{"docs/.pmngr/tasks/ACME-T-0001-x.md"},
				Fields: Fields{ItemID: "ACME-T-0001", Title: "X", Type: "task", Action: ActionCreate},
			},
			want: "create ACME-T-0001: X",
		},
		{
			name:     "a move names the board",
			template: "{{action}} {{id}} to {{status}} (board: {{board}})",
			change: Change{
				Repo:  "repo",
				Paths: []string{"docs/.pmngr/tasks/ACME-T-0002-y.md"},
				Fields: Fields{
					ItemID: "ACME-T-0002", Title: "Y", Type: "task",
					Action: ActionMove, Status: "in_progress", PrevStatus: "todo", Board: "team-alpha",
				},
			},
			want: "move ACME-T-0002 to in_progress (board: team-alpha)",
		},
		{
			name:     "a bulk write falls back to the built-in subject",
			template: "",
			change: Change{
				Repo:   "repo",
				Paths:  []string{"docs/.pmngr/tasks/ACME-T-0003-z.md", "docs/.pmngr/tasks/ACME-T-0004-w.md"},
				Fields: Fields{Action: ActionUpdate, Count: 2},
			},
			want: "pmngr: update 2 items",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			c, _ := committerFor(t, repo, time.Hour, tc.template)
			for _, p := range tc.change.Paths {
				write(t, repo, p, "body\n")
			}
			c.Enqueue(t.Context(), tc.change)
			// A second identical enqueue must not change the outcome.
			c.Enqueue(t.Context(), tc.change)
			c.Flush(context.Background())

			if got := log(t, repo)[0]; got != tc.want {
				t.Errorf("subject = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommitterReportsFailures(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		wantSeen bool
	}{
		{name: "a repository with no backend is silently skipped", repo: "unknown", wantSeen: false},
		{name: "a repository with a backend is committed", repo: "repo", wantSeen: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := newRepo(t)
			c, outcomes := committerFor(t, dir, -1, "")
			write(t, dir, "docs/a.md", "a\n")
			c.Enqueue(t.Context(), Change{
				Repo:   tc.repo,
				Paths:  []string{"docs/a.md"},
				Fields: Fields{ItemID: "ACME-T-0001", Title: "A"},
			})
			got := outcomes()
			if tc.wantSeen && len(got) == 0 {
				t.Fatal("the commit produced no outcome")
			}
			if !tc.wantSeen && len(got) != 0 {
				t.Fatalf("outcomes = %+v, want none", got)
			}
			if tc.wantSeen && got[0].Err != nil {
				t.Fatalf("outcome failed: %v", got[0].Err)
			}
		})
	}
}

func TestCommitterSurfacesAnActionableFailure(t *testing.T) {
	dir := newRepo(t)
	backend := open(t, dir, KindGoGit)
	var seen []Outcome
	c := NewCommitter(CommitterOptions{
		Debounce: -1,
		// A template that renders nothing at all cannot produce a subject.
		Template: MustParseTemplate("{{if false}}never{{end}}"),
		Backend:  func(string) (Backend, bool) { return backend, true },
		OnResult: func(out Outcome) { seen = append(seen, out) },
	})
	write(t, dir, "docs/a.md", "a\n")
	c.Enqueue(t.Context(), Change{Repo: "repo", Paths: []string{"docs/a.md"}, Fields: Fields{ItemID: "ACME-T-1"}})

	if len(seen) != 1 || seen[0].Err == nil {
		t.Fatalf("outcomes = %+v, want one failure", seen)
	}
	if seen[0].Code != CodeTemplateInvalid {
		t.Errorf("code = %q, want %q", seen[0].Code, CodeTemplateInvalid)
	}
	if !strings.Contains(seen[0].Message, "template") {
		t.Errorf("message = %q, want it to name the template", seen[0].Message)
	}
	if len(log(t, dir)) != 1 {
		t.Error("a failed render must not commit anything")
	}
}

func TestCommitterCloseIsIdempotent(t *testing.T) {
	dir := newRepo(t)
	c, _ := committerFor(t, dir, time.Hour, "")
	write(t, dir, "docs/a.md", "a\n")
	c.Enqueue(t.Context(), Change{Repo: "repo", Paths: []string{"docs/a.md"}, Fields: Fields{ItemID: "ACME-T-1", Title: "A"}})

	ctx := context.Background()
	if out := c.Close(ctx); len(out) != 1 {
		t.Fatalf("Close committed %d batches, want 1", len(out))
	}
	if out := c.Close(ctx); len(out) != 0 {
		t.Fatalf("the second Close committed %d batches, want none", len(out))
	}
	// A write after Close is dropped rather than queued forever.
	c.Enqueue(t.Context(), Change{Repo: "repo", Paths: []string{"docs/a.md"}, Fields: Fields{ItemID: "ACME-T-1"}})
	if c.Pending() != 0 {
		t.Error("a closed committer must accept nothing")
	}
}
