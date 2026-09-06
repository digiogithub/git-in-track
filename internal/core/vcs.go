package core

import "strings"

// The version-control vocabulary of the product, story GIT-US-0038.
//
// Everything here is pure: no filesystem, no process, no OS. It compiles to
// WebAssembly with the rest of the core, so the browser runtime, the companion
// server and the CLI all describe a repository with the same words. Deciding
// which kind a folder on disk actually is belongs to internal/gitops, which is
// allowed to stat files and run binaries; deciding what that means and how to
// say it belongs here.

// VCS is the version-control system a repository is managed with.
type VCS string

// The kinds the product knows.
const (
	// VCSNone is a folder that no supported VCS manages.
	VCSNone VCS = "none"
	// VCSGit is a plain git working tree.
	VCSGit VCS = "git"
	// VCSJujutsu is a Jujutsu repository. It stores its commits in a git
	// repository, so reads work, but the git-shaped write path does not: git's
	// HEAD sits at the parent of the working-copy commit and no bookmark
	// follows a git commit (GIT-EP-0010).
	VCSJujutsu VCS = "jj"
)

// VCSLayout is how a Jujutsu repository is arranged on disk. It is empty for
// every other kind.
type VCSLayout string

// The Jujutsu layouts.
const (
	// LayoutNone is the layout of anything that is not a jj repository.
	LayoutNone VCSLayout = ""
	// LayoutColocated means the git repository jj stores its commits in is the
	// workspace's own `.git`, so git reads — log, history, metrics — work
	// against the same working tree the user edits.
	LayoutColocated VCSLayout = "colocated"
	// LayoutInternal means the git store lives inside `.jj/`, so the workspace
	// root is not a git working tree at all. Reads that need git are
	// unavailable; the backlog files themselves are read and indexed normally.
	LayoutInternal VCSLayout = "internal"
)

// VCSInfo is what a repository is managed with, and how.
type VCSInfo struct {
	// Kind is the version-control system.
	Kind VCS `json:"kind"`
	// Layout is the Jujutsu layout, empty for every other kind.
	Layout VCSLayout `json:"layout,omitempty"`
	// GitDir reports whether a git repository is reachable from the workspace
	// root. It is true for plain git and for a colocated jj repository.
	GitDir bool `json:"gitDir"`
}

// IsJujutsu reports whether the repository is managed with Jujutsu.
func (v VCSInfo) IsJujutsu() bool { return v.Kind == VCSJujutsu }

// WritableByGit reports whether the product may run a git write command —
// commit, rebase, push — against this repository. It is false for every jj
// repository: a git commit there lands on the parent of the working-copy
// commit, moves no bookmark and is abandoned as an orphan by the next jj
// command (GIT-US-0038).
func (v VCSInfo) WritableByGit() bool { return v.Kind == VCSGit }

// Label is the one-word name of the kind, for a table cell.
func (v VCSInfo) Label() string {
	switch v.Kind {
	case VCSGit:
		return "git"
	case VCSJujutsu:
		if v.Layout == LayoutColocated {
			return "jj (colocated)"
		}
		return "jj"
	case VCSNone:
		return "none"
	}
	return string(v.Kind)
}

// Summary is the sentence a surface shows next to a repository. It states what
// works and what does not, which for a jj repository is the whole point: reads
// are fine and writes go through jj.
func (v VCSInfo) Summary() string {
	switch v.Kind {
	case VCSJujutsu:
		if v.Layout == LayoutInternal {
			return "managed by Jujutsu, with no colocated git working tree — " +
				"reads and writes go through jj"
		}
		return JujutsuSummary
	case VCSGit:
		return "a git working tree"
	case VCSNone:
		return "not a version-controlled working tree"
	}
	return string(v.Kind)
}

// JujutsuSummary is the fixed sentence every surface uses for a jj repository.
// It is one string so the CLI, the API and the web app cannot drift.
const JujutsuSummary = "managed by Jujutsu — reads and writes go through jj"

// MinJujutsuVersion is the oldest jj release the product is verified against.
// It is the version the reference repositories of GIT-EP-0010 run.
const MinJujutsuVersion = "0.41"

// The jj commands the refusals point at. They are the commands that do, in jj,
// what the git write path was about to do behind jj's back.
const (
	// JujutsuCommitCommand records the working-copy commit and starts a new one.
	JujutsuCommitCommand = "jj commit -m <message>"
	// JujutsuFetchCommand brings remote work in.
	JujutsuFetchCommand = "jj git fetch"
	// JujutsuRebaseCommand replays work onto another commit.
	JujutsuRebaseCommand = "jj rebase -d <destination>"
	// JujutsuPushCommand publishes a bookmark.
	JujutsuPushCommand = "jj git push"
	// JujutsuUndoCommand is jj's answer to `--abort` and to the reflog.
	JujutsuUndoCommand = "jj undo"
	// JujutsuResolveCommand resolves the conflicts jj records inside a commit.
	JujutsuResolveCommand = "jj resolve"
)

// JujutsuRefusal is the message a refused git write carries. `op` names what
// the product was asked to do and `command` is the jj command that does it;
// `path` is the repository, and may be empty when the caller has no path to
// name.
//
// The wording is fixed and tested because it is the only thing the user sees at
// the moment the product declines to act: it has to say what was refused, why,
// and exactly what to run instead.
func JujutsuRefusal(op, path, command string) string {
	var b strings.Builder
	b.WriteString(op)
	if path != "" {
		b.WriteString(" in ")
		b.WriteString(path)
	}
	b.WriteString(" was refused: ")
	b.WriteString(JujutsuSummary)
	b.WriteString(". A git write here would land on the parent of the working-copy " +
		"commit, move no bookmark and be abandoned by the next jj command")
	if command != "" {
		b.WriteString("; run `")
		b.WriteString(command)
		b.WriteString("` instead")
	}
	return b.String()
}
