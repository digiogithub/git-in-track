package vault

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the write half of the team surface: "team.create", the method
// that turns a folder into a team repository. Reading one is team.go.

// teamCreateParams is the input of "team.create": where the team.yaml goes and
// what identity it starts with.
type teamCreateParams struct {
	// VaultID routes the call; it is read by the workspace router, not here.
	VaultID string `json:"vaultId,omitempty"`
	// Root is the vault-relative folder the team repository is created at. An
	// empty value and "." both mean the repository root, which is where
	// R-TEAM-LOC-1 puts team.yaml.
	Root string `json:"root,omitempty"`
	// Key is the prefix of every sprint and retro id, matching
	// [A-Z][A-Z0-9-]{1,15}.
	Key string `json:"key"`
	// Name is the display name; it defaults to the key.
	Name string `json:"name,omitempty"`
	// Description is one optional paragraph.
	Description string `json:"description,omitempty"`
	// Timezone is an IANA name; it defaults to UTC.
	Timezone string `json:"timezone,omitempty"`
	// KnowledgePath is the team knowledge-base folder; it defaults to
	// "knowledge".
	KnowledgePath string `json:"knowledgePath,omitempty"`
	// Members are the people the team starts with. It may be empty.
	Members []core.Member `json:"members,omitempty"`
}

// teamCreated is what "team.create" answers with: the team repository as
// "team.get" reports it, and the files the host must persist.
type teamCreated struct {
	// Team is the very shape "team.get" answers with, read back from the file
	// that was just written rather than assembled from the request.
	Team any `json:"team"`
	// Writes are the files the core wrote, for a host that persists them
	// itself (browser-only mode).
	Writes WriteSet `json:"writes"`
}

// teamCreate scaffolds a team repository in this vault and re-indexes it.
//
// The whole decision lives in internal/core: this method only decodes the
// request and reports what was written. The caller holds the vault lock.
func (v *Vault) teamCreate(ctx context.Context, raw []byte) (any, error) {
	p, err := decodeParams[teamCreateParams](raw)
	if err != nil {
		return nil, err
	}
	root := path.Clean(strings.TrimSpace(p.Root))
	if root == "" {
		root = "."
	}

	v.fs.begin()
	if _, err := core.CreateTeam(v.fs, root, core.NewTeam{
		Key:           core.TeamKey(strings.TrimSpace(p.Key)),
		Name:          p.Name,
		Description:   p.Description,
		Timezone:      p.Timezone,
		KnowledgePath: p.KnowledgePath,
		Members:       p.Members,
	}); err != nil {
		return nil, classifyCreateTeam(err)
	}
	writes, err := v.commit(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := v.reload(ctx); err != nil {
		return nil, err
	}
	if v.team == nil {
		return nil, failf("internal", "%s was written but the repository is not a team repository", core.TeamFileName)
	}
	summary, err := v.teamGet()
	if err != nil {
		return nil, err
	}
	return teamCreated{Team: summary, Writes: writes}, nil
}

// classifyCreateTeam maps a scaffolding failure onto the stable code catalog
// the hosts switch on.
func classifyCreateTeam(err error) error {
	switch {
	case errors.Is(err, core.ErrTeamKey), errors.Is(err, core.ErrMemberHandle):
		return &Error{Code: "validation_failed", Message: err.Error()}
	case errors.Is(err, core.ErrTeamExists):
		return &Error{Code: "team_exists", Message: err.Error()}
	case errors.Is(err, core.ErrReadOnly):
		return &Error{Code: "read_only", Message: err.Error()}
	default:
		return fmt.Errorf("create team: %w", err)
	}
}
