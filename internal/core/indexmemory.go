package core

import (
	"fmt"
	"sort"
)

// checkMemory raises the memory findings that need more than one file
// (ADR-044 section 4): two memories of a scope sharing a key, two memories of a
// project sharing a version — which an offline merge can produce, and
// ReconcileMemoryVersions repairs — and a supersedes link whose target is not
// in the superseded status. The caller holds the lock.
func (ix *Index) checkMemory() {
	byScope := map[ProjectKey][]*Item{}
	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		if it.Type != TypeMemory || it.Memory == nil || it.Deleted {
			continue
		}
		key := ix.projectOf(it)
		byScope[key] = append(byScope[key], it)
	}
	scopes := make([]ProjectKey, 0, len(byScope))
	for key := range byScope {
		scopes = append(scopes, key)
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })

	for _, scope := range scopes {
		items := byScope[scope]
		keys := map[string]*Item{}
		versions := map[int64]*Item{}
		for _, it := range items {
			if first, dup := keys[it.Memory.Key]; dup && it.Memory.Key != "" {
				ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
					Code: CodeWarnMemoryKeyDup, Severity: SeverityWarning, Path: it.Path, Field: "memory.key",
					Message: fmt.Sprintf("key %q is also used by %s in the same scope", it.Memory.Key, first.ID),
				})
			} else {
				keys[it.Memory.Key] = it
			}
			if first, dup := versions[it.Memory.Version]; dup && it.Memory.Version > 0 {
				ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
					Code: CodeWarnMemoryVersionDup, Severity: SeverityWarning, Path: it.Path, Field: "memory.version",
					Message: fmt.Sprintf("version %d is also held by %s; reconcile the memory versions",
						it.Memory.Version, first.ID),
				})
			} else {
				versions[it.Memory.Version] = it
			}
		}
	}

	for _, id := range sortedIDs(ix.byID) {
		it := ix.byID[id]
		if it.Type != TypeMemory {
			continue
		}
		for _, l := range it.Links {
			if l.Kind != LinkSupersedes {
				continue
			}
			target, ok := ix.byID[ItemID(bareTarget(l.Target))]
			if !ok || target.Type != TypeMemory || target.Status == MemorySuperseded {
				continue
			}
			ix.derivedDiags = append(ix.derivedDiags, Diagnostic{
				Code: CodeWarnMemorySupersede, Severity: SeverityWarning, Path: it.Path, Field: "links",
				Message: fmt.Sprintf("%s supersedes %s, which is %q rather than superseded", it.ID, target.ID, target.Status),
			})
		}
	}
}

// Memories returns every memory item whose id carries key, deleted ones
// excluded, sorted by version and then id: the order of the
// alchemai.memory.v2 changes feed. It is unpaginated on purpose — a vault
// upserting by key, allocating the next version or serving a changes cursor
// needs the whole scope, and a scope holds facts, not a backlog.
func (ix *Index) Memories(key ProjectKey) []*Item {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []*Item
	for _, it := range ix.byID {
		if it.Type != TypeMemory || it.Deleted || ix.projectOf(it) != key {
			continue
		}
		c := cloneItem(it)
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool {
		vi, vj := memoryVersion(out[i]), memoryVersion(out[j])
		if vi != vj {
			return vi < vj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// memoryVersion is the version of a memory, 0 when it has no block.
func memoryVersion(it *Item) int64 {
	if it.Memory == nil {
		return 0
	}
	return it.Memory.Version
}
