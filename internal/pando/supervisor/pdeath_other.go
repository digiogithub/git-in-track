//go:build unix && !linux

package supervisor

import "syscall"

func setParentDeathSignal(*syscall.SysProcAttr) {}
