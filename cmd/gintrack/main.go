// Command gintrack is the git-in-track companion: it serves the web UI and the
// local REST API, exposes the backlog to AI agents over MCP, and offers the
// backlog itself on the command line.
//
// The command files in this package contain no business logic. They parse
// flags, build a request for internal/core or internal/server, and render the
// result, so that the CLI and the HTTP API cannot drift apart.
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
	"github.com/digiogithub/git-in-track/internal/selfupdate"
)

// Build information, set by the release pipeline with:
//
//	-ldflags "-X main.version=... -X main.commit=... -X main.date=..."
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "source"
)

func main() {
	// The parent-death watchdog of managed Pando (GIT-US-0187) is this same
	// binary re-executed. It is handled before cobra: no flags, no config, no
	// logging setup, and it is not a command users see.
	if len(os.Args) > 1 && os.Args[1] == supervisor.WatchdogCommandName {
		os.Exit(supervisor.RunWatchdog(os.Args[2:]))
	}
	// A Windows self-update leaves the previous image behind as .<name>.old
	// because a running .exe cannot be deleted; remove it now (GIT-US-0193).
	if runtime.GOOS == "windows" {
		if exe, err := selfupdate.CurrentExecutable(); err == nil {
			selfupdate.CleanupOld(exe)
		}
	}
	err := Execute(buildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
		BuiltBy: builtBy,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gintrack:", err)
	}
	os.Exit(exitCode(err))
}
