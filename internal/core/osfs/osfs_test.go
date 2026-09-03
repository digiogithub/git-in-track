package osfs_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

func newFS(t *testing.T) *osfs.FS {
	t.Helper()

	f, err := osfs.New(t.TempDir())
	if err != nil {
		t.Fatalf("osfs.New(): %v", err)
	}
	return f
}

func TestFSReadWrite(t *testing.T) {
	t.Parallel()

	f := newFS(t)
	const p = "docs/.pmngr/stories/ACME-US-0042-login.md"
	if err := f.WriteFile(p, []byte("hello")); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	data, err := f.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile(): %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
	if _, err := os.Stat(filepath.Join(f.Root(), "docs", ".pmngr", "stories", "ACME-US-0042-login.md")); err != nil {
		t.Errorf("the file is not on disk: %v", err)
	}
	info, err := f.Stat(p)
	if err != nil {
		t.Fatalf("Stat(): %v", err)
	}
	if info.Size != 5 || info.IsDir || info.ModTime.IsZero() {
		t.Errorf("info = %#v", info)
	}
	if _, err := f.ReadFile("missing.md"); !errors.Is(err, core.ErrNotExist) {
		t.Errorf("ReadFile(missing) = %v, want core.ErrNotExist", err)
	}
}

func TestFSWriteLeavesNoTempFiles(t *testing.T) {
	t.Parallel()

	f := newFS(t)
	if err := f.WriteFile("notes.md", []byte("a")); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := f.WriteFile("notes.md", []byte("bb")); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	entries, err := f.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "notes.md" {
		t.Errorf("entries = %#v, want only notes.md", entries)
	}
}

func TestFSRenameAndRemove(t *testing.T) {
	t.Parallel()

	f := newFS(t)
	if err := f.WriteFile("stories/old.md", []byte("x")); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if err := f.Rename("stories/old.md", "stories/renamed/new.md"); err != nil {
		t.Fatalf("Rename(): %v", err)
	}
	if _, err := f.ReadFile("stories/renamed/new.md"); err != nil {
		t.Errorf("ReadFile() after rename: %v", err)
	}
	if err := f.Remove("stories/renamed/new.md"); err != nil {
		t.Errorf("Remove(): %v", err)
	}
	if err := f.Remove("stories/renamed/new.md"); !errors.Is(err, core.ErrNotExist) {
		t.Errorf("Remove() twice = %v, want core.ErrNotExist", err)
	}
}

func TestFSRejectsEscapes(t *testing.T) {
	t.Parallel()

	f := newFS(t)
	for _, p := range []string{"", "/etc/passwd", "../outside.md", "a/../../outside.md"} {
		if err := f.WriteFile(p, []byte("x")); err == nil {
			t.Errorf("WriteFile(%q) succeeded, want an error", p)
		}
		if _, err := f.ReadFile(p); err == nil {
			t.Errorf("ReadFile(%q) succeeded, want an error", p)
		}
	}
}

func TestFSReadOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f, err := osfs.NewWithOptions(dir, osfs.ReadOnly())
	if err != nil {
		t.Fatalf("NewWithOptions(): %v", err)
	}
	if err := f.WriteFile("index.md", []byte("y")); !errors.Is(err, core.ErrReadOnly) {
		t.Errorf("WriteFile() = %v, want core.ErrReadOnly", err)
	}
	if data, err := f.ReadFile("index.md"); err != nil || string(data) != "x" {
		t.Errorf("reads must still work: %q, %v", data, err)
	}
}

func TestNewRejectsAFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := osfs.New(file); err == nil {
		t.Error("osfs.New() on a file succeeded, want an error")
	}
	if _, err := osfs.New(filepath.Join(dir, "missing")); err == nil {
		t.Error("osfs.New() on a missing directory succeeded, want an error")
	}
}
