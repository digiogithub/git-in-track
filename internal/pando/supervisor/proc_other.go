//go:build !unix

package supervisor

import "os/exec"

func prepareCmd(*exec.Cmd) {}

// signalGroup kills the child: there is no graceful signal or process group here.
func signalGroup(cmd *exec.Cmd, _ bool) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// PIDAlive reports whether a process with this pid may exist; it cannot tell here.
func PIDAlive(pid int) bool { return pid > 0 }
