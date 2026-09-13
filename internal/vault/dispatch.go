package vault

import (
	"context"
	"encoding/json"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the workspace router: the half of the CoreApi contract that has
// to decide which repository answers a call before the call is made.

// routeParams are the fields of any request that can name a repository. They are
// decoded leniently: a method whose params carry none of them falls through to
// the default repository, which is what keeps a single-repository session — the
// browser before a team repository is opened — working with no id at all.
type routeParams struct {
	VaultID string `json:"vaultId,omitempty"`
	Project string `json:"project,omitempty"`
	ID      string `json:"id,omitempty"`
	Ref     string `json:"ref,omitempty"`
}

// mountParams is the input of "workspace.mount".
type mountParams struct {
	VaultID   string `json:"vaultId"`
	Role      string `json:"role,omitempty"`
	RootLabel string `json:"rootLabel,omitempty"`
	// DocsFolders are the documentation folders the host declares for this
	// repository. Discovery probes the root and its first-level directories on
	// its own; a folder deeper than that is found only because it is listed
	// here (ADR-018).
	DocsFolders []string `json:"docsFolders,omitempty"`
}

// mountSummary is one repository as the contract reports it.
type mountSummary struct {
	ID       string     `json:"id"`
	Role     string     `json:"role"`
	Label    string     `json:"label"`
	Projects []string   `json:"projects"`
	Team     bool       `json:"team"`
	TeamKey  string     `json:"teamKey,omitempty"`
	Stats    IndexStats `json:"stats"`
}

// workspaceSummary is the answer of "workspace.list": every open repository,
// the team repositories among them, and the findings only a workspace can make.
//
// Teams holds every open team; Team repeats the first of them so that a client
// written against the single-team contract keeps working (GIT-US-0036).
type workspaceSummary struct {
	Vaults      []mountSummary    `json:"vaults"`
	Team        *teamSummary      `json:"team,omitempty"`
	Teams       []teamSummary     `json:"teams"`
	Diagnostics []core.Diagnostic `json:"diagnostics"`
}

// Call runs one CoreApi method against the workspace and returns the JSON
// envelope, never an error — the boundary with JavaScript has one shape.
func (w *Workspace) Call(method, params string) string {
	result, err := w.Dispatch(context.Background(), method, []byte(params))
	if err != nil {
		return failureEnvelope(err)
	}
	return successEnvelope(result)
}

// Dispatch routes one method. Workspace-wide methods are answered here; every
// other one is forwarded verbatim to the repository that owns it, so that a
// query has exactly one implementation whatever the host.
func (w *Workspace) Dispatch(ctx context.Context, method string, raw []byte) (any, error) {
	switch method {
	case "workspace.list":
		return w.list(), nil
	case "workspace.mount":
		return w.mount(raw)
	case "workspace.unmount":
		return w.unmount(raw)
	case "team.get":
		p, err := decodeParams[TeamScope](raw)
		if err != nil {
			return nil, err
		}
		return w.Team(p.Team)
	case "team.list":
		return w.TeamList(), nil
	case "team.project.add":
		p, err := decodeParams[TeamProjectAddParams](raw)
		if err != nil {
			return nil, err
		}
		return w.AddTeamProject(ctx, p)
	case "team.project.remove":
		p, err := decodeParams[TeamProjectRemoveParams](raw)
		if err != nil {
			return nil, err
		}
		return w.RemoveTeamProject(ctx, p)
	case "ref.resolve":
		ref, err := decodeRefParams(raw)
		if err != nil {
			return nil, err
		}
		return w.ResolveRef(ref), nil
	case "youtrack.import.preview", "youtrack.import.run":
		// The import writes into one project repository, named by "project" and
		// routed like any other project call, but it is answered here so that
		// its parameters are decoded once and both methods share one entry
		// point (GIT-US-0047).
		p, err := decodeParams[YouTrackImportParams](raw)
		if err != nil {
			return nil, err
		}
		target, err := w.route(method, raw)
		if err != nil {
			return nil, err
		}
		if method == "youtrack.import.preview" {
			return target.Vault.YouTrackImportPreview(ctx, p)
		}
		return target.Vault.YouTrackImportRun(ctx, p)
	case "youtrack.kb.status", "youtrack.kb.publish", "youtrack.kb.pull":
		// The knowledge-base methods address one project repository, named by
		// "project" and routed like any other project call, but their
		// parameters are decoded once here so that the three share one entry
		// point (GIT-US-0090).
		p, err := decodeParams[YouTrackKBParams](raw)
		if err != nil {
			return nil, err
		}
		target, err := w.route(method, raw)
		if err != nil {
			return nil, err
		}
		switch method {
		case "youtrack.kb.status":
			return target.Vault.YouTrackKBStatus(ctx, p)
		case "youtrack.kb.publish":
			return target.Vault.YouTrackKBPublish(ctx, p)
		default:
			return target.Vault.YouTrackKBPull(ctx, p)
		}
	case "youtrack.comment.push":
		// Routed by the project key inside the item id, which is what routeParams
		// reads from "id"; "itemId" is spelled out here because the comment push
		// names its item that way (GIT-US-0079).
		p, err := decodeParams[YouTrackCommentPushParams](raw)
		if err != nil {
			return nil, err
		}
		target, err := w.routeItem(method, raw, p.Project, core.ItemID(p.ItemID))
		if err != nil {
			return nil, err
		}
		return target.Vault.YouTrackCommentPush(ctx, p)
	case "item.references":
		p, err := decodeParams[ItemReferencesParams](raw)
		if err != nil {
			return nil, err
		}
		return w.ItemReferences(ctx, p)
	case "board.list":
		p, err := decodeParams[TeamScope](raw)
		if err != nil {
			return nil, err
		}
		return w.Boards(ctx, p.Team)
	case "board.get":
		p, err := decodeParams[BoardParams](raw)
		if err != nil {
			return nil, err
		}
		return w.BoardView(ctx, p.Team, p.Board)
	case "board.move":
		p, err := decodeParams[BoardMoveParams](raw)
		if err != nil {
			return nil, err
		}
		return w.MoveCard(ctx, p)
	case "board.update":
		p, err := decodeParams[BoardUpdateParams](raw)
		if err != nil {
			return nil, err
		}
		return w.UpdateBoard(ctx, p)
	case "board.create":
		p, err := decodeParams[BoardCreateParams](raw)
		if err != nil {
			return nil, err
		}
		return w.CreateBoard(ctx, p)
	case "board.delete":
		p, err := decodeParams[BoardDeleteParams](raw)
		if err != nil {
			return nil, err
		}
		return w.DeleteBoard(ctx, p)
	case "sprint.list":
		p, err := decodeParams[SprintListParams](raw)
		if err != nil {
			return nil, err
		}
		return w.Sprints(ctx, p)
	case "sprint.get":
		p, err := decodeParams[SprintParams](raw)
		if err != nil {
			return nil, err
		}
		return w.Sprint(ctx, p.Team, p.ID)
	case "sprint.create":
		p, err := decodeParams[SprintCreateParams](raw)
		if err != nil {
			return nil, err
		}
		return w.CreateSprint(ctx, p)
	case "sprint.update":
		p, err := decodeParams[SprintUpdateParams](raw)
		if err != nil {
			return nil, err
		}
		return w.UpdateSprint(ctx, p)
	case "sprint.start":
		p, err := decodeParams[SprintStartParams](raw)
		if err != nil {
			return nil, err
		}
		return w.StartSprint(ctx, p)
	case "sprint.metrics":
		p, err := decodeParams[SprintParams](raw)
		if err != nil {
			return nil, err
		}
		return w.SprintMetrics(ctx, p.Team, p.ID)
	case "sprint.close":
		p, err := decodeParams[SprintCloseParams](raw)
		if err != nil {
			return nil, err
		}
		return w.CloseSprint(ctx, p)
	case "sprint.transfer":
		p, err := decodeParams[SprintTransferParams](raw)
		if err != nil {
			return nil, err
		}
		return w.TransferSprintItems(ctx, p)
	case "retro.list":
		p, err := decodeParams[RetroListParams](raw)
		if err != nil {
			return nil, err
		}
		return w.Retros(ctx, p)
	case "retro.get":
		p, err := decodeParams[RetroParams](raw)
		if err != nil {
			return nil, err
		}
		return w.Retro(ctx, p.Team, p.ID)
	case "retro.create":
		p, err := decodeParams[RetroCreateParams](raw)
		if err != nil {
			return nil, err
		}
		return w.CreateRetro(ctx, p)
	case "retro.update":
		p, err := decodeParams[RetroUpdateParams](raw)
		if err != nil {
			return nil, err
		}
		return w.UpdateRetro(ctx, p)
	case "retro.promote":
		p, err := decodeParams[RetroPromoteParams](raw)
		if err != nil {
			return nil, err
		}
		return w.PromoteRetroAction(ctx, p)
	case "snapshot.list":
		p, err := decodeParams[TeamScope](raw)
		if err != nil {
			return nil, err
		}
		return w.SnapshotList(p.Team)
	case "snapshot.refresh":
		p, err := decodeParams[SnapshotRefreshParams](raw)
		if err != nil {
			return nil, err
		}
		return w.RefreshSnapshots(ctx, p)
	case "project.list":
		return w.Projects(ctx)
	case "search":
		p, err := decodeParams[struct {
			Q       string `json:"q"`
			Limit   int    `json:"limit,omitempty"`
			Project string `json:"project,omitempty"`
		}](raw)
		if err != nil {
			return nil, err
		}
		return w.Search(ctx, p.Q, p.Limit, p.Project)
	}

	target, err := w.route(method, raw)
	if err != nil {
		return nil, err
	}
	return target.Vault.Dispatch(ctx, method, raw)
}

// route picks the repository that answers a method.
//
// The order is deliberate: an explicit vaultId always wins, then the project a
// filter or a draft names, then the project key embedded in an item id, then
// the project half of a reference. A call that names nothing goes to the
// default repository, and a workspace holding exactly one repository sends
// everything there.
func (w *Workspace) route(method string, raw []byte) (*Mount, error) {
	var p routeParams
	if len(raw) > 0 && string(raw) != "null" {
		// A method whose params are not an object (there is none today, but the
		// contract allows one) simply routes by default.
		_ = json.Unmarshal(raw, &p)
	}

	if p.VaultID != "" {
		if m, ok := w.Lookup(p.VaultID); ok {
			return m, nil
		}
		if method == "vault.load" {
			// The browser loads a folder it has just picked: the repository is
			// created by the very call that fills it.
			return w.Attach(p.VaultID, RoleProject, newVault(Options{Now: w.clock(), Version: w.build()}))
		}
		return nil, failf("not_found", "no repository is mounted as %q", p.VaultID)
	}

	if p.Project != "" {
		if m, ok := w.MountForProject(core.ProjectKey(p.Project)); ok {
			return m, nil
		}
	}
	if p.ID != "" {
		if key, _, _, err := core.ParseItemID(p.ID); err == nil {
			if m, ok := w.MountForProject(key); ok {
				return m, nil
			}
		}
	}
	if p.Ref != "" {
		if ref, err := core.ParseRef(p.Ref); err == nil {
			if m, ok := w.MountForProject(ref.Project); ok {
				return m, nil
			}
		}
	}

	mounts := w.Mounts()
	if len(mounts) == 1 {
		return mounts[0], nil
	}
	if m, ok := w.Lookup(DefaultVaultID); ok {
		return m, nil
	}
	if len(mounts) == 0 {
		if method == "vault.load" {
			return w.Attach(DefaultVaultID, RoleProject, newVault(Options{Now: w.clock(), Version: w.build()}))
		}
		return nil, failf("not_found", "no repository is open")
	}
	return mounts[0], nil
}

// routeItem picks the repository that answers a method addressing one item by a
// field route does not read, such as the "itemId" of a comment push. An
// explicit project wins, then the project key inside the item id, then whatever
// route would have chosen on its own.
func (w *Workspace) routeItem(
	method string, raw []byte, project string, item core.ItemID,
) (*Mount, error) {
	if project != "" {
		if m, ok := w.MountForProject(core.ProjectKey(project)); ok {
			return m, nil
		}
	}
	if key, _, _, err := core.ParseItemID(string(item)); err == nil {
		if m, ok := w.MountForProject(key); ok {
			return m, nil
		}
	}
	return w.route(method, raw)
}

// clock returns the clock new vaults are built with.
func (w *Workspace) clock() func() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.now
}

// build returns the version string new vaults report.
func (w *Workspace) build() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.version
}

// list renders every open repository.
func (w *Workspace) list() workspaceSummary {
	out := workspaceSummary{Vaults: []mountSummary{}, Teams: []teamSummary{}, Diagnostics: w.Diagnostics()}
	for _, m := range w.Mounts() {
		entry := mountSummary{
			ID:       m.ID,
			Role:     m.Role,
			Label:    m.Vault.Root(),
			Projects: []string{},
			Stats:    m.Vault.Stats(),
		}
		for _, p := range m.Vault.Projects() {
			entry.Projects = append(entry.Projects, string(p.Key))
		}
		if team := m.Vault.Team(); team != nil {
			entry.Team = true
			entry.TeamKey = string(team.Key)
		}
		out.Vaults = append(out.Vaults, entry)
	}
	out.Teams = w.Teams()
	if len(out.Teams) > 0 {
		first := out.Teams[0]
		out.Team = &first
	}
	return out
}

// mount creates an empty in-memory repository the host then fills with
// "vault.load". It is the browser's way of opening a second folder.
func (w *Workspace) mount(raw []byte) (any, error) {
	p, err := decodeParams[mountParams](raw)
	if err != nil {
		return nil, err
	}
	if p.VaultID == "" {
		return nil, failf("invalid_request", "workspace.mount needs a vaultId")
	}
	v := newVault(Options{Root: p.RootLabel, DocsFolders: p.DocsFolders, Now: w.clock(), Version: w.build()})
	m, err := w.Attach(p.VaultID, p.Role, v)
	if err != nil {
		return nil, err
	}
	return mountSummary{
		ID: m.ID, Role: m.Role, Label: m.Vault.Root(),
		Projects: []string{}, Stats: m.Vault.Stats(),
	}, nil
}

// unmount drops a repository from the workspace. It never touches files.
func (w *Workspace) unmount(raw []byte) (any, error) {
	p, err := decodeParams[mountParams](raw)
	if err != nil {
		return nil, err
	}
	if !w.Detach(p.VaultID) {
		return nil, failf("not_found", "no repository is mounted as %q", p.VaultID)
	}
	return map[string]any{"unmounted": p.VaultID}, nil
}
