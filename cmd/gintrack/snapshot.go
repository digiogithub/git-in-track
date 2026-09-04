package main

import (
	"context"
	"fmt"
	"path"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// snapshotFlags mirrors the flags of docs/07 section 4.13.
type snapshotFlags struct {
	team          string
	generatedBy   string
	includeClosed bool
	maxAgeDays    int
	dryRun        bool
	asJSON        bool
}

// snapshotRow is what happened to one project's snapshot.
type snapshotRow struct {
	Project string `json:"project"`
	Repo    string `json:"repo,omitempty"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Items   int    `json:"items"`
	Reason  string `json:"reason,omitempty"`
}

// snapshotPayload is what `gintrack snapshot --json` prints.
type snapshotPayload struct {
	Team      string        `json:"team"`
	TeamPath  string        `json:"teamPath"`
	DryRun    bool          `json:"dryRun,omitempty"`
	Snapshots []snapshotRow `json:"snapshots"`
	Written   int           `json:"written"`
	Unchanged int           `json:"unchanged"`
	Skipped   int           `json:"skipped"`
}

// The statuses a row reports. They are the strings the API uses too, so that a
// script reads one vocabulary whichever surface it drives.
const (
	snapshotWritten   = "written"
	snapshotUnchanged = "unchanged"
	snapshotSkipped   = "skipped"
)

// newSnapshotCommand writes the committed index snapshots of the projects this
// machine has cloned into the team repository.
func newSnapshotCommand(flags *globalFlags) *cobra.Command {
	local := &snapshotFlags{}

	cmd := &cobra.Command{
		Use:   "snapshot [KEY...]",
		Short: "Refresh the committed index snapshots of the team repository",
		Long: `Generate ` + "`.pmngr/index/<projectKey>.json`" + ` in the team repository for every
project this machine has cloned, so that team boards can render those projects'
cards for people who have not cloned them.

A snapshot carries front-matter-derived fields only — id, type, title, status,
priority, assignees, labels, milestone, estimate — never a body and never a
comment. Items are sorted by id and the file is rewritten only when its content
changed, so regenerating a snapshot that did not move leaves the git history
alone.

Pass one or more project keys to limit the run. Run it in the project
repository's own CI to keep a project few people clone visible on the board.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshot(cmd, flags, local, args)
		},
	}

	cmd.Flags().StringVar(&local.team, "team", "", "id of the registered team repository")
	cmd.Flags().StringVar(&local.generatedBy, "generated-by", "", "handle recorded in the file (default: the configured author)")
	cmd.Flags().BoolVar(&local.includeClosed, "include-closed", false, "keep closed items regardless of their age")
	cmd.Flags().IntVar(&local.maxAgeDays, "max-age-days", 0, "how long a closed item stays in the snapshot (default: 30)")
	cmd.Flags().BoolVar(&local.dryRun, "dry-run", false, "report what would change without writing anything")
	cmd.Flags().BoolVar(&local.asJSON, "json", false, "print machine-readable JSON")
	return cmd
}

// teamRepoView is the registered team repository, opened and parsed.
type teamRepoView struct {
	Repo config.Repo
	FS   *osfs.FS
	Ref  *core.TeamRef
}

// openTeamRepo finds and opens the team repository of the workspace. The id
// wins when given; otherwise the repository registered as a team is used, and a
// workspace with several of them is an error rather than a guess.
func openTeamRepo(res *config.Resolution, id string) (*teamRepoView, error) {
	repos := res.Config.WorkspaceRepos(res.Workspace)
	var candidates []config.Repo
	for _, repo := range repos {
		switch {
		case id != "":
			if repo.ID == id {
				candidates = append(candidates, repo)
			}
		case repo.Role == config.RoleTeam:
			candidates = append(candidates, repo)
		}
	}
	switch {
	case len(candidates) == 0 && id != "":
		return nil, notFoundf("no repository %q is registered", id)
	case len(candidates) == 0:
		return nil, notFoundf("no team repository is registered: run `gintrack add <path> --team`")
	case len(candidates) > 1:
		return nil, usagef("the workspace holds %d team repositories; pass --team <id>", len(candidates))
	}

	repo := candidates[0]
	fsys, err := osfs.New(repo.Path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", repo.ID, err)
	}
	ref, found, err := core.DiscoverTeam(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read the team repository %s: %w", repo.ID, err)
	}
	if !found || ref.Config == nil {
		return nil, notFoundf("repository %s holds no usable %s", repo.ID, core.TeamFileName)
	}
	return &teamRepoView{Repo: repo, FS: fsys, Ref: ref}, nil
}

// runSnapshot regenerates the snapshots and writes the ones that changed.
func runSnapshot(cmd *cobra.Command, flags *globalFlags, local *snapshotFlags, args []string) error {
	res, err := flags.resolve()
	if err != nil {
		return err
	}
	team, err := openTeamRepo(res, local.team)
	if err != nil {
		return err
	}
	wanted := map[core.ProjectKey]bool{}
	for _, raw := range args {
		key := core.ProjectKey(raw)
		if !core.ValidProjectKey(key) {
			return usagef("%q is not a project key", raw)
		}
		wanted[key] = true
	}
	for key := range wanted {
		if _, declared := team.Ref.Config.Project(key); !declared {
			return notFoundf("%s declares no project %s", core.TeamFileName, key)
		}
	}

	owners, err := snapshotSources(cmd.Context(), res)
	if err != nil {
		return err
	}
	payload := snapshotPayload{
		Team:     string(team.Ref.Key),
		TeamPath: team.Repo.Path,
		DryRun:   local.dryRun,
	}
	generatedBy := local.generatedBy
	if generatedBy == "" {
		generatedBy = res.Config.Git.AuthorName
	}

	for _, declaration := range team.Ref.Config.Projects {
		key := declaration.Key
		if len(wanted) > 0 && !wanted[key] {
			continue
		}
		row := snapshotRow{
			Project: string(key),
			Path:    core.ProjectSnapshotPath(team.Ref.TeamDirPath, key),
			Status:  snapshotSkipped,
		}
		owner, cloned := owners[key]
		if !cloned {
			row.Reason = "not cloned in this workspace"
			payload.Skipped++
			payload.Snapshots = append(payload.Snapshots, row)
			continue
		}
		row.Repo = owner.Repo.ID
		snap, err := owner.Index.ProjectSnapshot(key, core.ProjectSnapshotOptions{
			GeneratedBy:   generatedBy,
			Repo:          declaration.Repo,
			DefaultBranch: declaration.Branch(),
			IncludeClosed: local.includeClosed || team.Ref.Config.Snapshots.IncludeClosed,
			MaxAge:        snapshotMaxAge(local.maxAgeDays),
		})
		if err != nil {
			return fmt.Errorf("build the %s snapshot: %w", key, err)
		}
		row.Items = len(snap.Items)
		changed, err := writeSnapshotFile(team, key, snap, local.dryRun)
		if err != nil {
			return err
		}
		row.Status = snapshotUnchanged
		if changed {
			row.Status = snapshotWritten
			payload.Written++
		} else {
			payload.Unchanged++
		}
		payload.Snapshots = append(payload.Snapshots, row)
	}
	sort.SliceStable(payload.Snapshots, func(i, j int) bool {
		return payload.Snapshots[i].Project < payload.Snapshots[j].Project
	})

	return renderSnapshot(cmd, flags, local, payload)
}

// snapshotMaxAge turns the flag into the duration the core expects.
func snapshotMaxAge(days int) time.Duration {
	if days <= 0 {
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}

// writeSnapshotFile writes one snapshot into the team repository and reports
// whether anything changed. A file whose content is identical is left alone, so
// that a scheduled run does not churn the git history (ADR-014).
func writeSnapshotFile(team *teamRepoView, key core.ProjectKey, snap core.ProjectSnapshot, dry bool) (bool, error) {
	target := core.ProjectSnapshotPath(team.Ref.TeamDirPath, key)
	current, found, err := core.ReadProjectSnapshot(team.FS, team.Ref.TeamDirPath, key)
	if err == nil && found && core.SameSnapshotContent(*current, snap) {
		return false, nil
	}
	if dry {
		return true, nil
	}
	data, encodeErr := core.EncodeProjectSnapshot(snap)
	if encodeErr != nil {
		return false, fmt.Errorf("encode the %s snapshot: %w", key, encodeErr)
	}
	if mkErr := team.FS.MkdirAll(path.Dir(target)); mkErr != nil {
		return false, fmt.Errorf("create %s: %w", path.Dir(target), mkErr)
	}
	if writeErr := team.FS.WriteFile(target, data); writeErr != nil {
		return false, fmt.Errorf("write %s: %w", target, writeErr)
	}
	return true, nil
}

// snapshotSources opens every registered project repository and indexes it, so
// that the snapshot of a project is built from paths relative to its own
// repository root, exactly as R-SNAP-1 requires.
func snapshotSources(ctx context.Context, res *config.Resolution) (map[core.ProjectKey]*repoView, error) {
	out := map[core.ProjectKey]*repoView{}
	for _, repo := range res.Config.WorkspaceRepos(res.Workspace) {
		if repo.Role == config.RoleTeam {
			continue
		}
		view, err := openRepo(ctx, repo, false)
		if err != nil {
			return nil, err
		}
		for _, p := range view.Projects {
			if p.Team {
				continue
			}
			if _, taken := out[p.Key]; taken {
				continue
			}
			out[p.Key] = view
		}
	}
	return out, nil
}

// renderSnapshot prints the run, as a table or as JSON.
func renderSnapshot(cmd *cobra.Command, flags *globalFlags, local *snapshotFlags, payload snapshotPayload) error {
	p := flags.printer(cmd, local.asJSON)
	if p.JSONMode() {
		return render(p.JSON(payload))
	}
	for _, row := range payload.Snapshots {
		switch row.Status {
		case snapshotSkipped:
			p.Printf("%-8s %-28s skipped   (%s)\n", row.Project, dash, row.Reason)
		case snapshotUnchanged:
			p.Printf("%-8s %-28s unchanged (%s)\n", row.Project, row.Path, plural(row.Items, "item", "items"))
		default:
			p.Printf("%-8s %-28s %s (%s)\n", row.Project, row.Path,
				map[bool]string{true: "would write", false: "written    "}[local.dryRun],
				plural(row.Items, "item", "items"))
		}
	}
	p.Printf("%d written, %d unchanged, %d skipped\n", payload.Written, payload.Unchanged, payload.Skipped)
	if payload.Written > 0 && !local.dryRun {
		p.Printf("commit them with `git -C %s commit -m \"chore(pmngr): refresh index snapshots\"`\n", payload.TeamPath)
	}
	return nil
}
