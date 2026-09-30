package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/selfupdate"
)

// noticeWait is the longest an exiting command waits for the background
// refresh of the update cache. The lookup itself may take longer; whatever it
// has not finished by then is simply tried again by the next command.
const noticeWait = 300 * time.Millisecond

// installNotApplicable says why update checks make no sense for this build, or
// returns "". It is the seam GIT-US-0194 points at the install-channel
// detection (package-manager installs); today it knows development builds
// only. It is a variable so that tests can replace it.
var installNotApplicable = func(build buildInfo) string {
	if build.Version == "dev" || build.BuiltBy == "source" {
		return "development build"
	}
	return ""
}

// newUpdateChecker builds the cached latest-version checker of this process.
// Tests replace it to point the lookup at a fake GitHub.
var newUpdateChecker = func(build buildInfo, cacheDir string) *selfupdate.Checker {
	return &selfupdate.Checker{
		Dir:           cacheDir,
		Current:       build.Version,
		NotApplicable: func() string { return installNotApplicable(build) },
	}
}

// stderrIsTerminal reports whether w is an interactive terminal.
var stderrIsTerminal = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// noticeSilent are the top-level commands that never print the update notice:
// `doctor` and `update` and `version` report it themselves or are about it,
// `serve` and `mcp` are long-running and speak over other channels, and the
// completion machinery must print nothing else.
var noticeSilent = map[string]bool{
	"doctor": true, "update": true, "version": true, "serve": true, "mcp": true,
	"completion": true, "help": true, "__complete": true, "__completeNoDesc": true,
}

// noticeInputs is everything the suppression rules look at.
type noticeInputs struct {
	command    string // top-level command name
	asJSON     bool
	quiet      bool
	stderrTTY  bool
	checkStart bool // update.checkOnStart
}

// noticeAllowed applies the opt-in and suppression rules of GIT-US-0195: the
// notice is printed only when the user opted in and the invocation is an
// interactive, human-readable one.
func noticeAllowed(in noticeInputs) bool {
	return in.checkStart && in.stderrTTY && !in.asJSON && !in.quiet && !noticeSilent[in.command]
}

// topLevelName returns the name of the command directly under the root.
func topLevelName(cmd *cobra.Command) string {
	for cmd.HasParent() && cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}
	return cmd.Name()
}

// jsonRequested reports whether the command was run with --json.
func jsonRequested(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("json")
	return f != nil && f.Value.String() == "true"
}

// updateNotice is the per-invocation state of the opt-in notice.
type updateNotice struct {
	checker *selfupdate.Checker
	done    chan struct{} // closed when the background refresh ends; nil when none ran
}

// startUpdateNotice decides, before the command runs, whether a notice may be
// printed and starts the cache refresh in the background when it is due. It
// never blocks and never fails the command.
func startUpdateNotice(cmd *cobra.Command, flags *globalFlags, build buildInfo) {
	flags.notice = nil
	in := noticeInputs{
		command:   topLevelName(cmd),
		asJSON:    jsonRequested(cmd),
		quiet:     flags.quiet,
		stderrTTY: stderrIsTerminal(cmd.ErrOrStderr()),
	}
	// Cheap static rules first: no configuration is read for a command that
	// can never print the line.
	in.checkStart = true
	if !noticeAllowed(in) {
		return
	}
	res, err := flags.resolve()
	if err != nil || !res.Config.Update.CheckOnStart {
		return
	}
	n := &updateNotice{checker: newUpdateChecker(build, res.Config.CacheDir(res.Path))}
	if _, fresh := n.checker.Cached(); !fresh {
		n.done = make(chan struct{})
		go func() {
			defer close(n.done)
			n.checker.Check(context.Background())
		}()
	}
	flags.notice = n
}

// finishUpdateNotice prints the single stderr line after the command ran, from
// the cache, waiting briefly for a refresh that is still in flight.
func finishUpdateNotice(cmd *cobra.Command, flags *globalFlags) {
	n := flags.notice
	flags.notice = nil
	if n == nil {
		return
	}
	if n.done != nil {
		select {
		case <-n.done:
		case <-time.After(noticeWait):
		}
	}
	st, _ := n.checker.Cached()
	if line := noticeLine(st); line != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), line)
	}
}

// noticeLine is the one line, or "" when there is nothing to say.
func noticeLine(st selfupdate.Status) string {
	if !st.UpdateAvailable || st.NotApplicable != "" {
		return ""
	}
	return fmt.Sprintf("gintrack %s is available (you have %s): run `gintrack update`",
		strings.TrimPrefix(st.Latest, "v"), strings.TrimPrefix(st.Current, "v"))
}

// updateCacheDir is the directory the checker stores its file in.
func updateCacheDir(res *config.Resolution) string { return res.Config.CacheDir(res.Path) }
