package core

import (
	"errors"
	"testing"
)

func TestMemFSReadWrite(t *testing.T) {
	t.Parallel()

	m := NewMemFS()
	if err := m.WriteFile("docs/.pmngr/stories/ACME-US-0042-login.md", []byte("hello")); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	data, err := m.ReadFile("docs/.pmngr/stories/ACME-US-0042-login.md")
	if err != nil {
		t.Fatalf("ReadFile(): %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
	info, err := m.Stat("docs/.pmngr/stories/ACME-US-0042-login.md")
	if err != nil {
		t.Fatalf("Stat(): %v", err)
	}
	if info.IsDir || info.Size != 5 || info.Name != "ACME-US-0042-login.md" {
		t.Errorf("info = %#v", info)
	}
	if _, err := m.ReadFile("missing.md"); !errors.Is(err, ErrNotExist) {
		t.Errorf("ReadFile(missing) = %v, want ErrNotExist", err)
	}

	// Mutating a returned slice must not corrupt the stored file.
	data[0] = 'x'
	again, err := m.ReadFile("docs/.pmngr/stories/ACME-US-0042-login.md")
	if err != nil || string(again) != "hello" {
		t.Errorf("stored content was aliased: %q, %v", again, err)
	}
}

func TestMemFSReadDir(t *testing.T) {
	t.Parallel()

	m := NewMemFSFromMap(map[string]string{
		"docs/.pmngr/project.yaml":                  "schema: 1\n",
		"docs/.pmngr/stories/ACME-US-0042-login.md": "---\n---\n",
		"docs/.pmngr/stories/ACME-US-0043-reset.md": "---\n---\n",
		"docs/index.md":                             "# KB\n",
	})

	entries, err := m.ReadDir("docs/.pmngr")
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	want := []DirEntry{{Name: "project.yaml"}, {Name: "stories", IsDir: true}}
	if len(entries) != len(want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}
	for i, e := range entries {
		if e != want[i] {
			t.Errorf("entries[%d] = %#v, want %#v", i, e, want[i])
		}
	}

	root, err := m.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.): %v", err)
	}
	if len(root) != 1 || root[0].Name != "docs" || !root[0].IsDir {
		t.Errorf("root = %#v", root)
	}

	if _, err := m.ReadDir("nowhere"); !errors.Is(err, ErrNotExist) {
		t.Errorf("ReadDir(nowhere) = %v, want ErrNotExist", err)
	}
}

func TestMemFSRenameAndRemove(t *testing.T) {
	t.Parallel()

	m := NewMemFSFromMap(map[string]string{
		"stories/ACME-US-0042-old.md":        "body",
		"comments/ACME-US-0042/one.md":       "a",
		"comments/ACME-US-0042/two.md":       "b",
		"comments/ACME-US-0043/unrelated.md": "c",
	})

	if err := m.Rename("stories/ACME-US-0042-old.md", "stories/ACME-US-0042-new.md"); err != nil {
		t.Fatalf("Rename(): %v", err)
	}
	if _, err := m.ReadFile("stories/ACME-US-0042-old.md"); !errors.Is(err, ErrNotExist) {
		t.Error("the old path still exists")
	}
	if data, err := m.ReadFile("stories/ACME-US-0042-new.md"); err != nil || string(data) != "body" {
		t.Errorf("new path = %q, %v", data, err)
	}

	if err := m.Rename("comments/ACME-US-0042", "comments/ACME-US-0044"); err != nil {
		t.Fatalf("Rename(dir): %v", err)
	}
	if _, err := m.ReadFile("comments/ACME-US-0044/two.md"); err != nil {
		t.Errorf("renaming a folder must move its files: %v", err)
	}
	if _, err := m.ReadFile("comments/ACME-US-0043/unrelated.md"); err != nil {
		t.Errorf("a sibling folder was touched: %v", err)
	}

	if err := m.Remove("comments/ACME-US-0044"); err == nil {
		t.Error("removing a non-empty directory must fail")
	}
	if err := m.Remove("stories/ACME-US-0042-new.md"); err != nil {
		t.Errorf("Remove(): %v", err)
	}
	if err := m.Remove("stories/ACME-US-0042-new.md"); !errors.Is(err, ErrNotExist) {
		t.Errorf("removing twice = %v, want ErrNotExist", err)
	}
}

func TestMemFSRejectsEscapes(t *testing.T) {
	t.Parallel()

	m := NewMemFS()
	for _, p := range []string{"", "/etc/passwd", "../outside.md", "docs/../../outside.md"} {
		if err := m.WriteFile(p, []byte("x")); err == nil {
			t.Errorf("WriteFile(%q) succeeded, want an error", p)
		}
		if _, err := m.ReadFile(p); err == nil {
			t.Errorf("ReadFile(%q) succeeded, want an error", p)
		}
	}
	// A relative path that stays inside the root is fine.
	if err := m.WriteFile("docs/../index.md", []byte("x")); err != nil {
		t.Errorf("WriteFile(docs/../index.md): %v", err)
	}
	if paths := m.Paths(); len(paths) != 1 || paths[0] != "index.md" {
		t.Errorf("paths = %v, want [index.md]", paths)
	}
}

func TestMemFSReadOnly(t *testing.T) {
	t.Parallel()

	m := NewMemFSFromMap(map[string]string{"index.md": "x"})
	m.ReadOnly = true
	if err := m.WriteFile("index.md", []byte("y")); !errors.Is(err, ErrReadOnly) {
		t.Errorf("WriteFile() = %v, want ErrReadOnly", err)
	}
	if err := m.Remove("index.md"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Remove() = %v, want ErrReadOnly", err)
	}
	if err := m.MkdirAll("a/b"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("MkdirAll() = %v, want ErrReadOnly", err)
	}
	if _, err := m.ReadFile("index.md"); err != nil {
		t.Errorf("reads must still work: %v", err)
	}
}

func TestMemFSMkdirAll(t *testing.T) {
	t.Parallel()

	m := NewMemFS()
	if err := m.MkdirAll("docs/.pmngr/comments/ACME-US-0042"); err != nil {
		t.Fatalf("MkdirAll(): %v", err)
	}
	info, err := m.Stat("docs/.pmngr/comments")
	if err != nil {
		t.Fatalf("Stat(): %v", err)
	}
	if !info.IsDir {
		t.Error("intermediate directories must exist")
	}
}
