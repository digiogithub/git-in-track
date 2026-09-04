package vault

import (
	"context"
	"path"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the snapshot half of the team surface: reading the committed
// `.pmngr/index/<projectKey>.json` files of a team repository so that the cards
// of a project nobody cloned still carry a title, a status and an age
// (docs/04 sections 6 and 7, story GIT-US-0019).
//
// Nothing here decides anything: internal/core reads and grades the files, this
// only says which repository they live in and which projects to look for.

// SnapshotPolicy returns the snapshot policy of the team repository, or the
// zero policy when this vault is not one.
func (v *Vault) SnapshotPolicy() core.SnapshotPolicy {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.team == nil || v.team.Config == nil {
		return core.SnapshotPolicy{}
	}
	return v.team.Config.Snapshots
}

// Snapshots reads the committed index snapshots of the projects team.yaml
// declares. It answers an empty set — never nil — for a vault that holds no
// team repository, so that a board render needs no special case.
func (v *Vault) Snapshots() *core.SnapshotSet {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.snapshots()
}

// snapshots reads the snapshot set. The caller holds the lock.
func (v *Vault) snapshots() *core.SnapshotSet {
	if v.team == nil || v.team.Config == nil {
		return core.ReadSnapshots(nil, "", nil, core.SnapshotPolicy{}, v.now())
	}
	return core.ReadSnapshots(
		v.base,
		v.team.TeamDirPath,
		core.SnapshotKeys(v.team.Config),
		v.team.Config.Snapshots,
		v.now(),
	)
}

// TeamProjects returns the project declarations of team.yaml, which is what a
// link to a remote file is built from (docs/04 section 7.3).
func (v *Vault) TeamProjects() []core.TeamProject {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.team == nil || v.team.Config == nil {
		return nil
	}
	return append([]core.TeamProject(nil), v.team.Config.Projects...)
}

// WriteProjectSnapshot writes a committed snapshot into the team repository and
// reports what the host must save. It is a no-op — reported as written=false —
// when the file on disk already carries the same content, so that regenerating
// a snapshot that did not change never touches the git history (ADR-014).
func (v *Vault) WriteProjectSnapshot(ctx context.Context, snap core.ProjectSnapshot) (WriteSet, bool, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.team == nil {
		return WriteSet{}, false, failf("not_found", "this repository has no %s at its root", core.TeamFileName)
	}
	key := snap.Project.Key
	if key == "" {
		return WriteSet{}, false, failf("invalid_request", "a snapshot must name its project")
	}
	target := core.ProjectSnapshotPath(v.team.TeamDirPath, key)
	if current, found, err := core.ReadProjectSnapshot(v.base, v.team.TeamDirPath, key); err == nil && found {
		if core.SameSnapshotContent(*current, snap) {
			return WriteSet{}, false, nil
		}
	}
	data, err := core.EncodeProjectSnapshot(snap)
	if err != nil {
		return WriteSet{}, false, failf("internal", "encode the %s snapshot: %v", key, err)
	}
	v.fs.begin()
	if err := v.fs.MkdirAll(path.Dir(target)); err != nil {
		return WriteSet{}, false, failf("internal", "create %s: %v", path.Dir(target), err)
	}
	if err := v.fs.WriteFile(target, data); err != nil {
		return WriteSet{}, false, failf("internal", "write %s: %v", target, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return WriteSet{}, false, err
	}
	return writes, true, nil
}

// ProjectSnapshotOf builds the committed snapshot of a project this vault
// serves, from the index it already holds. Generating is a read of the clone;
// writing is a call on the team repository's vault.
func (v *Vault) ProjectSnapshotOf(
	key core.ProjectKey, opts core.ProjectSnapshotOptions,
) (core.ProjectSnapshot, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if opts.Now.IsZero() {
		opts.Now = v.now()
	}
	snap, err := v.index.ProjectSnapshot(key, opts)
	if err != nil {
		return core.ProjectSnapshot{}, failf("not_found", "%v", err)
	}
	return snap, nil
}

// ------------------------------------------------------------- workspace ---

// SnapshotRefreshParams is the input of "snapshot.refresh": which projects to
// regenerate, and who is asking.
type SnapshotRefreshParams struct {
	// Projects limits the run to these keys; empty means every declared
	// project a repository of the workspace serves.
	Projects []string `json:"projects,omitempty"`
	// GeneratedBy is the handle recorded in the file.
	GeneratedBy string `json:"generatedBy,omitempty"`
	// IncludeClosed overrides the team policy for this run.
	IncludeClosed *bool `json:"includeClosed,omitempty"`
	// DryRun generates the snapshots and reports what would change without
	// writing anything.
	DryRun bool `json:"dryRun,omitempty"`
}

// Snapshot statuses reported by "snapshot.refresh" and "snapshot.list".
const (
	// SnapshotWritten means the file was created or its content changed.
	SnapshotWritten = "written"
	// SnapshotUnchanged means the regenerated file is identical to the one on
	// disk, so nothing was written (ADR-014).
	SnapshotUnchanged = "unchanged"
	// SnapshotSkipped means no open repository serves the project.
	SnapshotSkipped = "skipped"
)

// SnapshotResult is what happened to one project's snapshot.
type SnapshotResult struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Items   int    `json:"items"`
	// Reason explains a skipped project in one sentence.
	Reason string `json:"reason,omitempty"`
	// Info is the state of the file after the run, as a board reads it.
	Info core.SnapshotInfo `json:"info"`
	// Snapshot is the generated document, returned by a dry run only.
	Snapshot *core.ProjectSnapshot `json:"snapshot,omitempty"`
}

// SnapshotRefreshResult is the answer of "snapshot.refresh".
type SnapshotRefreshResult struct {
	Snapshots []SnapshotResult `json:"snapshots"`
	Writes    []RepoWriteSet   `json:"writes"`
	DryRun    bool             `json:"dryRun,omitempty"`
}

// SnapshotList reports the committed snapshot of every project team.yaml
// declares, whether it exists, when it was generated and whether it is stale.
func (w *Workspace) SnapshotList() (SnapshotRefreshResult, error) {
	c, err := w.boardContext()
	if err != nil {
		return SnapshotRefreshResult{}, err
	}
	out := SnapshotRefreshResult{Snapshots: []SnapshotResult{}, Writes: []RepoWriteSet{}}
	for _, key := range c.declared {
		info := c.snapshots.Info(key)
		row := SnapshotResult{
			Project: string(key), Path: info.Path, Items: info.Items,
			Status: SnapshotUnchanged, Info: info,
		}
		if _, cloned := c.owners[key]; !cloned {
			row.Reason = "no open repository serves this project; its cards come from this file"
		}
		out.Snapshots = append(out.Snapshots, row)
	}
	return out, nil
}

// RefreshSnapshots regenerates the committed snapshot of every declared project
// an open repository serves, and writes the ones whose content changed into the
// team repository (R-SNAP-6). A project nobody cloned is skipped with a reason,
// never guessed at.
func (w *Workspace) RefreshSnapshots(
	ctx context.Context, p SnapshotRefreshParams,
) (SnapshotRefreshResult, error) {
	c, err := w.boardContext()
	if err != nil {
		return SnapshotRefreshResult{}, err
	}
	team := c.team.Vault.Team()
	if team == nil || team.Config == nil {
		return SnapshotRefreshResult{}, failf("not_found", "the team repository has no usable %s", core.TeamFileName)
	}
	wanted := map[core.ProjectKey]bool{}
	for _, raw := range p.Projects {
		wanted[core.ProjectKey(raw)] = true
	}
	policy := team.Config.Snapshots
	includeClosed := policy.IncludeClosed
	if p.IncludeClosed != nil {
		includeClosed = *p.IncludeClosed
	}

	out := SnapshotRefreshResult{Snapshots: []SnapshotResult{}, Writes: []RepoWriteSet{}, DryRun: p.DryRun}
	for _, key := range c.declared {
		if len(wanted) > 0 && !wanted[key] {
			continue
		}
		declaration, _ := team.Config.Project(key)
		row := SnapshotResult{
			Project: string(key),
			Path:    core.ProjectSnapshotPath(team.TeamDirPath, key),
			Status:  SnapshotSkipped,
		}
		owner, cloned := c.owners[key]
		if !cloned {
			row.Reason = "no open repository serves this project; clone it to refresh its snapshot"
			row.Info = c.snapshots.Info(key)
			out.Snapshots = append(out.Snapshots, row)
			continue
		}
		snap, err := owner.Vault.ProjectSnapshotOf(key, core.ProjectSnapshotOptions{
			GeneratedBy:   p.GeneratedBy,
			Repo:          declaration.Repo,
			DefaultBranch: declaration.Branch(),
			IncludeClosed: includeClosed,
			MaxAge:        policy.MaxAge(),
		})
		if err != nil {
			return SnapshotRefreshResult{}, err
		}
		row.Items = len(snap.Items)
		if p.DryRun {
			row.Status = SnapshotUnchanged
			if current, found, readErr := core.ReadProjectSnapshot(
				c.team.Vault.BaseFS(), team.TeamDirPath, key,
			); readErr != nil || !found || !core.SameSnapshotContent(*current, snap) {
				row.Status = SnapshotWritten
			}
			copied := snap
			row.Snapshot = &copied
			out.Snapshots = append(out.Snapshots, row)
			continue
		}
		writes, written, err := c.team.Vault.WriteProjectSnapshot(ctx, snap)
		if err != nil {
			return SnapshotRefreshResult{}, err
		}
		row.Status = SnapshotUnchanged
		if written {
			row.Status = SnapshotWritten
			out.Writes = append(out.Writes, RepoWriteSet{
				VaultID: c.team.ID, Written: writes.Written, Removed: writes.Removed,
			})
		}
		out.Snapshots = append(out.Snapshots, row)
	}
	// The set is re-read so that the answer describes the files as they are now.
	fresh := c.team.Vault.Snapshots()
	for i := range out.Snapshots {
		out.Snapshots[i].Info = fresh.Info(core.ProjectKey(out.Snapshots[i].Project))
	}
	return out, nil
}
