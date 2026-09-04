package vault

import (
	"encoding/json"
	"fmt"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file holds the team-repository half of the CoreApi contract: the wire
// shapes of "team.get" and "ref.resolve", and the single-vault implementations
// of both. A Workspace overrides them with the cross-repository answer; a lone
// vault answers for what it can see itself, which is what keeps the two hosts
// on one code path.

// teamProjectSummary is one entry of team.yaml plus what the workspace knows
// about it locally: whether a clone of it is open, and where.
type teamProjectSummary struct {
	core.TeamProject
	// Cloned reports whether an open vault exposes this project key. A project
	// that is not cloned is listed and marked, never hidden (docs/04 section 7).
	Cloned bool `json:"cloned"`
	// VaultID is the repository the clone was found in, empty when not cloned.
	VaultID string `json:"vaultId,omitempty"`
	// LocalDocsPath is the documentation folder inside that repository.
	LocalDocsPath string `json:"localDocsPath,omitempty"`
	// Diagnostics are the findings about this project alone, such as a clone
	// whose project.yaml declares another key (W-TEAM-KEY-MISMATCH).
	Diagnostics []core.Diagnostic `json:"diagnostics,omitempty"`
}

// teamSummary is everything the UI needs to render a team repository.
type teamSummary struct {
	Key           string               `json:"key"`
	Name          string               `json:"name"`
	Description   string               `json:"description,omitempty"`
	Timezone      string               `json:"timezone,omitempty"`
	Root          string               `json:"root"`
	KnowledgePath string               `json:"knowledgePath"`
	VaultID       string               `json:"vaultId,omitempty"`
	Members       []core.Member        `json:"members"`
	Projects      []teamProjectSummary `json:"projects"`
	Policies      map[string]string    `json:"policies,omitempty"`
	Cadence       core.Cadence         `json:"cadence"`
	Defaults      core.TeamDefaults    `json:"defaults"`
	Snapshots     core.SnapshotPolicy  `json:"snapshots"`
	Diagnostics   []core.Diagnostic    `json:"diagnostics"`
}

// refResolution is the answer of "ref.resolve": where a `<KEY>/<ITEM-ID>`
// reference points and whether it can be read right now.
type refResolution struct {
	Ref     string `json:"ref"`
	Project string `json:"project"`
	Item    string `json:"item"`
	// Declared reports whether team.yaml lists the project at all.
	Declared bool `json:"declared"`
	// Cloned reports whether an open vault exposes the project.
	Cloned bool `json:"cloned"`
	// VaultID is the repository the item was resolved in.
	VaultID string `json:"vaultId,omitempty"`
	// Found is the item itself, present only when the project is cloned and the
	// item exists in it.
	Found *core.Item `json:"found,omitempty"`
	// Reason explains an unresolved reference in one sentence, for the UI.
	Reason string `json:"reason,omitempty"`
}

// refParams is the input of "ref.resolve".
type refParams struct {
	Ref string `json:"ref"`
}

// teamGet answers "team.get" for a single vault: the team repository it is,
// with every declared project marked cloned only when this very vault exposes
// it. The workspace answers the same method across every open repository.
func (v *Vault) teamGet() (any, error) {
	if v.team == nil {
		return nil, failf("not_found", "this repository has no %s at its root", core.TeamFileName)
	}
	local := map[core.ProjectKey]core.ProjectRef{}
	for _, p := range v.projects {
		if !p.Team {
			local[p.Key] = p
		}
	}
	return teamSummaryOf(v.team, "", func(key core.ProjectKey) (string, core.ProjectRef, bool) {
		ref, ok := local[key]
		return "", ref, ok
	}), nil
}

// teamSummaryOf renders a team repository. The lookup reports, for a declared
// project key, the id of the vault holding a clone and the project discovered
// in it; it is what tells a cloned project from a remote one.
func teamSummaryOf(
	team *core.TeamRef,
	vaultID string,
	lookup func(core.ProjectKey) (string, core.ProjectRef, bool),
) teamSummary {
	out := teamSummary{
		Key:           string(team.Key),
		Name:          team.Name,
		Root:          team.Root,
		KnowledgePath: team.KnowledgePath,
		VaultID:       vaultID,
		Members:       []core.Member{},
		Projects:      []teamProjectSummary{},
		Diagnostics:   append([]core.Diagnostic{}, team.Diagnostics...),
	}
	if team.Config == nil {
		return out
	}
	cfg := team.Config
	out.Description = cfg.Description
	out.Timezone = cfg.Timezone
	out.Policies = cfg.Policies
	out.Cadence = cfg.Cadence
	out.Defaults = cfg.Defaults
	out.Snapshots = cfg.Snapshots
	out.Members = append(out.Members, cfg.Members...)

	for _, p := range cfg.Projects {
		entry := teamProjectSummary{TeamProject: p}
		if id, ref, ok := lookup(p.Key); ok {
			entry.Cloned = true
			entry.VaultID = id
			entry.LocalDocsPath = ref.DocsPath
			if ref.Config != nil && ref.Config.Key != p.Key {
				entry.Diagnostics = append(entry.Diagnostics, core.Diagnostic{
					Code:     core.CodeTeamKeyMismatch,
					Severity: core.SeverityWarning,
					Path:     ref.ConfigPath,
					Field:    "key",
					Message: fmt.Sprintf("the clone declares key %q but %s lists it as %q",
						ref.Config.Key, core.TeamFileName, p.Key),
				})
			}
		}
		out.Projects = append(out.Projects, entry)
	}
	return out
}

// refResolve answers "ref.resolve" for a single vault.
func (v *Vault) refResolve(raw []byte) (any, error) {
	p, err := decodeParams[refParams](raw)
	if err != nil {
		return nil, err
	}
	ref, parseErr := core.ParseRef(p.Ref)
	if parseErr != nil {
		return nil, failf("invalid_request", "%v", parseErr)
	}
	return v.resolveRef(ref, "", v.team), nil
}

// ResolveRef looks a reference up in this vault, taking its lock. team may be
// the team repository of another vault, which is how a workspace answers
// "declared" for a project this vault knows nothing about.
func (v *Vault) ResolveRef(ref core.Ref, vaultID string, team *core.TeamRef) refResolution {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.resolveRef(ref, vaultID, team)
}

// resolveRef looks a reference up in this vault. The caller holds the lock.
func (v *Vault) resolveRef(ref core.Ref, vaultID string, team *core.TeamRef) refResolution {
	out := refResolution{
		Ref:     ref.String(),
		Project: string(ref.Project),
		Item:    string(ref.Item),
	}
	if team != nil && team.Config != nil {
		_, out.Declared = team.Config.Project(ref.Project)
	}
	found := false
	for _, p := range v.projects {
		if !p.Team && p.Key == ref.Project {
			found = true
			break
		}
	}
	if !found {
		out.Reason = fmt.Sprintf("project %s is not cloned on this machine", ref.Project)
		return out
	}
	out.Cloned = true
	out.VaultID = vaultID
	item, err := v.index.Item(ref.Item)
	if err != nil {
		out.Reason = fmt.Sprintf("project %s is open but has no item %s", ref.Project, ref.Item)
		return out
	}
	// A resolution is a routing answer, not a reader: the body stays out of it
	// so that a board rendering hundreds of cards does not carry the backlog.
	item.Body = ""
	out.Found = item
	return out
}

// decodeRefParams is a small helper shared by the workspace router.
func decodeRefParams(raw []byte) (core.Ref, error) {
	var p refParams
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &p); err != nil {
			return core.Ref{}, failf("invalid_request", "decode params: %v", err)
		}
	}
	ref, err := core.ParseRef(p.Ref)
	if err != nil {
		return core.Ref{}, failf("invalid_request", "%v", err)
	}
	return ref, nil
}
