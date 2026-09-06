package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the write half of the team project list: "team.project.add" and
// "team.project.remove", the two methods that connect a project repository to a
// team and disconnect it again (docs/04 section 3.9).
//
// Both are team-scoped like every other team method: the team they act on
// travels in the params and is resolved by Workspace.TeamMount, so a workspace
// holding two teams never connects a project to the wrong one (R-TEAM-ACT-1).
// Reading the list is team.go; the decisions are all in internal/core.

// TeamProjectExistsCode is the machine code of an entry whose key the team
// already declares. The list is keyed by project key alone (R-PROJ-1).
const TeamProjectExistsCode = "team_project_exists"

// TeamProjectReferencedCode is the machine code of a removal that would leave a
// board, a sprint or a retro action pointing at a project the team no longer
// declares. It is advisory in the same sense a WIP limit is: the caller may
// repeat the call with `force`, but never by accident.
const TeamProjectReferencedCode = "team_project_referenced"

// TeamProjectAddParams is the input of "team.project.add".
type TeamProjectAddParams struct {
	// TeamScope names the team repository this call acts on.
	TeamScope
	// Project is the entry to add, exactly as team.yaml holds it.
	Project core.TeamProject `json:"project"`
}

// TeamProjectRemoveParams is the input of "team.project.remove".
type TeamProjectRemoveParams struct {
	// TeamScope names the team repository this call acts on.
	TeamScope
	// Key is the project to disconnect.
	Key string `json:"key"`
	// Force accepts a removal that leaves references pointing at an undeclared
	// project. Without it such a removal is refused and the references are
	// reported instead.
	Force bool `json:"force,omitempty"`
}

// TeamProjectResult is the answer of both methods: the team as "team.get"
// reports it, the entry that changed, the references the removal broke and the
// files the host must persist.
type TeamProjectResult struct {
	Team teamSummary `json:"team"`
	// Project is the entry that was added or removed.
	Project core.TeamProject `json:"project"`
	// References are the board, sprint and retro references into the removed
	// project. They are reported on a forced removal so that the user sees what
	// was accepted; an add never produces any.
	References []core.ProjectReference `json:"references,omitempty"`
	Writes     []RepoWriteSet          `json:"writes"`
}

// AddTeamProject connects a project repository to a team by declaring it in
// team.yaml. It refuses a duplicate key with TeamProjectExistsCode.
func (w *Workspace) AddTeamProject(ctx context.Context, p TeamProjectAddParams) (TeamProjectResult, error) {
	m, err := w.TeamMount(p.Team)
	if err != nil {
		return TeamProjectResult{}, err
	}
	var added core.TeamProject
	writes, err := m.Vault.updateTeamProjects(ctx, func(cfg *core.TeamConfig) error {
		var addErr error
		added, addErr = core.AddTeamProject(cfg, p.Project)
		if addErr != nil {
			return fmt.Errorf("add team project: %w", addErr)
		}
		return nil
	})
	if err != nil {
		return TeamProjectResult{}, classifyTeamProject(err)
	}
	return TeamProjectResult{
		Team:    w.teamSummary(m),
		Project: added,
		Writes:  []RepoWriteSet{teamWrites(m.ID, writes)},
	}, nil
}

// RemoveTeamProject disconnects a project from a team.
//
// A project a board, a sprint or a retro action still references is refused
// with TeamProjectReferencedCode and the list of references, because removing
// it would turn every one of them into a reference to an undeclared project —
// a warning on every board render and a card that can no longer be moved. The
// caller repeats the call with `force` to accept exactly that.
func (w *Workspace) RemoveTeamProject(
	ctx context.Context, p TeamProjectRemoveParams,
) (TeamProjectResult, error) {
	m, err := w.TeamMount(p.Team)
	if err != nil {
		return TeamProjectResult{}, err
	}
	key := core.ProjectKey(strings.TrimSpace(p.Key))
	if key == "" {
		return TeamProjectResult{}, failf("invalid_request", "name the project to remove with \"key\"")
	}
	references, err := m.Vault.teamProjectReferences(ctx, key)
	if err != nil {
		return TeamProjectResult{}, err
	}
	if len(references) > 0 && !p.Force {
		return TeamProjectResult{}, &Error{
			Code:    TeamProjectReferencedCode,
			Message: describeReferences(key, references),
		}
	}

	var removed core.TeamProject
	writes, err := m.Vault.updateTeamProjects(ctx, func(cfg *core.TeamConfig) error {
		var removeErr error
		removed, removeErr = core.RemoveTeamProject(cfg, key)
		if removeErr != nil {
			return fmt.Errorf("remove team project: %w", removeErr)
		}
		return nil
	})
	if err != nil {
		return TeamProjectResult{}, classifyTeamProject(err)
	}
	return TeamProjectResult{
		Team:       w.teamSummary(m),
		Project:    removed,
		References: references,
		Writes:     []RepoWriteSet{teamWrites(m.ID, writes)},
	}, nil
}

// updateTeamProjects rewrites team.yaml under the vault lock and re-indexes the
// repository, so that the answer is built from the file that was just written
// rather than from the request.
func (v *Vault) updateTeamProjects(
	ctx context.Context, mutate func(*core.TeamConfig) error,
) (WriteSet, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.team == nil {
		return WriteSet{}, failf("not_found", "this repository has no %s at its root", core.TeamFileName)
	}
	root := v.team.Root
	v.fs.begin()
	if err := core.UpdateTeamProjects(v.fs, root, mutate); err != nil {
		return WriteSet{}, fmt.Errorf("update %s: %w", core.TeamFileName, err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return WriteSet{}, err
	}
	if _, err := v.reload(ctx); err != nil {
		return WriteSet{}, err
	}
	return writes, nil
}

// teamProjectReferences collects every board, sprint and retro reference into a
// project. It reads the artifacts of the team repository and lets the core
// decide, so the browser and the companion refuse the same removals.
func (v *Vault) teamProjectReferences(
	ctx context.Context, key core.ProjectKey,
) ([]core.ProjectReference, error) {
	boards, _, err := v.Boards(ctx)
	if err != nil {
		return nil, err
	}
	sprints, _, err := v.Sprints(ctx)
	if err != nil {
		return nil, err
	}
	retros, _, err := v.Retros(ctx)
	if err != nil {
		return nil, err
	}
	return core.TeamProjectReferences(key, core.TeamArtifacts{
		Boards: boards, Sprints: sprints, Retros: retros,
	}), nil
}

// describeReferences renders the refusal message: how many artifacts point at
// the project and the first few of them, so the user reads what breaks instead
// of a count.
func describeReferences(key core.ProjectKey, refs []core.ProjectReference) string {
	const shown = 3
	var b strings.Builder
	fmt.Fprintf(&b, "%d reference(s) in %s still point at project %s: ",
		len(refs), countArtifacts(refs), key)
	for i, ref := range refs {
		if i == shown {
			fmt.Fprintf(&b, " and %d more", len(refs)-shown)
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %s (%s: %s)", ref.Kind, ref.ID, ref.Field, ref.Ref)
	}
	b.WriteString(". Removing the project leaves them pointing at a project this team " +
		"no longer declares; repeat with force to accept that.")
	return b.String()
}

// countArtifacts renders "1 board, 1 sprint and 1 retro" — how many distinct
// artifacts of each kind the references sit in. Naming the kinds matters more
// than naming every artifact: it is what tells a user whether a running sprint
// is about to lose a card or only an old board is.
func countArtifacts(refs []core.ProjectReference) string {
	counts := map[string]map[string]bool{}
	for _, ref := range refs {
		if counts[ref.Kind] == nil {
			counts[ref.Kind] = map[string]bool{}
		}
		counts[ref.Kind][ref.ID] = true
	}
	var parts []string
	for _, kind := range []string{"board", "sprint", "retro"} {
		n := len(counts[kind])
		if n == 0 {
			continue
		}
		plural := kind
		if n > 1 {
			plural += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, plural))
	}
	switch len(parts) {
	case 0:
		return "no artifact"
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// classifyTeamProject maps a core failure onto the stable code catalog the
// hosts switch on.
func classifyTeamProject(err error) error {
	var already *Error
	if errors.As(err, &already) {
		return err
	}
	switch {
	case errors.Is(err, core.ErrTeamProjectExists):
		return &Error{Code: TeamProjectExistsCode, Message: err.Error()}
	case errors.Is(err, core.ErrTeamProjectUnknown), errors.Is(err, core.ErrTeamMissing):
		return &Error{Code: "not_found", Message: err.Error()}
	case errors.Is(err, core.ErrTeamProjectFields):
		return &Error{Code: "validation_failed", Message: err.Error()}
	case errors.Is(err, core.ErrReadOnly):
		return &Error{Code: "read_only", Message: err.Error()}
	default:
		return fmt.Errorf("update team projects: %w", err)
	}
}
