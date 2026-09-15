package vault

import (
	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the read seam the corpus exporter of GIT-US-0073 needs:
// everything it exports, and the single-document lookups that keep an
// incremental update from cloning the whole index for one changed file.
//
// Together with Items (itemedit.go) these methods satisfy both
// internal/pandosync.Source and internal/pandosync.Lookup. They are here rather
// than in the exporter because the vault owns the index lock, and because
// nothing in internal/pandosync may reach into internal/core's private state.
//
// Every one of them hands back a copy. The index holds live pointers that a
// watcher pass can rewrite under the caller's feet, so a leaked *core.KBPage
// would be a data race waiting for a reindex.

// Pages returns every indexed knowledge-base page, sorted by vault-relative
// path, each one cloned. It takes the vault lock.
//
// The clone is the point: core.Index.Pages returns the live pointers, and an
// exporter that walks them while the watcher reloads a file would read a page
// half-rewritten.
func (v *Vault) Pages() []*core.KBPage {
	v.mu.Lock()
	defer v.mu.Unlock()
	live := v.index.Pages()
	out := make([]*core.KBPage, 0, len(live))
	for _, p := range live {
		if p == nil {
			continue
		}
		// Index.Page clones, including every slice and the external refs.
		if clone, ok := v.index.Page(p.Path); ok {
			out = append(out, clone)
		}
	}
	return out
}

// Item returns one item by id, cloned, and reports whether it is indexed. A
// deleted item is still indexed and is returned with Deleted set, because the
// exporter has to remove its corpus file rather than leave it behind.
func (v *Vault) Item(id core.ItemID) (core.Item, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	it, err := v.index.Item(id)
	if err != nil || it == nil {
		return core.Item{}, false
	}
	return *it, true
}

// Page returns one knowledge-base page by its vault-relative path, cloned.
func (v *Vault) Page(vaultPath string) (*core.KBPage, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.index.Page(vaultPath)
}

// PageByProjectPath returns the page that project files at rel, the path
// relative to the project's documentation folder — the form a wikilink uses,
// and the form an exported corpus document is named by.
//
// It exists because a caller holding only (project, relative path) cannot build
// the vault-relative path on its own: the documentation folder of a project is
// discovered, not declared, and a monorepo has a different one per project.
func (v *Vault) PageByProjectPath(project core.ProjectKey, rel string) (*core.KBPage, bool) {
	if rel == "" {
		return nil, false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	// The scan walks the live pointers without copying them: only the match is
	// cloned, and only the two path fields are read on the way.
	for _, p := range v.index.Pages() {
		if p == nil || p.RelPath != rel {
			continue
		}
		if project != "" && p.Project != project {
			continue
		}
		return v.index.Page(p.Path)
	}
	return nil, false
}
