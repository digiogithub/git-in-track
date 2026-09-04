package gitops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The sync pipeline against real repositories (GIT-US-0021, AC 9).
//
// Every case builds a bare "remote" in a temporary directory and two clones of
// it, which is the two-person situation the whole design exists for: one person
// pushes, the other syncs.

// clonePair is a bare remote and two working clones of it.
type clonePair struct {
	remote string
	a      string
	b      string
}

// newClonePair builds a bare repository with one commit and two clones.
func newClonePair(t *testing.T) clonePair {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatalf("mkdir seed: %v", err)
	}
	seedGit := gitRunner(t, seed)
	seedGit("init", "--initial-branch=main")
	configure(t, seed)
	write(t, seed, "README.md", "# fixture\n")
	seedGit("add", "README.md")
	seedGit("commit", "-m", "chore: seed the fixture")

	remote := filepath.Join(root, "remote.git")
	rootGit := gitRunner(t, root)
	rootGit("clone", "--bare", seed, remote)

	pair := clonePair{remote: remote}
	for name, target := range map[string]*string{"a": &pair.a, "b": &pair.b} {
		dir := filepath.Join(root, name)
		rootGit("clone", remote, dir)
		configure(t, dir)
		*target = dir
	}
	return pair
}

// configure gives a repository an identity and a predictable pull behavior.
func configure(t *testing.T, dir string) {
	t.Helper()
	g := gitRunner(t, dir)
	g("config", "user.name", "Test User")
	g("config", "user.email", "test@example.com")
	g("config", "commit.gpgsign", "false")
}

// commitFile writes a file in a clone and commits it.
func commitFile(t *testing.T, dir, rel, text, subject string) {
	t.Helper()
	write(t, dir, rel, text)
	g := gitRunner(t, dir)
	g("add", rel)
	g("commit", "-m", subject)
}

// syncOpts is the option set the tests share: no waiting, no surprises.
func syncOpts(strategy Strategy) SyncOptions {
	return SyncOptions{
		Repo:     "TEST",
		Strategy: strategy,
		Push:     true,
		Backoff:  func(int) time.Duration { return 0 },
	}
}

func TestSyncStatus(t *testing.T) {
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("a fresh clone is up to date", func(t *testing.T) {
				pair := newClonePair(t)
				st, err := open(t, pair.a, kind).SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.State != StateUpToDate {
					t.Fatalf("state = %q, want %q (%+v)", st.State, StateUpToDate, st)
				}
				if st.Branch != "main" || st.Remote != "origin" || st.Upstream != "origin/main" {
					t.Fatalf("branch/remote/upstream = %q/%q/%q", st.Branch, st.Remote, st.Upstream)
				}
				if st.Ahead != 0 || st.Behind != 0 || !st.Clean {
					t.Fatalf("counters = %d/%d clean=%v", st.Ahead, st.Behind, st.Clean)
				}
			})

			t.Run("a local commit is ahead", func(t *testing.T) {
				pair := newClonePair(t)
				commitFile(t, pair.a, "docs/one.md", "one\n", "docs: add one")
				st, err := open(t, pair.a, kind).SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.Ahead != 1 || st.State != StateAhead {
					t.Fatalf("ahead = %d, state = %q", st.Ahead, st.State)
				}
			})

			t.Run("an uncommitted edit is dirty and tracked", func(t *testing.T) {
				pair := newClonePair(t)
				write(t, pair.a, "README.md", "# edited\n")
				st, err := open(t, pair.a, kind).SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.Clean || !st.Tracked || st.State != StateDirty {
					t.Fatalf("clean=%v tracked=%v state=%q", st.Clean, st.Tracked, st.State)
				}
			})

			t.Run("an untracked file does not block a rebase", func(t *testing.T) {
				pair := newClonePair(t)
				write(t, pair.a, "scratch.txt", "notes\n")
				st, err := open(t, pair.a, kind).SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.Tracked {
					t.Fatalf("an untracked file reported as a tracked change: %+v", st)
				}
			})

			t.Run("a remote commit is behind after a fetch", func(t *testing.T) {
				pair := newClonePair(t)
				commitFile(t, pair.b, "docs/two.md", "two\n", "docs: add two")
				gitRunner(t, pair.b)("push", "origin", "main")

				backend := open(t, pair.a, kind)
				if _, err := backend.Fetch(t.Context(), FetchRequest{}); err != nil {
					t.Fatalf("Fetch: %v", err)
				}
				st, err := backend.SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.Behind != 1 || st.State != StateBehind {
					t.Fatalf("behind = %d, state = %q", st.Behind, st.State)
				}
			})
		})
	}
}

func TestSyncPipeline(t *testing.T) {
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("a dry run previews both directions and changes nothing", func(t *testing.T) {
				pair := newClonePair(t)
				commitFile(t, pair.a, "docs/mine.md", "mine\n", "docs: add mine")
				commitFile(t, pair.b, "docs/theirs.md", "theirs\n", "docs: add theirs")
				gitRunner(t, pair.b)("push", "origin", "main")

				backend := open(t, pair.a, kind)
				head := revisionOf(t, backend)
				opts := syncOpts(StrategyRebase)
				opts.DryRun = true
				res, err := Sync(t.Context(), backend, opts)
				if err != nil {
					t.Fatalf("dry run: %v", err)
				}
				if res.Phase != PhaseDone || res.After.Ahead != 1 || res.After.Behind != 1 {
					t.Fatalf("phase=%q ahead=%d behind=%d", res.Phase, res.After.Ahead, res.After.Behind)
				}
				if len(res.Incoming) != 1 || res.Incoming[0].Subject != "docs: add theirs" {
					t.Fatalf("incoming = %+v", res.Incoming)
				}
				if len(res.Outgoing) != 1 || res.Outgoing[0].Subject != "docs: add mine" {
					t.Fatalf("outgoing = %+v", res.Outgoing)
				}
				if res.Pulled != 0 || res.Pushed != 0 {
					t.Fatalf("a dry run moved commits: pulled=%d pushed=%d", res.Pulled, res.Pushed)
				}
				if got := revisionOf(t, backend); got != head {
					t.Fatalf("a dry run moved HEAD: %s -> %s", head, got)
				}
				if _, err := os.Stat(filepath.Join(pair.a, "docs", "theirs.md")); err == nil {
					t.Fatal("a dry run wrote a file into the working tree")
				}
			})

			t.Run("a behind clone fast-forwards and pushes nothing", func(t *testing.T) {
				pair := newClonePair(t)
				commitFile(t, pair.b, "docs/theirs.md", "theirs\n", "docs: add theirs")
				gitRunner(t, pair.b)("push", "origin", "main")

				backend := open(t, pair.a, kind)
				res, err := Sync(t.Context(), backend, syncOpts(StrategyRebase))
				if err != nil {
					t.Fatalf("sync: %v", err)
				}
				if res.Phase != PhaseDone || res.Pulled != 1 || res.Pushed != 0 {
					t.Fatalf("phase=%q pulled=%d pushed=%d code=%s", res.Phase, res.Pulled, res.Pushed, res.Code)
				}
				if _, err := os.Stat(filepath.Join(pair.a, "docs", "theirs.md")); err != nil {
					t.Fatalf("the incoming file did not reach the working tree: %v", err)
				}
				if res.After.State != StateUpToDate {
					t.Fatalf("state after sync = %q", res.After.State)
				}
			})

			t.Run("an ahead clone pushes", func(t *testing.T) {
				pair := newClonePair(t)
				commitFile(t, pair.a, "docs/mine.md", "mine\n", "docs: add mine")

				backend := open(t, pair.a, kind)
				res, err := Sync(t.Context(), backend, syncOpts(StrategyRebase))
				if err != nil {
					t.Fatalf("sync: %v", err)
				}
				if res.Pushed != 1 || res.Phase != PhaseDone {
					t.Fatalf("pushed=%d phase=%q code=%s message=%s", res.Pushed, res.Phase, res.Code, res.Message)
				}
				// The other clone can now see the work, which is the point.
				gitRunner(t, pair.b)("fetch", "origin")
				other := open(t, pair.b, kind)
				st, err := other.SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.Behind != 1 {
					t.Fatalf("the second clone is %d behind, want 1", st.Behind)
				}
			})

			t.Run("a dirty tree refuses before fetching", func(t *testing.T) {
				pair := newClonePair(t)
				write(t, pair.a, "README.md", "# edited\n")
				res, err := Sync(t.Context(), open(t, pair.a, kind), syncOpts(StrategyRebase))
				if CodeOf(err) != CodeDirtyTree {
					t.Fatalf("code = %q, want %q (err %v)", CodeOf(err), CodeDirtyTree, err)
				}
				if res.Phase != PhaseFailed || res.Message == "" {
					t.Fatalf("phase=%q message=%q", res.Phase, res.Message)
				}
			})

			t.Run("no upstream is explained instead of guessed", func(t *testing.T) {
				dir := newRepo(t)
				res, err := Sync(t.Context(), open(t, dir, kind), syncOpts(StrategyRebase))
				if CodeOf(err) != CodeNoRemote {
					t.Fatalf("code = %q, want %q (err %v)", CodeOf(err), CodeNoRemote, err)
				}
				if res.Code != CodeNoRemote {
					t.Fatalf("result code = %q", res.Code)
				}
			})
		})
	}
}

// TestSyncDiverged needs a real rebase, which only the system backend has.
func TestSyncDiverged(t *testing.T) {
	kinds := backends(t)
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("diverged clones are reconciled and published", func(t *testing.T) {
				pair := newClonePair(t)
				commitFile(t, pair.a, "docs/mine.md", "mine\n", "docs: add mine")
				commitFile(t, pair.b, "docs/theirs.md", "theirs\n", "docs: add theirs")
				gitRunner(t, pair.b)("push", "origin", "main")

				backend := open(t, pair.a, kind)
				res, err := Sync(t.Context(), backend, syncOpts(StrategyRebase))
				if kind == KindGoGit {
					// go-git cannot rebase: it must say so, not half-apply.
					if CodeOf(err) != CodeUnsupported {
						t.Fatalf("code = %q, want %q (err %v)", CodeOf(err), CodeUnsupported, err)
					}
					return
				}
				if err != nil {
					t.Fatalf("sync: %v", err)
				}
				if res.Pulled != 1 || res.Pushed != 1 {
					t.Fatalf("pulled=%d pushed=%d", res.Pulled, res.Pushed)
				}
				// Both files exist locally and the remote now holds both.
				for _, rel := range []string{"docs/mine.md", "docs/theirs.md"} {
					if _, err := os.Stat(filepath.Join(pair.a, filepath.FromSlash(rel))); err != nil {
						t.Fatalf("%s missing after the rebase: %v", rel, err)
					}
				}
				gitRunner(t, pair.b)("pull", "--rebase", "origin", "main")
				if _, err := os.Stat(filepath.Join(pair.b, "docs", "mine.md")); err != nil {
					t.Fatalf("the other clone did not receive the pushed work: %v", err)
				}
			})

			t.Run("a conflict stops the sync, names the file and stays recoverable", func(t *testing.T) {
				if kind == KindGoGit {
					t.Skip("go-git cannot rebase, so it never reaches a conflict")
				}
				pair := newClonePair(t)
				commitFile(t, pair.a, "docs/same.md", "mine\n", "docs: mine")
				commitFile(t, pair.b, "docs/same.md", "theirs\n", "docs: theirs")
				gitRunner(t, pair.b)("push", "origin", "main")

				backend := open(t, pair.a, kind)
				res, err := Sync(t.Context(), backend, syncOpts(StrategyRebase))
				if CodeOf(err) != CodeConflict {
					t.Fatalf("code = %q, want %q (err %v)", CodeOf(err), CodeConflict, err)
				}
				if res.Phase != PhaseConflicts {
					t.Fatalf("phase = %q", res.Phase)
				}
				if len(res.Conflicts) != 1 || res.Conflicts[0].Path != "docs/same.md" {
					t.Fatalf("conflicts = %+v", res.Conflicts)
				}
				// The tree is left mid-rebase on purpose, and it is recoverable.
				st, err := backend.SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus: %v", err)
				}
				if st.State != StateConflicted || st.Operation != OpRebase {
					t.Fatalf("state=%q operation=%q", st.State, st.Operation)
				}
				// A second sync refuses rather than making it worse.
				if _, err := Sync(t.Context(), backend, syncOpts(StrategyRebase)); CodeOf(err) != CodeInProgress {
					t.Fatalf("second sync code = %q, want %q", CodeOf(err), CodeInProgress)
				}
				if err := backend.Abort(t.Context()); err != nil {
					t.Fatalf("Abort: %v", err)
				}
				st, err = backend.SyncStatus(t.Context())
				if err != nil {
					t.Fatalf("SyncStatus after abort: %v", err)
				}
				if st.Operation != "" || len(st.Conflicted) != 0 {
					t.Fatalf("the abort left the tree in %q with %d conflicts", st.Operation, len(st.Conflicted))
				}
			})

			t.Run("merge is an alternative to rebase", func(t *testing.T) {
				if kind == KindGoGit {
					t.Skip("go-git merges fast-forward only, which the fast-forward case covers")
				}
				pair := newClonePair(t)
				commitFile(t, pair.a, "docs/mine.md", "mine\n", "docs: add mine")
				commitFile(t, pair.b, "docs/theirs.md", "theirs\n", "docs: add theirs")
				gitRunner(t, pair.b)("push", "origin", "main")

				backend := open(t, pair.a, kind)
				res, err := Sync(t.Context(), backend, syncOpts(StrategyMerge))
				if err != nil {
					t.Fatalf("sync: %v", err)
				}
				if res.Strategy != StrategyMerge || res.Phase != PhaseDone {
					t.Fatalf("strategy=%q phase=%q", res.Strategy, res.Phase)
				}
				// A merge keeps the local commit and adds a merge commit, so two
				// commits reach the remote.
				if res.Pushed < 1 {
					t.Fatalf("pushed = %d", res.Pushed)
				}
			})
		})
	}
}

func TestSyncRetriesRejectedPush(t *testing.T) {
	cases := []struct {
		name        string
		rejections  int
		maxRetries  int
		wantRetries int
		wantCode    string
	}{
		{name: "one rejection is retried and succeeds", rejections: 1, maxRetries: 3, wantRetries: 1},
		{name: "two rejections are retried and succeed", rejections: 2, maxRetries: 3, wantRetries: 2},
		{
			name:       "exhausted retries end in an explained rejection",
			rejections: 9, maxRetries: 2, wantRetries: 2, wantCode: CodePushRejected,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubBackend{ahead: 1, rejectPushes: tc.rejections}
			opts := syncOpts(StrategyRebase)
			opts.MaxPushRetries = tc.maxRetries
			res, err := Sync(t.Context(), stub, opts)
			if tc.wantCode != "" {
				if CodeOf(err) != tc.wantCode {
					t.Fatalf("code = %q, want %q", CodeOf(err), tc.wantCode)
				}
				if res.Message == "" {
					t.Fatal("a rejected push must explain itself")
				}
			} else if err != nil {
				t.Fatalf("sync: %v", err)
			}
			if res.Retries != tc.wantRetries {
				t.Fatalf("retries = %d, want %d", res.Retries, tc.wantRetries)
			}
			if tc.wantCode == "" && res.Pushed != 1 {
				t.Fatalf("pushed = %d", res.Pushed)
			}
		})
	}
}

func TestSyncCancellation(t *testing.T) {
	stub := &stubBackend{ahead: 1, rejectPushes: 5}
	ctx, cancel := context.WithCancel(t.Context())
	opts := syncOpts(StrategyRebase)
	opts.Backoff = func(int) time.Duration {
		cancel()
		return 10 * time.Millisecond
	}
	res, err := Sync(ctx, stub, opts)
	if CodeOf(err) != CodeCancelled {
		t.Fatalf("code = %q, want %q (err %v)", CodeOf(err), CodeCancelled, err)
	}
	if res.Phase != PhaseFailed {
		t.Fatalf("phase = %q", res.Phase)
	}
}

func TestSyncProgress(t *testing.T) {
	stub := &stubBackend{}
	var phases []Phase
	opts := syncOpts(StrategyRebase)
	opts.Progress = func(p Progress) { phases = append(phases, p.Phase) }
	if _, err := Sync(t.Context(), stub, opts); err != nil {
		t.Fatalf("sync: %v", err)
	}
	want := []Phase{PhasePreflight, PhaseFetch, PhaseDone}
	if len(phases) != len(want) {
		t.Fatalf("phases = %v, want %v", phases, want)
	}
	for i, p := range want {
		if phases[i] != p {
			t.Fatalf("phases = %v, want %v", phases, want)
		}
	}
}

func TestRedactURL(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"a token is removed", "https://x-access-token:ghp_secret@github.com/acme/web.git", "https://***@github.com/acme/web.git"},
		{"a plain URL is untouched", "https://github.com/acme/web.git", "https://github.com/acme/web.git"},
		{"an ssh remote is untouched", "git@github.com:acme/web.git", "git@github.com:acme/web.git"},
		{"an empty remote stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactURL(tc.in); got != tc.want {
				t.Fatalf("redactURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// revisionOf reads HEAD through the backend under test.
func revisionOf(t *testing.T, b Backend) string {
	t.Helper()
	commits, err := b.Commits(t.Context(), LogRequest{To: "HEAD", Limit: 1})
	if err != nil || len(commits) == 0 {
		t.Fatalf("read HEAD: %v", err)
	}
	return commits[0].SHA
}

// stubBackend drives the pipeline without a repository, which is how the push
// retry ladder and cancellation are tested deterministically.
type stubBackend struct {
	ahead        int
	behind       int
	rejectPushes int
	fetches      int
}

func (s *stubBackend) Name() string               { return "stub" }
func (s *stubBackend) Path() string               { return "/stub" }
func (s *stubBackend) Capabilities() Capabilities { return Capabilities{Backend: "stub"} }

func (s *stubBackend) Identity(context.Context) (Identity, error) {
	return Identity{Name: "Stub", Email: "stub@example.com"}, nil
}

func (s *stubBackend) Status(context.Context) (Status, error) { return Status{Clean: true}, nil }

func (s *stubBackend) Commit(context.Context, CommitRequest) (CommitResult, error) {
	return CommitResult{Empty: true}, nil
}

func (s *stubBackend) SyncStatus(context.Context) (SyncStatus, error) {
	st := SyncStatus{
		Branch: "main", Remote: "origin", Upstream: "origin/main",
		Ahead: s.ahead, Behind: s.behind,
	}
	st.resolveState()
	return st, nil
}

func (s *stubBackend) Fetch(context.Context, FetchRequest) (FetchResult, error) {
	s.fetches++
	return FetchResult{Remote: "origin", Upstream: "origin/main"}, nil
}

func (s *stubBackend) Integrate(_ context.Context, req IntegrateRequest) (IntegrateResult, error) {
	s.behind = 0
	return IntegrateResult{Strategy: req.Strategy}, nil
}

func (s *stubBackend) Push(context.Context, PushRequest) (PushResult, error) {
	if s.rejectPushes > 0 {
		s.rejectPushes--
		return PushResult{}, &Error{
			Code: CodePushRejected, Op: "push",
			Message: "the remote already moved on",
			Err:     errors.New("non-fast-forward"),
		}
	}
	pushed := s.ahead
	s.ahead = 0
	return PushResult{Remote: "origin", Branch: "main", Pushed: pushed}, nil
}

func (s *stubBackend) Abort(context.Context) error { return nil }

func (s *stubBackend) Commits(context.Context, LogRequest) ([]Commit, error) {
	return []Commit{}, nil
}

func (s *stubBackend) Continue(context.Context) (IntegrateResult, error) {
	return IntegrateResult{}, nil
}

func (s *stubBackend) ConflictFile(context.Context, string) (ConflictVersions, error) {
	return ConflictVersions{}, nil
}

func (s *stubBackend) ResolvePath(context.Context, ResolveRequest) (ResolveResult, error) {
	return ResolveResult{}, nil
}
