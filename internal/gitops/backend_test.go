package gitops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// backends returns the backends to run a table against: go-git always, and the
// system binary when one is installed (docs/07 section 9).
func backends(t *testing.T) []Kind {
	t.Helper()
	kinds := []Kind{KindGoGit}
	if _, _, err := resolveGit(""); err == nil {
		kinds = append(kinds, KindSystem)
	} else {
		t.Log("no usable system git on PATH: the system-git subtests are skipped")
	}
	return kinds
}

// newRepo creates an initialized working tree with a configured identity and
// one commit, and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := gitRunner(t, dir)
	git("init", "--initial-branch=main")
	git("config", "user.name", "Test User")
	git("config", "user.email", "test@example.com")
	git("config", "commit.gpgsign", "false")
	write(t, dir, "README.md", "# fixture\n")
	git("add", "README.md")
	git("commit", "-m", "chore: seed the fixture")
	return dir
}

// gitRunner returns a helper that runs git in dir and fails the test on error.
func gitRunner(t *testing.T, dir string) func(args ...string) {
	t.Helper()
	bin, _, err := resolveGit("")
	if err != nil {
		t.Skipf("these tests need a git binary to build the fixture: %v", err)
	}
	return func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+filepath.Join(dir, ".gitconfig-absent"),
			"GIT_CONFIG_SYSTEM="+filepath.Join(dir, ".gitconfig-absent"),
			"GIT_TERMINAL_PROMPT=0",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// write puts a file in the repository, creating its parents.
func write(t *testing.T, dir, rel, text string) {
	t.Helper()
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
		t.Fatalf("write %s: %v", target, err)
	}
}

// open binds a backend of the given kind to dir.
func open(t *testing.T, dir string, kind Kind) Backend {
	t.Helper()
	b, err := Open(dir, Options{Backend: kind, Now: func() time.Time {
		return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("Open(%s, %s): %v", dir, kind, err)
	}
	return b
}

// log returns the subjects of the repository's commits, newest first.
func log(t *testing.T, dir string) []string {
	t.Helper()
	bin, _, err := resolveGit("")
	if err != nil {
		t.Skipf("no git binary: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), bin, "log", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func TestOpen(t *testing.T) {
	repo := newRepo(t)
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("a working tree opens", func(t *testing.T) {
				b := open(t, repo, kind)
				if b.Name() != string(kind) {
					t.Errorf("Name() = %q, want %q", b.Name(), kind)
				}
				if b.Path() != repo {
					t.Errorf("Path() = %q, want %q", b.Path(), repo)
				}
			})

			t.Run("a plain directory is not a repository", func(t *testing.T) {
				_, err := Open(t.TempDir(), Options{Backend: kind})
				if err == nil {
					t.Fatal("Open on a plain directory succeeded, want a failure")
				}
				if CodeOf(err) != CodeNotARepository {
					t.Errorf("code = %q, want %q", CodeOf(err), CodeNotARepository)
				}
				if !errors.Is(err, ErrGit) {
					t.Error("every failure of this package must match ErrGit")
				}
			})
		})
	}

	t.Run("auto resolves to a concrete backend", func(t *testing.T) {
		b, err := Open(repo, Options{Backend: KindAuto})
		if err != nil {
			t.Fatalf("Open auto: %v", err)
		}
		if b.Name() != string(KindSystem) && b.Name() != string(KindGoGit) {
			t.Errorf("Name() = %q, want a concrete backend", b.Name())
		}
	})

	t.Run("an unknown backend is refused", func(t *testing.T) {
		if _, err := Open(repo, Options{Backend: "svn"}); CodeOf(err) != CodeUnsupported {
			t.Errorf("code = %q, want %q", CodeOf(err), CodeUnsupported)
		}
	})
}

func TestBackendCommit(t *testing.T) {
	ctx := context.Background()

	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			t.Run("only the files the edit touched are staged", func(t *testing.T) {
				repo := newRepo(t)
				write(t, repo, "docs/.pmngr/stories/ACME-US-0001-a.md", "committed\n")
				write(t, repo, "docs/.pmngr/stories/ACME-US-0002-b.md", "left alone\n")
				b := open(t, repo, kind)

				res, err := b.Commit(ctx, CommitRequest{
					Paths:   []string{"docs/.pmngr/stories/ACME-US-0001-a.md"},
					Message: Message{Subject: "pmngr: create ACME-US-0001", Body: "Item: ACME-US-0001"},
				})
				if err != nil {
					t.Fatalf("Commit: %v", err)
				}
				if res.SHA == "" || res.Empty {
					t.Fatalf("result = %+v, want a commit", res)
				}
				if got := log(t, repo)[0]; got != "pmngr: create ACME-US-0001" {
					t.Errorf("subject = %q", got)
				}
				st, err := b.Status(ctx)
				if err != nil {
					t.Fatalf("Status: %v", err)
				}
				if len(st.Untracked) != 1 || st.Untracked[0] != "docs/.pmngr/stories/ACME-US-0002-b.md" {
					t.Errorf("untracked = %v, want only the untouched file", st.Untracked)
				}
			})

			t.Run("a write that changed nothing makes no commit", func(t *testing.T) {
				repo := newRepo(t)
				b := open(t, repo, kind)
				before := len(log(t, repo))

				res, err := b.Commit(ctx, CommitRequest{
					Paths:   []string{"README.md"},
					Message: Message{Subject: "pmngr: update README"},
				})
				if err != nil {
					t.Fatalf("Commit: %v", err)
				}
				if !res.Empty || res.SHA != "" {
					t.Errorf("result = %+v, want an empty no-op", res)
				}
				if after := len(log(t, repo)); after != before {
					t.Errorf("commit count went from %d to %d", before, after)
				}
			})

			t.Run("a deletion is staged as a deletion", func(t *testing.T) {
				repo := newRepo(t)
				write(t, repo, "docs/note.md", "gone soon\n")
				b := open(t, repo, kind)
				if _, err := b.Commit(ctx, CommitRequest{
					Paths:   []string{"docs/note.md"},
					Message: Message{Subject: "pmngr: create the note"},
				}); err != nil {
					t.Fatalf("Commit: %v", err)
				}
				if err := os.Remove(filepath.Join(repo, "docs", "note.md")); err != nil {
					t.Fatalf("remove: %v", err)
				}

				res, err := b.Commit(ctx, CommitRequest{
					Paths:   []string{"docs/note.md"},
					Message: Message{Subject: "pmngr: delete the note"},
				})
				if err != nil {
					t.Fatalf("Commit the deletion: %v", err)
				}
				if res.Empty {
					t.Fatal("a deletion must produce a commit")
				}
				st, err := b.Status(ctx)
				if err != nil {
					t.Fatalf("Status: %v", err)
				}
				if !st.Clean {
					t.Errorf("the tree should be clean after committing the deletion: %+v", st)
				}
			})

			t.Run("the author comes from the git configuration", func(t *testing.T) {
				repo := newRepo(t)
				b := open(t, repo, kind)
				id, err := b.Identity(ctx)
				if err != nil {
					t.Fatalf("Identity: %v", err)
				}
				if id.Name != "Test User" || id.Email != "test@example.com" {
					t.Errorf("identity = %s, want the repository configuration", id)
				}
			})

			t.Run("an explicit author overrides the configuration", func(t *testing.T) {
				repo := newRepo(t)
				b, err := Open(repo, Options{
					Backend:     kind,
					AuthorName:  "Configured Name",
					AuthorEmail: "configured@example.com",
				})
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				id, err := b.Identity(ctx)
				if err != nil {
					t.Fatalf("Identity: %v", err)
				}
				if id.Name != "Configured Name" || id.Email != "configured@example.com" {
					t.Errorf("identity = %s, want the configured override", id)
				}
			})

			t.Run("a missing identity is reported clearly", func(t *testing.T) {
				repo := newRepo(t)
				run := gitRunner(t, repo)
				run("config", "--unset", "user.name")
				run("config", "--unset", "user.email")
				// The user's own global configuration must not leak in.
				t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "absent"))
				t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "absent"))
				t.Setenv("HOME", t.TempDir())
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())

				b := open(t, repo, kind)
				write(t, repo, "docs/x.md", "x\n")
				_, err := b.Commit(ctx, CommitRequest{
					Paths:   []string{"docs/x.md"},
					Message: Message{Subject: "pmngr: create x"},
				})
				if err == nil {
					t.Skip("this machine still resolves an identity from somewhere; nothing to assert")
				}
				if CodeOf(err) != CodeNoIdentity {
					t.Fatalf("code = %q (%v), want %q", CodeOf(err), err, CodeNoIdentity)
				}
				if !strings.Contains(err.Error(), "git config user.name") {
					t.Errorf("the message must say how to fix it: %v", err)
				}
				if _, statErr := os.Stat(filepath.Join(repo, "docs", "x.md")); statErr != nil {
					t.Error("a failed commit must never lose the file content")
				}
			})

			t.Run("a batch of files is one commit", func(t *testing.T) {
				repo := newRepo(t)
				paths := []string{"docs/a.md", "docs/b.md", "docs/c.md"}
				for _, p := range paths {
					write(t, repo, p, p+"\n")
				}
				b := open(t, repo, kind)
				before := len(log(t, repo))
				if _, err := b.Commit(ctx, CommitRequest{
					Paths:   paths,
					Message: Message{Subject: "pmngr: update 3 items"},
				}); err != nil {
					t.Fatalf("Commit: %v", err)
				}
				if after := len(log(t, repo)); after != before+1 {
					t.Errorf("commit count went from %d to %d, want exactly one more", before, after)
				}
			})
		})
	}
}

func TestSystemBackendHonoursHooks(t *testing.T) {
	if _, _, err := resolveGit(""); err != nil {
		t.Skip("no system git on PATH")
	}
	ctx := context.Background()

	tests := []struct {
		name     string
		hook     string
		wantCode string
	}{
		{
			name:     "a passing hook lets the commit through",
			hook:     "#!/bin/sh\nexit 0\n",
			wantCode: "",
		},
		{
			name:     "a refusing hook fails the commit with its own output",
			hook:     "#!/bin/sh\necho 'the backlog is frozen' >&2\nexit 1\n",
			wantCode: CodeHookFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo(t)
			hook := filepath.Join(repo, ".git", "hooks", "pre-commit")
			if err := os.WriteFile(hook, []byte(tc.hook), 0o755); err != nil { //nolint:gosec // a hook has to be executable
				t.Fatalf("write the hook: %v", err)
			}
			write(t, repo, "docs/hooked.md", "content\n")
			b := open(t, repo, KindSystem)

			_, err := b.Commit(ctx, CommitRequest{
				Paths:   []string{"docs/hooked.md"},
				Message: Message{Subject: "pmngr: create the hooked file"},
			})
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("Commit: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("the hook refused the commit, so Commit must fail")
			}
			if CodeOf(err) != tc.wantCode {
				t.Errorf("code = %q, want %q (%v)", CodeOf(err), tc.wantCode, err)
			}
			if !strings.Contains(err.Error(), "the backlog is frozen") {
				t.Errorf("the hook output must reach the user: %v", err)
			}
			data, readErr := os.ReadFile(filepath.Join(repo, "docs", "hooked.md"))
			if readErr != nil || string(data) != "content\n" {
				t.Error("a refused commit must leave the file content untouched")
			}
		})
	}
}

func TestGoGitRefusesSigning(t *testing.T) {
	b := open(t, newRepo(t), KindGoGit)
	_, err := b.Commit(context.Background(), CommitRequest{
		Paths:   []string{"README.md"},
		Message: Message{Subject: "s"},
		Sign:    true,
	})
	if CodeOf(err) != CodeUnsupported {
		t.Fatalf("code = %q (%v), want %q", CodeOf(err), err, CodeUnsupported)
	}
}

func TestCapabilities(t *testing.T) {
	repo := newRepo(t)
	tests := []struct {
		name      string
		kind      Kind
		wantHooks bool
	}{
		{name: "go-git runs no hooks and signs nothing", kind: KindGoGit, wantHooks: false},
		{name: "system git brings hooks and signing", kind: KindSystem, wantHooks: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.kind == KindSystem {
				if _, _, err := resolveGit(""); err != nil {
					t.Skip("no system git on PATH")
				}
			}
			caps := open(t, repo, tc.kind).Capabilities()
			if caps.Backend != string(tc.kind) {
				t.Errorf("Backend = %q, want %q", caps.Backend, tc.kind)
			}
			if caps.Hooks != tc.wantHooks || caps.Signing != tc.wantHooks {
				t.Errorf("capabilities = %+v, want hooks and signing = %v", caps, tc.wantHooks)
			}
		})
	}
}

func TestParseGitVersion(t *testing.T) {
	tests := []struct {
		name, out, want string
		usable          bool
	}{
		{name: "linux", out: "git version 2.45.2\n", want: "2.45.2", usable: true},
		{name: "windows", out: "git version 2.45.2.windows.1\n", want: "2.45.2.windows.1", usable: true},
		{name: "too old", out: "git version 2.19.0\n", want: "2.19.0", usable: false},
		{name: "nonsense", out: "not git at all\n", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGitVersion(tc.out)
			if got != tc.want {
				t.Fatalf("parseGitVersion(%q) = %q, want %q", tc.out, got, tc.want)
			}
			if got != "" && atLeast(got, MinSystemGit) != tc.usable {
				t.Errorf("atLeast(%q) = %v, want %v", got, !tc.usable, tc.usable)
			}
		})
	}
}
