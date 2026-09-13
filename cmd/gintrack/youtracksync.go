package main

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/cmd/gintrack/output"
	"github.com/digiogithub/git-in-track/internal/server"
	"github.com/digiogithub/git-in-track/internal/syncengine"
	corevault "github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack/mapping"
)

// The three YouTrack commands that move content rather than configuration:
// `import` (GIT-US-0062), `push-comments` (GIT-US-0079) and `kb push|pull`
// (GIT-US-0094).
//
// None of them holds any integration logic. Each resolves the configuration,
// opens a companion with no listener, dispatches exactly one core method and
// prints what came back — the same `youtrack.import.*`, `youtrack.comment.push`
// and `youtrack.kb.*` methods the REST API and the MCP server call, so the
// idempotence rules, the validation and the git writes have one implementation
// whichever surface asked.
//
// The companion is what makes that possible. A command needs a YouTrack client
// built from the machine-local token, the two seams a vault reaches a tracker
// through and the engine that runs a queued job; `internal/server` already
// assembles all of it, so the commands build the same process and never bind a
// port (see internal/server/headless.go).

// ------------------------------------------------------------ the runner ---

// runner is the headless companion one command drives, together with the
// project it acts on.
type runner struct {
	*server.Headless
	// project is the git-in-track project key every call is scoped to.
	project string
}

// openRunner builds the companion and resolves the project the command acts on:
// the one named with --project-key, or the only one the workspace serves.
func openRunner(cmd *cobra.Command, flags *globalFlags, projectKey string) (*runner, error) {
	res, err := flags.resolve()
	if err != nil {
		return nil, err
	}
	repos, err := mountList(res.Config, res.Workspace, nil)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, notFoundf("no repository is registered in workspace %q: run `gintrack add <path>`", res.Workspace)
	}
	headless, err := server.NewHeadless(server.Options{
		Version:   cmd.Root().Version,
		Workspace: res.Workspace,
		Repos:     repos,
		// The committed half of the connection is read from each project.yaml
		// by the companion itself; this is the machine-local half, resolved
		// flag over environment over the 0600 file (ADR-032). Neither is ever
		// printed.
		YouTrack:   res.Config.YouTrackTokens(),
		Git:        res.Config.Git,
		ConfigPath: res.Path,
		SyncEngine: server.SyncEngine{CacheDir: res.Config.Index.CacheDir},
		// Logs go to stderr whatever the command prints on stdout, so
		// `… --json | jq` stays safe.
		Logger: slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		return nil, fmt.Errorf("open the workspace: %w", err)
	}
	key, err := resolveProjectKey(headless, projectKey)
	if err != nil {
		return nil, err
	}
	return &runner{Headless: headless, project: key}, nil
}

// resolveProjectKey picks the project a command acts on.
func resolveProjectKey(headless *server.Headless, projectKey string) (string, error) {
	keys := headless.Projects()
	if key := strings.ToUpper(strings.TrimSpace(projectKey)); key != "" {
		for _, known := range keys {
			if known == key {
				return key, nil
			}
		}
		return "", notFoundf("no project %q in this workspace: it serves %s", key, joinOrDash(keys))
	}
	switch len(keys) {
	case 0:
		return "", notFoundf("no project is registered in this workspace: run `gintrack add <path>`")
	case 1:
		return keys[0], nil
	default:
		return "", usagef("--project-key is required: the workspace holds %s", strings.Join(keys, ", "))
	}
}

// call dispatches one core method against the project of the runner.
func (r *runner) call(ctx context.Context, method string, params, out any) error {
	if err := r.Dispatch(ctx, method, params, out); err != nil {
		return vaultError(err)
	}
	return nil
}

// Dispatch scopes every call to the runner's project.
func (r *runner) Dispatch(ctx context.Context, method string, params, out any) error {
	return r.Headless.Dispatch(ctx, r.project, method, params, out) //nolint:wrapcheck // call adds the exit code
}

// follow waits for a queued job and reports why it ended badly, if it did.
//
// The signal handler is the command's own: Ctrl-C stops the waiting, not the
// job. A push that is halfway through a call to a tracker finishes; what the
// interrupt cancels is watching it, which is what the message says.
func (r *runner) follow(cmd *cobra.Command, jobID string) (syncengine.Job, error) {
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	job, err := r.WaitForJob(ctx, jobID)
	if err != nil {
		return job, failf(exitFailure, "%v", err)
	}
	return job, nil
}

// closeRunner drains the engine. The context is detached so that a job in
// flight is not cancelled by the very shutdown waiting for it.
func closeRunner(cmd *cobra.Command, r *runner) {
	if r != nil {
		r.Close(context.WithoutCancel(cmd.Context()))
	}
}

// ------------------------------------------------------------- the import --

// youtrackImportFlags are the flags of `gintrack youtrack import`.
type youtrackImportFlags struct {
	projectKey  string
	depth       int
	comments    bool
	attachments bool
	links       bool
	dryRun      bool
	asJSON      bool
}

// newYouTrackImportCommand imports issues into a project's backlog.
func newYouTrackImportCommand(flags *globalFlags) *cobra.Command {
	local := &youtrackImportFlags{}

	cmd := &cobra.Command{
		Use:   "import <query | ID...>",
		Short: "Import YouTrack issues into a project's backlog",
		Long: strings.TrimSpace(`
Import issues into the backlog, by query or by readable id.

One argument that is not an issue id is a YouTrack query; one or more arguments
that are issue ids import exactly those issues. Give one or the other, never
both:

  gintrack youtrack import "project: ACME #Unresolved" --depth 1
  gintrack youtrack import ACME-42 ACME-43 --comments

An import is idempotent. The pair (system, id) of an issue decides whether it
becomes a new item or patches the one already mirroring it, so importing the
same issue twice can never produce a second item.

Pass --dry-run to see the plan — what each issue would become, which item an
update would patch and what could not be resolved — without writing anything.`),
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runYouTrackImport(cmd, flags, local, args)
		},
	}
	cmd.Flags().StringVar(&local.projectKey, "project-key", "", "git-in-track project key (default: the only one)")
	cmd.Flags().IntVar(&local.depth, "depth", 0,
		fmt.Sprintf("how deep to follow subtasks, 0 to %d", corevault.YouTrackMaxDepth))
	cmd.Flags().BoolVar(&local.comments, "comments", false, "import the issue comment threads")
	cmd.Flags().BoolVar(&local.attachments, "attachments", false, "record the attachments of each issue")
	cmd.Flags().BoolVar(&local.links, "links", false, "import the non-hierarchy relations as links")
	cmd.Flags().BoolVar(&local.dryRun, "dry-run", false, "print the plan and write nothing")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runYouTrackImport dispatches the preview or the run and prints the table.
func runYouTrackImport(cmd *cobra.Command, flags *globalFlags, local *youtrackImportFlags, args []string) error {
	r, err := openRunner(cmd, flags, local.projectKey)
	if err != nil {
		return err
	}
	defer closeRunner(cmd, r)

	params := corevault.YouTrackImportParams{
		Project:            r.project,
		Depth:              local.depth,
		IncludeLinks:       local.links,
		IncludeComments:    local.comments,
		IncludeAttachments: local.attachments,
	}
	if ids, isIDs := issueIDArgs(args); isIDs {
		params.IDs = ids
	} else {
		params.Query = strings.Join(args, " ")
	}

	p := flags.printer(cmd, local.asJSON)
	if local.dryRun {
		var preview corevault.YouTrackImportPreview
		if err := r.call(cmd.Context(), "youtrack.import.preview", params, &preview); err != nil {
			return err
		}
		return printImportPreview(p, preview)
	}
	var result corevault.YouTrackImportResult
	if err := r.call(cmd.Context(), "youtrack.import.run", params, &result); err != nil {
		return err
	}
	return printImportResult(p, result)
}

// issueIDArgs reports whether the arguments are readable issue ids rather than
// a query, which is what lets one command accept both without a second flag.
//
// A readable id is `<PROJECT>-<number>` with no whitespace; a query has spaces,
// a colon or a `#`. Every argument must look like an id for the set to be one:
// a single stray word alongside two ids is much more likely to be a typed query
// than a mistyped id, and sending it as a query fails in YouTrack with a
// message about the query, which is the more useful failure.
func issueIDArgs(args []string) ([]string, bool) {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		dash := strings.LastIndex(trimmed, "-")
		if dash <= 0 || dash == len(trimmed)-1 || strings.ContainsAny(trimmed, " \t:#") {
			return nil, false
		}
		if strings.TrimLeft(trimmed[dash+1:], "0123456789") != "" {
			return nil, false
		}
		out = append(out, trimmed)
	}
	return out, len(out) > 0
}

// printImportPreview renders a dry run: what each issue would become.
func printImportPreview(p *output.Printer, preview corevault.YouTrackImportPreview) error {
	if p.JSONMode() {
		if err := render(p.JSON(preview)); err != nil {
			return err
		}
		printImportWarnings(p, preview.Warnings)
		return nil
	}
	rows := make([][]string, 0, len(preview.Issues))
	for _, issue := range preview.Issues {
		rows = append(rows, []string{
			issue.YouTrackID, issue.Action, orDash(issue.TargetID),
			string(issue.MappedType), issue.Title,
		})
	}
	if err := render(p.Table([]string{"ISSUE", "ACTION", "ITEM", "TYPE", "TITLE"}, rows)); err != nil {
		return err
	}
	p.Printf("%s would be imported into %s; nothing was written.\n",
		plural(len(preview.Issues), "issue", "issues"), preview.Project)
	printImportWarnings(p, preview.Warnings)
	return nil
}

// printImportResult renders a run: what each issue became.
func printImportResult(p *output.Printer, result corevault.YouTrackImportResult) error {
	if p.JSONMode() {
		if err := render(p.JSON(result)); err != nil {
			return err
		}
		printImportWarnings(p, result.Warnings)
		return importExit(p, result)
	}
	rows := make([][]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		action := issue.Action
		if issue.Error != "" {
			action = "failed"
		}
		rows = append(rows, []string{issue.YouTrackID, action, orDash(issue.ItemID), orDash(issue.Error)})
	}
	if err := render(p.Table([]string{"ISSUE", "ACTION", "ITEM", "ERROR"}, rows)); err != nil {
		return err
	}
	p.Printf("%d created, %d updated, %d failed in %s.\n",
		result.Created, result.Updated, result.Failed, result.Project)
	printImportWarnings(p, result.Warnings)
	return importExit(p, result)
}

// importExit fails the command when an issue could not be imported. The batch
// still landed: a failure is reported per issue and never aborts the others.
func importExit(p *output.Printer, result corevault.YouTrackImportResult) error {
	if result.Failed == 0 {
		return nil
	}
	p.Warnf("%s could not be imported.\n", plural(result.Failed, "issue", "issues"))
	return failf(exitFailure, "%s could not be imported", plural(result.Failed, "issue", "issues"))
}

// printImportWarnings writes the per-import findings to the notes stream, which
// is stderr in JSON mode.
func printImportWarnings(p *output.Printer, warnings []mapping.Warning) {
	for _, warning := range warnings {
		p.Warnf("warning: %s\n", warning.String())
	}
}

// ----------------------------------------------------- the comment push ---

// youtrackPushFlags are the flags of `gintrack youtrack push-comments`.
type youtrackPushFlags struct {
	projectKey string
	all        bool
	comment    string
	wait       bool
	asJSON     bool
}

// newYouTrackPushCommentsCommand pushes an item's comments to its issue.
func newYouTrackPushCommentsCommand(flags *globalFlags) *cobra.Command {
	local := &youtrackPushFlags{}

	cmd := &cobra.Command{
		Use:   "push-comments <ITEM-ID>",
		Short: "Push an item's comments to the YouTrack issue it mirrors",
		Long: strings.TrimSpace(`
Queue the comments of one item for the issue it mirrors.

Give --all for the whole thread or --comment <path> for one comment; one or the
other, never neither and never both. A comment that already carries a YouTrack
reference is skipped rather than posted twice.

Pushing is queued, not immediate: without --wait the command prints the job id
and returns, and the job runs in this process until it is done. With --wait it
follows the job and prints a table of comment, remote id and result, and exits
non-zero when any comment failed.

A local delete never deletes remotely: there is no job for it and deliberately
none, because a repository is not the authority on an issue's conversation.`),
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runYouTrackPushComments(cmd, flags, local, args[0])
		},
	}
	cmd.Flags().StringVar(&local.projectKey, "project-key", "", "git-in-track project key (default: the only one)")
	cmd.Flags().BoolVar(&local.all, "all", false, "push every comment of the item")
	cmd.Flags().StringVar(&local.comment, "comment", "", "push one comment, by its vault-relative path")
	cmd.Flags().BoolVar(&local.wait, "wait", false, "follow the queued job and print what it did")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runYouTrackPushComments queues the push and, when asked, waits for it.
func runYouTrackPushComments(
	cmd *cobra.Command, flags *globalFlags, local *youtrackPushFlags, itemID string,
) error {
	r, err := openRunner(cmd, flags, local.projectKey)
	if err != nil {
		return err
	}
	defer closeRunner(cmd, r)
	r.Start(cmd.Context())

	var result corevault.YouTrackCommentPushResult
	err = r.call(cmd.Context(), "youtrack.comment.push", corevault.YouTrackCommentPushParams{
		Project: r.project, ItemID: itemID, CommentPath: local.comment, All: local.all,
	}, &result)
	if err != nil {
		return err
	}

	p := flags.printer(cmd, local.asJSON)
	if local.wait && result.JobID != "" {
		if _, err := r.follow(cmd, result.JobID); err != nil {
			return err
		}
		// The job wrote the remote ids back into the comment files, so the
		// result is read again rather than reprinted from the queueing answer.
		if err := r.call(cmd.Context(), "youtrack.comment.push", corevault.YouTrackCommentPushParams{
			Project: r.project, ItemID: itemID, CommentPath: local.comment, All: local.all,
		}, &result); err != nil {
			return err
		}
	}
	return printCommentPush(p, result, local.wait)
}

// printCommentPush renders what a push queued, skipped and failed.
func printCommentPush(p *output.Printer, result corevault.YouTrackCommentPushResult, waited bool) error {
	if p.JSONMode() {
		if err := render(p.JSON(result)); err != nil {
			return err
		}
		return commentPushExit(p, result, waited)
	}
	rows := make([][]string, 0, len(result.Pushed)+len(result.Skipped)+len(result.Failed))
	for _, entry := range result.Skipped {
		rows = append(rows, []string{entry.CommentPath, orDash(entry.YouTrackCommentID), "skipped", entry.Reason})
	}
	for _, entry := range result.Pushed {
		state := "queued"
		if waited {
			state = "pushed"
		}
		rows = append(rows, []string{entry.CommentPath, orDash(entry.YouTrackCommentID), state, ""})
	}
	for _, entry := range result.Failed {
		rows = append(rows, []string{entry.CommentPath, dash, "failed", entry.Error})
	}
	if err := render(p.Table([]string{"COMMENT", "REMOTE", "RESULT", "NOTE"}, rows)); err != nil {
		return err
	}
	if result.JobID != "" && !waited {
		p.Printf("Queued as %s. Run with --wait to follow it.\n", result.JobID)
	}
	return commentPushExit(p, result, waited)
}

// commentPushExit fails the command when a comment could not be pushed.
func commentPushExit(p *output.Printer, result corevault.YouTrackCommentPushResult, waited bool) error {
	if len(result.Failed) == 0 {
		return nil
	}
	p.Warnf("%s could not be pushed.\n", plural(len(result.Failed), "comment", "comments"))
	_ = waited
	return failf(exitFailure, "%s could not be pushed", plural(len(result.Failed), "comment", "comments"))
}

// -------------------------------------------------- the knowledge base ---

// youtrackKBFlags are the flags of `gintrack youtrack kb push|pull`.
type youtrackKBFlags struct {
	projectKey string
	recursive  bool
	wait       bool
	asJSON     bool
}

// newYouTrackKBCommand groups the knowledge-base synchronization.
func newYouTrackKBCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kb",
		Short: "Synchronize knowledge-base pages with YouTrack articles",
		Long: strings.TrimSpace(`
Publish knowledge-base pages as YouTrack articles, or write articles back into
pages.

Three rules govern what crosses the boundary, in both directions: the page title
lives only in the article summary, the "## Feedback" block never leaves the
repository, and a page both sides changed produces <page>.conflict.md rather
than a merge.`),
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usageError(cmd.Help())
		},
	}
	cmd.AddCommand(
		newYouTrackKBDirectionCommand(flags, "push"),
		newYouTrackKBDirectionCommand(flags, "pull"),
		newYouTrackKBStatusCommand(flags),
	)
	return cmd
}

// newYouTrackKBDirectionCommand builds `kb push` or `kb pull`; the two differ
// only in the core method they call and the words they print.
func newYouTrackKBDirectionCommand(flags *globalFlags, direction string) *cobra.Command {
	local := &youtrackKBFlags{}
	method, verb := "youtrack.kb.publish", "published"
	short := "Publish knowledge-base pages as YouTrack articles"
	if direction == "pull" {
		method, verb = "youtrack.kb.pull", "pulled"
		short = "Write YouTrack articles back into knowledge-base pages"
	}

	cmd := &cobra.Command{
		Use:   direction + " <path>",
		Short: short,
		Long: strings.TrimSpace(fmt.Sprintf(`
%s

A path naming a page selects that page; a path naming a folder selects its
direct pages, or every page below it with --recursive.

The work is a background job. Without --wait the command prints the job id and
the pages it selected; with --wait it follows the job and prints page, action
and article id, and exits non-zero when any page failed or conflicted — so a CI
step can fail on a divergence instead of reporting success over it.`, short)),
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runYouTrackKB(cmd, flags, local, method, verb, args[0])
		},
	}
	cmd.Flags().StringVar(&local.projectKey, "project-key", "", "git-in-track project key (default: the only one)")
	cmd.Flags().BoolVar(&local.recursive, "recursive", false, "include every page below a folder")
	cmd.Flags().BoolVar(&local.wait, "wait", false, "follow the queued job and print what it did")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runYouTrackKB queues a publish or a pull and, when asked, waits for it.
func runYouTrackKB(
	cmd *cobra.Command, flags *globalFlags, local *youtrackKBFlags, method, verb, path string,
) error {
	r, err := openRunner(cmd, flags, local.projectKey)
	if err != nil {
		return err
	}
	defer closeRunner(cmd, r)
	r.Start(cmd.Context())

	params := corevault.YouTrackKBParams{Project: r.project, Path: path, Recursive: local.recursive}
	var queued corevault.YouTrackKBJobResult
	if err := r.call(cmd.Context(), method, params, &queued); err != nil {
		return err
	}

	p := flags.printer(cmd, local.asJSON)
	if !local.wait {
		return printKBQueued(p, queued)
	}
	job, err := r.follow(cmd, queued.JobID)
	if err != nil {
		return err
	}
	// The pages moved on disk, so their state is read again rather than
	// inferred from the queueing answer. The remote side is not consulted: the
	// job has just been there, and a conflict it wrote is recorded locally.
	var status corevault.YouTrackKBStatusResult
	if err := r.call(cmd.Context(), "youtrack.kb.status", params, &status); err != nil {
		return err
	}
	return printKBResult(p, queued, status, job, verb)
}

// printKBQueued renders a job that was queued and not waited for.
func printKBQueued(p *output.Printer, queued corevault.YouTrackKBJobResult) error {
	if p.JSONMode() {
		return render(p.JSON(queued))
	}
	rows := make([][]string, 0, len(queued.Pages))
	for _, page := range queued.Pages {
		rows = append(rows, []string{page})
	}
	if err := render(p.Table([]string{"PAGE"}, rows)); err != nil {
		return err
	}
	p.Printf("Queued %s as %s. Run with --wait to follow it.\n",
		plural(len(queued.Pages), "page", "pages"), queued.JobID)
	return nil
}

// kbWaitPayload is what `kb push|pull --wait --json` prints. It is a declared
// shape rather than a dump of the job record: a CI step branches on `ok` and on
// the per-page `state`, and neither may move with an internal change.
type kbWaitPayload struct {
	Project string `json:"project"`
	JobID   string `json:"jobId"`
	// State is the engine's final state of the job: done, failed or cancelled.
	State string `json:"state"`
	// Error is why the job ended badly, empty when it did not.
	Error string `json:"error,omitempty"`
	// Pages is the state of every selected page after the job ran.
	Pages []corevault.YouTrackKBPageStatus `json:"pages"`
	// Conflicts counts the pages both sides changed. A conflict is not an
	// error: the page is untouched and `<page>.conflict.md` holds the other
	// side. It is still a non-zero exit, because nobody has reconciled them.
	Conflicts int `json:"conflicts"`
	// OK is false when the job failed or any page conflicted, which is exactly
	// when the command exits non-zero.
	OK bool `json:"ok"`
}

// printKBResult renders a job that was waited for.
func printKBResult(
	p *output.Printer, queued corevault.YouTrackKBJobResult,
	status corevault.YouTrackKBStatusResult, job syncengine.Job, verb string,
) error {
	payload := kbWaitPayload{
		Project: queued.Project, JobID: queued.JobID,
		State: string(job.State), Error: server.JobFailure(job), Pages: status.Pages,
	}
	for _, page := range status.Pages {
		if page.State == corevault.KBStateConflict {
			payload.Conflicts++
		}
	}
	payload.OK = payload.Error == "" && payload.Conflicts == 0

	if p.JSONMode() {
		if err := render(p.JSON(payload)); err != nil {
			return err
		}
		return kbExit(p, payload, verb)
	}
	rows := make([][]string, 0, len(status.Pages))
	for _, page := range status.Pages {
		rows = append(rows, []string{page.Path, page.State, orDash(page.ArticleID), orDash(page.Error)})
	}
	if err := render(p.Table([]string{"PAGE", "STATE", "ARTICLE", "NOTE"}, rows)); err != nil {
		return err
	}
	p.Printf("%s %s in %s.\n", plural(len(status.Pages), "page", "pages"), verb, payload.Project)
	return kbExit(p, payload, verb)
}

// kbExit fails the command when the job failed or a page conflicted, which is
// what lets a CI step rely on the exit code.
func kbExit(p *output.Printer, payload kbWaitPayload, verb string) error {
	switch {
	case payload.Error != "":
		p.Warnf("the job failed: %s\n", payload.Error)
		return failf(exitFailure, "nothing was %s: %s", verb, payload.Error)
	case payload.Conflicts > 0:
		p.Warnf("%s changed on both sides; <page>.conflict.md holds the other side.\n",
			plural(payload.Conflicts, "page", "pages"))
		return failf(exitConflict, "%s changed on both sides", plural(payload.Conflicts, "page", "pages"))
	default:
		return nil
	}
}

// newYouTrackKBStatusCommand reports what each page's synchronization state is.
func newYouTrackKBStatusCommand(flags *globalFlags) *cobra.Command {
	local := &youtrackKBFlags{}
	remote := false

	cmd := &cobra.Command{
		Use:   "status [path]",
		Short: "Report the synchronization state of knowledge-base pages",
		Long: strings.TrimSpace(`
Print, for each page, whether it is linked to an article and how the two stand:
unlinked, in_sync, local_ahead, remote_ahead or conflict.

Only the repository is read. Pass --remote to consult the tracker as well: it is
one article read per page, so it is opt-in rather than the default.`),
		Args: rangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			return runYouTrackKBStatus(cmd, flags, local, remote, path)
		},
	}
	cmd.Flags().StringVar(&local.projectKey, "project-key", "", "git-in-track project key (default: the only one)")
	cmd.Flags().BoolVar(&local.recursive, "recursive", false, "include every page below a folder")
	cmd.Flags().BoolVar(&remote, "remote", false, "read each linked article; one request per page")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// runYouTrackKBStatus dispatches the status call and prints the table.
func runYouTrackKBStatus(
	cmd *cobra.Command, flags *globalFlags, local *youtrackKBFlags, remote bool, path string,
) error {
	r, err := openRunner(cmd, flags, local.projectKey)
	if err != nil {
		return err
	}
	defer closeRunner(cmd, r)

	var status corevault.YouTrackKBStatusResult
	if err := r.call(cmd.Context(), "youtrack.kb.status", corevault.YouTrackKBParams{
		Project: r.project, Path: path, Recursive: local.recursive, Remote: remote,
	}, &status); err != nil {
		return err
	}

	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(status))
	}
	rows := make([][]string, 0, len(status.Pages))
	for _, page := range status.Pages {
		rows = append(rows, []string{page.Path, page.State, orDash(page.ArticleID), orDash(page.Error)})
	}
	if err := render(p.Table([]string{"PAGE", "STATE", "ARTICLE", "NOTE"}, rows)); err != nil {
		return err
	}
	if !status.Remote {
		p.Printf("The tracker was not consulted; pass --remote to read each article.\n")
	}
	return nil
}
