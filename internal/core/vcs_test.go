package core

import (
	"strings"
	"testing"
)

// TestVCSInfoDescribes covers the vocabulary every surface renders: what a
// repository is called, what it means, and whether the product may write to it
// with git (GIT-US-0038).
func TestVCSInfoDescribes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		info     VCSInfo
		label    string
		writable bool
		summary  string
	}{
		{
			name:     "a git working tree",
			info:     VCSInfo{Kind: VCSGit, GitDir: true},
			label:    "git",
			writable: true,
			summary:  "a git working tree",
		},
		{
			name:     "a colocated jj workspace",
			info:     VCSInfo{Kind: VCSJujutsu, Layout: LayoutColocated, GitDir: true},
			label:    "jj (colocated)",
			writable: false,
			summary:  JujutsuSummary,
		},
		{
			name:     "a jj workspace with an internal store",
			info:     VCSInfo{Kind: VCSJujutsu, Layout: LayoutInternal},
			label:    "jj",
			writable: false,
			summary:  "managed by Jujutsu, with no colocated git working tree — reads and writes go through jj",
		},
		{
			name:     "an unmanaged folder",
			info:     VCSInfo{Kind: VCSNone},
			label:    "none",
			writable: false,
			summary:  "not a version-controlled working tree",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.info.Label(); got != tc.label {
				t.Errorf("Label() = %q, want %q", got, tc.label)
			}
			if got := tc.info.WritableByGit(); got != tc.writable {
				t.Errorf("WritableByGit() = %v, want %v", got, tc.writable)
			}
			if got := tc.info.Summary(); got != tc.summary {
				t.Errorf("Summary() = %q, want %q", got, tc.summary)
			}
			if got := tc.info.IsJujutsu(); got != (tc.info.Kind == VCSJujutsu) {
				t.Errorf("IsJujutsu() = %v for %q", got, tc.info.Kind)
			}
		})
	}
}

// TestJujutsuRefusal pins the wording of the refusal: it has to say what was
// refused, why, and exactly what to run instead.
func TestJujutsuRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		op      string
		path    string
		command string
		wants   []string
	}{
		{
			name:    "a commit names the repository and the jj command",
			op:      "commit",
			path:    "/repos/backlog",
			command: JujutsuCommitCommand,
			wants:   []string{"commit in /repos/backlog was refused", JujutsuSummary, JujutsuCommitCommand},
		},
		{
			name:    "a push with no path still explains itself",
			op:      "push",
			command: JujutsuPushCommand,
			wants:   []string{"push was refused", "abandoned by the next jj command", JujutsuPushCommand},
		},
		{
			name:  "no command means no advice, not a broken sentence",
			op:    "fetch",
			path:  "/repos/backlog",
			wants: []string{"fetch in /repos/backlog was refused", JujutsuSummary},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := JujutsuRefusal(tc.op, tc.path, tc.command)
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Errorf("the refusal does not carry %q:\n%s", want, got)
				}
			}
			if tc.command == "" && strings.Contains(got, "instead") {
				t.Errorf("the refusal advises nothing but says \"instead\":\n%s", got)
			}
		})
	}
}
