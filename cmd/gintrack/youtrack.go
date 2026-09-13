package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/cmd/gintrack/output"
	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// `gintrack youtrack`, story GIT-US-0058.
//
// Two commands, one for a person setting up a machine and one for the
// provisioning script that has to check the setup took. Both are thin: the
// connection lives in internal/config and the probe in internal/youtrack, and
// neither ever prints the token, in text or in JSON (ADR-032).

// youtrackProbeTimeout bounds the live probe of both commands, so a wrong URL
// fails in seconds rather than hanging a provisioning script.
const youtrackProbeTimeout = 20 * time.Second

// maxTokenBytes bounds what is read from standard input. A permanent token is
// well under a hundred bytes; anything larger is a file piped in by mistake.
const maxTokenBytes = 8 << 10

// newYouTrackCommand groups the YouTrack subcommands.
func newYouTrackCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "youtrack",
		Short: "Connect a project to YouTrack and check the connection",
		Long: strings.TrimSpace(`
Link a git-in-track project to a YouTrack project and verify the credentials.

The connection is stored in two halves. The instance URL, the YouTrack project
and the field mapping go into the project's project.yaml and are committed, so
that a clone knows where its items came from. The permanent token goes into this
machine's configuration file, mode 0600, and is never committed, never printed
and never sent anywhere but the YouTrack instance itself.`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageError(cmd.Help())
		},
	}
	cmd.AddCommand(newYouTrackConnectCommand(flags), newYouTrackStatusCommand(flags))
	return cmd
}

// youtrackConnectFlags are the flags of `gintrack youtrack connect`.
type youtrackConnectFlags struct {
	url          string
	project      string
	projectKey   string
	token        string
	pushComments string
	kbSync       string
	kbDirection  string
	asJSON       bool
}

// youtrackConnectPayload is what `connect --json` prints. There is no token
// field and there never will be one.
type youtrackConnectPayload struct {
	ProjectKey  string `json:"projectKey"`
	URL         string `json:"url"`
	Project     string `json:"project"`
	Login       string `json:"login"`
	FullName    string `json:"fullName,omitempty"`
	TokenSource string `json:"tokenSource"`
	// TokenInput says where connect read the token from: `flag`, `env` or
	// `stdin`. The value itself appears nowhere.
	TokenInput  string `json:"tokenInput"`
	ConfigPath  string `json:"configPath"`
	ProjectPath string `json:"projectPath"`
}

// newYouTrackConnectCommand links a project to a YouTrack instance.
func newYouTrackConnectCommand(flags *globalFlags) *cobra.Command {
	local := &youtrackConnectFlags{}

	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Link a project to a YouTrack instance",
		Long: strings.TrimSpace(`
Validate a YouTrack permanent token, store it on this machine and write the
instance URL and the YouTrack project into the project's project.yaml.

The token is read from --token, then from $GINTRACK_YOUTRACK_TOKEN, then from
standard input when it is piped:

  echo "$YT_TOKEN" | gintrack youtrack connect --url https://yt.example.com --project ACME

Nothing is written unless the token authenticates: a 401, a 403 and a 404 each
leave both the configuration file and project.yaml exactly as they were.`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runYouTrackConnect(cmd, flags, local)
		},
	}
	cmd.Flags().StringVar(&local.url, "url", "", "YouTrack instance URL, context path included")
	cmd.Flags().StringVar(&local.project, "project", "", "YouTrack project short name, the \"ACME\" of ACME-42")
	cmd.Flags().StringVar(&local.projectKey, "project-key", "", "git-in-track project key to link (default: the only one)")
	cmd.Flags().StringVar(&local.token, "token", "", "YouTrack permanent token (prefer the environment or standard input)")
	cmd.Flags().StringVar(&local.pushComments, "push-comments", "", "when to push comments: manual or auto")
	cmd.Flags().StringVar(&local.kbSync, "kb-sync", "", "when to sync knowledge-base pages: manual or on_write")
	cmd.Flags().StringVar(&local.kbDirection, "kb-sync-direction", "", "which way pages flow: push, pull or both")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runYouTrackConnect probes the instance and, only then, writes both halves.
func runYouTrackConnect(cmd *cobra.Command, flags *globalFlags, local *youtrackConnectFlags) error {
	if strings.TrimSpace(local.url) == "" {
		return usagef("--url is required: give the YouTrack instance URL, context path included")
	}
	if strings.TrimSpace(local.project) == "" {
		return usagef("--project is required: give the YouTrack project short name")
	}
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	token, source, err := resolveYouTrackToken(cmd, flags, local.token)
	if err != nil {
		return err
	}

	link := config.YouTrackLink{
		URL:             local.url,
		Project:         local.project,
		PushComments:    config.PushCommentsMode(local.pushComments),
		KBSync:          config.KBSyncMode(local.kbSync),
		KBSyncDirection: config.KBSyncDirection(local.kbDirection),
	}
	target, err := findProjectYAML(cmd.Context(), res, local.projectKey)
	if err != nil {
		return err
	}
	if existing, err := config.LoadYouTrackLink(target.path); err == nil && existing != nil {
		// Keep the field map a previous connect or the web UI wrote: this
		// command sets the connection, it does not reset the mapping.
		link.FieldMap = existing.FieldMap
	}
	if err := link.Normalized().Validate(); err != nil {
		return fail(exitValidation, err)
	}

	// The probe comes before every write, so a bad credential never leaves a
	// half-connected project behind.
	client, err := youtrack.New(youtrack.Options{BaseURL: link.URL, Token: token})
	if err != nil {
		return youtrackExitError(err)
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), youtrackProbeTimeout)
	defer cancel()

	me, err := client.Me(ctx)
	if err != nil {
		return youtrackExitError(err)
	}
	if _, err := client.Project(ctx, link.Project); err != nil {
		return youtrackExitError(err)
	}

	if _, err := config.SaveYouTrackLink(target.path, link); err != nil {
		return fmt.Errorf("write %s: %w", target.display, err)
	}
	// Connecting is the act of making the link permanent, so the token is
	// stored whichever of the three sources it came from. The file is written
	// 0600 by internal/config; nothing else on this machine can read it.
	cfg, err := config.Load(res.Path)
	if err != nil {
		return fmt.Errorf("read the configuration: %w", err)
	}
	cfg.SetYouTrackToken(target.key, token)
	if err := config.Save(res.Path, cfg); err != nil {
		return fmt.Errorf("save the configuration: %w", err)
	}

	p := flags.printer(cmd, local.asJSON)
	payload := youtrackConnectPayload{
		ProjectKey:  target.key,
		URL:         client.BaseURL(),
		Project:     link.Project,
		Login:       me.Login,
		FullName:    me.FullName,
		TokenSource: string(config.TokenSourceFile),
		TokenInput:  string(source),
		ConfigPath:  res.Path,
		ProjectPath: target.display,
	}
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	p.Printf("Connected %s to %s on %s as %s.\n", payload.ProjectKey, payload.Project, payload.URL, payload.Login)
	p.Printf("  link:  %s\n", payload.ProjectPath)
	p.Printf("  token: %s (read from the %s, stored %s)\n", payload.ConfigPath, payload.TokenInput, payload.TokenSource)
	return nil
}

// youtrackStatusFlags are the flags of `gintrack youtrack status`.
type youtrackStatusFlags struct {
	projectKey string
	offline    bool
	asJSON     bool
}

// youtrackStatusPayload is what `status --json` prints: the same information as
// the text form, in a shape a provisioning script can branch on, and no token.
type youtrackStatusPayload struct {
	ProjectKey  string `json:"projectKey"`
	Configured  bool   `json:"configured"`
	URL         string `json:"url,omitempty"`
	Project     string `json:"project,omitempty"`
	HasToken    bool   `json:"hasToken"`
	TokenSource string `json:"tokenSource"`
	ProjectPath string `json:"projectPath,omitempty"`
	Probed      bool   `json:"probed"`
	OK          bool   `json:"ok"`
	Login       string `json:"login,omitempty"`
	FullName    string `json:"fullName,omitempty"`
	Error       string `json:"error,omitempty"`
}

// newYouTrackStatusCommand reports the connection of a project.
func newYouTrackStatusCommand(flags *globalFlags) *cobra.Command {
	local := &youtrackStatusFlags{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report the YouTrack connection of a project",
		Long: strings.TrimSpace(`
Print the instance URL, the git-in-track to YouTrack project mapping, whether a
token is present and where it came from, and the result of a live probe.

Exit codes: 0 when the connection works, 4 when the project declares none, and 1
when the probe fails. Pass --offline to report the configuration without
touching the network.`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runYouTrackStatus(cmd, flags, local)
		},
	}
	cmd.Flags().StringVar(&local.projectKey, "project-key", "", "git-in-track project key (default: the only one)")
	cmd.Flags().BoolVar(&local.offline, "offline", false, "report the configuration without probing the instance")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runYouTrackStatus prints the connection and, unless asked not to, probes it.
func runYouTrackStatus(cmd *cobra.Command, flags *globalFlags, local *youtrackStatusFlags) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	target, err := findProjectYAML(cmd.Context(), res, local.projectKey)
	if err != nil {
		return err
	}
	link, err := config.LoadYouTrackLink(target.path)
	if err != nil {
		return fail(exitValidation, err)
	}
	token, source := res.Config.YouTrackToken(target.key)

	payload := youtrackStatusPayload{
		ProjectKey:  target.key,
		Configured:  link != nil,
		HasToken:    token != "",
		TokenSource: string(source),
		ProjectPath: target.display,
	}
	if link != nil {
		payload.URL, payload.Project = link.URL, link.Project
	}

	var probeErr error
	if link != nil && token != "" && !local.offline {
		payload.Probed = true
		client, err := youtrack.New(youtrack.Options{BaseURL: link.URL, Token: token})
		if err != nil {
			probeErr = err
		} else {
			ctx, cancel := context.WithTimeout(cmd.Context(), youtrackProbeTimeout)
			defer cancel()
			me, err := client.Me(ctx)
			if err != nil {
				probeErr = err
			} else {
				payload.OK, payload.Login, payload.FullName = true, me.Login, me.FullName
			}
		}
		if probeErr != nil {
			payload.Error = probeErr.Error()
		}
	}

	if err := printYouTrackStatus(flags.printer(cmd, local.asJSON), payload, local.offline); err != nil {
		return err
	}
	switch {
	case link == nil:
		return notFoundf("project %s declares no integrations.youtrack block: run `gintrack youtrack connect`", target.key)
	case token == "":
		return notFoundf("no YouTrack token is stored for project %s: run `gintrack youtrack connect`", target.key)
	case probeErr != nil:
		return youtrackExitError(probeErr)
	default:
		return nil
	}
}

// printYouTrackStatus renders the payload in whichever form was asked for.
func printYouTrackStatus(p *output.Printer, payload youtrackStatusPayload, offline bool) error {
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	p.Printf("project:  %s\n", payload.ProjectKey)
	p.Printf("instance: %s\n", orDash(payload.URL))
	p.Printf("mapping:  %s -> %s\n", payload.ProjectKey, orDash(payload.Project))
	p.Printf("link:     %s\n", orDash(payload.ProjectPath))
	if payload.HasToken {
		p.Printf("token:    present (%s)\n", payload.TokenSource)
	} else {
		p.Printf("token:    absent\n")
	}
	switch {
	case offline:
		p.Printf("probe:    skipped (--offline)\n")
	case !payload.Probed:
		p.Printf("probe:    skipped (nothing to probe)\n")
	case payload.OK:
		p.Printf("probe:    ok as %s\n", payload.Login)
	default:
		p.Printf("probe:    failed\n")
	}
	return nil
}

// youtrackSourceStdin marks a token that was piped in. It is not a provenance
// the configuration stores: it only says where connect read the value from.
const youtrackSourceStdin config.TokenSource = "stdin"

// resolveYouTrackToken applies the documented precedence: the flag, then
// GINTRACK_YOUTRACK_TOKEN, then standard input when it is piped rather than a
// terminal. There is no interactive prompt: reading a pipe is what keeps the
// command scriptable.
func resolveYouTrackToken(cmd *cobra.Command, flags *globalFlags, flagToken string) (string, config.TokenSource, error) {
	if token := strings.TrimSpace(flagToken); token != "" {
		return token, config.TokenSourceFlag, nil
	}
	if token := strings.TrimSpace(flags.reader()(config.EnvYouTrackToken)); token != "" {
		return token, config.TokenSourceEnv, nil
	}
	if !stdinIsPiped(cmd) {
		return "", config.TokenSourceNone, usagef(
			"no token: pass --token, set %s, or pipe the token into this command", config.EnvYouTrackToken)
	}
	raw, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxTokenBytes))
	if err != nil {
		return "", config.TokenSourceNone, fmt.Errorf("read the token from standard input: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", config.TokenSourceNone, usagef(
			"no token: standard input was empty, so pass --token or set %s", config.EnvYouTrackToken)
	}
	return token, youtrackSourceStdin, nil
}

// stdinIsPiped reports whether standard input is something other than a
// terminal. A test drives the command with a bytes reader, which is a pipe for
// this purpose: it has content to read and nobody to prompt.
func stdinIsPiped(cmd *cobra.Command) bool {
	in := cmd.InOrStdin()
	file, ok := in.(*os.File)
	if !ok {
		return in != nil
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

// youtrackTarget is the project.yaml a command writes to and the project key it
// belongs to.
type youtrackTarget struct {
	key string
	// path is the absolute file on disk.
	path string
	// display is the `<repo>:<path>` form the other commands print.
	display string
}

// findProjectYAML locates the project.yaml of the project a command acts on,
// defaulting to the only project of the workspace.
func findProjectYAML(ctx context.Context, res *config.Resolution, projectKey string) (youtrackTarget, error) {
	v, err := openVault(ctx, res, false)
	if err != nil {
		return youtrackTarget{}, err
	}
	var found projectView
	if projectKey == "" {
		found, err = v.only()
	} else {
		found, err = v.project(core.ProjectKey(strings.ToUpper(projectKey)))
	}
	if err != nil {
		return youtrackTarget{}, err
	}
	_, rel := repoPath(found.Ref.ConfigPath)
	return youtrackTarget{
		key:     string(found.Ref.Key),
		path:    filepath.Join(found.Repo.Path, filepath.FromSlash(rel)),
		display: displayPath(found.Ref.ConfigPath),
	}, nil
}

// youtrackExitError maps a client failure onto an exit code and a message that
// says what to fix. The error itself is already redacted by internal/youtrack;
// nothing here adds the token back.
func youtrackExitError(err error) error {
	switch {
	case errors.Is(err, youtrack.ErrUnauthorized):
		return failf(exitFailure, "YouTrack rejected the token (401): check that it has not expired and was copied whole, `perm:` prefix included")
	case errors.Is(err, youtrack.ErrForbidden):
		return failf(exitFailure, "YouTrack refused the request (403): the account this token belongs to lacks permission for that project")
	case errors.Is(err, youtrack.ErrNotFound):
		return failf(exitFailure, "YouTrack answered 404: the URL is most likely missing its context path, as in https://host/youtrack")
	case errors.Is(err, youtrack.ErrInvalidInput):
		return fail(exitValidation, err)
	default:
		return failf(exitFailure, "could not reach YouTrack: %v", err)
	}
}
