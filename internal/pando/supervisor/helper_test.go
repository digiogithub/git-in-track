package supervisor

import (
	"context"
	"fmt"
	"os"
	"time"
)

// helperSupervisor runs a Supervisor in this process until it is killed. The
// tests start it as a subprocess (SUPERVISOR_TEST_HELPER=1) and SIGKILL it. It
// prints "ready" on stdout once the child passes Health.
func helperSupervisor() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sup, err := New(Options{
		Binary: os.Getenv("SUPERVISOR_TEST_BINARY"), CacheDir: os.Getenv("SUPERVISOR_TEST_CACHE"),
		RepoRoot: os.Getenv("SUPERVISOR_TEST_REPO"), ReadyTimeout: 10 * time.Second,
		HealthInterval: 200 * time.Millisecond, StopTimeout: 2 * time.Second,
		Watchdog: []string{exe, WatchdogCommandName},
		ExtraEnv: []string{"FAKE_PANDO_RECORD=" + os.Getenv("SUPERVISOR_TEST_RECORD")},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := sup.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for sup.Status().State != StateReady {
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("ready")
	select {}
}
