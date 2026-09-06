package gitops

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain disables git's automatic maintenance for every repository these
// tests create or drive.
//
// Since git 2.48 `git commit` detaches the maintenance it schedules instead of
// waiting for it, so the child keeps writing into .git/objects after the
// command that started it has returned. A test that removes its temporary
// repository at that moment fails in cleanup with "directory not empty" — a
// race with git's own housekeeping, not with anything this package does. The
// settings travel through GIT_CONFIG_* so they reach the git processes the
// backends spawn, not just the ones the test helpers run.
func TestMain(m *testing.M) {
	for key, value := range map[string]string{
		"GIT_CONFIG_COUNT":   "2",
		"GIT_CONFIG_KEY_0":   "gc.auto",
		"GIT_CONFIG_VALUE_0": "0",
		"GIT_CONFIG_KEY_1":   "maintenance.auto",
		"GIT_CONFIG_VALUE_1": "false",
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	cleanup := setJujutsuConfig()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// setJujutsuConfig points every jj invocation of this package — the fixtures'
// and the backend's alike — at one throwaway configuration file, so that the
// tests neither read the developer's jj settings nor write into their home
// directory. It returns the function that removes it.
func setJujutsuConfig() func() {
	dir, err := os.MkdirTemp("", "gintrack-jj-config")
	if err != nil {
		panic(err)
	}
	path := filepath.Join(dir, "config.toml")
	body := "[user]\nname = \"Test User\"\nemail = \"test@example.com\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		panic(err)
	}
	for key, value := range map[string]string{
		"JJ_CONFIG": path,
		"JJ_USER":   "Test User",
		"JJ_EMAIL":  "test@example.com",
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	return func() { _ = os.RemoveAll(dir) }
}
