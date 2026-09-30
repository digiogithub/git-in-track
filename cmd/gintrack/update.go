package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
	"github.com/digiogithub/git-in-track/internal/selfupdate"
)

// The values of the "action" field of `gintrack update --json`.
const (
	updateUpToDate = "up_to_date"
	updateAvail    = "available"
	updateDone     = "updated"
	updateRefused  = "refused"
	updateFailed   = "failed"
)

// updateEnv is everything `gintrack update` takes from the machine. The zero
// value is production; tests replace it through updateSeam.
type updateEnv struct {
	// client options for the GitHub lookup (BaseURL, Repo, Token, ...).
	client selfupdate.Options
	// exe is the binary to replace; nil means the running executable.
	exe func() (string, error)
	// channel feeds DetectChannel (ReadFile, in particular).
	channel selfupdate.Env
	// goos and goarch pick the release asset; empty means the runtime's.
	goos, goarch string
	// interactive reports whether stdin is a terminal; nil checks the file.
	interactive func(io.Reader) bool
	// cacheDir is where the supervisor state files live; nil resolves the
	// configuration.
	cacheDir func() (string, error)
}

// updateSeam is the test seam: nil in production.
var updateSeam *updateEnv

// updateResult is what `gintrack update --json` prints.
type updateResult struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	URL     string `json:"url"`
	Action  string `json:"action"`
	Reason  string `json:"reason"`
	Channel string `json:"channel"`
	// Warning is the --force notice for an install owned by a package manager.
	Warning string `json:"warning,omitempty"`
	// OldPath is the previous binary when an update failed half way.
	OldPath string `json:"oldPath,omitempty"`
	// Running lists processes still running the old binary after an update.
	Running []string `json:"running,omitempty"`
}

// updateFlags are the flags of the command.
type updateFlags struct {
	check, prerelease, force, yes, asJSON bool
}

// newUpdateCommand builds `gintrack update`.
func newUpdateCommand(build buildInfo, flags *globalFlags) *cobra.Command {
	var f updateFlags
	cmd := &cobra.Command{
		Use:   "update [version]",
		Short: "Update gintrack to the latest (or a given) GitHub release",
		Long: `Download a gintrack release from GitHub, verify it against the release's
checksums.txt and replace the running binary with it. The installed binary is
not touched until the download is verified.

Without a version it installs the newest stable release (--prerelease includes
pre-releases). With a version it installs exactly that one, which is also the
only way to downgrade. A reinstall of the same version needs --force.

Installs owned by a package manager (Homebrew, Scoop, a container image) are
refused with the right upgrade command; --force replaces the binary anyway. A
source or dev build only updates to an explicit version.

Without --yes it asks for confirmation on a terminal and refuses to run off a
terminal. --check only reports: exit 0 when up to date, 10 when an update is
available. A running "gintrack serve" is never restarted; it is reported so
you can restart it.`,
		Example: `  gintrack update --check
  gintrack update --yes
  gintrack update 2.1.0 --yes`,
		Args: rangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := updateEnv{}
			if updateSeam != nil {
				env = *updateSeam
			}
			version := ""
			if len(args) == 1 {
				version = args[0]
			}
			u := &updater{cmd: cmd, flags: flags, f: f, env: env, build: build, version: version}
			return u.run(cmd.Context())
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&f.check, "check", false, "only report whether an update is available (exit 10 when it is)")
	fl.BoolVar(&f.prerelease, "prerelease", false, "consider pre-releases when looking for the latest")
	fl.BoolVar(&f.force, "force", false, "reinstall the same version, or replace a package-manager or dev install")
	fl.BoolVar(&f.yes, "yes", false, "do not ask for confirmation")
	fl.BoolVar(&f.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// updater carries one run of the command.
type updater struct {
	cmd     *cobra.Command
	flags   *globalFlags
	f       updateFlags
	env     updateEnv
	build   buildInfo
	version string
	res     updateResult
}

// say prints a human line; with --json it goes to stderr so stdout stays JSON.
func (u *updater) say(format string, args ...any) {
	w := u.cmd.OutOrStdout()
	if u.f.asJSON {
		w = u.cmd.ErrOrStderr()
	}
	if u.flags != nil && u.flags.quiet && !u.f.asJSON {
		return
	}
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}

// finish records the outcome, prints it and returns the error for the exit code.
func (u *updater) finish(action, reason string, code int, err error) error {
	u.res.Action, u.res.Reason = action, reason
	if u.f.asJSON {
		enc := json.NewEncoder(u.cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(u.res); encErr != nil && err == nil {
			err = fmt.Errorf("encode update result: %w", encErr)
		}
	}
	if err != nil && code != exitOK {
		return fail(code, err)
	}
	if code != exitOK {
		// A state, not a failure: nothing more for main to print.
		return &exitError{code: code, err: errors.New(reason)}
	}
	return err
}

func (u *updater) run(ctx context.Context) error {
	u.res.Current = strings.TrimPrefix(u.build.Version, "v")
	explicit := u.version != ""

	target, err := u.target()
	if err != nil {
		return u.finish(updateFailed, err.Error(), exitFailure, err)
	}
	chEnv := u.env.channel
	chEnv.Version, chEnv.BuiltBy, chEnv.ExplicitVersion = u.build.Version, u.build.BuiltBy, explicit
	u.res.Channel = string(selfupdate.DetectChannel(target, chEnv))
	warning, err := selfupdate.CheckChannel(target, chEnv, u.f.force)
	if err != nil {
		return u.finish(updateRefused, err.Error(), exitUpdateRefused, err)
	}
	if warning != "" {
		u.res.Warning = warning
		u.say("%s", warning)
	}

	cur, curErr := selfupdate.ParseSemver(u.build.Version)
	if !explicit && curErr != nil && !u.f.force {
		err := fmt.Errorf("the running version %q cannot be compared with a release; pass an explicit version (gintrack update 1.2.3) or use --force", u.build.Version)
		return u.finish(updateRefused, err.Error(), exitUpdateRefused, err)
	}

	opts := u.env.client
	opts.Version = u.build.Version
	client := selfupdate.New(opts)
	var rel *selfupdate.Release
	if explicit {
		rel, err = client.ByVersion(ctx, u.version)
	} else {
		rel, err = client.Latest(ctx, u.f.prerelease)
	}
	if err != nil {
		code := exitFailure
		if errors.Is(err, selfupdate.ErrNotFound) {
			code = exitNotFound
		}
		return u.finish(updateFailed, err.Error(), code, err)
	}
	u.res.Latest, u.res.URL = rel.Version(), rel.HTMLURL

	// Decide what the comparison means.
	downgrade := false
	cmp := 1 // a non-comparable current is always "different"
	if curErr == nil {
		latest, perr := selfupdate.ParseSemver(rel.TagName)
		if perr != nil {
			return u.finish(updateFailed, perr.Error(), exitFailure, perr)
		}
		cmp = latest.Compare(cur)
	}
	switch {
	case cmp == 0:
		if !u.f.force || u.f.check {
			return u.upToDate("gintrack "+u.res.Current+" is the latest version", "already at "+u.res.Latest)
		}
	case cmp < 0:
		if !explicit {
			return u.upToDate("gintrack "+u.res.Current+" is newer than the latest release "+u.res.Latest, "current is newer than the latest release")
		}
		downgrade = true
	}

	if u.f.check {
		verb := "Update available"
		if downgrade {
			verb = "Older release available"
		}
		u.say("%s: gintrack %s -> %s", verb, u.res.Current, u.res.Latest)
		if rel.HTMLURL != "" {
			u.say("Release notes: %s", rel.HTMLURL)
		}
		u.say("Run `gintrack update` to install it.")
		return u.finish(updateAvail, "a different release is available", exitUpdateAvailable, nil)
	}

	asset, err := selfupdate.SelectAsset(rel, orDefault(u.env.goos, runtime.GOOS), orDefault(u.env.goarch, runtime.GOARCH))
	if err != nil {
		return u.finish(updateFailed, err.Error(), exitFailure, err)
	}
	if !u.f.yes {
		if refusal := u.confirm(downgrade, cmp == 0); refusal != "" {
			err := errors.New(refusal)
			return u.finish(updateRefused, refusal, exitUpdateRefused, err)
		}
	}

	tmp, err := os.MkdirTemp(filepath.Dir(target), ".gintrack-update-")
	if err != nil {
		err = fmt.Errorf("cannot stage the update next to %s: %w%s", target, err, permissionHint(err))
		return u.finish(updateFailed, err.Error(), exitUpdateApply, err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	u.say("Downloading %s ...", asset.Name)
	bin, err := client.Download(ctx, rel, asset, tmp)
	if err != nil {
		code := exitFailure
		var mismatch *selfupdate.ChecksumMismatchError
		var size *selfupdate.SizeError
		if errors.As(err, &mismatch) || errors.As(err, &size) || errors.Is(err, selfupdate.ErrChecksumMissing) {
			code = exitUpdateVerify
			err = fmt.Errorf("verification failed, the installed binary was not touched: %w", err)
		}
		return u.finish(updateFailed, err.Error(), code, err)
	}

	if err := selfupdate.Apply(bin, target, selfupdate.ApplyOptions{}); err != nil {
		msg := err.Error() + permissionHint(err)
		var ae *selfupdate.ApplyError
		if errors.As(err, &ae) {
			u.res.OldPath = ae.OldPath
			if ae.BinaryMayBeMissing {
				msg = fmt.Sprintf("%s\nWARNING: %s may be missing. The previous binary is at %s: move it back or reinstall gintrack.", msg, target, ae.OldPath)
			}
		}
		return u.finish(updateFailed, msg, exitUpdateApply, errors.New(msg))
	}

	u.res.Running = u.running()
	u.say("Updated gintrack %s -> %s", u.res.Current, u.res.Latest)
	if len(u.res.Running) > 0 {
		u.say("Restart to use the new version (nothing was restarted):")
		for _, r := range u.res.Running {
			u.say("  - %s", r)
		}
	}
	return u.finish(updateDone, "updated to "+u.res.Latest, exitOK, nil)
}

func (u *updater) upToDate(line, reason string) error {
	u.say("%s.", line)
	return u.finish(updateUpToDate, reason, exitOK, nil)
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func (u *updater) target() (string, error) {
	if u.env.exe != nil {
		return u.env.exe()
	}
	exe, err := selfupdate.CurrentExecutable()
	if err != nil {
		return "", fmt.Errorf("locate the running binary: %w", err)
	}
	return exe, nil
}

// permissionHint explains the usual fix for a read-only install directory.
func permissionHint(err error) string {
	var ae *selfupdate.ApplyError
	if (errors.As(err, &ae) && ae.IsPermission()) || errors.Is(err, os.ErrPermission) {
		return "\nhint: the install directory is not writable by this user; re-run with the privileges that installed gintrack (for example sudo)"
	}
	return ""
}

// confirm asks the question and returns a refusal reason, empty on "yes".
func (u *updater) confirm(downgrade, reinstall bool) string {
	in := u.cmd.InOrStdin()
	interactive := u.env.interactive
	if interactive == nil {
		interactive = isTerminal
	}
	if !interactive(in) {
		return "not a terminal: pass --yes to update without asking, or --check to only look"
	}
	what := "Update"
	switch {
	case downgrade:
		what = "DOWNGRADE"
	case reinstall:
		what = "Reinstall"
	}
	_, _ = fmt.Fprintf(u.cmd.ErrOrStderr(), "%s gintrack %s -> %s? [y/N] ", what, u.res.Current, u.res.Latest)
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return ""
	}
	return "cancelled by the user"
}

// isTerminal reports whether r is a character device (a TTY).
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// running lists the processes that still run the replaced binary, from the
// supervisor state files under the cache dir. Nothing is signaled or started.
func (u *updater) running() []string {
	var cache string
	var err error
	if u.env.cacheDir != nil {
		cache, err = u.env.cacheDir()
	} else if u.flags != nil {
		cache, err = u.resolvedCacheDir()
	}
	if err != nil || cache == "" {
		return nil
	}
	states, _ := filepath.Glob(filepath.Join(cache, "pando", "*", "state.json"))
	var out []string
	seen := map[int]bool{}
	for _, sf := range states {
		st, live := supervisor.Inspect(filepath.Dir(sf))
		if live != supervisor.LiveRunning || st.State == supervisor.StateStopped {
			continue
		}
		if st.SupervisorPID > 0 && !seen[st.SupervisorPID] && supervisor.PIDAlive(st.SupervisorPID) {
			seen[st.SupervisorPID] = true
			out = append(out, fmt.Sprintf("gintrack serve (pid %d) runs the old binary; stop it and start `gintrack serve` again", st.SupervisorPID))
		}
		if st.PID > 0 && supervisor.PIDAlive(st.PID) {
			out = append(out, fmt.Sprintf("managed Pando (pid %d, %s) is restarted by its gintrack serve", st.PID, st.Root))
		}
	}
	return out
}

func (u *updater) resolvedCacheDir() (string, error) {
	res, err := u.flags.resolve()
	if err != nil {
		return "", err
	}
	return res.Config.CacheDir(res.Path), nil
}
