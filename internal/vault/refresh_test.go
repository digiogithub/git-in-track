package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// TestVaultRefreshesOnRead pins read-time freshness: a read that shows one
// item or one page re-reads the files behind it when they changed on disk, with
// no file event to announce the change, and reports what it folded in.
func TestVaultRefreshesOnRead(t *testing.T) {
	v, root := diskVault(t)
	var heard []core.IndexDelta
	v.OnRefresh(func(d core.IndexDelta) { heard = append(heard, d) })

	write := func(t *testing.T, rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s behind the vault's back: %v", rel, err)
		}
	}
	const story = "docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"

	t.Run("item.get serves an edit made on disk", func(t *testing.T) {
		heard = nil
		write(t, story, strings.Replace(onDisk(t, root, story), "title: Save payment methods", "title: Save cards on read", 1))
		item := decode[struct {
			Title string `json:"title"`
		}](t, call(t, v, "item.get", map[string]any{"id": "DEMO-US-0002"}))
		if item.Title != "Save cards on read" {
			t.Errorf("title = %q, want the edit made on disk", item.Title)
		}
		if len(heard) != 1 || len(heard[0].Updated) != 1 || heard[0].Updated[0] != "DEMO-US-0002" {
			t.Errorf("refresh hook heard %+v, want DEMO-US-0002 updated", heard)
		}
	})

	t.Run("an unchanged item is not re-read", func(t *testing.T) {
		heard = nil
		call(t, v, "item.get", map[string]any{"id": "DEMO-US-0002"})
		if len(heard) != 0 {
			t.Errorf("refresh hook heard %+v for an unchanged file", heard)
		}
	})

	t.Run("comment.list finds a comment written on disk", func(t *testing.T) {
		dir := "docs/.pmngr/comments/DEMO-US-0001"
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil || len(entries) == 0 {
			t.Fatalf("the fixture has no comment to copy: %v", err)
		}
		existing := onDisk(t, root, dir+"/"+entries[0].Name())
		write(t, dir+"/20260910T100000Z-disk.md", existing+"\nWritten on disk.\n")

		raw := call(t, v, "comment.list", map[string]any{"id": "DEMO-US-0001"})
		if !strings.Contains(string(raw), "Written on disk.") {
			t.Errorf("comment.list = %s, want the comment written on disk", raw)
		}
	})

	t.Run("kb.page serves an edit made on disk", func(t *testing.T) {
		const page = "docs/architecture/overview.md"
		write(t, page, onDisk(t, root, page)+"\n## Added on disk\n")
		raw := call(t, v, "kb.page", map[string]any{"path": page})
		if !strings.Contains(string(raw), "Added on disk") {
			t.Errorf("kb.page = %s, want the heading added on disk", raw)
		}
	})

	t.Run("a file removed on disk is dropped", func(t *testing.T) {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(story))); err != nil {
			t.Fatalf("remove the story: %v", err)
		}
		if _, err := v.Dispatch(context.Background(), "item.get", []byte(`{"id":"DEMO-US-0002"}`)); err == nil {
			t.Error("item.get still serves a story whose file is gone")
		}
		if v.Stats().Items != 4 {
			t.Errorf("items = %d, want 4", v.Stats().Items)
		}
	})
}
