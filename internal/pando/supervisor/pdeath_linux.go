//go:build linux

package supervisor

import "syscall"

func setParentDeathSignal(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGTERM }
