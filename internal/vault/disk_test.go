package vault

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// copyFixture copies the fixture vault into a fresh temporary directory and
// returns it. The tests here mutate the vault, so they must never touch the
// files under testdata/.
func copyFixture(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p) //nolint:gosec // fixture paths come from the walk itself
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644) //nolint:gosec // a fixture copy, not a secret
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return dst
}

// diskVault opens a copy of the fixture through osfs, with a pinned clock so
// that created and updated stamps are reproducible. It returns the vault and
// the host directory it is rooted at.
func diskVault(t *testing.T) (*Vault, string) {
	t.Helper()
	root := copyFixture(t, fixtureRoot)
	fsys, err := osfs.New(root)
	if err != nil {
		t.Fatalf("open %s: %v", root, err)
	}
	v, err := New(Options{
		FS:      fsys,
		Root:    "project-basic",
		Version: "test",
		Now:     func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) },
		Scan:    true,
	})
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	return v, root
}

// onDisk reads a vault-relative path from the host directory.
func onDisk(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // test-owned temp dir
	if err != nil {
		t.Fatalf("read %s from disk: %v", rel, err)
	}
	return string(data)
}

func TestVaultOpenFromDisk(t *testing.T) {
	root := copyFixture(t, fixtureRoot)
	fsys, err := osfs.New(root)
	if err != nil {
		t.Fatalf("open %s: %v", root, err)
	}
	v, err := Open(fsys, "project-basic")
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}

	stats := v.Stats()
	if stats.Projects != 1 || stats.Items != 5 || stats.Pages != 2 || stats.Comments != 1 {
		t.Errorf("stats = %+v, want the same vault the browser sees", stats)
	}
	if stats.Errors != 0 {
		t.Errorf("errors = %d: %+v", stats.Errors, stats.Diagnostics)
	}
	if v.Root() != "project-basic" {
		t.Errorf("root = %q, want project-basic", v.Root())
	}
	projects := v.Projects()
	if len(projects) != 1 || projects[0].Key != "DEMO" {
		t.Fatalf("projects = %+v, want DEMO", projects)
	}
	if projects[0].DocsPath != "docs" {
		t.Errorf("docsPath = %q, want docs", projects[0].DocsPath)
	}

	t.Run("the same queries answer over disk", func(t *testing.T) {
		page := decode[itemPage](t, call(t, v, "item.list", map[string]any{}))
		if page.Total != 5 {
			t.Errorf("total = %d, want 5", page.Total)
		}
		hits := decode[[]searchHit](t, call(t, v, "search", map[string]any{"q": "checkout"}))
		if len(hits) == 0 {
			t.Error("search returned nothing for a term the fixture uses")
		}
	})

	t.Run("a disk vault refuses vault.load", func(t *testing.T) {
		env := rawCall(t, v, "vault.load", map[string]any{"files": []map[string]string{}})
		if env.OK || env.Error.Code != "invalid_request" {
			t.Errorf("got ok=%v code=%q, want invalid_request", env.OK, env.Error.Code)
		}
		if v.Stats().Items != 5 {
			t.Error("a refused vault.load must leave the index alone")
		}
	})
}

func TestVaultDiskWrites(t *testing.T) {
	v, root := diskVault(t)
	const created = "docs/.pmngr/tasks/DEMO-T-0002-trim-pasted-addresses.md"

	result := decode[struct {
		Item struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			Rev    string `json:"rev"`
			Status string `json:"status"`
		} `json:"item"`
		Writes WriteSet `json:"writes"`
	}](t, call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "task", "title": "Trim pasted addresses",
		"parent": "DEMO-US-0001", "author": "claude",
		"body": "## Description\n\nTrim the address before validating it.\n",
	}))

	if result.Item.ID != "DEMO-T-0002" || result.Item.Path != created {
		t.Fatalf("created %s at %s, want DEMO-T-0002 at %s", result.Item.ID, result.Item.Path, created)
	}

	t.Run("the file is on disk, not only in the WriteSet", func(t *testing.T) {
		text := onDisk(t, root, created)
		if !strings.Contains(text, "id: DEMO-T-0002") ||
			!strings.Contains(text, "title: Trim pasted addresses") ||
			!strings.Contains(text, "Trim the address before validating it.") {
			t.Errorf("the file on disk does not carry the item:\n%s", text)
		}
		if !strings.Contains(onDisk(t, root, "docs/.pmngr/project.yaml"), "task: 2") {
			t.Error("write_counters is on: the counter on disk must have moved")
		}
	})

	t.Run("the WriteSet still reports what changed", func(t *testing.T) {
		written := map[string]string{}
		for _, f := range result.Writes.Written {
			written[f.Path] = f.Text
		}
		if _, ok := written[created]; !ok {
			t.Fatalf("writes = %+v, want the new file", result.Writes.Written)
		}
		if written[created] != onDisk(t, root, created) {
			t.Error("the WriteSet and the file on disk disagree")
		}
		if _, ok := written["docs/.pmngr/project.yaml"]; !ok {
			t.Error("project.yaml moved on disk, so it belongs in the WriteSet")
		}
		for p := range written {
			if strings.HasSuffix(p, ".tmp") {
				t.Errorf("%s: an atomic write temporary must never be reported", p)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "docs", ".pmngr", "tasks")); err != nil {
			t.Errorf("stat the tasks folder: %v", err)
		}
	})

	t.Run("a stale rev is refused and the file is untouched", func(t *testing.T) {
		before := onDisk(t, root, created)
		env := rawCall(t, v, "item.update", map[string]any{
			"id":    "DEMO-T-0002",
			"rev":   "sha256:0000000000000000",
			"patch": map[string]any{"set": map[string]any{"title": "Nope"}},
		})
		if env.OK {
			t.Fatal("an update with a stale rev must fail")
		}
		if env.Error.Code != "stale_revision" {
			t.Errorf("code = %q, want stale_revision", env.Error.Code)
		}
		if env.Error.Path == "" {
			t.Error("a stale revision must name the file it is about")
		}
		if onDisk(t, root, created) != before {
			t.Error("a refused write must not reach the disk")
		}
	})

	t.Run("move rewrites the status on disk", func(t *testing.T) {
		moved := decode[struct {
			Item struct {
				Status string `json:"status"`
			} `json:"item"`
		}](t, call(t, v, "item.move", map[string]any{
			"id": "DEMO-T-0002", "status": "in_progress", "rev": result.Item.Rev,
		}))
		if moved.Item.Status != "in_progress" {
			t.Fatalf("status = %q, want in_progress", moved.Item.Status)
		}
		if !strings.Contains(onDisk(t, root, created), "status: in_progress") {
			t.Error("the status change never reached the file")
		}
	})

	t.Run("a comment lands in the thread folder", func(t *testing.T) {
		added := decode[struct {
			Comment struct {
				Author string `json:"author"`
				Path   string `json:"path"`
			} `json:"comment"`
			Writes WriteSet `json:"writes"`
		}](t, call(t, v, "comment.add", map[string]any{
			"id": "DEMO-US-0001", "author": "Claude", "body": "Trimming lands in DEMO-T-0002.",
		}))
		if !strings.HasPrefix(added.Comment.Path, "docs/.pmngr/comments/DEMO-US-0001/") {
			t.Fatalf("comment path = %s", added.Comment.Path)
		}
		if !strings.Contains(onDisk(t, root, added.Comment.Path), "Trimming lands in DEMO-T-0002.") {
			t.Error("the comment body is not on disk")
		}
		thread := decode[[]json.RawMessage](t, call(t, v, "comment.list", map[string]any{"id": "DEMO-US-0001"}))
		if len(thread) != 2 {
			t.Errorf("the thread has %d comments, want 2", len(thread))
		}
	})

	t.Run("a knowledge base page is created on disk", func(t *testing.T) {
		page := decode[struct {
			Page kbPageResult `json:"page"`
		}](t, call(t, v, "kb.write", map[string]any{
			"path": "docs/runbooks/deploy.md",
			"text": "---\ntitle: Deploy\n---\n\nSee [[DEMO-EP-0001]].\n",
		}))
		if page.Page.Title != "Deploy" {
			t.Errorf("title = %q, want Deploy", page.Page.Title)
		}
		if !strings.Contains(onDisk(t, root, "docs/runbooks/deploy.md"), "See [[DEMO-EP-0001]].") {
			t.Error("the page is not on disk")
		}
		if v.Stats().Pages != 3 {
			t.Errorf("pages = %d, want 3 after the write", v.Stats().Pages)
		}
	})

	t.Run("a hard delete removes the file from disk", func(t *testing.T) {
		current := decode[struct {
			Rev string `json:"rev"`
		}](t, call(t, v, "item.get", map[string]any{"id": "DEMO-T-0002"}))
		call(t, v, "item.delete", map[string]any{
			"id": "DEMO-T-0002", "rev": current.Rev, "hard": true,
		})
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(created))); !os.IsNotExist(err) {
			t.Errorf("stat after a hard delete = %v, want the file to be gone", err)
		}
	})
}

func TestVaultDiskExternalChanges(t *testing.T) {
	v, root := diskVault(t)
	const story = "docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"

	t.Run("ApplyEvents re-reads the file from disk", func(t *testing.T) {
		patched := strings.Replace(onDisk(t, root, story), "title: Save payment methods", "title: Save cards", 1)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(story)), []byte(patched), 0o644); err != nil {
			t.Fatalf("edit the story behind the vault's back: %v", err)
		}
		// The event carries no content: on disk the file is the source of truth.
		delta, err := v.ApplyEvents(context.Background(), []core.FileEvent{{Kind: core.FileModified, Path: story}})
		if err != nil {
			t.Fatalf("apply events: %v", err)
		}
		if len(delta.Updated) != 1 || delta.Updated[0] != "DEMO-US-0002" {
			t.Fatalf("delta = %+v, want DEMO-US-0002 updated", delta)
		}
		item := decode[struct {
			Title string `json:"title"`
		}](t, call(t, v, "item.get", map[string]any{"id": "DEMO-US-0002"}))
		if item.Title != "Save cards" {
			t.Errorf("title = %q, want the edit made on disk", item.Title)
		}
	})

	t.Run("a removal drops the item", func(t *testing.T) {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(story))); err != nil {
			t.Fatalf("remove the story: %v", err)
		}
		delta, err := v.ApplyEvents(context.Background(), []core.FileEvent{{Kind: core.FileRemoved, Path: story}})
		if err != nil {
			t.Fatalf("apply events: %v", err)
		}
		if len(delta.Removed) != 1 {
			t.Errorf("delta = %+v, want one removed item", delta)
		}
		if v.Stats().Items != 4 {
			t.Errorf("items = %d, want 4", v.Stats().Items)
		}
	})

	t.Run("Reload picks up a file nobody announced", func(t *testing.T) {
		added := filepath.Join(root, "docs", ".pmngr", "tasks", "DEMO-T-0009-rescan.md")
		body := "---\nid: DEMO-T-0009\ntype: task\ntitle: Rescan\nstatus: todo\nparent: DEMO-US-0001\n---\n\n## Description\n\nAdded behind the vault's back.\n"
		if err := os.WriteFile(added, []byte(body), 0o644); err != nil {
			t.Fatalf("add a task on disk: %v", err)
		}
		stats, err := v.Reload(context.Background())
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if stats.Items != 5 {
			t.Errorf("items = %d, want 5 after the rescan", stats.Items)
		}
		item := decode[struct {
			Title string `json:"title"`
		}](t, call(t, v, "item.get", map[string]any{"id": "DEMO-T-0009"}))
		if item.Title != "Rescan" {
			t.Errorf("item.get = %+v, want the task found by the rescan", item)
		}
	})
}
