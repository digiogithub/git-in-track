package core

import (
	"errors"
	"fmt"
	"path"
)

// teamBacklogDirs are the folders a project backlog is made of. Finding any of
// them under the team repository's own `.pmngr/` breaks the hard rule of
// docs/04 section 1 — backlogs never leave their project repository — and is
// reported as E-TEAM-BACKLOG-IN-TEAM-REPO.
var teamBacklogDirs = []string{"epics", "stories", "tasks", "milestones", "comments"}

// TeamRef is a team repository discovered in a vault: where team.yaml is, where
// the knowledge base lives, and what the file says.
type TeamRef struct {
	Key  TeamKey `json:"key"`
	Name string  `json:"name"`
	// Root is the vault-relative repository root, "." for the vault itself.
	Root string `json:"root"`
	// ConfigPath is Root + "/team.yaml".
	ConfigPath string `json:"config_path"`
	// KnowledgePath is the vault-relative team knowledge-base folder.
	KnowledgePath string `json:"knowledge_path"`
	// TeamDirPath is the vault-relative `.pmngr/` folder holding boards,
	// sprints, retros and index snapshots (R-TEAM-LOC-2).
	TeamDirPath string `json:"team_dir_path"`
	// Config is the parsed file. It is nil only when team.yaml could not be
	// decoded at all, in which case Diagnostics says why.
	Config *TeamConfig `json:"-"`
	// Diagnostics are the findings of team.yaml validation.
	Diagnostics []Diagnostic `json:"-"`
}

// DiscoverTeam reads the team.yaml at root, if there is one. It reports false
// when the folder is not a team repository, which is not an error: most vaults
// hold a project and nothing else.
//
// A team.yaml that fails validation is still returned, with its findings in
// Diagnostics, so that the app opens the repository read-only rather than
// pretending it is not there (the same contract as DiscoverProjects).
func DiscoverTeam(fs FS, root string) (*TeamRef, bool, error) {
	if fs == nil {
		return nil, false, errors.New("discover team: nil file system")
	}
	if root == "" {
		root = "."
	}
	root = path.Clean(root)
	configPath := joinPath(root, TeamFileName)
	data, err := fs.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", configPath, err)
	}

	ref := &TeamRef{
		Root:          root,
		ConfigPath:    configPath,
		TeamDirPath:   joinPath(root, BacklogDirName),
		KnowledgePath: joinPath(root, DefaultKnowledgePath),
	}
	cfg, cfgErr := LoadTeamConfig(data)
	if cfg == nil {
		ref.Diagnostics = []Diagnostic{{
			Code:     CodeTeamSchema,
			Severity: SeverityError,
			Path:     configPath,
			Message:  fmt.Sprintf("cannot decode %s: %v", TeamFileName, cfgErr),
		}}
		return ref, true, nil
	}
	ref.Config = cfg
	ref.Key = cfg.Key
	ref.Name = cfg.Name
	ref.KnowledgePath = joinPath(root, cfg.KnowledgePath())
	for _, d := range cfg.Validate() {
		d.Path = configPath
		ref.Diagnostics = append(ref.Diagnostics, d)
	}
	ref.Diagnostics = append(ref.Diagnostics, teamLayoutDiagnostics(fs, ref)...)
	sortDiagnostics(ref.Diagnostics)
	return ref, true, nil
}

// teamLayoutDiagnostics enforces R-TEAM-LOC-2: the team `.pmngr/` holds team
// artifacts only, never a backlog.
func teamLayoutDiagnostics(fs FS, ref *TeamRef) []Diagnostic {
	var out []Diagnostic
	for _, dir := range teamBacklogDirs {
		full := joinPath(ref.TeamDirPath, dir)
		info, err := fs.Stat(full)
		if err != nil || !info.IsDir {
			continue
		}
		out = append(out, Diagnostic{
			Code:     CodeTeamBacklogInTeamRepo,
			Severity: SeverityError,
			Path:     full,
			Field:    "layout",
			Message:  "a backlog folder must live in its project repository, never in the team repository",
		})
	}
	return out
}

// KBScope returns the pseudo-project the index scans the team knowledge base
// under. It carries the team key so that a page of the team KB is attributed to
// the team rather than to a project, and Team marks it so that the indexer
// never looks for a backlog in it.
func (t *TeamRef) KBScope() ProjectRef {
	return ProjectRef{
		Key:         ProjectKey(t.Key),
		Name:        t.Name,
		DocsPath:    t.KnowledgePath,
		BacklogPath: joinPath(t.KnowledgePath, BacklogDirName),
		ConfigPath:  t.ConfigPath,
		Team:        true,
	}
}
