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
