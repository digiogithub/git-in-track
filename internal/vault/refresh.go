package vault

import (
	"context"
	"path"
	"sort"

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
