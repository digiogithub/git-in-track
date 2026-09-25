package vault

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// OnRefresh installs fn, which hears what a read-time refresh folded into the
// index (see freshen). The companion uses it to announce the change on its
// event stream exactly as it announces a watcher batch, so that the lists and
// boards other people have open follow. fn runs after the vault lock is
// released and may call back into the vault.
func (v *Vault) OnRefresh(fn func(core.IndexDelta)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.onRefresh = fn
}

// freshen re-reads, before a read that shows one item or one page, the files
// behind it that changed on disk since they were indexed, and reports what that
// changed. It is what makes a click in the UI show the latest version of a task
// even when no file-system event announced the edit: the watcher only covers
// the documentation and backlog folders, and even there an event can be lost.
//
// A vault over an in-memory file system has nothing to freshen: its host
// pushes every change through "vault.apply". The caller holds the lock.
func (v *Vault) freshen(ctx context.Context, method string, raw []byte) core.IndexDelta {
	if v.mem != nil || v.index == nil {
		return core.IndexDelta{}
	}
	var paths []string
	switch method {
	case "item.get", "comment.list":
		p, err := decodeParams[struct {
			ID string `json:"id"`
		}](raw)
		if err != nil || p.ID == "" {
			return core.IndexDelta{}
		}
		paths = v.index.ItemPaths(core.ItemID(p.ID))
	case "requirement.get":
		p, err := decodeParams[struct {
			Ref string `json:"ref"`
		}](raw)
		if err != nil {
			return core.IndexDelta{}
		}
		ref, err := parseRequirementRef(p.Ref)
		if err != nil {
			return core.IndexDelta{}
		}
		paths = v.index.ItemPaths(ref.Spec)
	case "kb.page":
		p, err := decodeParams[struct {
			Path string `json:"path"`
		}](raw)
		if err != nil || p.Path == "" {
			return core.IndexDelta{}
		}
		paths = []string{p.Path}
	default:
		return core.IndexDelta{}
	}
	events := v.index.StaleEvents(paths)
	if len(events) == 0 {
		return core.IndexDelta{}
	}
	delta, _, err := v.applyEvents(ctx, events)
	if err != nil {
		// The read still answers, from the index as it was: a refresh that
		// fails must never turn a working read into an error.
		return core.IndexDelta{}
	}
	return delta
}

// PageDirs lists the folders that hold at least one indexed knowledge-base
// page, sorted, the vault root excluded. The companion watches them when a
// project sits at the repository root, instead of the whole source tree.
func (v *Vault) PageDirs() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, page := range v.index.Pages() {
		dir := path.Dir(page.Path)
		if dir == "." || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

// Rescan brings the index up to date with the file system in one incremental
// pass: the projects are discovered again, every indexed folder is listed and
// only the files whose size or modification time moved are parsed again. It is
// the fallback for a host with no file watcher — a long-lived MCP server over
// stdio whose watcher could not start — which would otherwise answer every
// list and search from the index it built at startup, blind to the tasks and
// pages written by anyone else since. It costs one stat per indexed file.
//
// A vault over an in-memory file system has nothing to rescan: its host pushes
// every change through "vault.apply".
func (v *Vault) Rescan(ctx context.Context) (IndexStats, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.mem != nil || v.index == nil {
		return v.stats(), nil
	}
	changed, err := v.rediscover()
	if err != nil {
		return IndexStats{}, err
	}
	if changed {
		// A project appeared or vanished: the classification of every file may
		// have moved, which only a full build settles.
		return v.rebuild(ctx)
	}
	if _, err := v.index.Build(ctx, false); err != nil {
		return IndexStats{}, fmt.Errorf("rescan index: %w", err)
	}
	return v.stats(), nil
}

// WatchScopes reports the vault-relative subtrees worth watching for changes:
// the documentation folders the host declared, the ones discovery found, and
// the root-level backlog a team repository keeps.
//
// Everything else in a repository — the source tree, build output, a vendored
// dependency — is never indexed, so watching it buys nothing and costs one
// inotify watch per directory. A repository of ten thousand directories used to
// exhaust the whole watch budget and leave the repositories registered after it
// with no live updates at all.
//
// A project at the repository root indexes the whole tree, but watching the
// whole tree is exactly what exhausts the budget. Its scopes are the backlog and
// the folders that hold a page today; an edit anywhere else is still picked up
// when the file is opened (freshen) or by the next reindex.
func (v *Vault) WatchScopes(declared []string) []string {
	out := make([]string, 0, len(declared)+2)
	seen := map[string]bool{}
	add := func(folder string) {
		folder = strings.Trim(strings.TrimSpace(filepath.ToSlash(folder)), "/")
		if folder == "" || folder == "." || seen[folder] {
			return
		}
		seen[folder] = true
		out = append(out, folder)
	}
	for _, folder := range declared {
		add(folder)
	}
	rootProject := false
	for _, ref := range v.Projects() {
		if ref.DocsPath == "." {
			rootProject = true
			continue
		}
		add(ref.DocsPath)
	}
	// A team repository keeps its boards, sprints and retrospectives in a
	// backlog folder at the root, beside the documentation folder; a root
	// project keeps its whole backlog there.
	add(core.BacklogDirName)
	if rootProject {
		for _, dir := range v.PageDirs() {
			add(dir)
		}
	}
	return out
}
