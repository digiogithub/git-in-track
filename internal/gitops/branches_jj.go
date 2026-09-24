package gitops

import (
	"context"
	"strings"
)

// Branches lists the bookmarks as branches (GIT-US-0149): a local bookmark is
// a local branch, and a remote bookmark `main@origin` is the remote-tracking
// branch `origin/main`, spelled the git way every other method accepts and
// revsetOf translates back. The `git` pseudo-remote of a colocated repository
// is the local git repository jj exports to, not a host, and is left out; so
// is a conflicted or deleted bookmark, which names no single commit. The read
// runs with --ignore-working-copy, like every read of this backend: listing
// branches never writes an operation.
func (b *jujutsuBackend) Branches(ctx context.Context) ([]Branch, error) {
	raw, err := b.run(ctx, "bookmark", "list", "--all-remotes",
		"-T", `name ++ "`+jujutsuFieldSeparator+`" ++ if(remote, remote, "") ++ "`+
			jujutsuFieldSeparator+`" ++ if(normal_target, normal_target.commit_id(), "") ++ "\n"`)
	if err != nil {
		return nil, wrap("branches", CodeCommitFailed, err, "list the bookmarks of %s", b.path)
	}
	current := ""
	if line, lineErr := b.line(ctx); lineErr == nil && line.Kind == LineBookmark {
		current = line.Name
	}
	out := []Branch{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), jujutsuFieldSeparator)
		if len(fields) < 3 || fields[0] == "" || fields[2] == "" {
			continue
		}
		name, remote, sha := fields[0], fields[1], fields[2]
		switch remote {
		case "":
			out = append(out, Branch{Name: name, SHA: sha, Current: name == current})
		case jujutsuGitRemote:
		default:
			out = append(out, Branch{Name: remote + "/" + name, Remote: remote, SHA: sha})
		}
	}
	return sortBranches(out), nil
}
