package gitops

import (
	"strings"
)

// The structured conflict surface of story GIT-US-0022 (docs/06-git-sync.md
// section 5), generalized beyond git by GIT-US-0039 (ADR-022).
//
// A conflict is three sides — the merge base, the user's own side and the
// incoming one — plus whatever the working file holds. Reading them is what
// turns "there were conflicts" into a resolver, and writing one file back plus
// carrying the integration forward is what closes it.
//
// Where those three sides come from is the backend's business: git reads the
// index stages a stopped integration left behind (stage 1 base, stage 2 ours,
// stage 3 theirs), and a jj backend materializes them from the conflict
// recorded inside the commit, which is where jj keeps it. Nothing outside a
// backend may assume an index.
//
// The merge itself is not here: it is `internal/core`, because browser-only
// mode runs the same rules with no git to fall back on.

// The conflict-marker dialects a working file can be written in. They differ
// between VCSs — jj writes `%%%%%%%` and `+++++++` alongside git's `<<<<<<<` —
// so a reader is told which one it is looking at instead of guessing.
const (
	// MarkersGit is git's `<<<<<<<`/`=======`/`>>>>>>>`.
	MarkersGit = "git"
	// MarkersJujutsu is jj's materialized conflict, which adds `%%%%%%%` diff
	// hunks and `+++++++` snapshots.
	MarkersJujutsu = "jj"
)

// ConflictVersions is the three sides of one conflicted path, plus whatever the
// working tree currently holds.
type ConflictVersions struct {
	// Path is repository-relative and forward-slashed.
	Path string `json:"path"`
	// Kind is "content", "delete-modify", "add-add" or "unknown".
	Kind string `json:"kind"`
	// Base, Ours and Theirs are the three versions. A side that does not exist
	// — an add/add conflict has no base, a delete/modify has no ours — is the
	// empty string, and its Has flag is false.
	Base   string `json:"base,omitempty"`
	Ours   string `json:"ours,omitempty"`
	Theirs string `json:"theirs,omitempty"`

	HasBase   bool `json:"hasBase"`
	HasOurs   bool `json:"hasOurs"`
	HasTheirs bool `json:"hasTheirs"`

	// Rebased reports that the sides were swapped back into the user's frame
	// of reference. During a rebase git replays the local commits onto the
	// upstream, so its stage 2 ("ours") is the remote work and stage 3
	// ("theirs") is the user's own commit — the opposite of what "keep mine"
	// means to a person. Ours is therefore always the user's side here,
	// whatever the VCS underneath calls it.
	Rebased bool `json:"rebased"`
	// Working is what the file holds right now, conflict markers included. It
	// is what the "edit manually" escape hatch starts from.
	Working string `json:"working,omitempty"`
	// Markers is the dialect those markers are written in, MarkersGit or
	// MarkersJujutsu. A reader that parses Working must honor it rather than
	// assume git's.
	Markers string `json:"markers,omitempty"`
	// Binary reports a side that is not text. A binary conflict has no
	// structured resolution: it is keep-ours or keep-theirs, and nothing else
	// (docs/06 section 12, failure 7).
	Binary bool `json:"binary"`
}

// ResolveRequest writes one conflicted path's resolution and, when asked,
// continues the stopped integration.
type ResolveRequest struct {
	// Path is the conflicted path, repository-relative.
	Path string
	// Content is what the file must hold. It is written verbatim.
	Content string
	// Delete removes the path instead of writing it, which is how a
	// delete/modify conflict is resolved in favor of the deletion.
	Delete bool
	// Continue asks for the integration to be carried forward, the way
	// Backend.Resume does, once no conflicted path is left. It is a no-op while
	// others remain, and a no-op in a VCS whose Integration.Resume is
	// ResumeNone: there, resolving the last file finishes the job.
	Continue bool
}

// ResolveResult reports what a resolution did.
type ResolveResult struct {
	Path string `json:"path"`
	// Staged reports that the resolution was recorded: staged in git, written
	// into the working-copy commit in a VCS with no index.
	Staged bool `json:"staged"`
	// Remaining lists the paths that are still conflicted.
	Remaining []Conflict `json:"remaining,omitempty"`
	// Continued reports that the integration was carried forward.
	Continued bool `json:"continued"`
	// Result is what carrying it forward produced, when it happened.
	Result *IntegrateResult `json:"integration,omitempty"`
	// Status is the repository state after the resolution.
	Status SyncStatus `json:"status"`
}

// isBinary reports whether a blob is one the text resolver must not touch. Git
// uses the same rule: a NUL byte in the first 8 000 bytes means binary.
func isBinary(content string) bool {
	limit := len(content)
	if limit > 8000 {
		limit = 8000
	}
	return strings.IndexByte(content[:limit], 0) >= 0
}

// conflictOf finds one path in a conflicted set.
func conflictOf(conflicts []Conflict, path string) (Conflict, bool) {
	for _, c := range conflicts {
		if c.Path == path {
			return c, true
		}
	}
	return Conflict{}, false
}

// notConflicted is the failure of asking about a path that is not conflicted.
func notConflicted(op, path, tree string) *Error {
	return failf(op, CodeNotFound,
		"%s is not conflicted in %s: reload the conflict list, the integration may have moved on",
		path, tree)
}

// swapSides exchanges ours and theirs, which is what a rebase needs: git
// replays the local commits onto the upstream, so its "ours" is the remote
// work. Every caller — the resolver, keep-mine and keep-theirs — speaks the
// user's language instead, where "mine" is the commit the user made.
func (v *ConflictVersions) swapSides() {
	v.Ours, v.Theirs = v.Theirs, v.Ours
	v.HasOurs, v.HasTheirs = v.HasTheirs, v.HasOurs
	v.Rebased = true
}
