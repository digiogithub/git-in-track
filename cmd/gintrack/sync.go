package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/cmd/gintrack/output"
	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// `gintrack sync`, docs/07-cli-and-api.md section 4.7, story GIT-US-0021.
//
// The command drives the same pipeline as the companion's POST /api/v1/sync/run
// (fetch, then rebase or merge, then push) directly over internal/gitops, so a
// terminal user needs no server running.

// syncFlags mirrors the documented flag set.
type syncFlags struct {
	repos      []string
	dryRun     bool
	noPush     bool
	strategy   string
	message    string
	commitAll  bool
	resume     bool
	abort      bool
	noSnapshot bool
	asJSON     bool
}

// syncRepoReport is one repository's line in `--json`.
type syncRepoReport struct {
	Repo   string             `json:"repo"`
	Path   string             `json:"path"`
	Result *gitops.SyncResult `json:"result,omitempty"`
	// Skipped says why a registered repository was not synced.
	Skipped string `json:"skipped,omitempty"`
}

// syncPayload is what `gintrack sync --json` prints.
type syncPayload struct {
	DryRun  bool             `json:"dryRun"`
	Repos   []syncRepoReport `json:"repos"`
	Pulled  int              `json:"pulled"`
	Pushed  int              `json:"pushed"`
	Failed  int              `json:"failed"`
	Skipped int              `json:"skipped"`
}

// newSyncCommand builds `gintrack sync`.
func newSyncCommand(flags *globalFlags) *cobra.Command {
	local := &syncFlags{}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Fetch, integrate and push every registered repository",
		Long: `Synchronize the registered repositories with their remotes: fetch, then rebase
(or merge, per git.pullStrategy) and then push.

Nothing here is destructive. A fetch that fails leaves the working tree
untouched, an integration that conflicts leaves a rebase you can continue or
abort, and a rejected push leaves your commits exactly where they are. Use
--dry-run first to see what would happen; it fetches, which is read-only, and
changes nothing else.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd, flags, local)
		},
	}

	f := cmd.Flags()
	f.StringArrayVar(&local.repos, "repo", nil, "limit to one repository (repeatable)")
	f.BoolVar(&local.dryRun, "dry-run", false, "show what would happen; nothing is changed")
	f.BoolVar(&local.noPush, "no-push", false, "fetch and integrate, do not push")
	f.StringVar(&local.strategy, "strategy", "", "rebase or merge (default: git.pullStrategy)")
	f.StringVar(&local.message, "message", "", "commit message for uncommitted changes, with --commit-all")
	f.BoolVar(&local.commitAll, "commit-all", false, "commit the working tree before integrating")
	f.BoolVar(&local.resume, "continue", false, "resume a rebase or merge after resolving its conflicts")
	f.BoolVar(&local.abort, "abort", false, "abort an in-progress rebase or merge")
	f.BoolVar(&local.noSnapshot, "no-snapshot", false, "do not refresh the team index snapshots afterwards")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	cmd.MarkFlagsMutuallyExclusive("continue", "abort")
	cmd.MarkFlagsMutuallyExclusive("dry-run", "commit-all")
	return cmd
}

// runSync resolves the repositories and drives the pipeline over each of them.
func runSync(cmd *cobra.Command, flags *globalFlags, local *syncFlags) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	strategy := gitops.Strategy(res.Config.Git.PullStrategy)
	if local.strategy != "" {
		strategy = gitops.Strategy(local.strategy)
	}
	if strategy == "" {
		strategy = gitops.StrategyRebase
	}
	if !strategy.Valid() {
		return usagef("unknown strategy %q: use rebase or merge", local.strategy)
	}

	repos, err := syncRepos(res, local.repos)
	if err != nil {
		return err
	}
	payload := syncPayload{DryRun: local.dryRun, Repos: make([]syncRepoReport, 0, len(repos))}
	printer := flags.printer(cmd, local.asJSON)

	var firstErr error
	for _, repo := range repos {
		report := syncRepoReport{Repo: repo.ID, Path: repo.Path}
		backend, openErr := gitops.Open(repo.Path, gitops.Options{
			Backend:     gitops.Kind(res.Config.Git.Backend),
			AuthorName:  res.Config.Git.AuthorName,
			AuthorEmail: res.Config.Git.AuthorEmail,
		})
		if openErr != nil {
			report.Skipped = openErr.Error()
			payload.Skipped++
			payload.Repos = append(payload.Repos, report)
			continue
		}
		result, runErr := syncOneRepo(cmd.Context(), backend, res.Config.Git, local, strategy, repo.ID)
		report.Result = &result
		payload.Repos = append(payload.Repos, report)
		payload.Pulled += result.Pulled
		payload.Pushed += result.Pushed
		if runErr != nil {
			payload.Failed++
			if firstErr == nil {
				firstErr = syncExit(runErr)
			}
		}
	}

	if err := renderSync(printer, payload); err != nil {
		return err
	}
	if firstErr == nil && !local.dryRun && payload.Pulled > 0 && !local.noSnapshot {
		// R-SNAP-6(a) of docs/04 section 6: a sync refreshes the committed index
		// snapshots of the projects this machine has cloned.
		refreshSnapshotsAfterSync(cmd, flags)
	}
	return firstErr
}

// syncOneRepo runs one repository through the requested mode: abort, continue
// or a full sync.
func syncOneRepo(
	ctx context.Context, backend gitops.Backend, git config.Git,
	local *syncFlags, strategy gitops.Strategy, id string,
) (gitops.SyncResult, error) {
	switch {
	case local.abort:
		return abortOne(ctx, backend, id)
	case local.resume:
		return continueOne(ctx, backend, id)
	}
	if local.commitAll {
		if err := commitWorkingTree(ctx, backend, git, local.message); err != nil {
			return gitops.SyncResult{
				Repo: id, Phase: gitops.PhaseFailed,
				Code: gitops.CodeOf(err), Message: err.Error(),
			}, err
		}
	}
	return gitops.Sync(ctx, backend, gitops.SyncOptions{ //nolint:wrapcheck // gitops errors already carry a code and an actionable message
		Repo:           id,
		Strategy:       strategy,
		Push:           git.PushOnSync && !local.noPush,
		DryRun:         local.dryRun,
		MaxPushRetries: git.MaxPushRetries,
	})
}

// abortOne undoes a half-finished rebase or merge.
func abortOne(ctx context.Context, backend gitops.Backend, id string) (gitops.SyncResult, error) {
	if err := backend.Abort(ctx); err != nil {
		return gitops.SyncResult{
			Repo: id, Phase: gitops.PhaseFailed, Code: gitops.CodeOf(err), Message: err.Error(),
		}, err //nolint:wrapcheck // gitops errors already carry a code and an actionable message
	}
	st, _ := backend.SyncStatus(ctx)
	return gitops.SyncResult{Repo: id, Phase: gitops.PhaseDone, After: st}, nil
}

// continueOne resumes a rebase or merge whose conflicts were resolved.
func continueOne(ctx context.Context, backend gitops.Backend, id string) (gitops.SyncResult, error) {
	res, err := backend.Continue(ctx)
	if err != nil {
		return gitops.SyncResult{
			Repo: id, Phase: phaseOf(err), Code: gitops.CodeOf(err),
			Message: err.Error(), Conflicts: res.Conflicts,
		}, err //nolint:wrapcheck // gitops errors already carry a code and an actionable message
	}
	st, _ := backend.SyncStatus(ctx)
	return gitops.SyncResult{Repo: id, Phase: gitops.PhaseDone, After: st}, nil
}

// phaseOf classifies a failure the way the pipeline does.
func phaseOf(err error) gitops.Phase {
	if gitops.CodeOf(err) == gitops.CodeConflict {
		return gitops.PhaseConflicts
	}
	return gitops.PhaseFailed
}

// commitWorkingTree is `--commit-all`: everything uncommitted becomes one
// commit before the integration runs, which is the `dirtyPolicy: commit` of
// docs/06 section 4.1 made explicit on the command line.
func commitWorkingTree(ctx context.Context, backend gitops.Backend, git config.Git, message string) error {
	st, err := backend.SyncStatus(ctx)
	if err != nil {
		return err //nolint:wrapcheck // gitops errors already carry a code and a message
	}
	if len(st.Dirty) == 0 {
		return nil
	}
	subject := message
	if subject == "" {
		tpl, tplErr := gitops.ParseTemplate(git.MessageTemplate)
		if tplErr != nil {
			tpl = gitops.MustParseTemplate(gitops.DefaultTemplate)
		}
		rendered, renderErr := tpl.Render(gitops.Fields{
			Action: gitops.ActionUpdate, Count: len(st.Dirty),
		})
		if renderErr != nil {
			return renderErr //nolint:wrapcheck // gitops errors already carry a code and a message
		}
		subject = rendered.Subject
	}
	_, err = backend.Commit(ctx, gitops.CommitRequest{
		Paths:   st.Dirty,
		Message: gitops.Message{Subject: subject},
		Sign:    git.SignCommits,
	})
	return err //nolint:wrapcheck // gitops errors already carry a code and a message
}

// syncRepos resolves which registered repositories the run covers.
func syncRepos(res *config.Resolution, wanted []string) ([]config.Repo, error) {
	all := res.Config.WorkspaceRepos(res.Workspace)
	if len(wanted) == 0 {
		if len(all) == 0 {
			return nil, notFoundf("no repository is registered: run `gintrack add <path>`")
		}
		return all, nil
	}
	out := make([]config.Repo, 0, len(wanted))
	for _, id := range wanted {
		found := false
		for _, repo := range all {
			if repo.ID == id {
				out, found = append(out, repo), true
				break
			}
		}
		if !found {
			return nil, notFoundf("no repository %q is registered in this workspace", id)
		}
	}
	return out, nil
}

// syncExit maps a pipeline failure onto the documented exit codes: a conflict
// is 5, every other git failure is 6.
func syncExit(err error) error {
	if gitops.CodeOf(err) == gitops.CodeConflict {
		return fail(exitConflict, err)
	}
	return fail(exitGit, err)
}

// refreshSnapshotsAfterSync regenerates the team index snapshots once incoming
// work has landed. A workspace with no team repository simply has none.
func refreshSnapshotsAfterSync(cmd *cobra.Command, flags *globalFlags) {
	err := runSnapshot(cmd, flags, &snapshotFlags{}, nil)
	if err == nil {
		return
	}
	var exit *exitError
	if errors.As(err, &exit) && exit.code == exitNotFound {
		return
	}
	printer := cmd.ErrOrStderr()
	if _, printErr := fmt.Fprintf(printer,
		"warning: could not refresh the index snapshots: %v\n", err); printErr != nil {
		return
	}
}

// renderSync prints the report.
func renderSync(printer *output.Printer, payload syncPayload) error {
	if printer.JSONMode() {
		return render(printer.JSON(payload))
	}
	for _, repo := range payload.Repos {
		printer.Printf("%s  %s\n", repo.Repo, repo.Path)
		if repo.Skipped != "" {
			printer.Printf("  skipped                   %s\n", repo.Skipped)
			continue
		}
		for _, line := range syncLines(*repo.Result, payload.DryRun) {
			printer.Printf("  %s\n", line)
		}
	}
	if payload.DryRun {
		printer.Line("nothing was changed (--dry-run)")
	}
	return nil
}

// syncLines renders one repository's result as the indented block of docs/07
// section 4.7.
func syncLines(res gitops.SyncResult, dryRun bool) []string {
	out := []string{}
	label := func(name, text string) {
		out = append(out, fmt.Sprintf("%-25s %s", name, text))
	}
	verb := "would "
	if !dryRun {
		verb = ""
	}
	switch {
	case res.Code != "":
		label(res.Code, res.Message)
		if len(res.Conflicts) > 0 {
			for _, c := range res.Conflicts {
				label("conflict", c.Path+" ("+c.Kind+")")
			}
		}
		return out
	case res.After.State == gitops.StateUpToDate && res.Pulled == 0 && res.Pushed == 0:
		return []string{"up to date"}
	}
	if n := len(res.Incoming); n > 0 {
		label("integrate", fmt.Sprintf("%s%s %s", verb, res.Strategy, plural(n, "incoming commit", "incoming commits")))
		for _, c := range res.Incoming {
			label("", "  "+shortSHA(c.SHA)+" "+c.Subject)
		}
	}
	if n := len(res.Outgoing); n > 0 {
		label("push", fmt.Sprintf("%s%s", verb, plural(n, "commit", "commits")))
	}
	if !dryRun {
		label("done", fmt.Sprintf("pulled %d, pushed %d", res.Pulled, res.Pushed))
	}
	for _, w := range res.Warnings {
		label("warning", w)
	}
	return out
}

// shortSHA abbreviates a hash the way git does.
func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return strings.ToLower(sha[:7])
}
