package watcher

import (
	"github.com/digiogithub/git-in-track/internal/core"
)

// ToFileEvents maps a watcher batch to the file events the core index applies,
// so that a caller can hand a batch straight to an incremental reindex.
//
// The mapping is:
//
//	Create -> core.FileCreated
//	Write  -> core.FileModified
//	Chmod  -> core.FileModified (metadata changed; re-reading is the safe answer)
//	Remove -> core.FileRemoved
//	Rename -> core.FileRenamed when the destination is known, core.FileRemoved otherwise
//
// fsnotify reports a rename as a Rename on the source name and a Create on the
// destination, with no identifier tying the two together. When a repository's
// slice of the batch holds exactly one of each, the pair is unambiguous and
// becomes a single core.FileRenamed carrying OldPath; in every other case the
// events are emitted separately, which the index handles as a removal plus an
// insertion with identity coming from the front matter.
//
// Event.Repo is dropped: core paths are vault-relative. Group the batch with
// GroupByRepo first when more than one repository is registered.
func ToFileEvents(batch []Event) []core.FileEvent {
	if len(batch) == 0 {
		return nil
	}

	// Per repository, find the unambiguous rename pairs.
	type pair struct {
		renameIdx int
		createIdx int
		count     [2]int
	}
	pairs := make(map[string]*pair, 2)
	for i, ev := range batch {
		p, ok := pairs[ev.Repo]
		if !ok {
			p = &pair{renameIdx: -1, createIdx: -1}
			pairs[ev.Repo] = p
		}
		switch ev.Op {
		case Rename:
			p.count[0]++
			p.renameIdx = i
		case Create:
			p.count[1]++
			p.createIdx = i
		case Write, Remove, Chmod:
		}
	}
	renameOf := make(map[int]int, len(pairs)) // create index -> rename index
	skip := make(map[int]bool, len(pairs))
	for _, p := range pairs {
		if p.count[0] == 1 && p.count[1] == 1 && p.renameIdx >= 0 && p.createIdx >= 0 {
			renameOf[p.createIdx] = p.renameIdx
			skip[p.renameIdx] = true
		}
	}

	out := make([]core.FileEvent, 0, len(batch))
	for i, ev := range batch {
		if skip[i] {
			continue
		}
		switch ev.Op {
		case Create:
			if j, ok := renameOf[i]; ok {
				out = append(out, core.FileEvent{
					Kind:    core.FileRenamed,
					Path:    ev.Path,
					OldPath: batch[j].Path,
				})
				continue
			}
			out = append(out, core.FileEvent{Kind: core.FileCreated, Path: ev.Path})
		case Write, Chmod:
			out = append(out, core.FileEvent{Kind: core.FileModified, Path: ev.Path})
		case Remove, Rename:
			out = append(out, core.FileEvent{Kind: core.FileRemoved, Path: ev.Path})
		}
	}
	return out
}

// GroupByRepo splits a batch by repository key, preserving the batch order
// inside each group. It exists because a watcher can serve several vaults while
// core.FileEvent paths are relative to one.
func GroupByRepo(batch []Event) map[string][]Event {
	if len(batch) == 0 {
		return nil
	}
	out := make(map[string][]Event, 2)
	for _, ev := range batch {
		out[ev.Repo] = append(out[ev.Repo], ev)
	}
	return out
}
