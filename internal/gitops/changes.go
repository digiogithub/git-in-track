package gitops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/utils/diff"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// This file is the changed-files primitive of GIT-US-0112: which files changed
// between two revisions, or between a revision and the working tree, and which
// lines of each changed on the new side. It is what the trace and impact
// engines of GIT-EP-0024 compute suspect links and impact sets from.
//
// Every backend answers the same question with the same result on the same
// history. That is guaranteed by construction rather than by three parallel
// readings of three diff engines: a backend only produces the two sides of the
// comparison — a map from path to content id, plus a way to read a path's
// bytes — and the comparison itself, rename detection and line ranges
// included, is computed once, here. git's and jj's own rename heuristics and
// diff algorithms are close to each other but not identical, and "identical
// results" is an acceptance criterion.

// WorkingTree passed as the `to` of ChangedFiles compares against the files on
// disk instead of a revision.
const WorkingTree = ""

// ChangeStatus classifies one changed file.
type ChangeStatus string

// The statuses a FileChange carries.
const (
	// ChangeAdded is a path that exists only on the new side.
	ChangeAdded ChangeStatus = "added"
	// ChangeModified is a path whose content differs between the two sides.
	ChangeModified ChangeStatus = "modified"
	// ChangeDeleted is a path that exists only on the old side.
	ChangeDeleted ChangeStatus = "deleted"
	// ChangeRenamed is a path that was moved, with or without edits;
	// FileChange.OldPath names where it came from.
	ChangeRenamed ChangeStatus = "renamed"
)

// renameSimilarity is the minimum similarity, in percent, for a deleted and an
// added path to be paired as a rename. It is git's default (`-M50%`).
const renameSimilarity = 50

// renamePairLimit bounds the inexact rename search, which diffs every deleted
// path against every added one. Past it only exact (same-content) renames are
// detected, the way git's diff.renameLimit degrades.
const renamePairLimit = 100 * 100

// LineRange is one changed hunk on the new side, in the unified-diff
// convention of `git diff -U0`: Start is 1-based and Count lines starting at
// Start were added or replaced. A Count of zero is a pure deletion, and Start
// then names the new-side line after which the lines were removed (0 when they
// were removed from the top of the file).
type LineRange struct {
	Start int `json:"start"`
	Count int `json:"count"`
}

// FileChange is one path that differs between the two sides of ChangedFiles.
type FileChange struct {
	// Path is the repository-relative, forward-slashed path on the new side;
	// for a deletion it is the path that was removed.
	Path string `json:"path"`
	// OldPath is where a renamed file came from. Empty for every other status.
	OldPath string `json:"oldPath,omitempty"`
	// Status classifies the change.
	Status ChangeStatus `json:"status"`
	// Lines are the changed hunks on the new side, in file order. A deletion,
	// a pure rename and a binary file carry none; an added text file carries
	// one hunk covering the whole file.
	Lines []LineRange `json:"lines,omitempty"`
	// Binary reports that either side holds a NUL byte, so no line ranges are
	// computed.
	Binary bool `json:"binary,omitempty"`
}

// treeSide is one side of a comparison as a backend produces it: every file
// with a content id, and a bulk reader of file contents. Two ids that are equal
// promise equal contents; two ids that differ are verified byte for byte, so a
// side whose ids use a different hash (a working tree hashed as SHA-1 against
// a SHA-256 repository) is still compared correctly, only more slowly.
type treeSide struct {
	ids  map[string]string
	read func(ctx context.Context, paths []string) (map[string][]byte, error)
}

// compareSides turns two sides into the sorted list of changes.
func compareSides(ctx context.Context, from, to treeSide) ([]FileChange, error) {
	var deleted, added, modified []string
	for path, id := range from.ids {
		other, ok := to.ids[path]
		switch {
		case !ok:
			deleted = append(deleted, path)
		case other != id:
			modified = append(modified, path)
		}
	}
	for path := range to.ids {
		if _, ok := from.ids[path]; !ok {
			added = append(added, path)
		}
	}
	sort.Strings(deleted)
	sort.Strings(added)
	sort.Strings(modified)

	oldData, err := from.read(ctx, append(append([]string{}, deleted...), modified...))
	if err != nil {
		return nil, err
	}
	newData, err := to.read(ctx, append(append([]string{}, added...), modified...))
	if err != nil {
		return nil, err
	}

	out := make([]FileChange, 0, len(deleted)+len(added)+len(modified))
	for _, path := range modified {
		before, after := oldData[path], newData[path]
		if bytes.Equal(before, after) {
			continue
		}
		out = append(out, describeChange(FileChange{Path: path, Status: ChangeModified}, before, after))
	}

	renames := detectRenames(deleted, added, oldData, newData)
	for _, pair := range renames {
		out = append(out, describeChange(
			FileChange{Path: pair.to, OldPath: pair.from, Status: ChangeRenamed},
			oldData[pair.from], newData[pair.to]))
	}
	for _, path := range added {
		if _, ok := renamedTo(renames, path); ok {
			continue
		}
		out = append(out, describeChange(FileChange{Path: path, Status: ChangeAdded}, nil, newData[path]))
	}
	for _, path := range deleted {
		if renamedFrom(renames, path) {
			continue
		}
		change := FileChange{Path: path, Status: ChangeDeleted, Binary: isBinaryBytes(oldData[path])}
		out = append(out, change)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// describeChange fills in the binary flag and the line ranges of a change.
func describeChange(change FileChange, before, after []byte) FileChange {
	if isBinaryBytes(before) || isBinaryBytes(after) {
		change.Binary = true
		return change
	}
	change.Lines = lineRanges(string(before), string(after))
	return change
}

// isBinaryBytes applies git's heuristic, shared with the conflict resolver
// (isBinary): a NUL byte in the first 8 000 bytes.
func isBinaryBytes(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// lineRanges diffs two texts line by line and returns the new-side hunks.
func lineRanges(before, after string) []LineRange {
	if before == after {
		return nil
	}
	var (
		out     []LineRange
		current LineRange
		open    bool
		line    int // lines of the new side consumed so far
	)
	flush := func() {
		if !open {
			return
		}
		if current.Count == 0 {
			current.Start = line
		}
		out = append(out, current)
		open = false
	}
	for _, chunk := range diff.Do(before, after) {
		n := countLines(chunk.Text)
		switch chunk.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			line += n
		case diffmatchpatch.DiffInsert:
			if !open {
				open, current = true, LineRange{Start: line + 1}
			}
			current.Count += n
			line += n
		case diffmatchpatch.DiffDelete:
			if !open {
				open, current = true, LineRange{Start: line + 1}
			}
		}
	}
	flush()
	return out
}

// countLines counts the lines of a chunk; a last line without a newline counts.
func countLines(text string) int {
	n := strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

// renamePair is one detected rename.
type renamePair struct {
	from, to string
}

// renamedTo reports the rename whose new side is path.
func renamedTo(pairs []renamePair, path string) (renamePair, bool) {
	for _, pair := range pairs {
		if pair.to == path {
			return pair, true
		}
	}
	return renamePair{}, false
}

// renamedFrom reports whether path is the old side of a rename.
func renamedFrom(pairs []renamePair, path string) bool {
	for _, pair := range pairs {
		if pair.from == path {
			return true
		}
	}
	return false
}

// detectRenames pairs deleted paths with added ones: identical contents first,
// then — within renamePairLimit — the most similar text pairs at or above
// renameSimilarity. Ties break on the paths, so the pairing is deterministic.
// Empty files are never paired: every empty file is identical to every other.
func detectRenames(deleted, added []string, oldData, newData map[string][]byte) []renamePair {
	var pairs []renamePair
	usedOld := map[string]bool{}
	usedNew := map[string]bool{}

	byDigest := map[[sha256.Size]byte][]string{}
	for _, path := range deleted {
		if len(oldData[path]) == 0 {
			continue
		}
		digest := sha256.Sum256(oldData[path])
		byDigest[digest] = append(byDigest[digest], path)
	}
	for _, path := range added {
		if len(newData[path]) == 0 {
			continue
		}
		candidates := byDigest[sha256.Sum256(newData[path])]
		for _, old := range candidates {
			if usedOld[old] {
				continue
			}
			usedOld[old], usedNew[path] = true, true
			pairs = append(pairs, renamePair{from: old, to: path})
			break
		}
	}

	var olds, news []string
	for _, path := range deleted {
		if !usedOld[path] && len(oldData[path]) > 0 && !isBinaryBytes(oldData[path]) {
			olds = append(olds, path)
		}
	}
	for _, path := range added {
		if !usedNew[path] && len(newData[path]) > 0 && !isBinaryBytes(newData[path]) {
			news = append(news, path)
		}
	}
	if len(olds) == 0 || len(news) == 0 || len(olds)*len(news) > renamePairLimit {
		return pairs
	}
	type scored struct {
		renamePair
		score int
	}
	var candidates []scored
	for _, old := range olds {
		for _, path := range news {
			if score := similarity(oldData[old], newData[path]); score >= renameSimilarity {
				candidates = append(candidates, scored{renamePair{from: old, to: path}, score})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.to != b.to {
			return a.to < b.to
		}
		return a.from < b.from
	})
	for _, candidate := range candidates {
		if usedOld[candidate.from] || usedNew[candidate.to] {
			continue
		}
		usedOld[candidate.from], usedNew[candidate.to] = true, true
		pairs = append(pairs, candidate.renamePair)
	}
	return pairs
}

// similarity is the share, in percent, of the larger file's bytes that the two
// files have in common line for line — the measure git's `-M` threshold is
// expressed in.
func similarity(before, after []byte) int {
	larger := max(len(before), len(after))
	if larger == 0 {
		return 100
	}
	common := 0
	for _, chunk := range diff.Do(string(before), string(after)) {
		if chunk.Type == diffmatchpatch.DiffEqual {
			common += len(chunk.Text)
		}
	}
	return common * 100 / larger
}

// blobID is the git blob id of a file's content, which is what a SHA-1 tree
// records for it, so an unchanged working file matches its committed id
// without its committed bytes being read.
func blobID(data []byte) string {
	return plumbing.ComputeHash(plumbing.BlobObject, data).String()
}

// workingTreeSide reads the working tree at root as a comparison side.
//
// The files it holds are the ones a commit of the whole tree would record:
// every tracked path that is still on disk, plus every untracked file the
// ignore rules (.gitignore files, .git/info/exclude and the user's global
// excludes file) do not exclude. That is git's `status` view with untracked
// files included, and jj's snapshot view — computed here without running jj's
// snapshot, which would write an operation (see jujutsuBackend.run).
//
// It is compared byte for byte: git's clean filters (autocrlf, LFS) are not
// applied, and a change of mode alone is not a change.
func workingTreeSide(ctx context.Context, root string, tracked map[string]bool) (treeSide, error) {
	var patterns []gitignore.Pattern
	if global, err := gitignore.LoadGlobalPatterns(osfs.New("/")); err == nil {
		patterns = append(patterns, global...)
	}
	if local, err := gitignore.ReadPatterns(osfs.New(root), nil); err == nil {
		patterns = append(patterns, local...)
	}
	matcher := gitignore.NewMatcher(patterns)

	trackedDirs := map[string]bool{}
	for path := range tracked {
		for dir := parentDir(path); dir != ""; dir = parentDir(dir) {
			trackedDirs[dir] = true
		}
	}

	ids := map[string]string{}
	err := filepath.WalkDir(root, func(abs string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("read the working tree: %w", ctxErr)
		}
		if abs == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			return fmt.Errorf("locate %s: %w", abs, relErr)
		}
		rel = filepath.ToSlash(rel)
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == ".jj" {
				return filepath.SkipDir
			}
			if trackedDirs[rel] {
				return nil
			}
			if matcher.Match(strings.Split(rel, "/"), true) {
				return filepath.SkipDir
			}
			// A nested repository is its own history, never part of this one.
			if _, statErr := os.Lstat(filepath.Join(abs, ".git")); statErr == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if name == ".git" && !tracked[rel] {
			return nil // a linked worktree's gitfile
		}
		if !tracked[rel] && matcher.Match(strings.Split(rel, "/"), false) {
			return nil
		}
		data, readErr := readWorkingFile(abs, entry)
		if readErr != nil {
			if errors.Is(readErr, errNotAFile) {
				return nil
			}
			return readErr
		}
		ids[rel] = blobID(data)
		return nil
	})
	if err != nil {
		return treeSide{}, wrap("changed-files", CodeCommitFailed, err, "read the working tree of %s", root)
	}
	read := func(_ context.Context, paths []string) (map[string][]byte, error) {
		out := make(map[string][]byte, len(paths))
		for _, path := range paths {
			abs := filepath.Join(root, filepath.FromSlash(path))
			info, err := os.Lstat(abs)
			if err != nil {
				return nil, wrap("changed-files", CodeCommitFailed, err, "read %s", path)
			}
			data, err := readWorkingFile(abs, fs.FileInfoToDirEntry(info))
			if err != nil {
				return nil, wrap("changed-files", CodeCommitFailed, err, "read %s", path)
			}
			out[path] = data
		}
		return out, nil
	}
	return treeSide{ids: ids, read: read}, nil
}

// errNotAFile marks a directory entry that is neither a regular file nor a
// symbolic link — a socket or a FIFO — which no commit could record.
var errNotAFile = errors.New("not a regular file or symbolic link")

// readWorkingFile reads what a commit would record for a working file: its
// bytes, or for a symbolic link the target it points at.
func readWorkingFile(abs string, entry fs.DirEntry) ([]byte, error) {
	switch mode := entry.Type(); {
	case mode&fs.ModeSymlink != 0:
		target, err := os.Readlink(abs)
		if err != nil {
			return nil, err //nolint:wrapcheck // wrapped by the caller
		}
		return []byte(filepath.ToSlash(target)), nil
	case mode.IsRegular():
		return os.ReadFile(abs) //nolint:gosec,wrapcheck // abs is inside the working tree, wrapped by the caller
	default:
		return nil, errNotAFile
	}
}

// parentDir returns the forward-slashed parent of a path, empty at the root.
func parentDir(path string) string {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return ""
	}
	return path[:i]
}

// unknownRevision is the typed failure of a ref that names no commit.
func unknownRevision(ref, where string, err error) *Error {
	if strings.TrimSpace(ref) == "" {
		return &Error{Code: CodeUnknownRevision, Op: "changed-files",
			Message: "a revision to compare from is required", Err: err}
	}
	return &Error{Code: CodeUnknownRevision, Op: "changed-files",
		Message: "\"" + ref + "\" does not name a commit in " + where, Err: err}
}
