//go:build unix

package supervisor

import (
	"errors"
	"os/exec"
	"syscall"
)

// prepareCmd puts the child in its own process group, so a signal reaches its
// helpers too, and asks the kernel to end it with the supervisor where the OS
// can.
func prepareCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setParentDeathSignal(cmd.SysProcAttr)
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
