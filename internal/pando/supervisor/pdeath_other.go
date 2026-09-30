//go:build unix && !linux

package supervisor

import (
	"os"
	"syscall"
)

func setParentDeathSignal(*syscall.SysProcAttr) {}

// defaultWatchdog re-executes this binary as `gintrack __pando-watch`, the
// parent-death guarantee of platforms without Pdeathsig (macOS, the BSDs).
func defaultWatchdog() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	return []string{exe, WatchdogCommandName}
}
