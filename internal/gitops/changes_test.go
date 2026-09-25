package gitops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The changed-files primitive of GIT-US-0112. One history is built three ways —
// with git for the two git backends, with jj in both jj layouts — and every
// backend has to return the very same list for it.

// tweakedText is a ten-line file, long enough that editing one line of it
// keeps it above the rename similarity threshold.
const tweakedText = "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n"

// writeBaseTree writes the files of the base commit.
func writeBaseTree(t *testing.T, dir string) {
	t.Helper()
	write(t, dir, "README.md", "# fixture\n")
	write(t, dir, ".gitignore", "build/\n*.log\n")
	write(t, dir, "docs/keep.md", "keep\n")
	write(t, dir, "docs/edit.md", "one\ntwo\nthree\nfour\nfive\n")
	write(t, dir, "docs/trim.md", "a\nb\nc\n")
	write(t, dir, "docs/gone.md", "gone\n")
	write(t, dir, "docs/move.md", "a file that only moves\n")
	write(t, dir, "docs/tweak.md", tweakedText)
	write(t, dir, "img.bin", "\x00\x01\x02")
}

// applyMainChanges edits the base tree into the main commit.
func applyMainChanges(t *testing.T, dir string) {
	t.Helper()
	write(t, dir, "docs/edit.md", "one\ntwo\nTHREE\nfour\nfive\nsix\n")
	write(t, dir, "docs/trim.md", "a\nc\n")
	remove(t, dir, "docs/gone.md")
	move(t, dir, "docs/move.md", "docs/moved.md")
	move(t, dir, "docs/tweak.md", "docs/tweaked.md")
	write(t, dir, "docs/tweaked.md", strings.Replace(tweakedText, "l10\n", "ten\n", 1))
	write(t, dir, "docs/new.md", "new\nfile\n")
	write(t, dir, "img.bin", "\x00\x01\x03")
}

// applyWorkingChanges edits the working tree on top of main without recording
// anything: an edit, an untracked file, two ignored ones and a deletion.
func applyWorkingChanges(t *testing.T, dir string) {
	t.Helper()
	write(t, dir, "README.md", "# fixture\n\nedited\n")
	write(t, dir, "notes.md", "untracked\n")
	write(t, dir, "build/out.txt", "ignored\n")
	write(t, dir, "debug.log", "ignored\n")
	remove(t, dir, "docs/keep.md")
}

// wantMainChanges is what base..main has to report on every backend.
var wantMainChanges = []FileChange{
	{Path: "docs/edit.md", Status: ChangeModified, Lines: []LineRange{{Start: 3, Count: 1}, {Start: 6, Count: 1}}},
	{Path: "docs/gone.md", Status: ChangeDeleted},
	{Path: "docs/moved.md", OldPath: "docs/move.md", Status: ChangeRenamed},
	{Path: "docs/new.md", Status: ChangeAdded, Lines: []LineRange{{Start: 1, Count: 2}}},
	{Path: "docs/trim.md", Status: ChangeModified, Lines: []LineRange{{Start: 1, Count: 0}}},
	{Path: "docs/tweaked.md", OldPath: "docs/tweak.md", Status: ChangeRenamed, Lines: []LineRange{{Start: 10, Count: 1}}},
	{Path: "img.bin", Status: ChangeModified, Binary: true},
}

// wantWorkingChanges is what main..working tree has to report.
var wantWorkingChanges = []FileChange{
	{Path: "README.md", Status: ChangeModified, Lines: []LineRange{{Start: 2, Count: 2}}},
	{Path: "docs/keep.md", Status: ChangeDeleted},
	{Path: "notes.md", Status: ChangeAdded, Lines: []LineRange{{Start: 1, Count: 1}}},
}

// remove deletes a file of the fixture.
func remove(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
}

// move renames a file of the fixture.
func move(t *testing.T, dir, from, to string) {
	t.Helper()
	if err := os.Rename(filepath.Join(dir, filepath.FromSlash(from)), filepath.Join(dir, filepath.FromSlash(to))); err != nil {
		t.Fatalf("move %s to %s: %v", from, to, err)
	}
}

// changesFixture builds the history with git: `base`, then `main` one commit
// later, `origin/main` pointing at base, and the working tree edited.
func changesFixture(t *testing.T) (dir, baseSHA string) {
	t.Helper()
	dir = t.TempDir()
	g := gitRunner(t, dir)
	g("init", "--initial-branch=main")
	configure(t, dir)
	writeBaseTree(t, dir)
	g("add", "-A")
	g("commit", "-m", "chore: base")
	g("branch", "base")
	baseSHA = headOf(t, dir)
	g("update-ref", "refs/remotes/origin/main", baseSHA)
	applyMainChanges(t, dir)
	g("add", "-A")
	g("commit", "-m", "feat: main")
	applyWorkingChanges(t, dir)
	return dir, baseSHA
}

// headOf reads the commit HEAD points at.
func headOf(t *testing.T, dir string) string {
	t.Helper()
	b := open(t, dir, KindGoGit)
	commits, err := b.Commits(t.Context(), LogRequest{To: "HEAD"})
	if err != nil || len(commits) == 0 {
		t.Fatalf("read HEAD of %s: %v", dir, err)
	}
	return commits[0].SHA
}

func TestChangedFiles(t *testing.T) {
	dir, baseSHA := changesFixture(t)
	tests := []struct {
		name     string
		from, to string
		want     []FileChange
		code     string
	}{
		{name: "branch to branch", from: "base", to: "main", want: wantMainChanges},
		{name: "full SHA to branch", from: baseSHA, to: "main", want: wantMainChanges},
		{name: "short SHA to HEAD", from: baseSHA[:10], to: "HEAD", want: wantMainChanges},
		{name: "remote-tracking ref", from: "origin/main", to: "main", want: wantMainChanges},
		{name: "a revision against itself", from: "main", to: "main", want: []FileChange{}},
		{name: "revision to working tree", from: "main", to: WorkingTree, want: wantWorkingChanges},
		{name: "backwards swaps adds and deletes", from: "main", to: "base", want: []FileChange{
			{Path: "docs/edit.md", Status: ChangeModified, Lines: []LineRange{{Start: 3, Count: 1}, {Start: 5, Count: 0}}},
			{Path: "docs/gone.md", Status: ChangeAdded, Lines: []LineRange{{Start: 1, Count: 1}}},
			{Path: "docs/move.md", OldPath: "docs/moved.md", Status: ChangeRenamed},
			{Path: "docs/new.md", Status: ChangeDeleted},
			{Path: "docs/trim.md", Status: ChangeModified, Lines: []LineRange{{Start: 2, Count: 1}}},
			{Path: "docs/tweak.md", OldPath: "docs/tweaked.md", Status: ChangeRenamed, Lines: []LineRange{{Start: 10, Count: 1}}},
			{Path: "img.bin", Status: ChangeModified, Binary: true},
		}},
		{name: "an unknown from is typed", from: "no-such-branch", to: "main", code: CodeUnknownRevision},
		{name: "an unknown to is typed", from: "base", to: "no-such-branch", code: CodeUnknownRevision},
		{name: "an empty from is typed", from: "", to: "main", code: CodeUnknownRevision},
		{name: "an option-shaped ref is typed", from: "--all", to: "main", code: CodeUnknownRevision},
	}
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			b := open(t, dir, kind)
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					got, err := b.ChangedFiles(t.Context(), tc.from, tc.to)
					if tc.code != "" {
						if CodeOf(err) != tc.code {
							t.Fatalf("err = %v (code %q), want code %q", err, CodeOf(err), tc.code)
						}
						return
					}
					if err != nil {
						t.Fatalf("ChangedFiles(%q, %q): %v", tc.from, tc.to, err)
					}
					assertChanges(t, got, tc.want)
				})
			}
		})
	}
}

// TestJujutsuChangedFiles builds the same history with jj and expects the same
// answer, in both layouts, without the read writing a single operation.
func TestJujutsuChangedFiles(t *testing.T) {
	for _, colocated := range []bool{true, false} {
		name := "non-colocated"
		if colocated {
			name = "colocated"
		}
		t.Run(name, func(t *testing.T) {
			f := newJujutsuFixture(t, colocated)
			writeBaseTree(t, f.dir)
			f.commit("chore: base")
			f.bookmark("base", "@-")
			f.remote()
			f.run("git", "push", "--allow-new", "-b", "base")
			applyMainChanges(t, f.dir)
			f.commit("feat: main")
			f.bookmark("main", "@-")
			baseID := strings.TrimSpace(f.run("log", "--no-graph", "-r", "base", "-T", "commit_id"))
			applyWorkingChanges(t, f.dir)
			before := f.operations()

			tests := []struct {
				name     string
				from, to string
				want     []FileChange
				code     string
			}{
				{name: "bookmark to bookmark", from: "base", to: "main", want: wantMainChanges},
				{name: "commit id to bookmark", from: baseID, to: "main", want: wantMainChanges},
				{name: "short commit id", from: baseID[:10], to: "main", want: wantMainChanges},
				{name: "remote-tracking ref", from: "origin/base", to: "main", want: wantMainChanges},
				{name: "revision to working tree", from: "main", to: WorkingTree, want: wantWorkingChanges},
				{name: "an unknown ref is typed", from: "no-such-bookmark", to: "main", code: CodeUnknownRevision},
				{name: "an empty from is typed", from: "", to: "main", code: CodeUnknownRevision},
			}
			b := f.backend()
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					got, err := b.ChangedFiles(t.Context(), tc.from, tc.to)
					if tc.code != "" {
						if CodeOf(err) != tc.code {
							t.Fatalf("err = %v (code %q), want code %q", err, CodeOf(err), tc.code)
						}
						return
					}
					if err != nil {
						t.Fatalf("ChangedFiles(%q, %q): %v", tc.from, tc.to, err)
					}
					assertChanges(t, got, tc.want)
				})
			}
			if after := f.operations(); !equalStrings(before, after) {
				t.Errorf("reading the changed files wrote an operation: %v -> %v", before, after)
			}

			if !colocated {
				return
			}
			// The same colocated store read by the git backends gives the
			// same answer for the committed range.
			for _, opener := range []func(string, Options) (Backend, error){openGoGit, openSystem} {
				git, err := opener(f.dir, Options{})
				if err != nil {
					t.Logf("skip a git backend over the colocated store: %v", err)
					continue
				}
				got, err := git.ChangedFiles(t.Context(), "base", "main")
				if err != nil {
					t.Fatalf("%s: ChangedFiles: %v", git.Name(), err)
				}
				assertChanges(t, got, wantMainChanges)
			}
		})
	}
}

// assertChanges compares two change lists field by field.
func assertChanges(t *testing.T, got, want []FileChange) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changes differ\n got: %+v\nwant: %+v", got, want)
	}
}

func TestLineRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		before, after string
		want          []LineRange
	}{
		{name: "identical", before: "a\nb\n", after: "a\nb\n"},
		{name: "one line replaced", before: "a\nb\nc\n", after: "a\nB\nc\n", want: []LineRange{{2, 1}}},
		{name: "lines inserted", before: "a\nc\n", after: "a\nb1\nb2\nc\n", want: []LineRange{{2, 2}}},
		{name: "deleted from the top", before: "a\nb\n", after: "b\n", want: []LineRange{{0, 0}}},
		{name: "deleted from the end", before: "a\nb\n", after: "a\n", want: []LineRange{{1, 0}}},
		{name: "a new file", before: "", after: "a\nb\nc", want: []LineRange{{1, 3}}},
		{name: "a trailing newline added", before: "a\nb", after: "a\nb\n", want: []LineRange{{2, 1}}},
		{name: "two separate hunks", before: "1\n2\n3\n4\n5\n", after: "one\n2\n3\n4\nfive\n",
			want: []LineRange{{1, 1}, {5, 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := lineRanges(tc.before, tc.after); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("lineRanges = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectRenamesIsDeterministic(t *testing.T) {
	t.Parallel()
	same := []byte("identical content\n")
	oldData := map[string][]byte{"a.md": same, "b.md": same, "empty.md": {}}
	newData := map[string][]byte{"x.md": same, "y.md": same, "blank.md": {}}
	for range 20 {
		got := detectRenames([]string{"a.md", "b.md", "empty.md"}, []string{"blank.md", "x.md", "y.md"}, oldData, newData)
		want := []renamePair{{from: "a.md", to: "x.md"}, {from: "b.md", to: "y.md"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("renames = %v, want %v", got, want)
		}
	}
}
