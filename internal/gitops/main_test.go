package gitops

import (
	"os"
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
	os.Exit(m.Run())
}
