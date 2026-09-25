package gitops

import (
	"reflect"
	"strings"
	"testing"
)

// The branch listing of GIT-US-0149. The same history is read by every
// backend, and every backend has to name the branches the same way.

func TestBranches(t *testing.T) {
	dir, baseSHA := changesFixture(t)
	mainSHA := headOf(t, dir)
	g := gitRunner(t, dir)
	g("symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	g("branch", "feat/x", baseSHA)

	onMain := []Branch{
		{Name: "base", SHA: baseSHA},
		{Name: "feat/x", SHA: baseSHA},
		{Name: "main", SHA: mainSHA, Current: true},
		{Name: "origin/main", Remote: "origin", SHA: baseSHA},
	}
	detached := []Branch{
		{Name: "base", SHA: baseSHA},
		{Name: "feat/x", SHA: baseSHA},
		{Name: "main", SHA: mainSHA},
		{Name: "origin/main", Remote: "origin", SHA: baseSHA},
	}
	tests := []struct {
		name  string
		setup func()
		want  []Branch
	}{
		{name: "on a branch", want: onMain},
		{name: "detached HEAD marks no branch current", setup: func() {
			g("checkout", "--detach", "HEAD")
		}, want: detached},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			}
			for _, kind := range backends(t) {
				t.Run(string(kind), func(t *testing.T) {
					got, err := open(t, dir, kind).Branches(t.Context())
					if err != nil {
						t.Fatalf("Branches: %v", err)
					}
					if !reflect.DeepEqual(got, tc.want) {
						t.Errorf("Branches =\n%+v\nwant\n%+v", got, tc.want)
					}
				})
			}
		})
	}
}

func TestBranchesOfAnEmptyRepository(t *testing.T) {
	dir := t.TempDir()
	gitRunner(t, dir)("init", "--initial-branch=main")
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			got, err := open(t, dir, kind).Branches(t.Context())
			if err != nil {
				t.Fatalf("Branches: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("an unborn branch is not listed, got %+v", got)
			}
		})
	}
}

// TestJujutsuBranches lists bookmarks as branches in both layouts, without
// writing an operation, and names remote bookmarks the git way.
func TestJujutsuBranches(t *testing.T) {
	for _, colocated := range []bool{true, false} {
		name := "non-colocated"
		if colocated {
			name = "colocated"
		}
		t.Run(name, func(t *testing.T) {
			f := newJujutsuFixture(t, colocated)
			f.write("a.md", "a\n")
			f.commit("chore: base")
			f.bookmark("base", "@-")
			f.remote()
			f.run("git", "push", "--allow-new", "-b", "base")
			f.write("b.md", "b\n")
			f.commit("feat: main")
			f.bookmark("main", "@-")
			id := func(rev string) string {
				return strings.TrimSpace(f.run("log", "--no-graph", "-r", rev, "-T", "commit_id"))
			}
			baseID, mainID := id("base"), id("main")
			before := f.operations()

			got, err := f.backend().Branches(t.Context())
			if err != nil {
				t.Fatalf("Branches: %v", err)
			}
			want := []Branch{
				{Name: "base", SHA: baseID},
				{Name: "main", SHA: mainID, Current: true},
				{Name: "origin/base", Remote: "origin", SHA: baseID},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Branches =\n%+v\nwant\n%+v", got, want)
			}
			if after := f.operations(); !equalStrings(before, after) {
				t.Errorf("listing the branches wrote an operation: %v -> %v", before, after)
			}
		})
	}
}

func TestRemoteBranch(t *testing.T) {
	tests := []struct {
		short, remote, name string
		ok                  bool
	}{
		{short: "origin/main", remote: "origin", name: "main", ok: true},
		{short: "origin/feat/x", remote: "origin", name: "feat/x", ok: true},
		{short: "origin/HEAD"},
		{short: "origin"},
		{short: "/main"},
	}
	for _, tc := range tests {
		t.Run(tc.short, func(t *testing.T) {
			remote, name, ok := remoteBranch(tc.short)
			if remote != tc.remote || name != tc.name || ok != tc.ok {
				t.Errorf("remoteBranch(%q) = %q, %q, %v", tc.short, remote, name, ok)
			}
		})
	}
}
