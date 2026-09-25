package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"

	"github.com/digiogithub/git-in-track/internal/gitops"
)

// hookRepo creates a temporary git repository with go-git and a harness whose
// configuration and git environment are isolated from the developer's: no
// global core.hooksPath can redirect the hook, and nothing is ever installed
// into a real repository.
func hookRepo(t *testing.T) (*harness, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GINTRACK_CONFIG", filepath.Join(home, "state", "config.yaml"))
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	return &harness{t: t, Repo: dir}, dir
}

// hookPayload is the part of the --json payload the tests read.
type hookPayload struct {
	Repo       string             `json:"repo"`
	Hook       string             `json:"hook"`
	Dir        string             `json:"dir"`
	Result     *gitops.HookResult `json:"result"`
	Skipped    string             `json:"skipped"`
	Equivalent string             `json:"equivalent"`
	VCS        struct {
		Kind string `json:"kind"`
	} `json:"vcs"`
}

func TestSpecHookInstall(t *testing.T) {
	h, dir := hookRepo(t)
	hook := filepath.Join(dir, ".git", "hooks", "pre-push")

	got := decode[hookPayload](t, h.mustRun("spec", "hook", "install", "--repo", dir, "--json"))
	if got.Result == nil || got.Result.Action != gitops.HookCreated || got.Result.Path != hook {
		t.Fatalf("install = %+v", got.Result)
	}
	if got.Dir != filepath.Join(dir, ".git", "hooks") || got.VCS.Kind != "git" {
		t.Errorf("payload = %+v", got)
	}
	info, err := os.Stat(hook)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("hook missing or not executable: %v %v", info, err)
	}
	script := readFile(t, hook)
	for _, want := range []string{
		"#!/bin/sh\n" + specHookMarker,
		`spec impact --since "$since" --head "$local_sha" --tiers 1,2 --fail-on failing,suspect`,
		"@{upstream}", "origin/main",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}

	// Reinstalling is idempotent; a different --project rewrites our own hook.
	got = decode[hookPayload](t, h.mustRun("spec", "hook", "install", "--repo", dir, "--json"))
	if got.Result.Action != gitops.HookUnchanged {
		t.Errorf("reinstall = %s, want unchanged", got.Result.Action)
	}
	got = decode[hookPayload](t, h.mustRun("spec", "hook", "install", "--repo", dir, "--project", "ACME", "--json"))
	if got.Result.Action != gitops.HookUpdated || !strings.Contains(readFile(t, hook), "--project ACME") {
		t.Errorf("reinstall with --project = %s", got.Result.Action)
	}

	// Uninstall removes it; a second one finds nothing.
	out := h.mustRun("spec", "hook", "uninstall", "--repo", dir)
	if !strings.Contains(out, "removed") {
		t.Errorf("uninstall said %q", out)
	}
	if _, err := os.Stat(hook); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the hook is still there")
	}
	if out := h.mustRun("spec", "hook", "uninstall", "--repo", dir); !strings.Contains(out, "no gintrack hook") {
		t.Errorf("second uninstall said %q", out)
	}
}

func TestSpecHookDryRun(t *testing.T) {
	h, dir := hookRepo(t)
	out := h.mustRun("spec", "hook", "install", "--repo", dir, "--dry-run")
	if !strings.Contains(out, "would be installed") {
		t.Errorf("dry run said %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-push")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("--dry-run wrote the hook")
	}
}

func TestSpecHookForeign(t *testing.T) {
	h, dir := hookRepo(t)
	hooks := filepath.Join(dir, ".git", "hooks")
	hook := filepath.Join(hooks, "pre-push")
	foreign := "#!/bin/sh\necho my own hook\n"
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte(foreign), 0o755); err != nil { //nolint:gosec // a hook fixture
		t.Fatal(err)
	}

	_, stderr, code := h.run("spec", "hook", "install", "--repo", dir)
	if code != exitConflict || !strings.Contains(stderr, "--force") {
		t.Fatalf("install over a foreign hook: exit %d\n%s", code, stderr)
	}
	if _, stderr, code = h.run("spec", "hook", "uninstall", "--repo", dir); code != exitConflict {
		t.Fatalf("uninstall of a foreign hook: exit %d\n%s", code, stderr)
	}
	if readFile(t, hook) != foreign {
		t.Fatal("the foreign hook changed")
	}

	got := decode[hookPayload](t, h.mustRun("spec", "hook", "install", "--repo", dir, "--force", "--json"))
	if got.Result.Action != gitops.HookReplaced || got.Result.Backup != hook+gitops.HookBackupSuffix {
		t.Fatalf("forced install = %+v", got.Result)
	}
	if readFile(t, got.Result.Backup) != foreign || !strings.Contains(readFile(t, hook), specHookMarker) {
		t.Fatal("the forced install did not back up and replace the hook")
	}
	got = decode[hookPayload](t, h.mustRun("spec", "hook", "uninstall", "--repo", dir, "--json"))
	if got.Result.Action != gitops.HookRestored || readFile(t, hook) != foreign {
		t.Fatalf("uninstall = %+v, hook %q", got.Result, readFile(t, hook))
	}
}

func TestSpecHookHonorsHooksPath(t *testing.T) {
	h, dir := hookRepo(t)
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Raw.Section("core").SetOption("hooksPath", ".githooks")
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got := decode[hookPayload](t, h.mustRun("spec", "hook", "install", "--repo", dir, "--json"))
	if want := filepath.Join(dir, ".githooks", "pre-push"); got.Result.Path != want {
		t.Fatalf("installed at %s, want %s", got.Result.Path, want)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-push")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the hook also landed in .git/hooks")
	}
}

func TestSpecHookJujutsu(t *testing.T) {
	h, dir := hookRepo(t)
	// A colocated jj workspace: the .jj marker next to .git is what the
	// detection reads; no jj binary is needed.
	if err := os.MkdirAll(filepath.Join(dir, ".jj", "repo", "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := h.mustRun("spec", "hook", "install", "--repo", dir)
	for _, want := range []string{"jj git push", "push-gated", "spec impact --since", "--fail-on failing,suspect"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	got := decode[hookPayload](t, h.mustRun("spec", "hook", "install", "--repo", dir, "--json"))
	if got.VCS.Kind != "jj" || got.Skipped == "" || got.Equivalent == "" || got.Result != nil {
		t.Errorf("payload = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "pre-push")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a hook was written into a jj repository")
	}
	h.mustRun("spec", "hook", "uninstall", "--repo", dir)
}

func TestSpecHookErrors(t *testing.T) {
	h, dir := hookRepo(t)
	if _, stderr, code := h.run("spec", "hook", "install", "--repo", dir, "--hook", "pre-commit"); code != exitUsage {
		t.Errorf("--hook pre-commit: exit %d\n%s", code, stderr)
	}
	if _, stderr, code := h.run("spec", "hook", "install", "--repo", dir, "--project", "A B"); code != exitUsage {
		t.Errorf("--project with a space: exit %d\n%s", code, stderr)
	}
	if _, stderr, code := h.run("spec", "hook", "install", "--repo", t.TempDir()); code != exitGit {
		t.Errorf("not a repository: exit %d\n%s", code, stderr)
	}
}

// TestSpecHookScriptRuns executes the installed script the way git does for a
// push, against a stand-in gintrack that records its arguments and exits with
// a chosen code.
func TestSpecHookScriptRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a POSIX sh script")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	h, dir := hookRepo(t)
	h.mustRun("spec", "hook", "install", "--repo", dir)
	hook := filepath.Join(dir, ".git", "hooks", "pre-push")

	stub := filepath.Join(t.TempDir(), "gintrack-stub")
	argsFile := stub + ".args"
	body := "#!/bin/sh\necho \"$@\" >> '" + argsFile + "'\nexit ${STUB_EXIT:-0}\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	zero := strings.Repeat("0", 40)
	stdin := "refs/heads/feature " + sha + " refs/heads/feature " + zero + "\n" +
		"(delete) " + zero + " refs/heads/gone " + sha + "\n"

	run := func(exit string) (int, string) {
		_ = os.Remove(argsFile)
		cmd := exec.CommandContext(t.Context(), sh, hook, "origin", "https://example.invalid/repo.git")
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdin)
		cmd.Env = append(os.Environ(), "GINTRACK="+stub, "STUB_EXIT="+exit)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), stderr.String()
		}
		if err != nil {
			t.Fatalf("run the hook: %v", err)
		}
		return 0, stderr.String()
	}

	code, stderr := run("0")
	if code != 0 {
		t.Fatalf("passing gate: exit %d\n%s", code, stderr)
	}
	args := readFile(t, argsFile)
	// One run: the deletion pushes nothing. No upstream is configured, so the
	// base falls back to origin/main.
	want := "spec impact --since origin/main --head " + sha + " --tiers 1,2 --fail-on failing,suspect\n"
	if args != want {
		t.Errorf("stub called with %q, want %q", args, want)
	}

	if code, stderr = run("7"); code != exitGate || !strings.Contains(stderr, "push refused") {
		t.Errorf("tripped gate: exit %d\n%s", code, stderr)
	}
}
