package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// twoMounts returns a mount table with two in-memory repositories.
func twoMounts(t *testing.T) (*mountFS, *core.MemFS, *core.MemFS) {
	t.Helper()

	a := core.NewMemFSFromMap(map[string]string{"docs/.pmngr/project.yaml": "key: AAA\n"})
	b := core.NewMemFSFromMap(map[string]string{"docs/.pmngr/project.yaml": "key: BBB\n"})
	m := newMountFS()
	if err := m.mount("alpha", a); err != nil {
		t.Fatalf("mount alpha: %v", err)
	}
	if err := m.mount("beta", b); err != nil {
		t.Fatalf("mount beta: %v", err)
	}
	return m, a, b
}

func TestMountNameOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		wantName string
		wantRest string
	}{
		{in: ".", wantName: "", wantRest: "."},
		{in: "", wantName: "", wantRest: "."},
		{in: "alpha", wantName: "alpha", wantRest: "."},
		{in: "alpha/docs", wantName: "alpha", wantRest: "docs"},
		{in: "alpha/docs/.pmngr/x.md", wantName: "alpha", wantRest: "docs/.pmngr/x.md"},
		{in: "/alpha/docs", wantName: "alpha", wantRest: "docs"},
	}
	for _, tt := range tests {
		name, rest := mountNameOf(tt.in)
		if name != tt.wantName || rest != tt.wantRest {
			t.Errorf("mountNameOf(%q) = %q, %q, want %q, %q", tt.in, name, rest, tt.wantName, tt.wantRest)
		}
	}
}

func TestMountFSRootListsTheRepositories(t *testing.T) {
	t.Parallel()

	m, _, _ := twoMounts(t)
	entries, err := m.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 2 || entries[0].Name != "alpha" || entries[1].Name != "beta" {
		t.Fatalf("entries = %+v", entries)
	}
	for _, e := range entries {
		if !e.IsDir {
			t.Errorf("%s is not a directory", e.Name)
		}
	}
	if info, err := m.Stat("."); err != nil || !info.IsDir {
		t.Errorf("stat . = %+v, %v", info, err)
	}
	if info, err := m.Stat("alpha"); err != nil || info.Name != "alpha" {
		t.Errorf("stat alpha = %+v, %v", info, err)
	}
}

func TestMountFSRoutesToTheRightRepository(t *testing.T) {
	t.Parallel()

	m, a, b := twoMounts(t)
	data, err := m.ReadFile("alpha/docs/.pmngr/project.yaml")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "AAA") {
		t.Errorf("contents = %q", data)
	}
	if err := m.WriteFile("beta/docs/new.md", []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := b.ReadFile("docs/new.md"); err != nil {
		t.Errorf("the write did not reach beta: %v", err)
	}
	if _, err := a.ReadFile("docs/new.md"); err == nil {
		t.Error("the write reached alpha as well")
	}
}

func TestMountFSRefusesUnknownAndCrossRepositoryPaths(t *testing.T) {
	t.Parallel()

	m, _, _ := twoMounts(t)
	if _, err := m.ReadFile("gamma/x.md"); !errors.Is(err, core.ErrNotExist) {
		t.Errorf("read from an unmounted repository = %v", err)
	}
	if err := m.Rename("alpha/a.md", "beta/a.md"); err == nil {
		t.Error("a cross-repository rename was accepted")
	}
	if err := m.mount("alpha", core.NewMemFS()); err == nil {
		t.Error("a duplicate mount was accepted")
	}
	if err := m.mount("a/b", core.NewMemFS()); err == nil {
		t.Error("a mount name with a separator was accepted")
	}
}

func TestOverlayFSKeepsTheBaseUntouched(t *testing.T) {
	t.Parallel()

	base := core.NewMemFSFromMap(map[string]string{"docs/a.md": "original\n"})
	o := newOverlayFS(base)

	if err := o.WriteFile("docs/b.md", []byte("new\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := o.WriteFile("docs/a.md", []byte("changed\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := o.Remove("docs/a.md"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if data, _ := base.ReadFile("docs/a.md"); string(data) != "original\n" {
		t.Errorf("the base changed: %q", data)
	}
	if _, err := base.ReadFile("docs/b.md"); err == nil {
		t.Error("the base gained a file")
	}
	if _, err := o.ReadFile("docs/a.md"); !errors.Is(err, core.ErrNotExist) {
		t.Errorf("a removed file is still readable: %v", err)
	}

	changes := o.Changes()
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	if changes[0].Path != "docs/b.md" || string(changes[0].Data) != "new\n" {
		t.Errorf("first change = %+v", changes[0])
	}
	if changes[1].Path != "docs/a.md" || !changes[1].Removed {
		t.Errorf("second change = %+v", changes[1])
	}
}

func TestOverlayFSMergesDirectoryListings(t *testing.T) {
	t.Parallel()

	base := core.NewMemFSFromMap(map[string]string{"docs/a.md": "a\n", "docs/keep.md": "k\n"})
	o := newOverlayFS(base)
	if err := o.WriteFile("docs/b.md", []byte("b\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := o.Remove("docs/a.md"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	entries, err := o.ReadDir("docs")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "b.md,keep.md" {
		t.Errorf("entries = %v", names)
	}
}

func TestOverlayFSHidesIntermediateFiles(t *testing.T) {
	t.Parallel()

	base := core.NewMemFSFromMap(map[string]string{"docs/a.md": "a\n"})
	o := newOverlayFS(base)
	// This is what the store's atomic write does: a temporary file, then a
	// rename over the target.
	if err := o.WriteFile("docs/a.md.tmp", []byte("b\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := o.Rename("docs/a.md.tmp", "docs/a.md"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	changes := o.Changes()
	if len(changes) != 1 || changes[0].Path != "docs/a.md" || string(changes[0].Data) != "b\n" {
		t.Errorf("changes = %+v, want only the final file", changes)
	}
}
