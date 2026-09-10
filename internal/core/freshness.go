package core

import (
	"errors"
	"path"
	"strings"
)

// StaleEvents compares the given vault-relative files with what the index
// recorded when it last read them and returns the events that would bring the
// index up to date: a removal for an indexed file that is gone, a creation for
// a file on disk the index never saw, a modification when the size or the
// modification time moved. An unchanged file yields nothing.
//
// It is the read-time half of freshness: a watcher that misses a change (an
// exhausted inotify budget, a folder outside the watched scopes, a network
// file system) must not leave a person looking at a stale task. The check costs
// one stat per path, so a caller can run it before every single-file read.
func (ix *Index) StaleEvents(paths []string) []FileEvent {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []FileEvent
	seen := make(map[string]bool, len(paths))
	for _, raw := range paths {
		p := path.Clean(raw)
		if raw == "" || seen[p] {
			continue
		}
		seen[p] = true
		if ev, ok := ix.staleLocked(p); ok {
			out = append(out, ev)
		}
	}
	return out
}

// staleLocked checks one file. The caller holds at least the read lock.
func (ix *Index) staleLocked(p string) (FileEvent, bool) {
	prev, indexed := ix.files[p]
	info, err := ix.fs.Stat(p)
	switch {
	case err != nil:
		if indexed && errors.Is(err, ErrNotExist) {
			return FileEvent{Kind: FileRemoved, Path: p}, true
		}
		// Anything else (a path outside the vault, a permission problem) is
		// left to the watcher and the next full pass.
		return FileEvent{}, false
	case info.IsDir:
		return FileEvent{}, false
	case !indexed:
		return FileEvent{Kind: FileCreated, Path: p}, true
	case prev.Size != info.Size || !prev.ModTime.Equal(NewTimestamp(info.ModTime).Time):
		return FileEvent{Kind: FileModified, Path: p}, true
	default:
		return FileEvent{}, false
	}
}

// ItemPaths lists the files an item is made of as the file system holds them
// now: the item file, the comments the index knows and every Markdown file its
// comment folder holds, so that a comment written behind the index's back is
// found too. An unknown id yields nil.
func (ix *Index) ItemPaths(id ItemID) []string {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	it, ok := ix.byID[id]
	if !ok {
		return nil
	}
	out := []string{it.Path}
	for _, c := range ix.commentsByItem[id] {
		out = append(out, c.Path)
	}
	if backlog, ok := backlogOf(it.Path); ok {
		dir := path.Join(backlog, CommentsDirName, string(id))
		if entries, err := ix.fs.ReadDir(dir); err == nil {
			for _, e := range entries {
				if !e.IsDir && isMarkdown(e.Name) {
					out = append(out, path.Join(dir, e.Name))
				}
			}
		}
	}
	return out
}

// backlogOf returns the `.pmngr` folder a vault-relative path lives in.
func backlogOf(p string) (string, bool) {
	if strings.HasPrefix(p, BacklogDirName+"/") {
		return BacklogDirName, true
	}
	if i := strings.Index(p, "/"+BacklogDirName+"/"); i >= 0 {
		return p[:i+1+len(BacklogDirName)], true
	}
	return "", false
}
