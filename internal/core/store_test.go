package core

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// storeTestProjectYAML is the configuration the store tests write against.
const storeTestProjectYAML = `schema: 1
key: ACME
name: ACME Platform
workflow:
  initial: backlog
  statuses:
    - { id: backlog,     name: Backlog,     category: todo }
    - { id: todo,        name: To Do,       category: todo }
    - { id: in_progress, name: In Progress, category: in_progress }
    - { id: in_review,   name: In Review,   category: in_progress }
    - { id: done,        name: Done,        category: done,      terminal: true }
    - { id: cancelled,   name: Cancelled,   category: cancelled, terminal: true }
  transitions:
    backlog:     [todo, cancelled]
    todo:        [in_progress, backlog, cancelled]
    in_progress: [in_review, todo, cancelled]
    in_review:   [done, in_progress, cancelled]
    done:        [in_progress]
    cancelled:   [backlog]
id_allocation:
  strategy: scan
  write_counters: true
  counters:
    story: 0
defaults:
  story:
    status: backlog
    priority: medium
    assignees: [jose]
    labels: [auth]
  task:
    status: todo
    priority: high
labels:
  - { name: auth, color: "#2563eb" }
people:
  - { handle: jose }
  - { handle: marta }
`

// testClock is a Clock a test moves by hand.
type testClock struct{ now time.Time }

// Now returns the current fake time.
func (c *testClock) Now() time.Time { return c.now }

// advance moves the fake clock forward.
func (c *testClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// storeTestBase is the instant the store tests start from.
var storeTestBase = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// newTestStore returns a store over an empty vault whose only file is
// project.yaml, plus the file system and the clock behind it.
func newTestStore(t *testing.T) (*FileStore, *MemFS, *testClock) {
	t.Helper()

	fsys := NewMemFSFromMap(map[string]string{"docs/.pmngr/project.yaml": storeTestProjectYAML})
	cfg, err := LoadProjectConfig([]byte(storeTestProjectYAML))
	if err != nil {
		t.Fatalf("load project.yaml: %v", err)
	}
	clock := &testClock{now: storeTestBase}
	store := NewStore(fsys, "docs", cfg)
	store.Clock = clock
	return store, fsys, clock
}

// storeFixtureFS loads a fixture vault from testdata/store/<name> into a MemFS,
// keeping the paths it has on disk.
func storeFixtureFS(t *testing.T, name string) *MemFS {
	t.Helper()

	root := filepath.Join("testdata", "store", name)
	out := NewMemFS()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // test fixture under testdata
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		return out.WriteFile(filepath.ToSlash(rel), data)
	})
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return out
}

// storeFixtureConfig parses the project.yaml of a fixture vault.
func storeFixtureConfig(t *testing.T, fsys FS) *ProjectConfig {
	t.Helper()

	data, err := fsys.ReadFile("docs/.pmngr/project.yaml")
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	cfg, err := LoadProjectConfig(data)
	if err != nil {
		t.Fatalf("parse project.yaml: %v", err)
	}
	return cfg
}

func TestStoreCreateAppliesDefaultsAndAllocates(t *testing.T) {
	t.Parallel()

	store, fsys, clock := newTestStore(t)
	ctx := context.Background()

	story, err := store.Create(ctx, ItemDraft{
		Type:   TypeStory,
		Title:  "Login with SSO",
		Author: "jose",
		Body:   "## Description\n\nSign in through the corporate IdP.",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if story.ID != "ACME-US-0001" {
		t.Errorf("id = %s, want ACME-US-0001", story.ID)
	}
	if story.Path != "docs/.pmngr/stories/ACME-US-0001-login-with-sso.md" {
		t.Errorf("path = %s", story.Path)
	}
	if story.Status != "backlog" || story.Priority != PriorityMedium {
		t.Errorf("defaults were not materialized: status=%s priority=%s", story.Status, story.Priority)
	}
	if len(story.Assignees) != 1 || story.Assignees[0] != "jose" || len(story.Labels) != 1 || story.Labels[0] != "auth" {
		t.Errorf("list defaults were not materialized: %v %v", story.Assignees, story.Labels)
	}
	if story.Created.String() != "2026-09-03T12:00:00Z" || story.Updated != story.Created {
		t.Errorf("timestamps = %s / %s, want the injected clock", story.Created, story.Updated)
	}
	if !story.Rev.Valid() {
		t.Errorf("rev = %q, want a content hash", story.Rev)
	}

	// The file on disk is the canonical serialization, and nothing else was left.
	data, err := fsys.ReadFile(story.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := ComputeRev(data); got != story.Rev {
		t.Errorf("rev = %s, on-disk rev = %s", story.Rev, got)
	}
	for _, p := range fsys.Paths() {
		if strings.HasSuffix(p, ".tmp") {
			t.Errorf("the atomic write left %s behind", p)
		}
	}

	// A second creation takes the next number, and a task lands in tasks/.
	clock.advance(time.Minute)
	task, err := store.Create(ctx, ItemDraft{
		Type:   TypeTask,
		Title:  "Wire the callback route",
		Parent: story.ID,
		Author: "marta",
	})
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}
	if task.ID != "ACME-T-0001" || task.Path != "docs/.pmngr/tasks/ACME-T-0001-wire-the-callback-route.md" {
		t.Errorf("task = %s at %s", task.ID, task.Path)
	}
	if task.Status != "todo" || task.Priority != PriorityHigh {
		t.Errorf("task defaults: status=%s priority=%s", task.Status, task.Priority)
	}

	round, err := store.Get(ctx, story.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if round.Title != story.Title || round.Rev != story.Rev || round.Body != story.Body {
		t.Errorf("round trip differs:\n%+v\n%+v", round, story)
	}
}

func TestStoreCreateStampsStartedWhenBornInProgress(t *testing.T) {
	t.Parallel()

	store, _, _ := newTestStore(t)
	it, err := store.Create(context.Background(), ItemDraft{
		Type: TypeStory, Title: "Already running", Status: "in_progress", Author: "jose",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if it.Started.IsZero() {
		t.Error("started was not stamped for an item created in an in_progress status")
	}
}

func TestStoreCreateRejectsAnEmptyTitle(t *testing.T) {
	t.Parallel()

	store, fsys, _ := newTestStore(t)
	before := len(fsys.Paths())
	if _, err := store.Create(context.Background(), ItemDraft{Type: TypeStory, Title: "  "}); err == nil {
		t.Fatal("Create accepted an empty title")
	}
	if len(fsys.Paths()) != before {
		t.Errorf("a rejected create still wrote something: %v", fsys.Paths())
	}
}

func TestStoreUpdateAppliesASparsePatch(t *testing.T) {
	t.Parallel()

	store, _, clock := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{
		Type: TypeStory, Title: "Login with SSO", Author: "jose",
		Labels: []string{"auth"}, Body: "## Description\n\nFirst.",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	clock.advance(2 * time.Hour)

	estimate := 5.0
	priority := PriorityHigh
	updated, err := store.Update(ctx, it.ID, ItemPatch{
		Priority:     &priority,
		Estimate:     &estimate,
		AddLabels:    []string{"security", "auth"},
		RemoveLabels: []string{"nothing"},
		AddAssignees: []string{"marta"},
		AddLinks:     []Link{{Kind: LinkBlocks, Target: "ACME-T-0009"}},
		BodyAppend:   "## Notes\n\nAppended.",
	}, it.Rev)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Priority != PriorityHigh || updated.Estimate == nil || *updated.Estimate != 5 {
		t.Errorf("scalars: priority=%s estimate=%v", updated.Priority, updated.Estimate)
	}
	if strings.Join(updated.Labels, ",") != "auth,security" {
		t.Errorf("labels = %v, want [auth security] with no duplicate", updated.Labels)
	}
	if strings.Join(updated.Assignees, ",") != "jose,marta" {
		t.Errorf("assignees = %v", updated.Assignees)
	}
	if len(updated.Links) != 1 || updated.Links[0].Target != "ACME-T-0009" {
		t.Errorf("links = %+v", updated.Links)
	}
	if !strings.HasSuffix(updated.Body, "Appended.") || !strings.Contains(updated.Body, "First.") {
		t.Errorf("body = %q", updated.Body)
	}
	if updated.Updated.String() != "2026-09-03T14:00:00Z" {
		t.Errorf("updated = %s, want the advanced clock", updated.Updated)
	}
	if updated.Created != it.Created {
		t.Errorf("created changed: %s -> %s", it.Created, updated.Created)
	}
	if updated.Rev == it.Rev {
		t.Error("rev did not change after a write")
	}

	// Unsetting clears fields, and a body replacement wins over the old one.
	body := "## Description\n\nReplaced."
	cleared, err := store.Update(ctx, it.ID, ItemPatch{
		Body:  &body,
		Unset: []string{"estimate", "labels"},
	}, updated.Rev)
	if err != nil {
		t.Fatalf("Update unset: %v", err)
	}
	if cleared.Estimate != nil || cleared.Labels != nil {
		t.Errorf("unset left estimate=%v labels=%v", cleared.Estimate, cleared.Labels)
	}
	if cleared.Body != body {
		t.Errorf("body = %q, want %q", cleared.Body, body)
	}
	if _, err := store.Update(ctx, it.ID, ItemPatch{Unset: []string{"id"}}, cleared.Rev); err == nil {
		t.Error("unsetting the id was accepted")
	}
}

func TestStoreUpdateRejectsAStaleRevision(t *testing.T) {
	t.Parallel()

	store, _, clock := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	stale := it.Rev
	clock.advance(time.Minute)
	title := "Login with SSO everywhere"
	fresh, err := store.Update(ctx, it.ID, ItemPatch{Title: &title}, stale)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	clock.advance(time.Minute)
	other := "Someone else's title"
	_, err = store.Update(ctx, it.ID, ItemPatch{Title: &other}, stale)
	if !errors.Is(err, ErrStaleRevision) || !errors.Is(err, ErrRevMismatch) {
		t.Fatalf("error = %v, want ErrStaleRevision", err)
	}
	var conflict *StaleRevisionError
	if !errors.As(err, &conflict) {
		t.Fatalf("error %T does not carry the current rev", err)
	}
	if conflict.Current != fresh.Rev || conflict.Expected != stale {
		t.Errorf("conflict = %+v, want current %s expected %s", conflict, fresh.Rev, stale)
	}
	if conflict.Code() != StaleRevisionCode {
		t.Errorf("code = %q, want %q", conflict.Code(), StaleRevisionCode)
	}

	// The rejected write must not have touched the file.
	current, err := store.Get(ctx, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Title != title {
		t.Errorf("title = %q, want the accepted %q", current.Title, title)
	}
}

func TestStoreUpdateRenamesOnTitleChange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		rename   bool
		wantPath string
	}{
		{"renaming on", true, "docs/.pmngr/stories/ACME-US-0001-login-with-entra-id.md"},
		{"renaming off", false, "docs/.pmngr/stories/ACME-US-0001-login-with-sso.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, fsys, _ := newTestStore(t)
			store.RenameOnTitleChange = tc.rename
			ctx := context.Background()
			it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			title := "Login with Entra ID"
			updated, err := store.Update(ctx, it.ID, ItemPatch{Title: &title}, it.Rev)
			if err != nil {
				t.Fatalf("Update: %v", err)
			}
			if updated.Path != tc.wantPath {
				t.Errorf("path = %s, want %s", updated.Path, tc.wantPath)
			}
			if updated.ID != it.ID {
				t.Errorf("the id changed with the file name: %s", updated.ID)
			}
			if tc.rename {
				if _, err := fsys.Stat(it.Path); !errors.Is(err, ErrNotExist) {
					t.Errorf("the old file %s survived the rename", it.Path)
				}
			}
			if _, err := store.Get(ctx, it.ID); err != nil {
				t.Errorf("Get after rename: %v", err)
			}
		})
	}
}

func TestStoreDelete(t *testing.T) {
	t.Parallel()

	t.Run("soft delete keeps the file", func(t *testing.T) {
		t.Parallel()
		store, fsys, _ := newTestStore(t)
		ctx := context.Background()
		it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Delete(ctx, it.ID, it.Rev); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		data, err := fsys.ReadFile(it.Path)
		if err != nil {
			t.Fatalf("the file is gone after a soft delete: %v", err)
		}
		if !strings.Contains(string(data), "deleted: true") {
			t.Errorf("the file is not flagged deleted:\n%s", data)
		}
		got, err := store.Get(ctx, it.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !got.Deleted {
			t.Error("deleted was not parsed back")
		}
	})

	t.Run("hard delete removes the file", func(t *testing.T) {
		t.Parallel()
		store, fsys, _ := newTestStore(t)
		ctx := context.Background()
		it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.DeleteWith(ctx, it.ID, it.Rev, DeleteOptions{Hard: true}); err != nil {
			t.Fatalf("DeleteWith: %v", err)
		}
		if _, err := fsys.Stat(it.Path); !errors.Is(err, ErrNotExist) {
			t.Errorf("the file survived a hard delete")
		}
		if _, err := store.Get(ctx, it.ID); !errors.Is(err, ErrItemNotFound) {
			t.Errorf("Get after a hard delete = %v, want ErrItemNotFound", err)
		}
	})

	t.Run("a stale rev is refused", func(t *testing.T) {
		t.Parallel()
		store, _, _ := newTestStore(t)
		ctx := context.Background()
		it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Delete(ctx, it.ID, "sha256:0000000000000000"); !errors.Is(err, ErrStaleRevision) {
			t.Fatalf("Delete = %v, want ErrStaleRevision", err)
		}
	})
}

func TestStoreMoveStampsTimestamps(t *testing.T) {
	t.Parallel()

	store, _, clock := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !it.Started.IsZero() || !it.Closed.IsZero() {
		t.Fatalf("a new backlog item is already started or closed: %+v", it)
	}

	steps := []struct {
		to          Status
		wantStarted string
		wantClosed  string
	}{
		{"todo", "", ""},
		{"in_progress", "2026-09-03T14:00:00Z", ""},
		{"in_review", "2026-09-03T14:00:00Z", ""},
		{"done", "2026-09-03T14:00:00Z", "2026-09-03T16:00:00Z"},
		{"in_progress", "2026-09-03T14:00:00Z", ""},
	}
	current := it
	for _, step := range steps {
		clock.advance(time.Hour)
		moved, err := store.Move(ctx, current.ID, step.to, current.Rev)
		if err != nil {
			t.Fatalf("Move to %s: %v", step.to, err)
		}
		if moved.Status != step.to {
			t.Fatalf("status = %s, want %s", moved.Status, step.to)
		}
		if moved.Started.String() != step.wantStarted {
			t.Errorf("after %s: started = %q, want %q", step.to, moved.Started, step.wantStarted)
		}
		if moved.Closed.String() != step.wantClosed {
			t.Errorf("after %s: closed = %q, want %q", step.to, moved.Closed, step.wantClosed)
		}
		if moved.Updated.String() != NewTimestamp(clock.now).String() {
			t.Errorf("after %s: updated = %s, want %s", step.to, moved.Updated, NewTimestamp(clock.now))
		}
		current = moved
	}
}

func TestStoreMoveEnforcesTheWorkflow(t *testing.T) {
	t.Parallel()

	store, _, _ := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("an undeclared status is an error", func(t *testing.T) {
		_, err := store.Move(ctx, it.ID, "shipped", it.Rev)
		if err == nil {
			t.Fatal("Move accepted an unknown status")
		}
		var d *DiagnosticError
		if !errors.As(err, &d) || d.Diagnostic.Code != CodeStatusUnknown {
			t.Fatalf("error = %v, want E-STATUS-UNKNOWN", err)
		}
	})

	t.Run("a transition outside the workflow is refused", func(t *testing.T) {
		_, err := store.Move(ctx, it.ID, "done", it.Rev)
		if !errors.Is(err, ErrTransitionDenied) {
			t.Fatalf("error = %v, want ErrTransitionDenied", err)
		}
		var denied *TransitionError
		if !errors.As(err, &denied) || denied.From != "backlog" || denied.To != "done" {
			t.Fatalf("error = %+v", err)
		}
		if denied.Code() != TransitionDeniedCode {
			t.Errorf("code = %q", denied.Code())
		}
	})

	t.Run("force bypasses the transitions", func(t *testing.T) {
		moved, err := store.MoveWith(ctx, it.ID, "done", it.Rev, MoveOptions{Force: true})
		if err != nil {
			t.Fatalf("MoveWith(force): %v", err)
		}
		if moved.Status != "done" || moved.Closed.IsZero() {
			t.Errorf("forced move = %+v", moved)
		}
	})
}

func TestStoreGetRaw(t *testing.T) {
	t.Parallel()

	store, _, _ := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{
		Type: TypeStory, Title: "Login with SSO", Author: "jose", Body: "## Description\n\nBody.",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	fm, body, rev, err := store.GetRaw(ctx, it.ID)
	if err != nil {
		t.Fatalf("GetRaw: %v", err)
	}
	if !strings.Contains(string(fm), "id: ACME-US-0001") || strings.Contains(string(fm), "---") {
		t.Errorf("front matter = %q", fm)
	}
	if string(body) != it.Body {
		t.Errorf("body = %q, want %q", body, it.Body)
	}
	if rev != it.Rev {
		t.Errorf("rev = %s, want %s", rev, it.Rev)
	}
}

func TestStoreLookupFollowsRedirectsAndReportsDuplicates(t *testing.T) {
	t.Parallel()

	fsys := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fsys)
	cfg.IDAllocation.Redirects = map[ItemID]ItemID{"ACME-US-0999": "ACME-T-0107"}
	store := NewStore(fsys, "docs/.pmngr", cfg)
	ctx := context.Background()

	if _, err := store.Get(ctx, "ACME-US-0043"); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("Get on a duplicated id = %v, want ErrDuplicateID", err)
	}
	got, err := store.Get(ctx, "ACME-US-0999")
	if err != nil {
		t.Fatalf("Get through a redirect: %v", err)
	}
	if got.ID != "ACME-T-0107" {
		t.Errorf("redirect resolved to %s", got.ID)
	}
	if _, err := store.Get(ctx, "ACME-US-4242"); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("Get on an unknown id = %v, want ErrItemNotFound", err)
	}
}

func TestStorePages(t *testing.T) {
	t.Parallel()

	fsys := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fsys)
	store := NewStore(fsys, "docs", cfg)
	ctx := context.Background()

	page, err := store.ReadPage(ctx, "ACME", "architecture/sso-overview.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.Title != "SSO overview" || page.Project != "ACME" {
		t.Errorf("page = %+v", page)
	}
	if !strings.Contains(page.Body, "[[ACME-US-0043]]") {
		t.Errorf("body = %q", page.Body)
	}

	written, err := store.WritePage(ctx, "ACME", "architecture/sso-overview.md",
		[]byte("---\ntitle: SSO overview\n---\n\n# SSO overview\n\nRewritten.\n"), page.Rev)
	if err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if written.Rev == page.Rev {
		t.Error("the rev did not change after a page write")
	}
	if _, err := store.WritePage(ctx, "ACME", "architecture/sso-overview.md", []byte("x"), page.Rev); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("WritePage with a stale rev = %v, want ErrStaleRevision", err)
	}

	created, err := store.WritePage(ctx, "ACME", "notes/new-page.md", []byte("# New page\n"), "")
	if err != nil {
		t.Fatalf("WritePage(create): %v", err)
	}
	if created.Title != "New page" {
		t.Errorf("title = %q", created.Title)
	}
	if _, err := fsys.Stat(path.Join("docs", "notes", "new-page.md")); err != nil {
		t.Errorf("the page was not written where it belongs: %v", err)
	}

	for _, bad := range []string{"../outside.md", "/etc/passwd", ".pmngr/project.yaml", "."} {
		if _, err := store.ReadPage(ctx, "ACME", bad); err == nil {
			t.Errorf("ReadPage(%q) was accepted", bad)
		}
	}
	if _, err := store.ReadPage(ctx, "OTHER", "index.md"); err == nil {
		t.Error("ReadPage accepted a foreign project key")
	}
}

func TestStoreWritesAreAtomic(t *testing.T) {
	t.Parallel()

	// A failure while renaming must leave the previous file untouched and no
	// temporary file behind.
	store, fsys, _ := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	before, err := fsys.ReadFile(it.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	failing := &failingFS{FS: fsys, failRenameTo: it.Path}
	broken := NewStore(failing, "docs", store.cfg)
	broken.Clock = store.Clock
	broken.RenameOnTitleChange = false
	title := "Never written"
	if _, err := broken.Update(ctx, it.ID, ItemPatch{Title: &title}, it.Rev); err == nil {
		t.Fatal("Update succeeded over a failing rename")
	}
	after, err := fsys.ReadFile(it.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("the file changed despite the failed write:\n%s", after)
	}
	for _, p := range fsys.Paths() {
		if strings.HasSuffix(p, ".tmp") {
			t.Errorf("a temporary file survived: %s", p)
		}
	}
}

// failingFS wraps an FS and fails one operation, so that a test can watch the
// write path recover.
type failingFS struct {
	FS
	failRenameTo string
	failWriteTo  string
}

// Rename fails for the configured target and delegates otherwise.
func (f *failingFS) Rename(oldPath, newPath string) error {
	if newPath == f.failRenameTo {
		return errors.New("rename refused by the test")
	}
	return f.FS.Rename(oldPath, newPath)
}

// WriteFile fails for the configured target and delegates otherwise.
func (f *failingFS) WriteFile(p string, data []byte) error {
	if f.failWriteTo != "" && strings.TrimSuffix(p, ".tmp") == f.failWriteTo {
		return errors.New("write refused by the test")
	}
	return f.FS.WriteFile(p, data)
}

func TestStoreCreateRefusesAnIDThatIsAlreadyTaken(t *testing.T) {
	t.Parallel()

	store, _, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := store.Create(ctx, ItemDraft{
		ID: "ACME-US-0001", Type: TypeStory, Title: "A second claim", Author: "marta",
	})
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("Create with a taken id = %v, want ErrDuplicateID", err)
	}
}
