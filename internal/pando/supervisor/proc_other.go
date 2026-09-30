//go:build !unix

package supervisor

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// lifeline exists for the unix build; it is never created here.
type lifeline struct{}

func (*lifeline) started() {}
func (*lifeline) Close()   {}

func prepareCmd(*exec.Cmd, []string) (*lifeline, error) { return nil, nil }

// signalGroup kills the child: there is no graceful signal or process group here.
func signalGroup(cmd *exec.Cmd, _ bool) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// PIDAlive reports whether a process with this pid may exist; it cannot tell here.
func PIDAlive(pid int) bool { return pid > 0 }

// defaultWatchdog is empty here: there is no watchdog without unix pipes and
// process groups.
func defaultWatchdog() []string { return nil }

// processCommand cannot tell what a pid runs here.
func processCommand(context.Context, int) (string, error) { return "", errNoProcInfo }

// killOrphan does nothing here.
func killOrphan(int, time.Duration) {}

// errNoProcInfo means the platform cannot tell what a pid runs.
var errNoProcInfo = errors.New("supervisor: cannot inspect processes on this platform")

// WatchdogCommandName is the hidden gintrack subcommand of the unix watchdog.
const WatchdogCommandName = "__pando-watch"

// RunWatchdog is not available here.
func RunWatchdog([]string) int { return 2 }
