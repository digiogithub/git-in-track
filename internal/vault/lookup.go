package vault

import (
	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the read seam the search surface needs: every indexed document,
// and the single-document lookups that resolve one semantic hit back into live
// backlog state without cloning the whole index (GIT-EP-0020).
//
// They are here rather than in the caller because the vault owns the index
// lock, and because nothing outside this package may reach into
// internal/core's private state.
//
// Every one of them hands back a copy. The index holds live pointers that a
// watcher pass can rewrite under the caller's feet, so a leaked *core.KBPage
// would be a data race waiting for a reindex.

// Pages returns every indexed knowledge-base page, sorted by vault-relative
// path, each one cloned. It takes the vault lock.
//
// The clone is the point: core.Index.Pages returns the live pointers, and a
// caller that walks them while the watcher reloads a file would read a page
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
// deleted item is still indexed and is returned with Deleted set, because a
// caller resolving a stale reference has to see the tombstone rather than a
// bare "not found".
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
// and the form a Pando hit's path is reported in.
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

// Requirement returns one requirement by its ref and reports whether its spec
// still holds that block. It takes the vault lock. The view is built from the
// spec's current body; its trace, links and extra values may share the
// index's, so a caller treats it as read-only. It is how a semantic hit inside a spec is re-read as the live
// requirement it landed in (GIT-US-0118).
func (v *Vault) Requirement(ref core.RequirementRef) (core.RequirementView, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	view, err := v.index.Requirement(ref)
	if err != nil {
		return core.RequirementView{}, false
	}
	return view, true
}
