package gitops

import (
	"context"
	"strings"
)

// Branches lists the local and remote-tracking branches, local first, each
// sorted by name (GIT-US-0149). `for-each-ref` reads refs only, so it is safe
// to run while an integration is in progress; symbolic refs such as
// `origin/HEAD` are left out.
func (b *systemBackend) Branches(ctx context.Context) ([]Branch, error) {
	raw, err := b.run(ctx, "for-each-ref",
		"--format=%(refname)"+commitFieldSeparator+"%(objectname)"+commitFieldSeparator+
			"%(symref)"+commitFieldSeparator+"%(HEAD)",
		"refs/heads", "refs/remotes")
	if err != nil {
		return nil, wrap("branches", CodeCommitFailed, err, "list the branches of %s", b.path)
	}
	out := []Branch{}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), commitFieldSeparator)
		if len(fields) < 4 || fields[0] == "" || fields[2] != "" {
			continue
		}
		if name, ok := strings.CutPrefix(fields[0], "refs/heads/"); ok {
			out = append(out, Branch{Name: name, SHA: fields[1], Current: fields[3] == "*"})
			continue
		}
		short := strings.TrimPrefix(fields[0], "refs/remotes/")
		if remote, _, ok := remoteBranch(short); ok {
			out = append(out, Branch{Name: short, Remote: remote, SHA: fields[1]})
		}
	}
	return sortBranches(out), nil
}
