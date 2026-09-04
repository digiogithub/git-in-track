package core

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// FS is the seam that lets the same code run over a directory on disk natively
// and over File System Access API handles in the browser. Paths are always
// vault-relative and use forward slashes, never the host separator.
//
// Implementations: osfs.FS (native, package internal/core/osfs), MemFS (tests and
// fixtures) and the JS bridge in wasm/.
type FS interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte) error
	Remove(path string) error
	Rename(old, new string) error //nolint:predeclared // signature fixed by docs/07 section 6.5
	Stat(path string) (FileInfo, error)
	ReadDir(path string) ([]DirEntry, error)
	MkdirAll(path string) error
}

// FileInfo is the subset of file metadata the core needs. It deliberately avoids
// io/fs.FileInfo so that the browser bridge does not have to fake a FileMode.
type FileInfo struct {
	Name    string    // base name
	Size    int64     // length in bytes, 0 for directories
	ModTime time.Time // last modification time, zero when the host cannot report it
	IsDir   bool
}

// DirEntry is one entry of a directory listing.
type DirEntry struct {
	Name  string
	IsDir bool
}

// MemFS is an in-memory FS used by tests and by fixtures. The zero value is not
// usable; call NewMemFS.
type MemFS struct {
	mu    sync.RWMutex
	files map[string][]byte
	dirs  map[string]bool
	times map[string]time.Time

	// Now supplies modification times. It defaults to time.Now.
	Now func() time.Time

	// ReadOnly makes every mutating operation fail with ErrReadOnly, mirroring a
	// browser mount obtained from <input webkitdirectory>.
	ReadOnly bool
}

// NewMemFS returns an empty in-memory file system.
func NewMemFS() *MemFS {
	return &MemFS{
		files: make(map[string][]byte),
		dirs:  map[string]bool{".": true},
		times: make(map[string]time.Time),
		Now:   time.Now,
	}
}

// NewMemFSFromMap returns an in-memory file system pre-populated with files.
// Parent directories are created implicitly.
func NewMemFSFromMap(files map[string]string) *MemFS {
	m := NewMemFS()
	for p, content := range files {
		if err := m.WriteFile(p, []byte(content)); err != nil {
			panic(fmt.Sprintf("core: seed MemFS with %s: %v", p, err))
		}
	}
	return m
}

// cleanPath normalises a vault-relative path and rejects escapes.
func cleanPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("absolute path %q", p)
	}
	c := path.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("path %q escapes the root", p)
	}
	return c, nil
}

func (m *MemFS) now() time.Time {
	if m.Now == nil {
		return time.Now()
	}
	return m.Now()
}

// ReadFile returns the contents of the file at path.
func (m *MemFS) ReadFile(p string) ([]byte, error) {
	c, err := cleanPath(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[c]
	if !ok {
		return nil, fmt.Errorf("read %s: %w", p, ErrNotExist)
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

// WriteFile creates or replaces the file at path, creating parent directories.
func (m *MemFS) WriteFile(p string, data []byte) error {
	c, err := cleanPath(p)
	if err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ReadOnly {
		return fmt.Errorf("write %s: %w", p, ErrReadOnly)
	}
	m.mkdirAllLocked(path.Dir(c))
	buf := make([]byte, len(data))
	copy(buf, data)
	m.files[c] = buf
	m.times[c] = m.now()
	return nil
}

// Remove deletes a file or an empty directory.
func (m *MemFS) Remove(p string) error {
	c, err := cleanPath(p)
	if err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ReadOnly {
		return fmt.Errorf("remove %s: %w", p, ErrReadOnly)
	}
	if _, ok := m.files[c]; ok {
		delete(m.files, c)
		delete(m.times, c)
		return nil
	}
	if m.dirs[c] {
		for f := range m.files {
			if strings.HasPrefix(f, c+"/") {
				return fmt.Errorf("remove %s: directory not empty", p)
			}
		}
		delete(m.dirs, c)
		return nil
	}
	return fmt.Errorf("remove %s: %w", p, ErrNotExist)
}

// Rename moves a file. Renaming a directory moves everything under it.
func (m *MemFS) Rename(oldPath, newPath string) error {
	from, err := cleanPath(oldPath)
	if err != nil {
		return fmt.Errorf("rename %s: %w", oldPath, err)
	}
	to, err := cleanPath(newPath)
	if err != nil {
		return fmt.Errorf("rename to %s: %w", newPath, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ReadOnly {
		return fmt.Errorf("rename %s: %w", oldPath, ErrReadOnly)
	}
	if data, ok := m.files[from]; ok {
		m.mkdirAllLocked(path.Dir(to))
		m.files[to] = data
		m.times[to] = m.now()
		delete(m.files, from)
		delete(m.times, from)
		return nil
	}
	if m.dirs[from] {
		prefix := from + "/"
		for f, data := range m.files {
			if !strings.HasPrefix(f, prefix) {
				continue
			}
			dst := to + "/" + strings.TrimPrefix(f, prefix)
			m.mkdirAllLocked(path.Dir(dst))
			m.files[dst] = data
			m.times[dst] = m.times[f]
			delete(m.files, f)
			delete(m.times, f)
		}
		m.mkdirAllLocked(to)
		delete(m.dirs, from)
		return nil
	}
	return fmt.Errorf("rename %s: %w", oldPath, ErrNotExist)
}

// Stat returns metadata about a file or directory.
func (m *MemFS) Stat(p string) (FileInfo, error) {
	c, err := cleanPath(p)
	if err != nil {
		return FileInfo{}, fmt.Errorf("stat %s: %w", p, err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if data, ok := m.files[c]; ok {
		return FileInfo{Name: path.Base(c), Size: int64(len(data)), ModTime: m.times[c]}, nil
	}
	if m.dirs[c] {
		return FileInfo{Name: path.Base(c), IsDir: true}, nil
	}
	return FileInfo{}, fmt.Errorf("stat %s: %w", p, ErrNotExist)
}

// ReadDir lists the direct children of a directory, sorted by name.
func (m *MemFS) ReadDir(p string) ([]DirEntry, error) {
	c, err := cleanPath(p)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", p, err)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.dirs[c] {
		return nil, fmt.Errorf("read dir %s: %w", p, ErrNotExist)
	}
	prefix := c + "/"
	if c == "." {
		prefix = ""
	}
	seen := make(map[string]bool)
	var entries []DirEntry
	add := func(name string, isDir bool) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		entries = append(entries, DirEntry{Name: name, IsDir: isDir})
	}
	for f := range m.files {
		rest, ok := childOf(f, prefix)
		if !ok {
			continue
		}
		name, nested := splitFirst(rest)
		add(name, nested)
	}
	for d := range m.dirs {
		if d == c {
			continue
		}
		rest, ok := childOf(d, prefix)
		if !ok {
			continue
		}
		name, _ := splitFirst(rest)
		add(name, true)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// MkdirAll creates a directory and every missing parent.
func (m *MemFS) MkdirAll(p string) error {
	c, err := cleanPath(p)
	if err != nil {
		return fmt.Errorf("mkdir %s: %w", p, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ReadOnly {
		return fmt.Errorf("mkdir %s: %w", p, ErrReadOnly)
	}
	m.mkdirAllLocked(c)
	return nil
}

func (m *MemFS) mkdirAllLocked(p string) {
	for p != "." && p != "/" && p != "" {
		m.dirs[p] = true
		p = path.Dir(p)
	}
	m.dirs["."] = true
}

// Paths returns every file path in the file system, sorted. It exists for tests.
func (m *MemFS) Paths() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.files))
	for f := range m.files {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// childOf reports whether p is inside prefix and returns the remainder.
func childOf(p, prefix string) (string, bool) {
	if prefix == "" {
		return p, true
	}
	if !strings.HasPrefix(p, prefix) {
		return "", false
	}
	return strings.TrimPrefix(p, prefix), true
}

// splitFirst returns the first path segment of rest and whether more follow.
func splitFirst(rest string) (string, bool) {
	if i := strings.Index(rest, "/"); i >= 0 {
		return rest[:i], true
	}
	return rest, false
}

// Ensure MemFS satisfies the interface at compile time.
var _ FS = (*MemFS)(nil)
