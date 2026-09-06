package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// A workspace is several repositories open at once: the project clones the user
// has and, optionally, the team repository that declares them (docs/04 section
// 1). It is the second half of the CoreApi contract, and it is implemented once
// for both hosts:
//
//   - the browser mounts one in-memory vault per picked folder and pushes files
//     into each with "vault.load";
//   - the companion attaches the vault it already opened over every registered
//     repository.
//
// Everything cross-repository lives here — reference resolution, unified search
// and the team surface — while everything about one repository is routed to the
// vault that owns it, unchanged. There is no second implementation of a query.

// RoleProject and RoleTeam are the two roles a mounted repository can have.
const (
	RoleProject = "project"
	RoleTeam    = "team"
)

// DefaultVaultID is the repository a call that names none is routed to. It
// exists so that a host with a single repository — every browser session before
// GIT-US-0016, and every test written against a lone vault — keeps working
// without passing an id.
const DefaultVaultID = "default"

// Mount is one repository of a workspace.
type Mount struct {
	// ID is the stable handle a request addresses the repository by.
	ID string `json:"id"`
	// Role is "project" or "team". It is what the user declared; the team
	// surface itself follows team.yaml, not this field.
	Role string `json:"role"`
	// Label is the human-readable root: a directory handle name in the browser,
	// the repository folder in the companion.
	Label string `json:"label"`
	// Vault is the repository's own index and store.
	Vault *Vault `json:"-"`
}

// Workspace holds several vaults and routes the contract across them. It is
// safe for concurrent use, and it is free of os and syscall/js: the file
// systems are always injected by the host.
type Workspace struct {
	mu      sync.RWMutex
	order   []string
	mounts  map[string]*Mount
	now     func() time.Time
	version string
	// history reads the past of a repository's files, for the metrics of
	// GIT-US-0028. It is nil in the browser, where there is no git to walk.
	history HistorySource
}

// NewWorkspace returns an empty workspace.
func NewWorkspace() *Workspace {
	return &Workspace{mounts: map[string]*Mount{}}
}

// SetVersion sets the build string every vault mounted afterwards reports.
func (w *Workspace) SetVersion(build string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.version = build
	for _, m := range w.mounts {
		m.Vault.SetVersion(build)
	}
}

// SetClock replaces the clock of every mounted vault and of the ones mounted
// afterwards. It exists for tests and for reproducible fixtures.
func (w *Workspace) SetClock(now func() time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.now = now
	for _, m := range w.mounts {
		m.Vault.SetClock(now)
	}
}

// Attach registers a vault the host already opened. Re-attaching an id replaces
// what was there, which is what a re-mount of the same folder is.
func (w *Workspace) Attach(id, role string, v *Vault) (*Mount, error) {
	if v == nil {
		return nil, failf("invalid_request", "attach %q: no vault", id)
	}
	if id == "" {
		id = DefaultVaultID
	}
	if role != RoleTeam {
		role = RoleProject
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	m := &Mount{ID: id, Role: role, Label: v.Root(), Vault: v}
	if _, exists := w.mounts[id]; !exists {
		w.order = append(w.order, id)
	}
	w.mounts[id] = m
	return m, nil
}

// Detach drops a repository. It reports false when nothing was mounted there.
func (w *Workspace) Detach(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.mounts[id]; !ok {
		return false
	}
	delete(w.mounts, id)
	for i, existing := range w.order {
		if existing == id {
			w.order = append(w.order[:i], w.order[i+1:]...)
			break
		}
	}
	return true
}

// Mounts returns every repository in mount order.
func (w *Workspace) Mounts() []*Mount {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshot()
}

// snapshot returns the mounts in order. The caller holds at least the read lock.
func (w *Workspace) snapshot() []*Mount {
	out := make([]*Mount, 0, len(w.order))
	for _, id := range w.order {
		if m, ok := w.mounts[id]; ok {
			out = append(out, m)
		}
	}
	return out
}

// Lookup returns the repository registered under id.
func (w *Workspace) Lookup(id string) (*Mount, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	m, ok := w.mounts[id]
	return m, ok
}

// TeamScope names the team repository a call acts on. It is embedded in the
// params of every team-scoped method, so that the choice travels with the
// request instead of living as session state on the host: a companion serving
// two browser tabs and a browser-only tab serving itself then behave
// identically (ADR-019, GIT-US-0036).
//
// Team accepts either the `key:` of a team.yaml or the id of the repository
// that holds it. It is optional while a workspace holds a single team.
type TeamScope struct {
	Team string `json:"team,omitempty"`
}

// TeamMounts returns every repository holding a team.yaml, in mount order.
func (w *Workspace) TeamMounts() []*Mount {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.teamMounts()
}

func (w *Workspace) teamMounts() []*Mount {
	var out []*Mount
	for _, m := range w.snapshot() {
		if m.Vault.Team() != nil {
			out = append(out, m)
		}
	}
	return out
}

// TeamMount resolves the team repository a call acts on. An empty key selects
// the only team of the workspace; naming one becomes mandatory as soon as two
// are open, so that a call is never answered by an arbitrary team (R-TEAM-ACT-1).
func (w *Workspace) TeamMount(key string) (*Mount, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.teamMount(key)
}

func (w *Workspace) teamMount(key string) (*Mount, error) {
	teams := w.teamMounts()
	if len(teams) == 0 {
		return nil, failf("not_found", "no open repository holds a %s", core.TeamFileName)
	}
	if key == "" {
		if len(teams) == 1 {
			return teams[0], nil
		}
		return nil, failf("invalid_request",
			"this workspace holds %d team repositories; name the one to act on with \"team\"",
			len(teams))
	}
	for _, m := range teams {
		if m.ID == key {
			return m, nil
		}
		if team := m.Vault.Team(); team != nil && string(team.Key) == key {
			return m, nil
		}
	}
	return nil, failf("not_found", "no open team repository is called %q", key)
}

// anyTeamMount returns a team repository without asking which one. It backs the
// answers that are about the workspace rather than about one team — reference
// resolution falling back when no team declares the project, and the legacy
// single-team field of "workspace.list".
func (w *Workspace) anyTeamMount() (*Mount, bool) {
	teams := w.teamMounts()
	if len(teams) == 0 {
		return nil, false
	}
	return teams[0], true
}

// MountForProject returns the repository exposing a project key.
func (w *Workspace) MountForProject(key core.ProjectKey) (*Mount, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.mountForProject(key)
}

func (w *Workspace) mountForProject(key core.ProjectKey) (*Mount, bool) {
	if key == "" {
		return nil, false
	}
	for _, m := range w.snapshot() {
		for _, p := range m.Vault.Projects() {
			if p.Key == key {
				return m, true
			}
		}
	}
	// A team key addresses the team knowledge base, which is a scope of the
	// team repository rather than a project.
	for _, m := range w.snapshot() {
		if team := m.Vault.Team(); team != nil && string(team.Key) == string(key) {
			return m, true
		}
	}
	return nil, false
}

// ResolveRef resolves `<projectKey>/<itemId>` across every open repository. It
// never fails on a reference that simply points at a project nobody cloned:
// that is the normal state of a team board, and the answer says so.
func (w *Workspace) ResolveRef(ref core.Ref) refResolution {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.resolveRef(ref)
}

func (w *Workspace) resolveRef(ref core.Ref) refResolution {
	// A reference belongs to the team that declares the project, whichever of
	// the open teams that is; the first team answers for a project none of them
	// declares, so that the reason and the remote link stay as good as they were
	// when a workspace held a single team.
	var team *core.TeamRef
	var snapshots *core.SnapshotSet
	if m, ok := w.teamMountForProject(ref.Project); ok {
		team = m.Vault.Team()
		snapshots = m.Vault.Snapshots()
	}
	if m, ok := w.mountForProject(ref.Project); ok {
		return m.Vault.ResolveRef(ref, m.ID, team, snapshots)
	}
	out := refResolution{
		Ref:      ref.String(),
		Project:  string(ref.Project),
		Item:     string(ref.Item),
		Reason:   fmt.Sprintf("project %s is not cloned on this machine", ref.Project),
		Declared: false,
	}
	if team != nil && team.Config != nil {
		_, out.Declared = team.Config.Project(ref.Project)
	}
	describeRemoteRef(&out, ref, team, snapshots)
	return out
}

// teamMountForProject returns the team repository whose team.yaml declares a
// project key, falling back to the first open team when none does.
func (w *Workspace) teamMountForProject(key core.ProjectKey) (*Mount, bool) {
	for _, m := range w.teamMounts() {
		team := m.Vault.Team()
		if team == nil || team.Config == nil {
			continue
		}
		if _, declared := team.Config.Project(key); declared {
			return m, true
		}
	}
	return w.anyTeamMount()
}

// Team renders one team repository of the workspace, with every declared
// project marked cloned or not against the repositories that are open. An
// empty key selects the only open team; see TeamMount for the rule.
func (w *Workspace) Team(key string) (teamSummary, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	m, err := w.teamMount(key)
	if err != nil {
		return teamSummary{}, err
	}
	return w.teamSummary(m), nil
}

// TeamListResult is the answer of "team.list": every open team repository, in
// mount order. It is a list even when it holds one entry, because a client has
// to be able to tell "no team is open" from "one team is open".
type TeamListResult struct {
	Teams []teamSummary `json:"teams"`
	Total int           `json:"total"`
}

// Teams renders every open team repository, in mount order.
func (w *Workspace) Teams() []teamSummary {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := []teamSummary{}
	for _, m := range w.teamMounts() {
		out = append(out, w.teamSummary(m))
	}
	return out
}

// TeamList renders every open team repository as "team.list" answers it.
func (w *Workspace) TeamList() TeamListResult {
	teams := w.Teams()
	return TeamListResult{Teams: teams, Total: len(teams)}
}

// teamSummary renders one team mount. The caller holds at least the read lock.
func (w *Workspace) teamSummary(m *Mount) teamSummary {
	mounts := w.snapshot()
	lookup := func(key core.ProjectKey) (string, core.ProjectRef, bool) {
		for _, candidate := range mounts {
			for _, p := range candidate.Vault.Projects() {
				if p.Key == key {
					return candidate.ID, p, true
				}
			}
		}
		return "", core.ProjectRef{}, false
	}
	return teamSummaryOf(m.Vault.Team(), m.ID, m.Vault.Snapshots(), lookup)
}

// Search ranks results across every open repository, keeping the source of each
// hit. Ranking is per vault and the merge is by score, which is exact because
// the scoring of docs/02 section 8 does not depend on corpus statistics.
func (w *Workspace) Search(ctx context.Context, q string, limit int, project string) ([]searchHit, error) {
	w.mu.RLock()
	mounts := w.snapshot()
	w.mu.RUnlock()

	params, err := json.Marshal(map[string]any{"q": q, "limit": limit, "project": project})
	if err != nil {
		return nil, failf("internal", "encode search params: %v", err)
	}
	var all []searchHit
	for _, m := range mounts {
		if project != "" {
			if owner, ok := w.MountForProject(core.ProjectKey(project)); ok && owner.ID != m.ID {
				continue
			}
		}
		result, err := m.Vault.Dispatch(ctx, "search", params)
		if err != nil {
			return nil, err
		}
		hits, ok := result.([]searchHit)
		if !ok {
			continue
		}
		for _, h := range hits {
			h.VaultID = m.ID
			all = append(all, h)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		if all[i].Project != all[j].Project {
			return all[i].Project < all[j].Project
		}
		return all[i].Path < all[j].Path
	})
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	if all == nil {
		all = []searchHit{}
	}
	return all, nil
}

// Projects returns every project of every open repository, each carrying the id
// of the repository it was found in.
func (w *Workspace) Projects(ctx context.Context) ([]projectSummary, error) {
	w.mu.RLock()
	mounts := w.snapshot()
	w.mu.RUnlock()

	out := []projectSummary{}
	for _, m := range mounts {
		result, err := m.Vault.Dispatch(ctx, "project.list", nil)
		if err != nil {
			return nil, err
		}
		list, ok := result.([]projectSummary)
		if !ok {
			continue
		}
		for _, p := range list {
			p.VaultID = m.ID
			out = append(out, p)
		}
	}
	return out, nil
}

// Diagnostics reports the findings that only a workspace can make: a project
// key declared twice by two open repositories, and two team repositories
// claiming the same team key.
//
// Holding several teams is not a finding. It is the point of GIT-US-0036: a
// second team repository used to register and then be ignored with an
// error-severity diagnostic, which described a limitation rather than a
// problem with the files. What remains a real error is an ambiguous key,
// because a request naming it could then be answered by either repository.
func (w *Workspace) Diagnostics() []core.Diagnostic {
	w.mu.RLock()
	mounts := w.snapshot()
	w.mu.RUnlock()

	var out []core.Diagnostic
	owner := map[core.ProjectKey]string{}
	teamOwner := map[string]string{}
	for _, m := range mounts {
		if team := m.Vault.Team(); team != nil {
			key := string(team.Key)
			if previous, taken := teamOwner[key]; taken {
				out = append(out, core.Diagnostic{
					Code:     core.CodeTeamKey,
					Severity: core.SeverityError,
					Path:     m.ID + "/" + core.TeamFileName,
					Field:    "key",
					Message: fmt.Sprintf("team key %s is already declared by repository %s",
						key, previous),
				})
			} else {
				teamOwner[key] = m.ID
			}
		}
		for _, p := range m.Vault.Projects() {
			if previous, taken := owner[p.Key]; taken {
				out = append(out, core.Diagnostic{
					Code:     core.CodeTeamKeyDup,
					Severity: core.SeverityError,
					Path:     m.ID + "/" + p.ConfigPath,
					Field:    "key",
					Message: fmt.Sprintf("project key %s is already served by repository %s",
						p.Key, previous),
				})
				continue
			}
			owner[p.Key] = m.ID
		}
	}
	sortDiagnostics(out)
	return out
}

// sortDiagnostics orders findings deterministically, by path then message.
func sortDiagnostics(d []core.Diagnostic) {
	sort.SliceStable(d, func(i, j int) bool {
		if d[i].Path != d[j].Path {
			return d[i].Path < d[j].Path
		}
		return d[i].Message < d[j].Message
	})
}
