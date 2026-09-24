package gitops

import (
	"sort"
	"strings"
)

// This file is the branch listing of GIT-US-0149: the local and remote
// branches a ref picker offers, next to the recent commits Commits already
// lists. It is a read — no backend fetches, snapshots or writes to answer it.

// Branch is one local or remote-tracking branch — a jj bookmark on a jj
// repository.
type Branch struct {
	// Name is what the other methods accept as a ref: `main` for a local
	// branch, `origin/main` for a remote-tracking one, whatever the VCS.
	Name string `json:"name"`
	// Remote names the remote of a remote-tracking branch, empty for a local
	// one.
	Remote string `json:"remote,omitempty"`
	// SHA is the commit the branch points at.
	SHA string `json:"sha"`
	// Current marks the checked-out branch (git) or the bookmark the line of
	// work publishes to (jj).
	Current bool `json:"current,omitempty"`
}

// sortBranches orders a listing the way every backend returns it: local
// branches first, then remote-tracking ones, each by name.
func sortBranches(branches []Branch) []Branch {
	sort.Slice(branches, func(i, j int) bool {
		a, b := branches[i], branches[j]
		if (a.Remote == "") != (b.Remote == "") {
			return a.Remote == ""
		}
		return a.Name < b.Name
	})
	return branches
}

// remoteBranch splits a short remote-tracking name (`origin/main`) into its
// remote and branch halves, or reports false for a name that is not one — the
// symbolic `origin/HEAD` included, which names no branch of its own.
func remoteBranch(short string) (remote, name string, ok bool) {
	remote, name, ok = strings.Cut(short, "/")
	if !ok || remote == "" || name == "" || name == "HEAD" {
		return "", "", false
	}
	return remote, name, true
}
