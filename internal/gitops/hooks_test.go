package gitops

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
)

// isolateGitConfig keeps the developer's global and system git configuration —
// a core.hooksPath in ~/.gitconfig above all — out of HooksDir.
func isolateGitConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// initHookRepo creates an empty repository with go-git and returns its
// working tree, symlinks resolved so paths compare equal.
func initHookRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	return dir
}

// setHooksPath writes core.hooksPath into the repository configuration.
func setHooksPath(t *testing.T, dir, value string) {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Raw.Section("core").SetOption("hooksPath", value)
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestHooksDir(t *testing.T) {
	isolateGitConfig(t)
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			tests := []struct {
				name      string
				hooksPath string
				want      func(dir string) string
			}{
				{"default", "", func(dir string) string { return filepath.Join(dir, ".git", "hooks") }},
				{"relative core.hooksPath", ".githooks", func(dir string) string { return filepath.Join(dir, ".githooks") }},
				{"absolute core.hooksPath", "ABS", func(dir string) string { return filepath.Join(dir, "shared-hooks") }},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					dir := initHookRepo(t)
					switch tt.hooksPath {
					case "":
					case "ABS":
						setHooksPath(t, dir, filepath.Join(dir, "shared-hooks"))
					default:
						setHooksPath(t, dir, tt.hooksPath)
					}
					got, err := HooksDir(t.Context(), dir, Options{Backend: kind})
					if err != nil {
						t.Fatalf("HooksDir: %v", err)
					}
					if want := tt.want(dir); got != want {
						t.Errorf("HooksDir = %s, want %s", got, want)
					}
				})
			}
		})
	}
}

func TestHooksDirLinkedWorktree(t *testing.T) {
	isolateGitConfig(t)
	mainTree, err := filepath.EvalSymlinks(newRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	gitRunner(t, mainTree)("worktree", "add", "-q", "-b", "side", linked)
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			got, err := HooksDir(t.Context(), linked, Options{Backend: kind})
			if err != nil {
				t.Fatalf("HooksDir: %v", err)
			}
			if want := filepath.Join(mainTree, ".git", "hooks"); got != want {
				t.Errorf("HooksDir = %s, want the main worktree's %s", got, want)
			}
		})
	}
}

func TestHooksDirNotARepository(t *testing.T) {
	isolateGitConfig(t)
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			_, err := HooksDir(t.Context(), t.TempDir(), Options{Backend: kind})
			if CodeOf(err) != CodeNotARepository {
				t.Fatalf("HooksDir = %v, want %s", err, CodeNotARepository)
			}
		})
	}
}

const testMarker = "# installed by test"

func TestInstallAndUninstallHook(t *testing.T) {
	script := "#!/bin/sh\n" + testMarker + "\nexit 0\n"
	req := func(dir string) HookRequest {
		return HookRequest{Dir: dir, Name: "pre-push", Marker: testMarker, Script: script}
	}
	read := func(t *testing.T, path string) string {
		t.Helper()
		data, err := os.ReadFile(path) //nolint:gosec // the test's own temp file
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("install into a missing folder, then idempotent", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "hooks")
		res, err := InstallHook(req(dir))
		if err != nil || res.Action != HookCreated {
			t.Fatalf("install = %+v, %v", res, err)
		}
		info, err := os.Stat(res.Path)
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("hook is not executable: %v %v", info, err)
		}
		if res, err = InstallHook(req(dir)); err != nil || res.Action != HookUnchanged {
			t.Fatalf("reinstall = %+v, %v", res, err)
		}
		r := req(dir)
		r.Script = script + "# v2\n"
		if res, err = InstallHook(r); err != nil || res.Action != HookUpdated {
			t.Fatalf("update = %+v, %v", res, err)
		}
		if got := read(t, res.Path); got != r.Script {
			t.Errorf("content = %q", got)
		}
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		r := req(dir)
		r.DryRun = true
		res, err := InstallHook(r)
		if err != nil || res.Action != HookCreated || !res.DryRun {
			t.Fatalf("dry install = %+v, %v", res, err)
		}
		if _, err := os.Stat(res.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry run wrote %s", res.Path)
		}
	})

	t.Run("foreign hook refused, forced with backup, restored on uninstall", func(t *testing.T) {
		dir := t.TempDir()
		foreign := "#!/bin/sh\necho mine\n"
		path := filepath.Join(dir, "pre-push")
		if err := os.WriteFile(path, []byte(foreign), 0o755); err != nil { //nolint:gosec // a hook fixture
			t.Fatal(err)
		}
		if _, err := InstallHook(req(dir)); !errors.Is(err, ErrForeignHook) {
			t.Fatalf("install over a foreign hook = %v, want ErrForeignHook", err)
		}
		if got := read(t, path); got != foreign {
			t.Fatalf("the foreign hook changed: %q", got)
		}
		if _, err := UninstallHook(req(dir)); !errors.Is(err, ErrForeignHook) {
			t.Fatalf("uninstall of a foreign hook = %v, want ErrForeignHook", err)
		}
		r := req(dir)
		r.Force = true
		res, err := InstallHook(r)
		if err != nil || res.Action != HookReplaced || res.Backup != path+HookBackupSuffix {
			t.Fatalf("forced install = %+v, %v", res, err)
		}
		if got := read(t, res.Backup); got != foreign {
			t.Errorf("backup = %q", got)
		}
		if got := read(t, path); got != script {
			t.Errorf("hook = %q", got)
		}
		res, err = UninstallHook(req(dir))
		if err != nil || res.Action != HookRestored {
			t.Fatalf("uninstall = %+v, %v", res, err)
		}
		if got := read(t, path); got != foreign {
			t.Errorf("restored hook = %q", got)
		}
		if _, err := os.Stat(res.Backup); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the backup is still there")
		}
	})

	t.Run("a stale backup blocks a forced install", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"pre-push", "pre-push" + HookBackupSuffix} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil { //nolint:gosec // a hook fixture
				t.Fatal(err)
			}
		}
		r := req(dir)
		r.Force = true
		if _, err := InstallHook(r); !errors.Is(err, ErrHookBackupExists) {
			t.Fatalf("forced install = %v, want ErrHookBackupExists", err)
		}
	})

	t.Run("uninstall removes only its own hook", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := InstallHook(req(dir)); err != nil {
			t.Fatal(err)
		}
		res, err := UninstallHook(req(dir))
		if err != nil || res.Action != HookRemoved {
			t.Fatalf("uninstall = %+v, %v", res, err)
		}
		if res, err = UninstallHook(req(dir)); err != nil || res.Action != HookAbsent {
			t.Fatalf("second uninstall = %+v, %v", res, err)
		}
	})
}

func TestIsManagedHook(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"marker after shebang", "#!/bin/sh\n" + testMarker + " (pre-push)\n", true},
		{"no marker", "#!/bin/sh\necho hi\n", false},
		{"marker too deep", "#!/bin/sh\n\n\n\n\n\n" + testMarker + "\n", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsManagedHook([]byte(tt.content), testMarker); got != tt.want {
				t.Errorf("IsManagedHook = %v, want %v", got, tt.want)
			}
		})
	}
}
