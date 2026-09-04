package server

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// browserLaunchTimeout bounds the helper process. Opening a browser is a
// courtesy: if the launcher hangs, the server carries on without it.
const browserLaunchTimeout = 10 * time.Second

// browserCommand returns the command that opens a URL on an operating system,
// and whether that system is one this build knows how to ask.
//
// It is a pure function so that the mapping is testable everywhere, including
// on the platforms it is not describing.
func browserCommand(goos, target string) (name string, args []string, ok bool) {
	switch goos {
	case "darwin":
		return "open", []string{target}, true
	case "windows":
		// rundll32 is the launcher that survives quoting oddities in cmd.exe.
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}, true
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris":
		return "xdg-open", []string{target}, true
	default:
		return "", nil, false
	}
}

// OpenBrowser asks the desktop to open a URL. Every failure is returned rather
// than acted upon: the caller logs it and keeps serving, because a companion
// without a browser is still a working companion.
func OpenBrowser(ctx context.Context, target string) error {
	name, args, ok := browserCommand(runtime.GOOS, target)
	if !ok {
		return fmt.Errorf("opening a browser is not supported on %s", runtime.GOOS)
	}
	launchCtx, cancel := context.WithTimeout(ctx, browserLaunchTimeout)
	defer cancel()

	// #nosec G204 -- the program name is a constant per platform and the only
	// argument is the URL this process is listening on.
	cmd := exec.CommandContext(launchCtx, name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open %s with %s: %w", target, name, err)
	}
	// The helper detaches immediately on every platform; reaping it keeps no
	// zombie behind.
	go func() { _ = cmd.Wait() }()
	return nil
}
