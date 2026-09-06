package gitops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The write half of the Jujutsu backend, story GIT-US-0041.
//
// Every table drives a real throwaway jj repository, and the ones that talk to
// a remote build a bare git repository next to it and a second jj workspace
// cloned from it — the two-person situation the sync pipeline exists for. They
// skip cleanly when no jj (or no git) binary is installed.

// newJujutsuClone builds a colocated jj workspace cloned from a bare remote.
func newJujutsuClone(t *testing.T, origin string) *jjFixture {
	t.Helper()
	bin, _, err := ResolveJujutsu("")
	if err != nil {
		t.Skipf("these tests need a jj binary to build the fixture: %v", err)
	}
	f := &jjFixture{
		t: t, dir: t.TempDir(), bin: bin,
		clock: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
	}
	f.run("git", "clone", "--colocate", origin, ".")
	return f
}

// jujutsuPair is a bare remote plus two jj workspaces that share it.
type jujutsuPair struct {
	origin string
	a      *jjFixture
	b      *jjFixture
}

// newJujutsuPair seeds a bare remote with one commit on `main` and clones it
// twice.
func newJujutsuPair(t *testing.T) jujutsuPair {
	t.Helper()
	seed := newJujutsuFixture(t, true)
	seed.write("README.md", "# fixture\n")
	seed.commit("chore: seed the fixture")
	seed.bookmark("main", "@-")
	origin := seed.remote()
	seed.run("git", "push", "--remote", "origin", "-b", "main")
	return jujutsuPair{origin: origin, a: newJujutsuClone(t, origin), b: newJujutsuClone(t, origin)}
}

// revisions lists the commit ids of every revision in the repository, so that
// two readings can be compared for a commit that went missing.
func (f *jjFixture) revisions(revset string) []string {
	f.t.Helper()
	raw := f.run("--ignore-working-copy", "log", "--no-graph",
		"-T", `commit_id ++ "\n"`, "-r", revset)
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// changes lists the change ids a revset selects. A change id survives every
// rewrite of the commit it names, which is what makes it the right identity to
// ask "did anything get abandoned?" with.
func (f *jjFixture) changes(revset string) []string {
	f.t.Helper()
	raw := f.run("--ignore-working-copy", "log", "--no-graph",
		"-T", `change_id ++ "\n"`, "-r", revset)
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// bookmarkCommit reports the commit one bookmark points at.
func (f *jjFixture) bookmarkCommit(name string) string {
	f.t.Helper()
	ids := f.revisions("bookmarks(exact:" + jujutsuSymbol(name) + ")")
	if len(ids) != 1 {
		f.t.Fatalf("bookmark %s points at %d commits, want exactly one", name, len(ids))
	}
	return ids[0]
}

// changedIn lists the paths one commit changed against its parent.
func (f *jjFixture) changedIn(revision string) []string {
	f.t.Helper()
	raw := f.run("--ignore-working-copy", "diff", "--summary", "-r", revision)
	out := []string{}
	for _, line := range strings.Split(raw, "\n") {
		if len(line) > 2 && line[1] == ' ' {
			out = append(out, strings.TrimSpace(line[2:]))
		}
	}
	sortStrings(out)
	return out
}

// TestJujutsuCommitOnSaveKeepsTheGraphIntact is the centerpiece of the story.
//
// The failure the whole epic exists to prevent is a write that orphans the
// working-copy commit or records work no bookmark can reach: a `git commit`
// behind a jj workspace lands on `@-`, moves no bookmark, and the next jj
// command re-parents `@` onto it and abandons the previous working copy. This
// test drives commit on save through the jj backend and proves, against a real
// repository, that none of it happens.
func TestJujutsuCommitOnSaveKeepsTheGraphIntact(t *testing.T) {
	pair := newJujutsuPair(t)
	f := pair.a
	b := f.backend()

	beforeBookmark := f.bookmarkCommit("main")
	beforeChanges := f.changes("all()")

	// Two edits, of which the commit must record exactly one: commit on save
	// batches per item, and everything else has to stay in the working copy.
	f.write("docs/wanted.md", "wanted\n")
	f.write("docs/untouched.md", "untouched\n")

	res, err := b.Commit(t.Context(), CommitRequest{
		Paths:   []string{"docs/wanted.md"},
		Message: Message{Subject: "docs: record the wanted file"},
	})
	if err != nil {
		t.Fatalf("commit on save: %v", err)
	}
	if res.Empty || res.SHA == "" {
		t.Fatalf("commit on save recorded nothing: %+v", res)
	}

	t.Run("the commit holds exactly the requested paths", func(t *testing.T) {
		if got := f.changedIn(res.SHA); len(got) != 1 || got[0] != "docs/wanted.md" {
			t.Fatalf("the commit changed %v, want only docs/wanted.md", got)
		}
	})

	t.Run("the rest of the working copy is still uncommitted", func(t *testing.T) {
		st, statusErr := b.Status(t.Context())
		if statusErr != nil {
			t.Fatalf("status: %v", statusErr)
		}
		rest := append(append([]string{}, st.Modified...), st.Untracked...)
		if len(rest) != 1 || rest[0] != "docs/untouched.md" {
			t.Fatalf("the working copy holds %v, want only docs/untouched.md", rest)
		}
	})

	t.Run("no change was abandoned", func(t *testing.T) {
		// Change ids, not commit ids: `jj commit` rewrites the working-copy
		// commit into the recorded one, which keeps its change id. What must
		// never happen is a change vanishing from the graph, which is exactly
		// what a git commit behind jj causes on the next jj command.
		after := f.changes("all()")
		for _, id := range beforeChanges {
			if !containsString(after, id) {
				t.Fatalf("change %s disappeared from the change graph", id)
			}
		}
		if len(after) != len(beforeChanges)+1 {
			t.Fatalf("the graph holds %d changes, want exactly one more than the %d before",
				len(after), len(beforeChanges))
		}
	})

	t.Run("the working-copy commit is intact and descends from the commit", func(t *testing.T) {
		if parents := f.revisions("@-"); len(parents) != 1 || parents[0] != res.SHA {
			t.Fatalf("the parent of @ is %v, want the new commit %s", parents, res.SHA)
		}
		// `@` still exists as a commit of its own, which is what git's commit
		// onto `@-` destroys.
		if wc := f.revisions("@"); len(wc) != 1 || wc[0] == res.SHA {
			t.Fatalf("the working-copy commit is %v, want a commit of its own", wc)
		}
	})

	t.Run("nothing is unreachable from the working copy", func(t *testing.T) {
		if orphans := f.revisions("all() ~ ::@ ~ ::bookmarks() ~ ::remote_bookmarks()"); len(orphans) != 0 {
			t.Fatalf("the graph holds %d orphan(s): %v", len(orphans), orphans)
		}
	})

	t.Run("the bookmark advanced exactly one commit", func(t *testing.T) {
		afterBookmark := f.bookmarkCommit("main")
		if afterBookmark != res.SHA {
			t.Fatalf("main points at %s, want the new commit %s", afterBookmark, res.SHA)
		}
		if parents := f.revisions(res.SHA + "-"); len(parents) != 1 || parents[0] != beforeBookmark {
			t.Fatalf("the new commit descends from %v, want the previous bookmark %s",
				parents, beforeBookmark)
		}
	})

	t.Run("jj git push would publish the work", func(t *testing.T) {
		out := f.run("--ignore-working-copy", "git", "push", "--remote", "origin",
			"-b", "main", "--dry-run")
		if !strings.Contains(out, short(res.SHA)) {
			t.Fatalf("the dry run does not publish the commit %s:\n%s", short(res.SHA), out)
		}
		push, pushErr := b.Push(t.Context(), PushRequest{DryRun: true})
		if pushErr != nil {
			t.Fatalf("dry-run push: %v", pushErr)
		}
		if push.Target != "main" || push.Remote != "origin" {
			t.Fatalf("push target = %+v, want the bookmark main on origin", push)
		}
	})
}

// containsString reports whether a slice holds a value.
func containsString(in []string, want string) bool {
	for _, got := range in {
		if got == want {
			return true
		}
	}
	return false
}

// TestJujutsuCommitScope pins what a commit covers, which in a VCS with no
// index is the whole of "record exactly these paths".
func TestJujutsuCommitScope(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		request CommitRequest
		empty   bool
		want    []string
	}{
		{
			name:    "one path of several",
			files:   map[string]string{"a.md": "a\n", "b.md": "b\n"},
			request: CommitRequest{Paths: []string{"a.md"}},
			want:    []string{"a.md"},
		},
		{
			name:    "several paths at once",
			files:   map[string]string{"a.md": "a\n", "b.md": "b\n", "c.md": "c\n"},
			request: CommitRequest{Paths: []string{"a.md", "c.md"}},
			want:    []string{"a.md", "c.md"},
		},
		{
			name:    "a path that changed nothing is an empty commit, not a failure",
			files:   map[string]string{"b.md": "b\n"},
			request: CommitRequest{Paths: []string{"a.md"}},
			empty:   true,
		},
		{
			name:    "no paths at all records nothing",
			files:   map[string]string{"b.md": "b\n"},
			request: CommitRequest{},
			empty:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newJujutsuFixture(t, true)
			f.write("README.md", "# fixture\n")
			f.commit("chore: seed the fixture")
			f.bookmark("main", "@-")
			b := f.backend()
			for path, body := range tc.files {
				f.write(path, body)
			}

			req := tc.request
			req.Message = Message{Subject: "docs: record the change"}
			res, err := b.Commit(t.Context(), req)
			if err != nil {
				t.Fatalf("commit: %v", err)
			}
			if tc.empty {
				if !res.Empty || res.SHA != "" {
					t.Fatalf("commit = %+v, want an empty result", res)
				}
				return
			}
			if res.Empty || res.SHA == "" {
				t.Fatalf("commit = %+v, want a recorded commit", res)
			}
			got := f.changedIn(res.SHA)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("the commit changed %v, want %v", got, tc.want)
			}
			if f.bookmarkCommit("main") != res.SHA {
				t.Fatalf("the bookmark did not follow the commit %s", res.SHA)
			}
		})
	}
}

// TestJujutsuCommitWithoutABookmark covers the line of work that has no
// bookmark: the commit is still recorded and still reachable from `@`, and the
// absence of a bookmark is not an error — it is a legitimate jj state.
func TestJujutsuCommitWithoutABookmark(t *testing.T) {
	f := newJujutsuFixture(t, true)
	f.write("README.md", "# fixture\n")
	f.commit("chore: seed the fixture")
	f.remote()
	b := f.backend()

	f.write("docs/one.md", "one\n")
	res, err := b.Commit(t.Context(), CommitRequest{
		Paths:   []string{"docs/one.md"},
		Message: Message{Subject: "docs: add one"},
	})
	if err != nil {
		t.Fatalf("commit without a bookmark: %v", err)
	}
	if res.SHA == "" {
		t.Fatalf("commit = %+v, want a recorded commit", res)
	}
	if parents := f.revisions("@-"); len(parents) != 1 || parents[0] != res.SHA {
		t.Fatalf("the commit %s is not the parent of the working copy: %v", res.SHA, parents)
	}
	if _, pushErr := b.Push(t.Context(), PushRequest{}); CodeOf(pushErr) != CodeNoUpstream {
		t.Fatalf("push answered %v, want %q: jj publishes bookmarks, not working copies",
			pushErr, CodeNoUpstream)
	}
}

// TestJujutsuSyncPipeline drives the whole pipeline — fetch, integrate, push —
// against a real remote, in the states the sync preflight cares about.
func TestJujutsuSyncPipeline(t *testing.T) {
	tests := []struct {
		name     string
		strategy Strategy
	}{
		{name: "rebase", strategy: StrategyRebase},
		{name: "merge", strategy: StrategyMerge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pair := newJujutsuPair(t)

			// The other person publishes first.
			pair.b.write("docs/theirs.md", "theirs\n")
			pair.b.commit("docs: add theirs")
			pair.b.bookmark("main", "@-")
			pair.b.run("git", "push", "--remote", "origin", "-b", "main")

			// We have work of our own, still uncommitted: in jj that never
			// blocks a sync, because the working copy is a commit.
			b := pair.a.backend()
			pair.a.write("docs/mine.md", "mine\n")
			if _, err := b.Commit(t.Context(), CommitRequest{
				Paths:   []string{"docs/mine.md"},
				Message: Message{Subject: "docs: add mine"},
			}); err != nil {
				t.Fatalf("commit on save: %v", err)
			}
			pair.a.write("docs/scratch.md", "not committed\n")

			res, err := Sync(t.Context(), b, SyncOptions{
				Repo: "TEST", Strategy: tc.strategy, Push: true,
				Backoff: func(int) time.Duration { return 0 },
			})
			if err != nil {
				t.Fatalf("sync: %v (%+v)", err, res)
			}
			if res.Phase != PhaseDone {
				t.Fatalf("phase = %q, code = %q, message = %s", res.Phase, res.Code, res.Message)
			}
			// The untracked scratch file leaves the repository "dirty", which
			// is the truth; what matters is that both sides are level.
			if res.After.Ahead != 0 || res.After.Behind != 0 {
				t.Fatalf("counters after the sync = %d/%d (%+v)",
					res.After.Ahead, res.After.Behind, res.After)
			}
			if _, statErr := os.Stat(filepath.Join(pair.a.dir, "docs", "theirs.md")); statErr != nil {
				t.Fatalf("the incoming file did not reach the working copy: %v", statErr)
			}
			if raw, readErr := os.ReadFile(filepath.Join(pair.a.dir, "docs", "scratch.md")); readErr != nil ||
				string(raw) != "not committed\n" {
				t.Fatalf("the uncommitted file was lost by the sync: %q, %v", raw, readErr)
			}
			// The other side can now see our commit, which is the only proof
			// that the bookmark, and not just the commit, reached the remote.
			pair.b.run("git", "fetch", "--remote", "origin")
			if ids := pair.b.revisions(`::"main"@"origin" & description(substring:"docs: add mine")`); len(ids) != 1 {
				t.Fatalf("the remote bookmark does not carry our commit: %v", ids)
			}
		})
	}
}

// TestJujutsuPushRejectionIsRetried proves that the retry ladder of docs/06
// section 4.2 keeps its meaning in jj: a bookmark the remote moved under us is
// a rejection, and the answer is fetch + integrate + push.
func TestJujutsuPushRejectionIsRetried(t *testing.T) {
	pair := newJujutsuPair(t)
	b := pair.a.backend()

	// Both sides commit, and the other one pushes first.
	pair.a.write("docs/mine.md", "mine\n")
	if _, err := b.Commit(t.Context(), CommitRequest{
		Paths: []string{"docs/mine.md"}, Message: Message{Subject: "docs: add mine"},
	}); err != nil {
		t.Fatalf("commit on save: %v", err)
	}
	pair.b.write("docs/theirs.md", "theirs\n")
	pair.b.commit("docs: add theirs")
	pair.b.bookmark("main", "@-")
	pair.b.run("git", "push", "--remote", "origin", "-b", "main")

	t.Run("a stale bookmark is a rejection, not a broken repository", func(t *testing.T) {
		_, err := b.Push(t.Context(), PushRequest{})
		if code := CodeOf(err); code != CodePushRejected {
			t.Fatalf("push answered %q (%v), want %q", code, err, CodePushRejected)
		}
	})

	t.Run("the ladder fetches, integrates and pushes", func(t *testing.T) {
		res, err := Sync(t.Context(), b, SyncOptions{
			Repo: "TEST", Strategy: StrategyRebase, Push: true,
			Backoff: func(int) time.Duration { return 0 },
		})
		if err != nil {
			t.Fatalf("sync: %v (%+v)", err, res)
		}
		if res.Phase != PhaseDone || res.Pushed == 0 {
			t.Fatalf("phase=%q pushed=%d code=%q", res.Phase, res.Pushed, res.Code)
		}
	})
}

// TestJujutsuConflictRoundTrip closes the loop of GIT-US-0022 in a repository
// with no index: the resolver reads the three sides out of the conflict jj
// recorded inside the commit, writes a merged file back, and the bookmark's own
// commit becomes publishable again.
func TestJujutsuConflictRoundTrip(t *testing.T) {
	pair := newJujutsuPair(t)

	pair.b.write("docs/shared.md", "theirs\n")
	pair.b.commit("docs: their edit")
	pair.b.bookmark("main", "@-")
	pair.b.run("git", "push", "--remote", "origin", "-b", "main")

	b := pair.a.backend()
	pair.a.write("docs/shared.md", "mine\n")
	if _, err := b.Commit(t.Context(), CommitRequest{
		Paths: []string{"docs/shared.md"}, Message: Message{Subject: "docs: my edit"},
	}); err != nil {
		t.Fatalf("commit on save: %v", err)
	}
	if _, err := b.Fetch(t.Context(), FetchRequest{}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	res, err := b.Integrate(t.Context(), IntegrateRequest{
		Strategy: StrategyRebase, Upstream: "origin/main",
	})
	if CodeOf(err) != CodeConflict {
		t.Fatalf("integrate answered %v, want %q", err, CodeConflict)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "docs/shared.md" {
		t.Fatalf("conflicts = %+v, want docs/shared.md", res.Conflicts)
	}

	t.Run("the integration is complete, not half-finished", func(t *testing.T) {
		if res.Unfinished || res.Resume != ResumeNone || res.Undo != UndoOperationLog {
			t.Fatalf("integration = %+v, want a completed operation undone from the log",
				res.Integration)
		}
	})

	t.Run("the three sides come out of the conflicted commit", func(t *testing.T) {
		versions, readErr := b.ConflictFile(t.Context(), "docs/shared.md")
		if readErr != nil {
			t.Fatalf("conflict file: %v", readErr)
		}
		if versions.Markers != MarkersJujutsu {
			t.Fatalf("markers = %q, want %q", versions.Markers, MarkersJujutsu)
		}
		if !strings.Contains(versions.Ours, "mine") || !strings.Contains(versions.Theirs, "theirs") {
			t.Fatalf("sides = ours %q / theirs %q, want ours to be our own edit",
				versions.Ours, versions.Theirs)
		}
	})

	t.Run("a merged file resolves the conflicted commit", func(t *testing.T) {
		out, resolveErr := b.ResolvePath(t.Context(), ResolveRequest{
			Path: "docs/shared.md", Content: "mine and theirs\n", Continue: true,
		})
		if resolveErr != nil {
			t.Fatalf("resolve: %v", resolveErr)
		}
		if !out.Staged || len(out.Remaining) != 0 {
			t.Fatalf("resolve = %+v, want the path recorded and nothing left", out)
		}
		if conflicted := pair.a.revisions("conflicts()"); len(conflicted) != 0 {
			t.Fatalf("the change graph still holds %d conflicted commit(s): %v",
				len(conflicted), conflicted)
		}
		if raw, readErr := os.ReadFile(filepath.Join(pair.a.dir, "docs", "shared.md")); readErr != nil ||
			string(raw) != "mine and theirs\n" {
			t.Fatalf("the resolved file holds %q, %v", raw, readErr)
		}
	})

	t.Run("the resolved work publishes", func(t *testing.T) {
		if _, pushErr := b.Push(t.Context(), PushRequest{}); pushErr != nil {
			t.Fatalf("push after the resolution: %v", pushErr)
		}
		pair.b.run("git", "fetch", "--remote", "origin")
		if ids := pair.b.revisions(`::"main"@"origin" & description(substring:"docs: my edit")`); len(ids) != 1 {
			t.Fatalf("the resolved commit did not reach the remote: %v", ids)
		}
	})
}

// TestJujutsuUndoAndResume pins the two halves of jj's answer to git's
// `--abort` and `--continue`: the operation log undoes anything, and there is
// never anything to continue.
func TestJujutsuUndoAndResume(t *testing.T) {
	f := newJujutsuFixture(t, true)
	f.write("README.md", "# fixture\n")
	f.commit("chore: seed the fixture")
	f.bookmark("main", "@-")
	b := f.backend()

	f.write("docs/one.md", "one\n")
	res, err := b.Commit(t.Context(), CommitRequest{
		Paths: []string{"docs/one.md"}, Message: Message{Subject: "docs: add one"},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	t.Run("undo takes the last operation back", func(t *testing.T) {
		// Commit on save is two operations — the commit and the bookmark move
		// — so it takes two undos, which is what jj's own operation log means.
		if err := b.Undo(t.Context()); err != nil {
			t.Fatalf("undo the bookmark move: %v", err)
		}
		if got := f.bookmarkCommit("main"); got == res.SHA {
			t.Fatalf("the bookmark still points at %s after the undo", res.SHA)
		}
		if err := b.Undo(t.Context()); err != nil {
			t.Fatalf("undo the commit: %v", err)
		}
		if ids := f.revisions("::@ & description(\"docs: add one\")"); len(ids) != 0 {
			t.Fatalf("the commit survived the undo: %v", ids)
		}
	})

	t.Run("there is nothing to resume", func(t *testing.T) {
		_, resumeErr := b.Resume(t.Context())
		if code := CodeOf(resumeErr); code != CodeUnsupported {
			t.Fatalf("resume answered %q (%v), want %q", code, resumeErr, CodeUnsupported)
		}
		if !strings.Contains(resumeErr.Error(), "always completes") {
			t.Fatalf("the refusal does not explain why: %v", resumeErr)
		}
	})
}

// TestJujutsuWritesAreNonInteractive proves the credential rules of
// GIT-US-0023 reach jj: it runs the user's own git for the network, so a
// missing credential must fail with an actionable message instead of hanging on
// a prompt, and nothing it prints may carry the secret an URL holds.
func TestJujutsuWritesAreNonInteractive(t *testing.T) {
	if _, _, err := resolveGit(""); err != nil {
		t.Skipf("this test needs a git binary for jj to run: %v", err)
	}
	f := newJujutsuFixture(t, true)
	f.write("README.md", "# fixture\n")
	f.commit("chore: seed the fixture")
	f.bookmark("main", "@-")
	f.run("git", "remote", "add", "origin",
		"https://user:hunter2@127.0.0.1:9/digiogithub/nothing.git")
	b := f.backend()

	done := make(chan error, 1)
	go func() {
		_, err := b.Fetch(t.Context(), FetchRequest{})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a fetch against an unreachable host succeeded")
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("the failure leaked the credential: %v", err)
		}
		var gitErr *Error
		if !errors.As(err, &gitErr) || gitErr.Message == "" {
			t.Fatalf("the failure is not an actionable error: %v", err)
		}
		if strings.Contains(gitErr.Detail, "hunter2") {
			t.Fatalf("the detail leaked the credential: %s", gitErr.Detail)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the fetch hung, which means something prompted")
	}
}
