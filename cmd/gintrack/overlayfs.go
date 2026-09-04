package main

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// overlayFS buffers writes in memory over a read-only view of a real file
// system. It is how --dry-run works: the command runs the very same core write
// path it would run for real, and afterwards reports the files that would have
// been written instead of writing them. Nothing is duplicated, so a dry run can
// never drift from the real thing.
type overlayFS struct {
	base    core.FS
	written map[string][]byte
	removed map[string]bool
	dirs    map[string]bool
	order   []string
}

// change is one file the dry run would have written or removed.
type change struct {
	Path    string
	Data    []byte
	Removed bool
}

// newOverlayFS wraps a file system.
func newOverlayFS(base core.FS) *overlayFS {
	return &overlayFS{
		base:    base,
		written: make(map[string][]byte),
		removed: make(map[string]bool),
		dirs:    make(map[string]bool),
	}
}

// Changes returns what the dry run would have done, in the order it happened.
// Temporary files a store writes and renames away are not reported.
func (o *overlayFS) Changes() []change {
	out := make([]change, 0, len(o.order))
	for _, p := range o.order {
		switch {
		case o.removed[p]:
			if _, err := o.base.Stat(p); err != nil {
				continue // it never existed: an intermediate file
			}
			out = append(out, change{Path: p, Removed: true})
		default:
			data, ok := o.written[p]
			if !ok {
				continue
			}
			out = append(out, change{Path: p, Data: data})
		}
	}
	return out
}

// record remembers the order files were touched in.
func (o *overlayFS) record(p string) {
	for _, seen := range o.order {
		if seen == p {
			return
		}
	}
	o.order = append(o.order, p)
}

// ReadFile returns the buffered contents when there are any.
func (o *overlayFS) ReadFile(p string) ([]byte, error) {
	p = path.Clean(p)
	if data, ok := o.written[p]; ok {
		return append([]byte(nil), data...), nil
	}
	if o.removed[p] {
		return nil, fmt.Errorf("read %s: %w", p, core.ErrNotExist)
	}
	data, err := o.base.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return data, nil
}

// WriteFile buffers a write.
func (o *overlayFS) WriteFile(p string, data []byte) error {
	p = path.Clean(p)
	o.written[p] = append([]byte(nil), data...)
	delete(o.removed, p)
	o.record(p)
	return nil
}

// Remove buffers a removal.
func (o *overlayFS) Remove(p string) error {
	p = path.Clean(p)
	delete(o.written, p)
	o.removed[p] = true
	o.record(p)
	return nil
}

// Rename buffers a move.
func (o *overlayFS) Rename(oldPath, newPath string) error {
	data, err := o.ReadFile(oldPath)
	if err != nil {
		return err
	}
	if err := o.WriteFile(newPath, data); err != nil {
		return err
	}
	return o.Remove(oldPath)
}

// Stat reports the buffered state first.
func (o *overlayFS) Stat(p string) (core.FileInfo, error) {
	p = path.Clean(p)
	if data, ok := o.written[p]; ok {
		return core.FileInfo{Name: path.Base(p), Size: int64(len(data)), ModTime: time.Time{}}, nil
	}
	if o.removed[p] {
		return core.FileInfo{}, fmt.Errorf("stat %s: %w", p, core.ErrNotExist)
	}
	if o.dirs[p] {
		return core.FileInfo{Name: path.Base(p), IsDir: true}, nil
	}
	info, err := o.base.Stat(p)
	if err != nil {
		return core.FileInfo{}, fmt.Errorf("stat %s: %w", p, err)
	}
	return info, nil
}

// ReadDir merges the buffered entries into the listing of the real directory.
func (o *overlayFS) ReadDir(p string) ([]core.DirEntry, error) {
	p = path.Clean(p)
	seen := make(map[string]core.DirEntry)
	entries, err := o.base.ReadDir(p)
	if err != nil && !errors.Is(err, core.ErrNotExist) {
		return nil, fmt.Errorf("read dir %s: %w", p, err)
	}
	for _, e := range entries {
		seen[e.Name] = e
	}
	for written := range o.written {
		name, ok := childName(p, written)
		if !ok {
			continue
		}
		full := name
		if p != "." {
			full = p + "/" + name
		}
		seen[name] = core.DirEntry{Name: name, IsDir: full != written}
	}
	for dir := range o.dirs {
		if name, ok := childName(p, dir); ok {
			seen[name] = core.DirEntry{Name: name, IsDir: true}
		}
	}
	for removed := range o.removed {
		if name, ok := childName(p, removed); ok {
			delete(seen, name)
		}
	}
	out := make([]core.DirEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// childName returns the name of the entry of dir that p belongs to.
func childName(dir, p string) (string, bool) {
	if dir == "." {
		name, _, _ := strings.Cut(p, "/")
		return name, name != ""
	}
	rest, ok := strings.CutPrefix(p, dir+"/")
	if !ok || rest == "" {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "/")
	return name, true
}

// MkdirAll buffers a directory creation.
func (o *overlayFS) MkdirAll(p string) error {
	for dir := path.Clean(p); dir != "." && dir != "/"; dir = path.Dir(dir) {
		o.dirs[dir] = true
		delete(o.removed, dir)
	}
	return nil
}

// Ensure overlayFS satisfies the core interface at compile time.
var _ core.FS = (*overlayFS)(nil)
