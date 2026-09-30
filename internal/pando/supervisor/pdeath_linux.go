//go:build linux

package supervisor

import "syscall"

func setParentDeathSignal(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGTERM }

// defaultWatchdog is empty on Linux: Pdeathsig already ties the child to the
// supervisor.
func defaultWatchdog() []string { return nil }
