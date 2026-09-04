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
// the team repository among them if there is one, and the findings only a
// workspace can make.
type workspaceSummary struct {
	Vaults      []mountSummary    `json:"vaults"`
	Team        *teamSummary      `json:"team,omitempty"`
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
		summary, ok := w.Team()
		if !ok {
			return nil, failf("not_found", "no open repository holds a %s", core.TeamFileName)
		}
		return summary, nil
	case "ref.resolve":
		ref, err := decodeRefParams(raw)
		if err != nil {
			return nil, err
		}
		return w.ResolveRef(ref), nil
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
	out := workspaceSummary{Vaults: []mountSummary{}, Diagnostics: w.Diagnostics()}
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
	if summary, ok := w.Team(); ok {
		out.Team = &summary
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
	v := newVault(Options{Root: p.RootLabel, Now: w.clock(), Version: w.build()})
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
