package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The Jujutsu safety layer of GIT-US-0038.
//
// Every fixture here is a real jj repository created under the test's own
// temporary folder, and every test skips cleanly when no jj binary is
// installed, the way the git tables skip without git.

// jjRunner returns a helper that runs jj in dir with a hermetic configuration,
// skipping the test when no jj binary is installed.
func jjRunner(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	bin, _, err := ResolveJujutsu("")
	if err != nil {
		t.Skipf("these tests need a jj binary to build the fixture: %v", err)
	}
	config := filepath.Join(t.TempDir(), "jj-config.toml")
	if writeErr := os.WriteFile(config, []byte("[user]\nname = \"Test User\"\nemail = \"test@example.com\"\n"), 0o600); writeErr != nil {
		t.Fatalf("write the jj configuration: %v", writeErr)
	}
	return func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"JJ_CONFIG="+config,
			"JJ_USER=Test User",
			"JJ_EMAIL=test@example.com",
			"GIT_TERMINAL_PROMPT=0",
		)
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			t.Fatalf("jj %s: %v\n%s", strings.Join(args, " "), cmdErr, out)
		}
	}
}

// newJujutsuRepo builds a jj repository with one bookmarked commit and an
// edited working copy, colocated or not, and returns its path.
func newJujutsuRepo(t *testing.T, colocated bool) string {
	t.Helper()
	dir := t.TempDir()
	jj := jjRunner(t, dir)
	if colocated {
		jj("git", "init", "--colocate")
	} else {
		jj("git", "init", "--no-colocate")
	}
	write(t, dir, "README.md", "# fixture\n")
	jj("describe", "-m", "chore: seed the fixture")
	jj("bookmark", "create", "main", "-r", "@")
	jj("new")
	write(t, dir, "README.md", "# fixture\n\nedited\n")
	jj("status")
	return dir
}

// TestDetectVCS covers every layout the detector has to tell apart.
func TestDetectVCS(t *testing.T) {
	t.Parallel()

	legacy := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		store := filepath.Join(dir, ".jj", "repo", "store")
		if err := os.MkdirAll(filepath.Join(store, "git"), 0o755); err != nil {
			t.Fatalf("build the legacy fixture: %v", err)
		}
		if err := os.WriteFile(filepath.Join(store, "git_target"), []byte("git\n"), 0o600); err != nil {
			t.Fatalf("write git_target: %v", err)
		}
		return dir
	}

	tests := []struct {
		name   string
		build  func(t *testing.T) string
		kind   core.VCS
		layout core.VCSLayout
		gitDir bool
	}{
		{
			name:   "a plain git working tree is git",
			build:  func(t *testing.T) string { t.Helper(); return newRepo(t) },
			kind:   core.VCSGit,
			layout: core.LayoutNone,
			gitDir: true,
		},
		{
			name:   "an unmanaged folder is none",
			build:  func(t *testing.T) string { t.Helper(); return t.TempDir() },
			kind:   core.VCSNone,
			layout: core.LayoutNone,
		},
		{
			name:   "a colocated jj workspace is jj, not git",
			build:  func(t *testing.T) string { t.Helper(); return newJujutsuRepo(t, true) },
			kind:   core.VCSJujutsu,
			layout: core.LayoutColocated,
			gitDir: true,
		},
		{
			name:   "a non-colocated jj workspace keeps its store inside .jj",
			build:  func(t *testing.T) string { t.Helper(); return newJujutsuRepo(t, false) },
			kind:   core.VCSJujutsu,
			layout: core.LayoutInternal,
		},
		{
			name:   "the legacy .jj/repo/store/git layout is internal",
			build:  legacy,
			kind:   core.VCSJujutsu,
			layout: core.LayoutInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			info := DetectVCS(tc.build(t))
			if info.Kind != tc.kind {
				t.Fatalf("kind = %q, want %q", info.Kind, tc.kind)
			}
			if info.Layout != tc.layout {
				t.Errorf("layout = %q, want %q", info.Layout, tc.layout)
			}
			if info.GitDir != tc.gitDir {
				t.Errorf("gitDir = %v, want %v", info.GitDir, tc.gitDir)
			}
			if info.WritableByGit() != (tc.kind == core.VCSGit) {
				t.Errorf("writableByGit = %v for %q", info.WritableByGit(), tc.kind)
			}
		})
	}
}

// TestJujutsuGuardRefusesEveryWrite proves that no write reaches the backend
// underneath the guard: the spy counts every delegated call and must see none.
func TestJujutsuGuardRefusesEveryWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		call    func(context.Context, Backend) error
		command string
	}{
		{
			name: "commit on save",
			call: func(ctx context.Context, b Backend) error {
				_, err := b.Commit(ctx, CommitRequest{Paths: []string{"a.md"}})
				return err
			},
			command: core.JujutsuCommitCommand,
		},
		{
			name: "fetch",
			call: func(ctx context.Context, b Backend) error {
				_, err := b.Fetch(ctx, FetchRequest{})
				return err
			},
			command: core.JujutsuFetchCommand,
		},
		{
			name: "integrate",
			call: func(ctx context.Context, b Backend) error {
				_, err := b.Integrate(ctx, IntegrateRequest{})
				return err
			},
			command: core.JujutsuRebaseCommand,
		},
		{
			name: "push",
			call: func(ctx context.Context, b Backend) error {
				_, err := b.Push(ctx, PushRequest{})
				return err
			},
			command: core.JujutsuPushCommand,
		},
		{
			name:    "abort",
			call:    func(ctx context.Context, b Backend) error { return b.Undo(ctx) },
			command: core.JujutsuUndoCommand,
		},
		{
			name: "continue",
			call: func(ctx context.Context, b Backend) error {
				_, err := b.Resume(ctx)
				return err
			},
			command: core.JujutsuResolveCommand,
		},
		{
			name: "resolve a conflicted path",
			call: func(ctx context.Context, b Backend) error {
				_, err := b.ResolvePath(ctx, ResolveRequest{Path: "a.md"})
				return err
			},
			command: core.JujutsuResolveCommand,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spy := &jujutsuSpy{stubBackend: &stubBackend{}}
			guarded := guardJujutsu(spy, core.VCSInfo{
				Kind: core.VCSJujutsu, Layout: core.LayoutColocated, GitDir: true,
			})
			err := tc.call(t.Context(), guarded)
			if err == nil {
				t.Fatalf("%s was allowed in a jj repository", tc.name)
			}
			if code := CodeOf(err); code != CodeJujutsuWriteRefused {
				t.Errorf("code = %q, want %q", code, CodeJujutsuWriteRefused)
			}
			if !strings.Contains(err.Error(), tc.command) {
				t.Errorf("the refusal does not name %q: %s", tc.command, err)
			}
			if !strings.Contains(err.Error(), core.JujutsuSummary) {
				t.Errorf("the refusal does not say what the repository is: %s", err)
			}
			if spy.writes > 0 {
				t.Errorf("the guard delegated %d write(s) to the backend underneath it", spy.writes)
			}
			if !errors.Is(err, ErrGit) {
				t.Errorf("the refusal is not classifiable with errors.Is")
			}
		})
	}
}

// TestJujutsuGuardStatusIsHonest checks the two corrections the read-only guard
// of GIT-US-0038 applies: a jj working copy is neither a detached HEAD nor a
// permanently dirty tree.
//
// The guard is what drives a jj repository when no jj binary is installed.
// Since GIT-US-0040 a repository with one is driven by the jj backend instead,
// which answers the same questions from jj itself and is covered by
// jj_backend_test.go, so the guard is built here directly rather than through
// Open.
func TestJujutsuGuardStatusIsHonest(t *testing.T) {
	dir := newJujutsuRepo(t, true)
	info := DetectVCS(dir)
	if !info.IsJujutsu() || info.Layout != core.LayoutColocated {
		t.Fatalf("DetectVCS = %+v, want a colocated jj repository", info)
	}

	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			var (
				plain Backend
				err   error
			)
			if kind == KindSystem {
				plain, err = openSystem(dir, Options{})
			} else {
				plain, err = openGoGit(dir, Options{})
			}
			if err != nil {
				t.Fatalf("open the jj repository as git: %v", err)
			}
			b := guardJujutsu(plain, info)
			if got := VCSOf(b); !got.IsJujutsu() || got.Layout != core.LayoutColocated {
				t.Fatalf("VCSOf = %+v, want a colocated jj repository", got)
			}
			if caps := b.Capabilities(); caps.Writes || caps.VCS != string(core.VCSJujutsu) {
				t.Errorf("capabilities = %+v, want vcs=jj and writes=false", caps)
			}

			st, err := b.Status(t.Context())
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			if st.Anonymous {
				t.Error("the jj working copy is reported as a detached HEAD")
			}
			if st.Name != JujutsuWorkingCopy {
				t.Errorf("branch = %q, want %q", st.Name, JujutsuWorkingCopy)
			}
			if len(st.Staged) != 0 {
				t.Errorf("staged = %v, want none: the index belongs to jj", st.Staged)
			}

			sync, err := b.SyncStatus(t.Context())
			if err != nil {
				t.Fatalf("sync status: %v", err)
			}
			if !sync.Jujutsu {
				t.Error("the sync status does not report the repository as jj")
			}
			if sync.Anonymous {
				t.Error("the sync status still reports a detached HEAD")
			}
			if sync.State != StateJujutsu {
				t.Errorf("state = %q, want %q", sync.State, StateJujutsu)
			}
		})
	}
}

// TestNoGitWriteReachesAJujutsuRepository is the story's reason to exist. It
// drives the two write paths that could reach a jj repository — commit on save
// and a full sync — against a real one and proves that git's refs, HEAD and
// object graph are byte-identical afterwards.
func TestNoGitWriteReachesAJujutsuRepository(t *testing.T) {
	dir := newJujutsuRepo(t, true)
	before := gitSnapshot(t, dir)

	b, err := Open(dir, Options{Backend: KindSystem})
	if err != nil {
		t.Fatalf("open the jj repository: %v", err)
	}

	t.Run("commit on save", func(t *testing.T) {
		var got Outcome
		committer := NewCommitter(CommitterOptions{
			Debounce: -1,
			Backend:  func(string) (Backend, bool) { return b, true },
			OnResult: func(out Outcome) { got = out },
		})
		committer.Enqueue(t.Context(), Change{
			Repo:   "fixture",
			Paths:  []string{"README.md"},
			Fields: Fields{Action: "update", Type: "story", ItemID: "GIT-US-0038"},
		})
		committer.Close(t.Context())
		if got.Err == nil {
			t.Fatal("commit on save was allowed in a jj repository")
		}
		if got.Code != CodeJujutsuWriteRefused {
			t.Errorf("code = %q, want %q", got.Code, CodeJujutsuWriteRefused)
		}
		if !strings.Contains(got.Message, core.JujutsuCommitCommand) {
			t.Errorf("the outcome does not name the jj command: %s", got.Message)
		}
	})

	t.Run("sync", func(t *testing.T) {
		res, syncErr := Sync(t.Context(), b, SyncOptions{Strategy: StrategyRebase, Push: true})
		if syncErr == nil {
			t.Fatal("a sync was allowed in a jj repository")
		}
		if code := CodeOf(syncErr); code != CodeJujutsuWriteRefused {
			t.Errorf("code = %q, want %q", code, CodeJujutsuWriteRefused)
		}
		if res.Phase != PhaseFailed {
			t.Errorf("phase = %q, want %q", res.Phase, PhaseFailed)
		}
		if !strings.Contains(syncErr.Error(), core.JujutsuPushCommand) {
			t.Errorf("the refusal does not name the jj command: %v", syncErr)
		}
		if strings.Contains(syncErr.Error(), "check out the branch") {
			t.Errorf("the refusal still gives git advice: %v", syncErr)
		}
	})

	t.Run("a dry run is refused too", func(t *testing.T) {
		if _, dryErr := Sync(t.Context(), b, SyncOptions{DryRun: true}); CodeOf(dryErr) != CodeJujutsuWriteRefused {
			t.Errorf("dry run error = %v, want %q", dryErr, CodeJujutsuWriteRefused)
		}
	})

	if after := gitSnapshot(t, dir); after != before {
		t.Fatalf("the repository changed under jj:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestOpenNonColocatedJujutsuRepository covers the layout git cannot serve at
// all. Before GIT-US-0040 it was refused outright; the jj backend reads it, and
// asking for that backend where there is no jj working copy is still refused.
func TestOpenNonColocatedJujutsuRepository(t *testing.T) {
	dir := newJujutsuRepo(t, false)
	b, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("a non-colocated jj repository could not be opened: %v", err)
	}
	if b.Name() != string(KindJujutsu) {
		t.Fatalf("backend = %q, want %q", b.Name(), KindJujutsu)
	}
	info := VCSOf(b)
	if !info.IsJujutsu() || info.Layout != core.LayoutInternal {
		t.Fatalf("VCSOf = %+v, want an internal jj layout", info)
	}
	st, err := b.Status(t.Context())
	if err != nil {
		t.Fatalf("status of a non-colocated repository: %v", err)
	}
	if st.Anonymous {
		t.Error("a non-colocated jj working copy was reported as anonymous")
	}

	t.Run("the jj backend refuses a working tree that is not jj", func(t *testing.T) {
		if _, gitErr := Open(newRepo(t), Options{Backend: KindJujutsu}); gitErr == nil {
			t.Fatal("the jj backend opened a plain git working tree")
		}
	})
}

// TestResolveJujutsu checks the binary probe and the minimum version.
func TestResolveJujutsu(t *testing.T) {
	t.Parallel()

	t.Run("the binary is resolved with its version", func(t *testing.T) {
		t.Parallel()
		path, version, err := ResolveJujutsu("")
		if err != nil {
			t.Skipf("no jj binary on PATH: %v", err)
		}
		if path == "" || version == "" {
			t.Fatalf("path = %q, version = %q", path, version)
		}
	})

	t.Run("a missing binary is reported, not fatal", func(t *testing.T) {
		t.Parallel()
		if _, _, err := ResolveJujutsu("jj-does-not-exist"); err == nil {
			t.Fatal("a missing jj binary resolved")
		}
	})

	versions := []struct {
		version string
		old     bool
	}{
		{version: "0.41.0", old: false},
		{version: "0.42.1", old: false},
		{version: "1.0.0", old: false},
		{version: "0.40.0", old: true},
		{version: "0.7.2", old: true},
		{version: "", old: false},
	}
	for _, tc := range versions {
		t.Run("version "+tc.version, func(t *testing.T) {
			t.Parallel()
			if got := JujutsuTooOld(tc.version); got != tc.old {
				t.Errorf("JujutsuTooOld(%q) = %v, want %v", tc.version, got, tc.old)
			}
		})
	}
}

// jujutsuSpy counts every write the guard might delegate. The guard must
// answer all of them itself, so the counter stays at zero.
type jujutsuSpy struct {
	*stubBackend
	writes int
}

func (s *jujutsuSpy) Commit(ctx context.Context, req CommitRequest) (CommitResult, error) {
	s.writes++
	return s.stubBackend.Commit(ctx, req)
}

func (s *jujutsuSpy) Fetch(ctx context.Context, req FetchRequest) (FetchResult, error) {
	s.writes++
	return s.stubBackend.Fetch(ctx, req)
}

func (s *jujutsuSpy) Integrate(ctx context.Context, req IntegrateRequest) (IntegrateResult, error) {
	s.writes++
	return s.stubBackend.Integrate(ctx, req)
}

func (s *jujutsuSpy) Push(ctx context.Context, req PushRequest) (PushResult, error) {
	s.writes++
	return s.stubBackend.Push(ctx, req)
}

func (s *jujutsuSpy) Undo(ctx context.Context) error {
	s.writes++
	return s.stubBackend.Undo(ctx)
}

func (s *jujutsuSpy) Resume(ctx context.Context) (IntegrateResult, error) {
	s.writes++
	return s.stubBackend.Resume(ctx)
}

func (s *jujutsuSpy) ResolvePath(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	s.writes++
	return s.stubBackend.ResolvePath(ctx, req)
}

// gitSnapshot renders everything a git write would change: HEAD, every ref and
// the whole object graph.
func gitSnapshot(t *testing.T, dir string) string {
	t.Helper()
	bin, _, err := resolveGit("")
	if err != nil {
		t.Skipf("this test needs a git binary to verify the repository: %v", err)
	}
	var out strings.Builder
	for _, args := range [][]string{
		{"rev-parse", "HEAD"},
		{"for-each-ref", "--format=%(refname) %(objectname)"},
		{"rev-list", "--all", "--count"},
	} {
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = dir
		raw, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), runErr, raw)
		}
		out.WriteString(strings.Join(args, " ") + ": " + string(raw))
	}
	return out.String()
}
