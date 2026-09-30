//go:build unix

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// prepareCmd puts the child in its own process group, so a signal reaches its
// helpers too, and asks the kernel to end it with the supervisor where the OS
// can.
//
// With a watchdog command the child runs under it instead (see RunWatchdog):
// the returned lifeline must be closed once the command was waited for, and
// started called right after a successful Start. Without one, Pdeathsig is
// used where the OS has it and the lifeline is nil.
func prepareCmd(cmd *exec.Cmd, watchdog []string) (*lifeline, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(watchdog) > 0 {
		return wrapWithWatchdog(cmd, watchdog)
	}
	setParentDeathSignal(cmd.SysProcAttr)
	return nil, nil
}

// signalGroup sends SIGTERM (or SIGKILL when force is set) to the child's
// process group.
func signalGroup(cmd *exec.Cmd, force bool) {
	if cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = cmd.Process.Signal(sig)
	}
}

// PIDAlive reports whether a process with this pid exists.
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// processCommand returns the command line of a process: /proc where there is
// one, ps(1) elsewhere (macOS).
func processCommand(ctx context.Context, pid int) (string, error) {
	if b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline"); err == nil {
		return strings.ReplaceAll(string(b), "\x00", " "), nil
	}
	out, err := exec.CommandContext(ctx, "ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // pid is an int
	if err != nil {
		return "", fmt.Errorf("ps: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// killOrphan ends the process group led by pid (the child was started with
// Setpgid): SIGTERM, then SIGKILL after grace.
func killOrphan(pid int, grace time.Duration) {
	send := func(sig syscall.Signal) {
		if err := syscall.Kill(-pid, sig); err != nil {
			_ = syscall.Kill(pid, sig)
		}
	}
	send(syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !PIDAlive(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	send(syscall.SIGKILL)
	for i := 0; i < 100 && PIDAlive(pid); i++ {
		time.Sleep(20 * time.Millisecond)
	}
}
