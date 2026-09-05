package core

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Sentinel errors of team scaffolding.
var (
	// ErrTeamExists reports a folder that already holds a team.yaml. A team
	// repository is opened, never re-created: the file is the routing table of
	// the whole workspace and overwriting it would drop the project list.
	ErrTeamExists = errors.New("the folder already holds a team repository")
	// ErrTeamKey reports a key that does not match [A-Z][A-Z0-9-]{1,15}.
	ErrTeamKey = errors.New("invalid team key")
	// ErrMemberHandle reports a member handle that does not match
	// [a-z0-9][a-z0-9-]{0,31}, or one declared twice.
	ErrMemberHandle = errors.New("invalid member handle")
)

// teamArtifactDirs are the folders a team `.pmngr/` is made of (R-TEAM-LOC-2).
// They hold team artifacts only: a backlog folder here is the hard rule of
// docs/04 section 1 broken, which is why the list is closed.
var teamArtifactDirs = []string{"boards", "sprints", "retros", "index"}

// knowledgeIndexName is the landing page of a fresh team knowledge base.
const knowledgeIndexName = "index.md"

// NewTeam is the input of CreateTeam: everything the scaffolder needs that is
// not a documented default.
type NewTeam struct {
	// Key is the prefix of every sprint and retro id, matching
	// [A-Z][A-Z0-9-]{1,15}.
	Key TeamKey
	// Name is the display name shown wherever the team is named. It defaults to
	// the key.
	Name string
	// Description is one optional paragraph.
	Description string
	// Timezone is an IANA name used for sprint boundaries and retro scheduling.
	// It defaults to UTC.
	Timezone string
	// KnowledgePath is the team knowledge-base folder. It defaults to
	// "knowledge" (docs/04 section 3.1).
	KnowledgePath string
	// Members are the people the team starts with. It may be empty: a team
	// repository is created before anybody has been listed in it, and the file
	// says so with a warning rather than an error (ADR-020).
	Members []Member
}

// NewTeamConfig returns the configuration a fresh team repository starts with:
// every documented default of docs/04 section 3.1 and the identity spec
// carries.
//
// It declares no project. A team repository that lists a project nobody can
// reach is worse than one that lists none, and the project list is what decides
// whether a board card renders live or from a snapshot (ADR-007).
func NewTeamConfig(spec NewTeam) TeamConfig {
	cfg := DefaultTeamConfig()
	cfg.Key = spec.Key
	cfg.Name = strings.TrimSpace(spec.Name)
	if cfg.Name == "" {
		cfg.Name = string(spec.Key)
	}
	cfg.Description = strings.TrimSpace(spec.Description)
	if tz := strings.TrimSpace(spec.Timezone); tz != "" {
		cfg.Timezone = tz
	}
	if kb := strings.TrimSpace(spec.KnowledgePath); kb != "" {
		cfg.Knowledge.Path = path.Clean(kb)
	}
	cfg.Members = append([]Member{}, spec.Members...)
	cfg.Projects = []TeamProject{}
	return cfg
}

// MarshalTeamConfig encodes a team.yaml with the two-space indentation every
// other YAML emitter of the core uses, so a file written by the tool and a file
// written by hand from docs/04 section 3.4 diff as one block.
//
// It is the only emitter of team.yaml, which is what makes the round trip
// LoadTeamConfig -> MarshalTeamConfig byte-stable.
func MarshalTeamConfig(cfg TeamConfig) ([]byte, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, fmt.Errorf("encode %s: %w", TeamFileName, err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encode %s: %w", TeamFileName, err)
	}
	return []byte(buf.String()), nil
}

// knowledgeIndex is the landing page a fresh team knowledge base starts with.
// It is a plain Markdown page: the team KB has no front matter and no .pmngr/
// of its own (R-TEAM-LOC-4).
func knowledgeIndex(cfg TeamConfig) []byte {
	var b strings.Builder
	b.WriteString("# " + cfg.Name + "\n\n")
	b.WriteString("Team knowledge base. Pages here are plain Markdown and are rendered by the\n")
	b.WriteString("same pipeline a project's documentation uses.\n\n")
	b.WriteString("Suggested structure:\n\n")
	b.WriteString("- `ways-of-working/` — definition of done, code review, on-call.\n")
	b.WriteString("- `decisions/` — team-level decision records.\n")
	b.WriteString("- `people/` — onboarding, roles.\n\n")
	b.WriteString("Link to an item of a project this team owns with a qualified wikilink,\n")
	b.WriteString("for example `[[" + string(cfg.Key) + "/" + string(cfg.Key) + "-US-0001]]` (R-TKB-1).\n")
	return []byte(b.String())
}

// validateNewTeam checks what the scaffolder refuses to write at all, as
// opposed to what team.yaml validation reports once the file exists.
func validateNewTeam(spec NewTeam) error {
	if !ValidTeamKey(spec.Key) {
		return fmt.Errorf("%w: %q does not match [A-Z][A-Z0-9-]{1,15}", ErrTeamKey, spec.Key)
	}
	seen := make(map[string]bool, len(spec.Members))
	for _, m := range spec.Members {
		if !ValidMemberHandle(m.Handle) {
			return fmt.Errorf("%w: %q does not match [a-z0-9][a-z0-9-]{0,31}", ErrMemberHandle, m.Handle)
		}
		if seen[m.Handle] {
			return fmt.Errorf("%w: %q is declared twice", ErrMemberHandle, m.Handle)
		}
		seen[m.Handle] = true
	}
	return nil
}

// CreateTeam writes a new team repository at root: the team.yaml of docs/04
// section 3, the `.pmngr/` artifact folders of section 2 and the knowledge base
// of section 4 with a landing page.
//
// It is pure core: it touches nothing but the FS the caller supplies, so the
// browser creates a team repository through the very same code the CLI runs.
//
// It fails with ErrTeamKey on a key the grammar refuses, with ErrMemberHandle
// on a malformed or duplicated member handle, and with ErrTeamExists when the
// folder already holds a team.yaml.
func CreateTeam(fs FS, root string, spec NewTeam) (*TeamRef, error) {
	if fs == nil {
		return nil, errors.New("create team: nil file system")
	}
	if err := validateNewTeam(spec); err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	dir := path.Clean(root)
	if dir == "" {
		dir = "."
	}
	if dir == ".." || strings.HasPrefix(dir, "../") || path.IsAbs(dir) {
		return nil, fmt.Errorf("create team: %q is outside the repository", root)
	}

	configPath := joinPath(dir, TeamFileName)
	if _, err := fs.Stat(configPath); err == nil {
		return nil, fmt.Errorf("create team: %w: %s", ErrTeamExists, configPath)
	} else if !errors.Is(err, ErrNotExist) {
		return nil, fmt.Errorf("create team: stat %s: %w", configPath, err)
	}

	cfg := NewTeamConfig(spec)
	data, err := MarshalTeamConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}

	teamDir := joinPath(dir, BacklogDirName)
	knowledgeDir := joinPath(dir, cfg.KnowledgePath())
	folders := []string{teamDir, knowledgeDir}
	for _, name := range teamArtifactDirs {
		folders = append(folders, joinPath(teamDir, name))
	}
	for _, folder := range folders {
		if err := fs.MkdirAll(folder); err != nil {
			return nil, fmt.Errorf("create team: make %s: %w", folder, err)
		}
	}

	indexPath := joinPath(knowledgeDir, knowledgeIndexName)
	if _, err := fs.Stat(indexPath); errors.Is(err, ErrNotExist) {
		if err := fs.WriteFile(indexPath, knowledgeIndex(cfg)); err != nil {
			return nil, fmt.Errorf("create team: write %s: %w", indexPath, err)
		}
	}
	if err := fs.WriteFile(configPath, data); err != nil {
		return nil, fmt.Errorf("create team: write %s: %w", configPath, err)
	}

	// The reference is read back rather than assembled, so that what the caller
	// gets is exactly what discovery will report for this repository from now
	// on, diagnostics included.
	ref, found, err := DiscoverTeam(fs, dir)
	if err != nil {
		return nil, fmt.Errorf("create team: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("create team: %s was written but is not discoverable", configPath)
	}
	return ref, nil
}
