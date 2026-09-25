package gitops

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
)

// Hook files managed by gintrack (GIT-US-0134).
//
// A managed hook is a script gintrack writes into the repository's hooks
// folder. It carries a marker line near its top, so a later install recognizes
// it as its own and rewrites it, and an uninstall removes it and nothing else:
// a hook the user or another tool wrote is "foreign" and is never replaced
// without an explicit force, and then only after it has been backed up.

// ErrForeignHook means the hook file exists and was not written by gintrack.
var ErrForeignHook = errors.New("a hook gintrack did not install is in the way")

// ErrHookBackupExists means a forced install would back up a foreign hook, but
// a backup from an earlier forced install is still there.
var ErrHookBackupExists = errors.New("a hook backup is already in the way")

// HookBackupSuffix is appended to a foreign hook's file name when a forced
// install moves it aside. Uninstall moves it back.
const HookBackupSuffix = ".gintrack-backup"

// markerLines is how far from the top of a hook file the marker is looked for.
const markerLines = 5

// HookAction is what an install or uninstall did, or would do on a dry run.
type HookAction string

const (
	// HookCreated means no hook was there and the script was written.
	HookCreated HookAction = "created"
	// HookUpdated means a managed hook with other content was rewritten.
	HookUpdated HookAction = "updated"
	// HookUnchanged means the managed hook already held the script.
	HookUnchanged HookAction = "unchanged"
	// HookReplaced means a foreign hook was backed up and the script written.
	HookReplaced HookAction = "replaced"
	// HookRemoved means the managed hook was deleted.
	HookRemoved HookAction = "removed"
	// HookRestored means the managed hook was deleted and the backup of the
	// foreign hook it replaced was moved back.
	HookRestored HookAction = "restored"
	// HookAbsent means there was no managed hook to remove.
	HookAbsent HookAction = "absent"
)

// HookRequest is one install or uninstall of a managed hook.
type HookRequest struct {
	// Dir is the hooks folder, as HooksDir resolves it.
	Dir string
	// Name is the hook, e.g. "pre-push".
	Name string
	// Marker is the line that identifies a managed hook, e.g.
	// "# installed by gintrack spec hook".
	Marker string
	// Script is the full content to install. Uninstall ignores it.
	Script string
	// Force backs up and replaces a foreign hook on install.
	Force bool
	// DryRun decides everything and writes nothing.
	DryRun bool
}

// HookResult reports what happened to one hook file.
type HookResult struct {
	Path   string     `json:"path"`
	Action HookAction `json:"action"`
	// Backup is the file a foreign hook was (or would be) moved to on a forced
	// install, or moved back from on uninstall.
	Backup string `json:"backup,omitempty"`
	DryRun bool   `json:"dryRun,omitempty"`
}

// IsManagedHook reports whether a hook's content carries the marker within its
// first lines.
func IsManagedHook(content []byte, marker string) bool {
	if marker == "" {
		return false
	}
	sc := bufio.NewScanner(bytes.NewReader(content))
	for i := 0; i < markerLines && sc.Scan(); i++ {
		if strings.HasPrefix(strings.TrimSpace(sc.Text()), marker) {
			return true
		}
	}
	return false
}

// InstallHook writes the script as the named hook, executable. A managed hook
// is rewritten in place; a foreign one is refused with ErrForeignHook unless
// Force is set, in which case it is first renamed with HookBackupSuffix.
func InstallHook(req HookRequest) (HookResult, error) {
	path := filepath.Join(req.Dir, req.Name)
	res := HookResult{Path: path, DryRun: req.DryRun}
	current, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Action = HookCreated
	case err != nil:
		return res, fmt.Errorf("read %s: %w", path, err)
	case IsManagedHook(current, req.Marker):
		res.Action = HookUpdated
		if string(current) == req.Script {
			res.Action = HookUnchanged
		}
	case !req.Force:
		return res, fmt.Errorf("%w: %s", ErrForeignHook, path)
	default:
		res.Action = HookReplaced
		res.Backup = path + HookBackupSuffix
		if _, err := os.Lstat(res.Backup); err == nil {
			return res, fmt.Errorf("%w: %s", ErrHookBackupExists, res.Backup)
		}
	}
	if req.DryRun {
		return res, nil
	}
	if res.Action == HookReplaced {
		if err := os.Rename(path, res.Backup); err != nil {
			return res, fmt.Errorf("back up %s: %w", path, err)
		}
	}
	if res.Action != HookUnchanged {
		if err := os.MkdirAll(req.Dir, 0o755); err != nil {
			return res, fmt.Errorf("create %s: %w", req.Dir, err)
		}
		if err := os.WriteFile(path, []byte(req.Script), 0o755); err != nil { //nolint:gosec // a hook must be executable
			return res, fmt.Errorf("write %s: %w", path, err)
		}
	}
	// WriteFile keeps the mode of an existing file and the umask trims a new
	// one; a hook without its executable bit is silently skipped by git.
	if err := os.Chmod(path, 0o755); err != nil { //nolint:gosec // a hook must be executable
		return res, fmt.Errorf("make %s executable: %w", path, err)
	}
	return res, nil
}

// UninstallHook removes the named hook when it is a managed one, and moves back
// the foreign hook a forced install backed up. A foreign hook is left in place
// and reported with ErrForeignHook.
func UninstallHook(req HookRequest) (HookResult, error) {
	path := filepath.Join(req.Dir, req.Name)
	res := HookResult{Path: path, DryRun: req.DryRun}
	current, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Action = HookAbsent
		return res, nil
	case err != nil:
		return res, fmt.Errorf("read %s: %w", path, err)
	case !IsManagedHook(current, req.Marker):
		return res, fmt.Errorf("%w: %s", ErrForeignHook, path)
	}
	res.Action = HookRemoved
	backup := path + HookBackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		res.Action, res.Backup = HookRestored, backup
	}
	if req.DryRun {
		return res, nil
	}
	if err := os.Remove(path); err != nil {
		return res, fmt.Errorf("remove %s: %w", path, err)
	}
	if res.Action == HookRestored {
		if err := os.Rename(backup, path); err != nil {
			return res, fmt.Errorf("restore %s: %w", backup, err)
		}
	}
	return res, nil
}

// HooksDir resolves the folder git runs the hooks of the working tree at root
// from — what `git rev-parse --git-path hooks` answers: core.hooksPath when it
// is set, relative to the working-tree root, and the hooks folder of the
// repository's common git directory otherwise, so a linked worktree shares the
// hooks of its main one. The system backend asks git itself; go-git reads the
// same configuration chain (repository and global).
func HooksDir(ctx context.Context, root string, opts Options) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", wrap("hooks-dir", CodeNotARepository, err, "%s is not a usable path", root)
	}
	kind := opts.Backend
	if kind == "" || kind == KindJujutsu {
		kind = KindAuto
	}
	switch kind {
	case KindSystem:
		return systemHooksDir(ctx, abs, opts.GitBinary)
	case KindGoGit:
		return goGitHooksDir(abs)
	case KindAuto:
		// No git binary, or one that cannot answer: go-git reads the same
		// configuration.
		if dir, err := systemHooksDir(ctx, abs, opts.GitBinary); err == nil {
			return dir, nil
		}
		return goGitHooksDir(abs)
	}
	return "", failf("hooks-dir", CodeUnsupported, "unknown git backend %q", string(kind))
}

// systemHooksDir asks git where the hooks live.
func systemHooksDir(ctx context.Context, root, binary string) (string, error) {
	if binary == "" {
		binary = "git"
	}
	cmd := exec.CommandContext(ctx, binary, "rev-parse", "--git-path", "hooks") //nolint:gosec // the binary is the configured git
	cmd.Dir = root
	cmd.Env = nonInteractiveEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return "", wrap("hooks-dir", CodeNotARepository, err, "%s is not inside a git working tree", root)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", failf("hooks-dir", CodeNotARepository, "git reported no hooks folder for %s", root)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Clean(dir), nil
}

// goGitHooksDir is systemHooksDir without a git binary.
func goGitHooksDir(root string) (string, error) {
	repo, err := git.PlainOpenWithOptions(root, &git.PlainOpenOptions{DetectDotGit: true, EnableDotGitCommonDir: true})
	if err != nil {
		return "", wrap("hooks-dir", CodeNotARepository, err, "%s is not inside a git working tree", root)
	}
	cfg, err := repo.ConfigScoped(gitconfig.GlobalScope)
	if err != nil {
		return "", wrap("hooks-dir", CodeNotARepository, err, "read the git configuration of %s", root)
	}
	if hp := strings.TrimSpace(cfg.Raw.Section("core").Option("hooksPath")); hp != "" {
		if hp == "~" || strings.HasPrefix(hp, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("expand core.hooksPath %q: %w", hp, err)
			}
			hp = filepath.Join(home, strings.TrimPrefix(hp, "~"))
		}
		if !filepath.IsAbs(hp) {
			hp = filepath.Join(root, hp)
		}
		return filepath.Clean(hp), nil
	}
	common, err := commonGitDir(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "hooks"), nil
}

// commonGitDir finds the git directory shared by every worktree of the
// repository at root: `.git` itself, or — for a linked worktree whose `.git`
// is a `gitdir:` file — the folder its `commondir` names.
func commonGitDir(root string) (string, error) {
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", wrap("hooks-dir", CodeNotARepository, err, "%s has no .git", root)
	}
	if info.IsDir() {
		return dotGit, nil
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dotGit, err)
	}
	line := strings.TrimSpace(string(data))
	gitDir, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return "", failf("hooks-dir", CodeNotARepository, "%s is not a gitdir file", dotGit)
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	if common, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		dir := strings.TrimSpace(string(common))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(gitDir, dir)
		}
		return filepath.Clean(dir), nil
	}
	return filepath.Clean(gitDir), nil
}
