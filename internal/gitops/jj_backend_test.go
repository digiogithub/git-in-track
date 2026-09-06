package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The jj backend of GIT-US-0040.
//
// Every table here drives a real jj repository built under the test's own
// temporary folder, and every one of them skips cleanly when no jj binary is
// installed, the way the git tables skip without git.

// jjFixture is a throwaway jj repository plus the helpers that build it.
type jjFixture struct {
	t   *testing.T
	dir string
	bin string
	// clock stamps every jj invocation, one day later than the last.
	clock time.Time
}

// newJujutsuFixture creates an empty jj workspace, colocated or not.
func newJujutsuFixture(t *testing.T, colocated bool) *jjFixture {
	t.Helper()
	bin, _, err := ResolveJujutsu("")
	if err != nil {
		t.Skipf("these tests need a jj binary to build the fixture: %v", err)
	}
	f := &jjFixture{
		t: t, dir: t.TempDir(), bin: bin,
		clock: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	if colocated {
		f.run("git", "init", "--colocate")
	} else {
		f.run("git", "init", "--no-colocate")
	}
	return f
}

// run executes jj in the fixture and fails the test when it does not succeed.
func (f *jjFixture) run(args ...string) string {
	f.t.Helper()
	out, err := f.try(args...)
	if err != nil {
		f.t.Fatalf("jj %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// try executes jj in the fixture and returns its combined output.
//
// Every commit is stamped a day after the previous one. jj records the author
// timestamp of a whole operation at once, so three commits made in the same
// test would otherwise share an instant and the history would have no order to
// report — a fixture artifact that has nothing to do with a real repository.
func (f *jjFixture) try(args ...string) (string, error) {
	f.t.Helper()
	cmd := exec.CommandContext(f.t.Context(), f.bin, args...)
	cmd.Dir = f.dir
	f.clock = f.clock.AddDate(0, 0, 1)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"JJ_TIMESTAMP="+f.clock.Format(time.RFC3339),
	)
	raw, err := cmd.CombinedOutput()
	return string(raw), err //nolint:wrapcheck // a test helper reports the raw failure
}

// write puts a file in the fixture's working copy.
func (f *jjFixture) write(rel, text string) {
	f.t.Helper()
	write(f.t, f.dir, rel, text)
}

// commit records the working copy under a message and starts a new one.
func (f *jjFixture) commit(message string) {
	f.t.Helper()
	f.run("commit", "-m", message)
}

// bookmark points a bookmark at a revision, creating it when it is new.
func (f *jjFixture) bookmark(name, revision string) {
	f.t.Helper()
	f.run("bookmark", "set", name, "-r", revision, "--allow-backwards")
}

// remote creates a bare git repository and registers it as origin.
func (f *jjFixture) remote() string {
	f.t.Helper()
	bin, _, err := resolveGit("")
	if err != nil {
		f.t.Skipf("this test needs a git binary to build a remote: %v", err)
	}
	path := filepath.Join(f.t.TempDir(), "origin.git")
	cmd := exec.CommandContext(f.t.Context(), bin, "init", "--bare", "--initial-branch=main", path)
	if raw, initErr := cmd.CombinedOutput(); initErr != nil {
		f.t.Fatalf("git init --bare: %v\n%s", initErr, raw)
	}
	f.run("git", "remote", "add", "origin", path)
	return path
}

// operations lists the ids in the operation log, newest first. Comparing two
// readings of it is how the tests prove that reading created nothing.
func (f *jjFixture) operations() []string {
	f.t.Helper()
	raw := f.run("--ignore-working-copy", "op", "log", "--no-graph", "-T", `id.short() ++ "\n"`)
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// backend opens the jj backend on the fixture.
func (f *jjFixture) backend() Backend {
	f.t.Helper()
	b, err := Open(f.dir, Options{})
	if err != nil {
		f.t.Fatalf("open the jj repository: %v", err)
	}
	if b.Name() != string(KindJujutsu) {
		f.t.Fatalf("backend = %q, want %q", b.Name(), KindJujutsu)
	}
	return b
}

// TestJujutsuBackendIsSelected proves that a jj working tree is driven by the
// jj backend whatever `git.backend` says, in both layouts — including the
// non-colocated one, which no git backend can serve at all.
func TestJujutsuBackendIsSelected(t *testing.T) {
	tests := []struct {
		name      string
		colocated bool
		requested Kind
	}{
		{name: "a colocated repository under auto", colocated: true, requested: KindAuto},
		{name: "a colocated repository under system", colocated: true, requested: KindSystem},
		{name: "a colocated repository under go-git", colocated: true, requested: KindGoGit},
		{name: "a non-colocated repository under auto", colocated: false, requested: KindAuto},
		{name: "a non-colocated repository under system", colocated: false, requested: KindSystem},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newJujutsuFixture(t, tc.colocated)
			f.write("README.md", "# fixture\n")
			f.commit("chore: seed the fixture")

			b, err := Open(f.dir, Options{Backend: tc.requested})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if b.Name() != string(KindJujutsu) {
				t.Fatalf("backend = %q, want %q", b.Name(), KindJujutsu)
			}
			caps := b.Capabilities()
			if caps.VCS != "jj" || caps.Writes {
				t.Errorf("capabilities = %+v, want vcs=jj and writes=false", caps)
			}
			if caps.Version == "" {
				t.Error("the capabilities do not report the jj version")
			}
			if info := VCSOf(b); !info.IsJujutsu() {
				t.Errorf("VCSOf = %+v, want a jj repository", info)
			}
		})
	}
}

// TestJujutsuStatus covers the line of work and the dirty set, which are the
// two things git reads wrongly behind jj.
func TestJujutsuStatus(t *testing.T) {
	tests := []struct {
		name      string
		colocated bool
		build     func(f *jjFixture)
		want      Line
		modified  []string
		untracked []string
		clean     bool
	}{
		{
			name:      "a bookmark is the line of work, not a detached HEAD",
			colocated: true,
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
			},
			want:  Line{Name: "main", Kind: LineBookmark, PushTarget: "main"},
			clean: true,
		},
		{
			name:      "a non-colocated repository reads the same way",
			colocated: false,
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
			},
			want:  Line{Name: "main", Kind: LineBookmark, PushTarget: "main"},
			clean: true,
		},
		{
			name:      "a working copy with no bookmark is @, and is not anonymous",
			colocated: true,
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
			},
			want:  Line{Name: JujutsuWorkingCopy, Kind: LineWorkingCopy},
			clean: true,
		},
		{
			name:      "an added path is untracked and a changed one is modified",
			colocated: true,
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.write("docs/keep.md", "keep\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
				f.write("README.md", "# fixture\n\nedited\n")
				f.write("docs/new.md", "new\n")
				f.run("status")
			},
			want:      Line{Name: "main", Kind: LineBookmark, PushTarget: "main"},
			modified:  []string{"README.md"},
			untracked: []string{"docs/new.md"},
		},
		{
			name:      "a removed path is modified, because jj records the removal",
			colocated: true,
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.write("docs/gone.md", "gone\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
				if err := os.Remove(filepath.Join(f.dir, "docs", "gone.md")); err != nil {
					f.t.Fatalf("remove the file: %v", err)
				}
				f.run("status")
			},
			want:     Line{Name: "main", Kind: LineBookmark, PushTarget: "main"},
			modified: []string{"docs/gone.md"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newJujutsuFixture(t, tc.colocated)
			tc.build(f)

			st, err := f.backend().Status(t.Context())
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if st.Line != tc.want {
				t.Errorf("line = %+v, want %+v", st.Line, tc.want)
			}
			if st.Anonymous {
				t.Error("a jj working copy was reported as anonymous")
			}
			if len(st.Staged) != 0 {
				t.Errorf("staged = %v, want none: jj has no index", st.Staged)
			}
			if !equalStrings(st.Modified, tc.modified) {
				t.Errorf("modified = %v, want %v", st.Modified, tc.modified)
			}
			if !equalStrings(st.Untracked, tc.untracked) {
				t.Errorf("untracked = %v, want %v", st.Untracked, tc.untracked)
			}
			if st.Clean != tc.clean {
				t.Errorf("clean = %v, want %v", st.Clean, tc.clean)
			}
		})
	}
}

// TestJujutsuSyncStatus covers the states the placeholder `jujutsu` used to
// hide, plus the integration a jj repository reports.
func TestJujutsuSyncStatus(t *testing.T) {
	tests := []struct {
		name     string
		build    func(f *jjFixture)
		state    State
		ahead    int
		behind   int
		upstream string
	}{
		{
			name: "no remote at all",
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
			},
			state: StateNoRemote,
		},
		{
			name: "a remote the bookmark does not track yet",
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
				f.remote()
			},
			state: StateNoUpstream,
		},
		{
			name: "everything published",
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
				f.remote()
				f.run("git", "push", "--allow-new", "-b", "main")
			},
			state:    StateUpToDate,
			upstream: "origin/main",
		},
		{
			name: "a commit the remote does not have",
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
				f.remote()
				f.run("git", "push", "--allow-new", "-b", "main")
				f.write("README.md", "# fixture\n\nmore\n")
				f.commit("docs: add more")
				f.bookmark("main", "@-")
			},
			state:    StateAhead,
			ahead:    1,
			upstream: "origin/main",
		},
		{
			name: "a bookmark moved back behind what was published",
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.write("README.md", "# fixture\n\nmore\n")
				f.commit("docs: add more")
				f.bookmark("main", "@-")
				f.remote()
				f.run("git", "push", "--allow-new", "-b", "main")
				f.bookmark("main", "@--")
			},
			state:    StateBehind,
			behind:   1,
			upstream: "origin/main",
		},
		{
			name: "an uncommitted change with nothing to publish",
			build: func(f *jjFixture) {
				f.write("README.md", "# fixture\n")
				f.commit("chore: seed the fixture")
				f.bookmark("main", "@-")
				f.remote()
				f.run("git", "push", "--allow-new", "-b", "main")
				f.write("README.md", "# fixture\n\nedited\n")
				f.run("status")
			},
			state:    StateDirty,
			upstream: "origin/main",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newJujutsuFixture(t, true)
			tc.build(f)

			st, err := f.backend().SyncStatus(t.Context())
			if err != nil {
				t.Fatalf("sync status: %v", err)
			}
			if st.State != tc.state {
				t.Errorf("state = %q, want %q", st.State, tc.state)
			}
			if st.Ahead != tc.ahead || st.Behind != tc.behind {
				t.Errorf("ahead/behind = %d/%d, want %d/%d", st.Ahead, st.Behind, tc.ahead, tc.behind)
			}
			if st.Upstream != tc.upstream {
				t.Errorf("upstream = %q, want %q", st.Upstream, tc.upstream)
			}
			if !st.Jujutsu {
				t.Error("the sync status does not report the repository as jj")
			}
			if st.Anonymous {
				t.Error("a jj working copy was reported as anonymous")
			}
			if st.Unfinished {
				t.Error("a jj repository reported an unfinished integration")
			}
			if st.Undo != UndoOperationLog {
				t.Errorf("undo = %q, want %q", st.Undo, UndoOperationLog)
			}
			if st.Resume != ResumeNone {
				t.Errorf("resume = %q, want %q", st.Resume, ResumeNone)
			}
		})
	}
}

// TestJujutsuConflictFile reads the three sides out of the conflict jj records
// inside a commit, which is the only way to read them where there is no index.
func TestJujutsuConflictFile(t *testing.T) {
	tests := []struct {
		name      string
		colocated bool
	}{
		{name: "colocated", colocated: true},
		{name: "non-colocated", colocated: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newJujutsuFixture(t, tc.colocated)
			f.write("f.txt", "line1\nline2\nline3\n")
			f.run("describe", "-m", "base")
			f.bookmark("base", "@")
			f.run("new", "-m", "ours")
			f.write("f.txt", "line1\nOURS\nline3\n")
			f.bookmark("ours", "@")
			f.run("new", "base", "-m", "theirs")
			f.write("f.txt", "line1\nTHEIRS\nline3\n")
			f.bookmark("theirs", "@")
			f.run("new", "ours", "theirs", "-m", "merged")

			b := f.backend()
			st, err := b.SyncStatus(t.Context())
			if err != nil {
				t.Fatalf("sync status: %v", err)
			}
			if st.State != StateConflicted {
				t.Fatalf("state = %q, want %q", st.State, StateConflicted)
			}
			if len(st.Conflicted) != 1 || st.Conflicted[0].Path != "f.txt" {
				t.Fatalf("conflicted = %+v, want f.txt", st.Conflicted)
			}
			if st.Conflicted[0].Kind != ConflictContent {
				t.Errorf("kind = %q, want %q", st.Conflicted[0].Kind, ConflictContent)
			}

			versions, err := b.ConflictFile(t.Context(), "f.txt")
			if err != nil {
				t.Fatalf("conflict file: %v", err)
			}
			if versions.Markers != MarkersJujutsu {
				t.Errorf("markers = %q, want %q", versions.Markers, MarkersJujutsu)
			}
			if versions.Base != "line1\nline2\nline3\n" {
				t.Errorf("base = %q", versions.Base)
			}
			if versions.Ours != "line1\nOURS\nline3\n" {
				t.Errorf("ours = %q", versions.Ours)
			}
			if versions.Theirs != "line1\nTHEIRS\nline3\n" {
				t.Errorf("theirs = %q", versions.Theirs)
			}
			if !versions.HasBase || !versions.HasOurs || !versions.HasTheirs {
				t.Errorf("sides = %+v, want all three present", versions)
			}
			if !strings.Contains(versions.Working, "<<<<<<<") {
				t.Errorf("the working file does not hold the materialized conflict: %q", versions.Working)
			}
			if _, err := b.ConflictFile(t.Context(), "absent.txt"); CodeOf(err) != CodeNotFound {
				t.Errorf("an unconflicted path answered %v, want %q", err, CodeNotFound)
			}
		})
	}
}

// TestJujutsuHistory proves the metrics can be rebuilt from either layout —
// including the non-colocated one, which had no history at all before.
func TestJujutsuHistory(t *testing.T) {
	tests := []struct {
		name      string
		colocated bool
	}{
		{name: "colocated", colocated: true},
		{name: "non-colocated", colocated: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newJujutsuFixture(t, tc.colocated)
			f.write("docs/item.md", "status: todo\n")
			f.commit("feat: create the item")
			f.write("docs/item.md", "status: in_progress\n")
			f.commit("feat: start the item")
			f.write("docs/item.md", "status: done\n")
			f.commit("feat: finish the item")

			history, err := f.backend().History(t.Context(), HistoryRequest{Paths: []string{"docs/item.md"}})
			if err != nil {
				t.Fatalf("history: %v", err)
			}
			if history.Head == "" {
				t.Error("the history does not report the commit it was read at")
			}
			var states []string
			for _, rev := range history.Revisions {
				states = append(states, strings.TrimSpace(string(rev.Data)))
			}
			want := []string{"status: todo", "status: in_progress", "status: done"}
			if !equalStrings(states, want) {
				t.Errorf("revisions = %v, want %v", states, want)
			}
		})
	}
}

// TestJujutsuCommits lists commits over a revset, translating the git-shaped
// refs the callers pass.
func TestJujutsuCommits(t *testing.T) {
	f := newJujutsuFixture(t, true)
	f.write("README.md", "# fixture\n")
	f.commit("chore: seed the fixture")
	f.bookmark("main", "@-")
	f.remote()
	f.run("git", "push", "--allow-new", "-b", "main")
	f.write("README.md", "# fixture\n\nmore\n")
	f.commit("docs: add more")
	f.bookmark("main", "@-")

	b := f.backend()
	commits, err := b.Commits(t.Context(), LogRequest{From: "origin/main", To: "main"})
	if err != nil {
		t.Fatalf("commits: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("commits = %+v, want exactly the unpublished one", commits)
	}
	if commits[0].Subject != "docs: add more" {
		t.Errorf("subject = %q, want %q", commits[0].Subject, "docs: add more")
	}
	if commits[0].SHA == "" || commits[0].Date == "" || commits[0].Author == "" {
		t.Errorf("commit = %+v, want every field filled", commits[0])
	}
}

// TestJujutsuReadsCreateNoOperation is the safety property of this story.
//
// Almost every jj command snapshots the working copy before it runs, which
// writes a new working-copy commit and an entry in the operation log. A status
// poll or an indexer that did that would rewrite the user's repository as a
// side effect of reading it, so the whole read surface is driven here with an
// edited working copy and the operation log has to come out untouched.
func TestJujutsuReadsCreateNoOperation(t *testing.T) {
	f := newJujutsuFixture(t, true)
	f.write("README.md", "# fixture\n")
	f.write("docs/item.md", "status: todo\n")
	f.commit("chore: seed the fixture")
	f.bookmark("main", "@-")
	f.remote()
	f.run("git", "push", "--allow-new", "-b", "main")
	// An edit jj has not snapshotted yet: the tempting moment to snapshot.
	f.write("README.md", "# fixture\n\nedited\n")
	f.write("docs/new.md", "new\n")

	b := f.backend()
	before := f.operations()

	reads := []struct {
		name string
		call func() error
	}{
		{name: "status", call: func() error { _, err := b.Status(t.Context()); return err }},
		{name: "sync status", call: func() error { _, err := b.SyncStatus(t.Context()); return err }},
		{name: "identity", call: func() error { _, err := b.Identity(t.Context()); return err }},
		{name: "commits", call: func() error {
			_, err := b.Commits(t.Context(), LogRequest{To: "HEAD", Limit: 10})
			return err
		}},
		{name: "history", call: func() error {
			_, err := b.History(t.Context(), HistoryRequest{Paths: []string{"docs/item.md"}})
			return err
		}},
	}
	for _, read := range reads {
		t.Run(read.name, func(t *testing.T) {
			if err := read.call(); err != nil {
				t.Fatalf("%s: %v", read.name, err)
			}
			if after := f.operations(); !equalStrings(after, before) {
				t.Fatalf("%s created a jj operation:\nbefore: %v\nafter:  %v", read.name, before, after)
			}
		})
	}

	// The un-snapshotted edit is still on disk, untouched by any of the reads.
	raw, err := os.ReadFile(filepath.Join(f.dir, "README.md"))
	if err != nil || string(raw) != "# fixture\n\nedited\n" {
		t.Fatalf("the working file changed under the reads: %q, %v", raw, err)
	}
}

// TestJujutsuBackendRefusesEveryWrite pins the boundary of this story: the read
// half landed, the write half is GIT-US-0041 and is still refused with the jj
// command that does the same thing safely.
func TestJujutsuBackendRefusesEveryWrite(t *testing.T) {
	f := newJujutsuFixture(t, true)
	f.write("README.md", "# fixture\n")
	f.commit("chore: seed the fixture")
	f.bookmark("main", "@-")
	b := f.backend()
	before := f.operations()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "commit", call: func() error {
			_, err := b.Commit(t.Context(), CommitRequest{Paths: []string{"README.md"}})
			return err
		}},
		{name: "fetch", call: func() error { _, err := b.Fetch(t.Context(), FetchRequest{}); return err }},
		{name: "integrate", call: func() error {
			_, err := b.Integrate(t.Context(), IntegrateRequest{})
			return err
		}},
		{name: "push", call: func() error { _, err := b.Push(t.Context(), PushRequest{}); return err }},
		{name: "undo", call: func() error { return b.Undo(t.Context()) }},
		{name: "resume", call: func() error { _, err := b.Resume(t.Context()); return err }},
		{name: "resolve", call: func() error {
			_, err := b.ResolvePath(t.Context(), ResolveRequest{Path: "README.md"})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if code := CodeOf(err); code != CodeJujutsuWriteRefused {
				t.Fatalf("%s answered %v, want %q", tc.name, err, CodeJujutsuWriteRefused)
			}
		})
	}
	if after := f.operations(); !equalStrings(after, before) {
		t.Fatalf("a refused write still touched the repository:\nbefore: %v\nafter:  %v", before, after)
	}
}

// TestParseJujutsuConflict pins the reconstruction of the three sides from
// jj's marker dialect, which is not git's.
func TestParseJujutsuConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		materialized             string
		base, ours, theirs       string
		hasBase, hasOurs, hasThe bool
	}{
		{
			name: "a two-sided content conflict",
			materialized: "line1\n" +
				"<<<<<<< conflict 1 of 1\n" +
				"%%%%%%% diff from: aaa 111\n" +
				`\\\\\\\        to: bbb 222 "ours"` + "\n" +
				"-line2\n" +
				"+OURS\n" +
				"+++++++ ccc 333 \"theirs\"\n" +
				"THEIRS\n" +
				">>>>>>> conflict 1 of 1 ends\n" +
				"line3\n",
			base:    "line1\nline2\nline3\n",
			ours:    "line1\nOURS\nline3\n",
			theirs:  "line1\nTHEIRS\nline3\n",
			hasBase: true, hasOurs: true, hasThe: true,
		},
		{
			name: "a side the conflict deletes materializes empty",
			materialized: "<<<<<<< conflict 1 of 1\n" +
				"%%%%%%% diff from: aaa 111\n" +
				`\\\\\\\        to: bbb 222 "del"` + "\n" +
				"-a\n" +
				"+++++++ ccc 333 \"mod\"\n" +
				"b\n" +
				">>>>>>> conflict 1 of 1 ends\n",
			base:    "a\n",
			ours:    "",
			theirs:  "b\n",
			hasBase: true, hasOurs: false, hasThe: true,
		},
		{
			name:         "a file with no conflict is the same on every side",
			materialized: "plain\n",
			base:         "plain\n",
			ours:         "plain\n",
			theirs:       "plain\n",
			hasBase:      true, hasOurs: true, hasThe: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseJujutsuConflict(tc.materialized)
			if got.Base != tc.base {
				t.Errorf("base = %q, want %q", got.Base, tc.base)
			}
			if got.Ours != tc.ours {
				t.Errorf("ours = %q, want %q", got.Ours, tc.ours)
			}
			if got.Theirs != tc.theirs {
				t.Errorf("theirs = %q, want %q", got.Theirs, tc.theirs)
			}
			if got.HasBase != tc.hasBase || got.HasOurs != tc.hasOurs || got.HasTheirs != tc.hasThe {
				t.Errorf("presence = %v/%v/%v, want %v/%v/%v",
					got.HasBase, got.HasOurs, got.HasTheirs, tc.hasBase, tc.hasOurs, tc.hasThe)
			}
			if got.Markers != MarkersJujutsu {
				t.Errorf("markers = %q, want %q", got.Markers, MarkersJujutsu)
			}
		})
	}
}

// TestJujutsuTextParsers covers the two line formats jj answers in that are not
// templates, so a change in either is caught here rather than in a fixture.
func TestJujutsuTextParsers(t *testing.T) {
	t.Parallel()

	t.Run("the conflict list", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			line, path, kind string
		}{
			{line: "f.txt    2-sided conflict", path: "f.txt", kind: ConflictContent},
			{
				line: "docs/my file.md    2-sided conflict including 1 deletion",
				path: "docs/my file.md", kind: ConflictDeleteModify,
			},
			{line: "", path: "", kind: ""},
		}
		for _, tc := range tests {
			path, description := splitConflictLine(tc.line)
			if path != tc.path {
				t.Errorf("path of %q = %q, want %q", tc.line, path, tc.path)
			}
			if path == "" {
				continue
			}
			if kind := jujutsuConflictKind(description); kind != tc.kind {
				t.Errorf("kind of %q = %q, want %q", tc.line, kind, tc.kind)
			}
		}
	})

	t.Run("the diff summary", func(t *testing.T) {
		t.Parallel()
		tests := []struct{ raw, want string }{
			{raw: "docs/a.md", want: "docs/a.md"},
			{raw: "{old.md => new.md}", want: "new.md"},
			{raw: "docs/{old.md => new.md}", want: "docs/new.md"},
		}
		for _, tc := range tests {
			if got := summaryPath(tc.raw); got != tc.want {
				t.Errorf("summaryPath(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		}
	})
}

// equalStrings compares two string slices, treating nil and empty as equal.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
