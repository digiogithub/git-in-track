package main

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// mountFS presents several repositories as one vault, each under a directory
// named after its repository id: "acme-api/docs/.pmngr/stories/…".
//
// The core indexes and queries one file system at a time, and the registered
// repositories live at unrelated places on disk; mounting them side by side is
// what lets a single core.Index sort, filter and paginate across all of them
// instead of the command line stitching per-repository results together.
type mountFS struct {
	names  []string
	mounts map[string]core.FS
}

// newMountFS returns an empty mount table.
func newMountFS() *mountFS {
	return &mountFS{mounts: make(map[string]core.FS)}
}

// mount adds a repository under a name. The name must be a single path segment.
func (m *mountFS) mount(name string, fsys core.FS) error {
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return fmt.Errorf("mount %q: the name must be a single path segment", name)
	}
	if _, taken := m.mounts[name]; taken {
		return fmt.Errorf("mount %q: already mounted", name)
	}
	m.mounts[name] = fsys
	m.names = append(m.names, name)
	sort.Strings(m.names)
	return nil
}

// mountNameOf returns the mount a vault path belongs to and the path inside it.
func mountNameOf(p string) (name, rest string) {
	clean := path.Clean(strings.TrimPrefix(path.Clean("/"+p), "/"))
	if clean == "" || clean == "." {
		return "", "."
	}
	name, rest, found := strings.Cut(clean, "/")
	if !found || rest == "" {
		return name, "."
	}
	return name, rest
}

// resolve maps a vault path onto the mount that holds it.
func (m *mountFS) resolve(op, p string) (core.FS, string, error) {
	name, rest := mountNameOf(p)
	if name == "" {
		return nil, "", fmt.Errorf("%s %s: %w", op, p, core.ErrNotExist)
	}
	fsys, ok := m.mounts[name]
	if !ok {
		return nil, "", fmt.Errorf("%s %s: no repository %q: %w", op, p, name, core.ErrNotExist)
	}
	return fsys, rest, nil
}

// ReadFile returns the contents of a file.
func (m *mountFS) ReadFile(p string) ([]byte, error) {
	fsys, rest, err := m.resolve("read", p)
	if err != nil {
		return nil, err
	}
	data, err := fsys.ReadFile(rest)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return data, nil
}

// WriteFile creates or replaces a file.
func (m *mountFS) WriteFile(p string, data []byte) error {
	fsys, rest, err := m.resolve("write", p)
	if err != nil {
		return err
	}
	if err := fsys.WriteFile(rest, data); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// Remove deletes a file or an empty directory.
func (m *mountFS) Remove(p string) error {
	fsys, rest, err := m.resolve("remove", p)
	if err != nil {
		return err
	}
	if err := fsys.Remove(rest); err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	return nil
}

// Rename moves a file. Moving between repositories is refused: it would be a
// copy plus a delete, which is not what a caller of Rename expects.
func (m *mountFS) Rename(oldPath, newPath string) error {
	fromName, fromRest := mountNameOf(oldPath)
	toName, toRest := mountNameOf(newPath)
	if fromName != toName {
		return fmt.Errorf("rename %s to %s: the paths are in different repositories", oldPath, newPath)
	}
	fsys, ok := m.mounts[fromName]
	if !ok {
		return fmt.Errorf("rename %s: no repository %q: %w", oldPath, fromName, core.ErrNotExist)
	}
	if err := fsys.Rename(fromRest, toRest); err != nil {
		return fmt.Errorf("rename %s: %w", oldPath, err)
	}
	return nil
}

// Stat returns metadata about a file or directory.
func (m *mountFS) Stat(p string) (core.FileInfo, error) {
	name, rest := mountNameOf(p)
	if name == "" {
		return core.FileInfo{Name: ".", IsDir: true}, nil
	}
	fsys, ok := m.mounts[name]
	if !ok {
		return core.FileInfo{}, fmt.Errorf("stat %s: no repository %q: %w", p, name, core.ErrNotExist)
	}
	info, err := fsys.Stat(rest)
	if err != nil {
		return core.FileInfo{}, fmt.Errorf("stat %s: %w", p, err)
	}
	if rest == "." {
		info.Name = name
	}
	return info, nil
}

// ReadDir lists a directory. The vault root lists the mounted repositories.
func (m *mountFS) ReadDir(p string) ([]core.DirEntry, error) {
	name, rest := mountNameOf(p)
	if name == "" {
		out := make([]core.DirEntry, 0, len(m.names))
		for _, n := range m.names {
			out = append(out, core.DirEntry{Name: n, IsDir: true})
		}
		return out, nil
	}
	fsys, ok := m.mounts[name]
	if !ok {
		return nil, fmt.Errorf("read dir %s: no repository %q: %w", p, name, core.ErrNotExist)
	}
	entries, err := fsys.ReadDir(rest)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", p, err)
	}
	return entries, nil
}

// MkdirAll creates a directory and every missing parent.
func (m *mountFS) MkdirAll(p string) error {
	name, rest := mountNameOf(p)
	if name == "" {
		return nil
	}
	fsys, ok := m.mounts[name]
	if !ok {
		return fmt.Errorf("mkdir %s: no repository %q: %w", p, name, core.ErrNotExist)
	}
	if err := fsys.MkdirAll(rest); err != nil {
		return fmt.Errorf("mkdir %s: %w", p, err)
	}
	return nil
}

// Ensure mountFS satisfies the core interface at compile time.
var _ core.FS = (*mountFS)(nil)
