// Package osfs implements core.FS on top of the local file system.
//
// It is the native counterpart of the browser bridge in wasm/: it is the only
// place in the core's dependency tree that imports "os" and "path/filepath", so
// that internal/core itself stays compilable for GOOS=js GOARCH=wasm.
//
// Every path handed to an FS is vault-relative and uses forward slashes; this
// package translates it to a host path under its root and refuses anything that
// escapes it, in the spirit of os.Root.
package osfs

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// FS is a read/write file system rooted at a directory.
type FS struct {
	root     string
	readOnly bool
	// FileMode of created files and directories.
	fileMode os.FileMode
	dirMode  os.FileMode
}

// Option customizes an FS.
type Option func(*FS)

// ReadOnly makes every mutating operation fail with core.ErrReadOnly.
func ReadOnly() Option {
	return func(f *FS) { f.readOnly = true }
}

// New returns an FS rooted at dir. The directory must exist and be a directory.
func New(dir string) (*FS, error) {
	return NewWithOptions(dir)
}

// NewWithOptions returns an FS rooted at dir with the given options applied.
func NewWithOptions(dir string, opts ...Option) (*FS, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve root %s: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("open root %s: not a directory", dir)
	}
	f := &FS{root: abs, fileMode: 0o644, dirMode: 0o755}
	for _, o := range opts {
		o(f)
	}
	return f, nil
}

// Root returns the absolute host path the file system is rooted at.
func (f *FS) Root() string { return f.root }

// resolve turns a vault-relative path into a host path under the root.
func (f *FS) resolve(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute path %q", p)
	}
	clean := path.Clean(filepath.ToSlash(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q escapes the root", p)
	}
	if clean == "." {
		return f.root, nil
	}
	return filepath.Join(f.root, filepath.FromSlash(clean)), nil
}

// translate maps host errors onto the core sentinels.
func translate(op, p string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s %s: %w", op, p, core.ErrNotExist)
	}
	return fmt.Errorf("%s %s: %w", op, p, err)
}

// ReadFile returns the contents of the file at path.
func (f *FS) ReadFile(p string) ([]byte, error) {
	host, err := f.resolve(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	data, err := os.ReadFile(host) //nolint:gosec // the path is confined to the root by resolve
	if err != nil {
		return nil, translate("read", p, err)
	}
	return data, nil
}

// WriteFile creates or replaces a file, creating parent directories as needed.
// The write is atomic where the platform allows it: the bytes go to a temporary
// file in the destination directory which is then renamed into place, so a
// reader never observes a half-written item.
func (f *FS) WriteFile(p string, data []byte) error {
	if f.readOnly {
		return fmt.Errorf("write %s: %w", p, core.ErrReadOnly)
	}
	host, err := f.resolve(p)
	if err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	dir := filepath.Dir(host)
	if err := os.MkdirAll(dir, f.dirMode); err != nil {
		return translate("write", p, err)
	}
	tmp, err := os.CreateTemp(dir, ".gintrack-*.tmp")
	if err != nil {
		return translate("write", p, err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName) // no-op once the rename succeeded
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return translate("write", p, err)
	}
	if err := tmp.Close(); err != nil {
		return translate("write", p, err)
	}
	if err := os.Chmod(tmpName, f.fileMode); err != nil {
		return translate("write", p, err)
	}
	if err := os.Rename(tmpName, host); err != nil {
		return translate("write", p, err)
	}
	return nil
}

// Remove deletes a file or an empty directory.
func (f *FS) Remove(p string) error {
	if f.readOnly {
		return fmt.Errorf("remove %s: %w", p, core.ErrReadOnly)
	}
	host, err := f.resolve(p)
	if err != nil {
		return fmt.Errorf("remove %s: %w", p, err)
	}
	if err := os.Remove(host); err != nil {
		return translate("remove", p, err)
	}
	return nil
}

// Rename moves a file or a directory, creating the destination's parent.
func (f *FS) Rename(oldPath, newPath string) error {
	if f.readOnly {
		return fmt.Errorf("rename %s: %w", oldPath, core.ErrReadOnly)
	}
	from, err := f.resolve(oldPath)
	if err != nil {
		return fmt.Errorf("rename %s: %w", oldPath, err)
	}
	to, err := f.resolve(newPath)
	if err != nil {
		return fmt.Errorf("rename to %s: %w", newPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(to), f.dirMode); err != nil {
		return translate("rename", newPath, err)
	}
	if err := os.Rename(from, to); err != nil {
		return translate("rename", oldPath, err)
	}
	return nil
}

// Stat returns metadata about a file or directory.
func (f *FS) Stat(p string) (core.FileInfo, error) {
	host, err := f.resolve(p)
	if err != nil {
		return core.FileInfo{}, fmt.Errorf("stat %s: %w", p, err)
	}
	info, err := os.Stat(host)
	if err != nil {
		return core.FileInfo{}, translate("stat", p, err)
	}
	return core.FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}, nil
}

// ReadDir lists the direct children of a directory, sorted by name.
func (f *FS) ReadDir(p string) ([]core.DirEntry, error) {
	host, err := f.resolve(p)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", p, err)
	}
	entries, err := os.ReadDir(host)
	if err != nil {
		return nil, translate("read dir", p, err)
	}
	out := make([]core.DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, core.DirEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	return out, nil
}

// MkdirAll creates a directory and every missing parent.
func (f *FS) MkdirAll(p string) error {
	if f.readOnly {
		return fmt.Errorf("mkdir %s: %w", p, core.ErrReadOnly)
	}
	host, err := f.resolve(p)
	if err != nil {
		return fmt.Errorf("mkdir %s: %w", p, err)
	}
	if err := os.MkdirAll(host, f.dirMode); err != nil {
		return translate("mkdir", p, err)
	}
	return nil
}

// Ensure FS satisfies the interface at compile time.
var _ core.FS = (*FS)(nil)
