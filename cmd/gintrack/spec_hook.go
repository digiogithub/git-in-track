package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/gitops"
	"github.com/digiogithub/git-in-track/internal/trace"
)

// This file is `gintrack spec hook install|uninstall` (GIT-US-0134, docs/07
// section 4.21): a git hook that runs the requirement-impact gate of `spec
// impact --fail-on` before a push. Where the hooks live and how a managed hook
// file is written, recognized and removed is internal/gitops; this file only
// renders the script and reports.

// specHookMarker is the line every script this command writes starts with,
// right after the shebang. Install rewrites a hook carrying it and refuses any
// other; uninstall removes a hook carrying it and nothing else.
const specHookMarker = "# installed by gintrack spec hook"

// specHookNames are the hooks the command can install. pre-push is the one the
// gate is meant for: it sees the commits being pushed. A pre-commit gate would
// run against an uncommitted tree, where every touched requirement is suspect
// (docs/09 section 2), so it is not offered.
var specHookNames = []string{"pre-push"}

// specHookFlags are the flags of both subcommands.
type specHookFlags struct {
	hook    string
	repo    string
	project string
	force   bool
	dryRun  bool
	asJSON  bool
}

// specHookPayload is what `gintrack spec hook install|uninstall --json` prints.
type specHookPayload struct {
	Repo    string       `json:"repo"`
	VCS     core.VCSInfo `json:"vcs"`
	Hook    string       `json:"hook"`
	Backend string       `json:"backend,omitempty"`
	Dir     string       `json:"dir,omitempty"`
	// Result is what happened to the hook file; absent for a jj repository,
	// where nothing is written.
	Result *gitops.HookResult `json:"result,omitempty"`
	// Skipped explains why nothing was written, and Equivalent is the jj
	// configuration that does the same job.
	Skipped    string `json:"skipped,omitempty"`
	Equivalent string `json:"equivalent,omitempty"`
}

func newSpecHookCommand(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Install or remove a pre-push hook running the requirement-impact gate",
		Args:  noArgs,
	}
	cmd.AddCommand(newSpecHookInstallCommand(flags), newSpecHookUninstallCommand(flags))
	return cmd
}

func newSpecHookInstallCommand(flags *globalFlags) *cobra.Command {
	local := &specHookFlags{}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the pre-push hook of the requirement-impact gate",
		Long: `Write a git hook that runs, for every branch being pushed,

  gintrack spec impact --since <upstream> --head <pushed commit> --tiers 1,2 --fail-on failing,suspect

and refuses the push when it exits non-zero: 7 when a requirement the pushed
commits touch is failing or suspect, anything else when the gate could not
run. <upstream> is the branch's upstream (@{upstream}) and origin/main when it
has none; GINTRACK_HOOK_SINCE overrides it. The hook runs no tests: it reads the
results already recorded with "gintrack spec ingest", so run "make spec-check"
or your suites and "spec ingest" before pushing. "git push --no-verify" skips
it once.

The hook goes where git runs hooks from ("git rev-parse --git-path hooks"),
which honors core.hooksPath. A hook this command wrote is recognized by its
marker line and rewritten; any other hook of the same name is left alone
unless --force, which first moves it to <hook>.gintrack-backup (uninstall
moves it back). In a Jujutsu repository nothing is written: "jj git push"
runs no git hooks, and the command prints the jj alias that does the same job.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSpecHook(cmd, flags, local, true)
		},
	}
	addSpecHookFlags(cmd, local)
	cmd.Flags().StringVar(&local.project, "project", "", "project key the hook passes to spec impact (needed with several projects)")
	cmd.Flags().BoolVar(&local.force, "force", false, "back up and replace a hook gintrack did not install")
	return cmd
}

func newSpecHookUninstallCommand(flags *globalFlags) *cobra.Command {
	local := &specHookFlags{}
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the pre-push hook this command installed",
		Long: `Remove the hook "gintrack spec hook install" wrote, and move back the hook a
forced install backed up. A hook gintrack did not write is never removed.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSpecHook(cmd, flags, local, false)
		},
	}
	addSpecHookFlags(cmd, local)
	return cmd
}

// addSpecHookFlags registers the flags both subcommands share.
func addSpecHookFlags(cmd *cobra.Command, local *specHookFlags) {
	f := cmd.Flags()
	f.StringVar(&local.hook, "hook", "pre-push", "hook to manage: "+strings.Join(specHookNames, ", "))
	f.StringVar(&local.repo, "repo", ".", "repository whose hook to manage (its working-tree root is found upwards)")
	f.BoolVar(&local.dryRun, "dry-run", false, "report what would change, write nothing")
	f.BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
}

func runSpecHook(cmd *cobra.Command, flags *globalFlags, local *specHookFlags, install bool) error {
	if !validSpecHook(local.hook) {
		return usagef("unknown --hook %q: use %s", local.hook, strings.Join(specHookNames, ", "))
	}
	if strings.ContainsAny(local.project, " \t\n'\"\\$`") {
		return usagef("--project %q is not a project key", local.project)
	}
	root, err := trace.FindRoot(local.repo)
	if err != nil {
		return usageError(err)
	}
	payload := specHookPayload{Repo: root, VCS: gitops.DetectVCS(root), Hook: local.hook}
	p := flags.printer(cmd, local.asJSON)

	switch {
	case payload.VCS.IsJujutsu():
		payload.Skipped = "jj git push runs no git hooks; nothing was written"
		payload.Equivalent = jjGateAlias(local.project)
		if p.JSONMode() {
			return render(p.JSON(payload))
		}
		p.Printf("%s is %s: \"jj git push\" runs no git hooks, so no hook was written.\n", root, payload.VCS.Summary())
		if install {
			p.Printf("The equivalent is a jj alias that runs the gate before pushing; add it with \"jj config edit --repo\":\n\n%s\nthen push with \"jj push-gated\" (it takes the arguments of \"jj git push\").\n", payload.Equivalent)
		}
		return nil
	case payload.VCS.Kind != core.VCSGit:
		return failf(exitGit, "%s is not inside a git working tree", root)
	}

	kind := gitops.KindAuto
	if res, err := flags.resolve(); err == nil && res.Config.Git.Backend != "" && res.Config.Git.Backend != config.BackendAuto {
		kind = gitops.Kind(res.Config.Git.Backend)
	}
	payload.Backend, _ = gitops.Resolve(kind, "")
	dir, err := gitops.HooksDir(cmd.Context(), root, gitops.Options{Backend: kind})
	if err != nil {
		return fail(exitGit, err)
	}
	payload.Dir = dir

	req := gitops.HookRequest{
		Dir: dir, Name: local.hook, Marker: specHookMarker,
		Script: specHookScript(local.hook, local.project), Force: local.force, DryRun: local.dryRun,
	}
	var result gitops.HookResult
	if install {
		result, err = gitops.InstallHook(req)
	} else {
		result, err = gitops.UninstallHook(req)
	}
	switch {
	case errors.Is(err, gitops.ErrForeignHook) && install:
		return failf(exitConflict, "%s exists and was not installed by gintrack; rerun with --force to back it up and replace it", result.Path)
	case errors.Is(err, gitops.ErrForeignHook):
		return failf(exitConflict, "%s was not installed by gintrack; it was left in place", result.Path)
	case errors.Is(err, gitops.ErrHookBackupExists):
		return failf(exitConflict, "%s is in the way of backing up %s; move it first", result.Backup, result.Path)
	case err != nil:
		return fmt.Errorf("manage the %s hook: %w", local.hook, err)
	}
	payload.Result = &result
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	p.Printf("%s\n", describeHookResult(result))
	return nil
}

// validSpecHook reports whether the command manages this hook.
func validSpecHook(name string) bool {
	for _, n := range specHookNames {
		if n == name {
			return true
		}
	}
	return false
}

// describeHookResult is the one line a hook install or uninstall prints.
func describeHookResult(r gitops.HookResult) string {
	would := ""
	if r.DryRun {
		would = "would be "
	}
	switch r.Action {
	case gitops.HookCreated:
		return fmt.Sprintf("%s %sinstalled", r.Path, would)
	case gitops.HookUpdated:
		return fmt.Sprintf("%s %supdated", r.Path, would)
	case gitops.HookUnchanged:
		return fmt.Sprintf("%s is already installed and up to date", r.Path)
	case gitops.HookReplaced:
		return fmt.Sprintf("%s %sinstalled; the previous hook %smoved to %s", r.Path, would, would, r.Backup)
	case gitops.HookRemoved:
		return fmt.Sprintf("%s %sremoved", r.Path, would)
	case gitops.HookRestored:
		return fmt.Sprintf("%s %sremoved; the previous hook %srestored from %s", r.Path, would, would, r.Backup)
	case gitops.HookAbsent:
		return fmt.Sprintf("%s: no gintrack hook is installed", r.Path)
	}
	return r.Path
}

// specImpactGateArgs are the arguments the hook and the jj alias pass to spec
// impact after --since: the tiers and states `make spec-check` gates on in CI
// (docs/09 section 2).
func specImpactGateArgs(project string) string {
	args := "--tiers 1,2 --fail-on failing,suspect"
	if project != "" {
		args += " --project " + project
	}
	return args
}

// jjGateAlias is the jj configuration that runs the gate before `jj git push`.
func jjGateAlias(project string) string {
	return `[aliases]
push-gated = ["util", "exec", "--", "sh", "-c", """
gintrack spec impact --since "${GINTRACK_HOOK_SINCE:-origin/main}" ` + specImpactGateArgs(project) + ` && jj git push "$@"
""", "jj-push-gated"]
`
}

// specHookScript renders the POSIX sh hook. It is regenerated on every install
// and compared byte for byte, so it holds nothing machine- or time-specific.
func specHookScript(hook, project string) string {
	return `#!/bin/sh
` + specHookMarker + ` (` + hook + `); remove it with "gintrack spec hook uninstall".
# Reinstalling overwrites this file: do not edit it.
#
# The requirement-impact gate (docs/07 section 4.21, docs/09 section 2): for every
# branch being pushed it runs
#
#   gintrack spec impact --since <upstream> --head <pushed commit> ` + specImpactGateArgs(project) + `
#
# and refuses the push when that exits non-zero (7: a requirement is failing or
# suspect; anything else: the gate could not run). <upstream> is the branch's
# @{upstream}, else origin/main; GINTRACK_HOOK_SINCE overrides it, and GINTRACK
# names the gintrack binary. No tests run here: the gate reads the results
# recorded with "gintrack spec ingest", so run "make spec-check" (or your suites
# and "gintrack spec ingest") first. "git push --no-verify" skips the gate once.

gintrack=${GINTRACK:-gintrack}
if ! command -v "$gintrack" >/dev/null 2>&1; then
	echo "gintrack spec hook: $gintrack is not on PATH; set GINTRACK, or push with --no-verify" >&2
	exit 1
fi

status=0
while read -r local_ref local_sha remote_ref remote_sha; do
	# A deleted ref pushes no commits.
	case "$local_sha" in *[!0]*) ;; *) continue ;; esac
	since=${GINTRACK_HOOK_SINCE:-}
	if [ -z "$since" ]; then
		since=$(git rev-parse --abbrev-ref "${local_ref#refs/heads/}@{upstream}" 2>/dev/null) || since=origin/main
	fi
	echo "gintrack spec hook: impact $since..$local_ref (to $remote_ref)" >&2
	"$gintrack" spec impact --since "$since" --head "$local_sha" ` + specImpactGateArgs(project) + ` || status=$?
done

if [ "$status" -ne 0 ]; then
	echo "gintrack spec hook: push refused (exit $status). Fix the requirements above, or rerun the tests and \"gintrack spec ingest\" at this commit; \"git push --no-verify\" skips the gate once." >&2
fi
exit "$status"
`
}
