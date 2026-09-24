package trace

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"

	"github.com/digiogithub/git-in-track/internal/core"
)

// defaultSkipDirs are directory names never descended into, wherever they
// sit: version-control metadata, dependency trees, the backlog itself
// (R-MARK-1: nothing under .pmngr/ is scanned) and common build output.
var defaultSkipDirs = map[string]bool{
	".git": true, ".jj": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, ".pmngr": true,
	"dist": true,
}

// defaultMaxFileSize bounds the files read: a marker lives in source, and a
// multi-megabyte file is generated or binary.
const defaultMaxFileSize = 2 << 20

// Options tune a Cache.
type Options struct {
	// Exclude lists extra repository-relative paths (files or directories,
	// "/"-separated) that are never scanned, on top of .gitignore and the
	// built-in skip list.
	Exclude []string
	// MaxFileSize is the largest file read, in bytes; 0 means 2 MiB.
	MaxFileSize int64
}

// Cache is the derived marker index of one repository working tree. It is
// never a source of truth: it holds nothing that cannot be rebuilt from the
// files, and it is kept in memory only. Rebuild scans the whole tree; Update
// rescans only the paths a diff reports as changed. A Cache is safe for
// concurrent use.
type Cache struct {
	root string
	opts Options

	mu    sync.RWMutex
	files map[string]FileResult // repository-relative path -> non-empty result
}

// NewCache returns an empty cache over the working tree at root. Call Rebuild
// before reading it.
func NewCache(root string, opts Options) *Cache {
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = defaultMaxFileSize
	}
	return &Cache{root: root, opts: opts, files: map[string]FileResult{}}
}

// Scan walks the working tree at root and returns a freshly built cache.
func Scan(ctx context.Context, root string, opts Options) (*Cache, error) {
	c := NewCache(root, opts)
	if err := c.Rebuild(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Rebuild rescans the whole working tree, replacing the cache content.
func (c *Cache) Rebuild(ctx context.Context) error {
	files := map[string]FileResult{}
	patterns := map[string][]gitignore.Pattern{}
	base := readIgnoreFile(filepath.Join(c.root, ".git", "info", "exclude"), nil)
	err := filepath.WalkDir(c.root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			if abs == c.root {
				return err
			}
			return nil // an unreadable entry is skipped, not fatal
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("trace scan: %w", err)
		}
		rel, rerr := filepath.Rel(c.root, abs)
		if rerr != nil {
			return fmt.Errorf("trace scan: %w", rerr)
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				patterns["."] = append(base, readIgnoreFile(filepath.Join(abs, ".gitignore"), nil)...)
				return nil
			}
			parent := patterns[path.Dir(rel)]
			if c.skipDir(rel, d.Name()) || gitignore.NewMatcher(parent).Match(strings.Split(rel, "/"), true) {
				return filepath.SkipDir
			}
			patterns[rel] = append(parent[:len(parent):len(parent)],
				readIgnoreFile(filepath.Join(abs, ".gitignore"), strings.Split(rel, "/"))...)
			return nil
		}
		if !d.Type().IsRegular() || !Scannable(rel) || c.excluded(rel) {
			return nil
		}
		if gitignore.NewMatcher(patterns[path.Dir(rel)]).Match(strings.Split(rel, "/"), false) {
			return nil
		}
		if res := c.scanPath(abs, rel); !res.empty() {
			files[rel] = res
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("trace scan %s: %w", c.root, err)
	}
	c.mu.Lock()
	c.files = files
	c.mu.Unlock()
	return nil
}

// Update rescans the given repository-relative paths — typically the paths of
// a diff (gitops ChangedFiles, both sides of a rename) — and drops the ones
// that no longer exist or are no longer scanned. A changed .gitignore changes
// what the walk sees, so it triggers a full Rebuild.
func (c *Cache) Update(ctx context.Context, paths []string) error {
	for _, p := range paths {
		if path.Base(filepath.ToSlash(p)) == ".gitignore" {
			return c.Rebuild(ctx)
		}
	}
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("trace update: %w", err)
		}
		rel := path.Clean(filepath.ToSlash(p))
		res := FileResult{}
		if c.scanned(rel) {
			res = c.scanPath(filepath.Join(c.root, filepath.FromSlash(rel)), rel)
		}
		c.mu.Lock()
		if res.empty() {
			delete(c.files, rel)
		} else {
			c.files[rel] = res
		}
		c.mu.Unlock()
	}
	return nil
}

// scanned reports whether a full walk would scan the path: it is a regular
// file of a scanned type, and neither it nor any parent directory is skipped
// or ignored.
func (c *Cache) scanned(rel string) bool {
	if rel == "." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) || !Scannable(rel) || c.excluded(rel) {
		return false
	}
	info, err := os.Lstat(filepath.Join(c.root, filepath.FromSlash(rel)))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	segs := strings.Split(rel, "/")
	ps := readIgnoreFile(filepath.Join(c.root, ".git", "info", "exclude"), nil)
	ps = append(ps, readIgnoreFile(filepath.Join(c.root, ".gitignore"), nil)...)
	for i := 1; i < len(segs); i++ {
		dir := strings.Join(segs[:i], "/")
		if c.skipDir(dir, segs[i-1]) || gitignore.NewMatcher(ps).Match(segs[:i], true) {
			return false
		}
		ps = append(ps, readIgnoreFile(filepath.Join(c.root, filepath.FromSlash(dir), ".gitignore"), segs[:i])...)
	}
	return !gitignore.NewMatcher(ps).Match(segs, false)
}

func (c *Cache) skipDir(rel, name string) bool {
	return defaultSkipDirs[name] || rel == "web/dist" || c.excluded(rel)
}

func (c *Cache) excluded(rel string) bool {
	for _, e := range c.opts.Exclude {
		e = strings.Trim(filepath.ToSlash(e), "/")
		if e != "" && (rel == e || strings.HasPrefix(rel, e+"/")) {
			return true
		}
	}
	return false
}

// scanPath reads and scans one file; an unreadable, oversized or binary file
// contributes nothing.
func (c *Cache) scanPath(abs, rel string) FileResult {
	info, err := os.Stat(abs)
	if err != nil || info.Size() > c.opts.MaxFileSize {
		return FileResult{}
	}
	src, err := os.ReadFile(abs)
	if err != nil || bytes.IndexByte(src[:min(len(src), 8000)], 0) >= 0 {
		return FileResult{}
	}
	return ScanFile(rel, src)
}

// readIgnoreFile parses one gitignore file whose patterns apply below domain.
// A missing file has no patterns.
func readIgnoreFile(name string, domain []string) []gitignore.Pattern {
	f, err := os.Open(name)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var ps []gitignore.Pattern
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		s := strings.TrimRight(sc.Text(), "\r")
		if strings.HasPrefix(s, "#") || strings.TrimSpace(s) == "" {
			continue
		}
		ps = append(ps, gitignore.ParsePattern(s, domain))
	}
	return ps
}

// Files returns the paths that hold at least one marker or finding, sorted.
func (c *Cache) Files() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.files))
	for p := range c.files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// File returns the result of one repository-relative path.
func (c *Cache) File(p string) FileResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.files[path.Clean(filepath.ToSlash(p))]
}

// Markers returns every marker, sorted by path, line and ref.
func (c *Cache) Markers() []Marker {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Marker
	for _, r := range c.files {
		out = append(out, r.Markers...)
	}
	sortMarkers(out)
	return out
}

// MarkersFor returns the markers naming one requirement, sorted. The project
// qualifier is not compared: a ref's spec ID already carries its key.
func (c *Cache) MarkersFor(ref core.RequirementRef) []Marker {
	var out []Marker
	for _, m := range c.Markers() {
		if m.Ref == ref {
			out = append(out, m)
		}
	}
	return out
}

// Findings returns the W-MARKER-SYNTAX findings of the scan, sorted by path
// and line. Dangling refs need the index and are reported by Dangling.
func (c *Cache) Findings() []Finding {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Finding
	for _, r := range c.files {
		out = append(out, r.Findings...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func sortMarkers(ms []Marker) {
	sort.Slice(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Ref.Spec != b.Ref.Spec {
			return a.Ref.Spec < b.Ref.Spec
		}
		return a.Ref.Number < b.Ref.Number
	})
}
