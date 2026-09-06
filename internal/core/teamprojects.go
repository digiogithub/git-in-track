package core

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

// This file is the write half of the `projects:` list of team.yaml, the routing
// table that decides which project a board may show, whether a card renders
// live from a clone or read-only from a committed index snapshot, and where a
// remote blob link points (docs/04 section 3.3 and section 7).
//
// Everything here is pure core over a core.FS, so the browser connects a
// project to a team through the very code the CLI and the companion run, and
// the file always goes back out through MarshalTeamConfig — the single emitter
// GIT-US-0034 introduced, which is what keeps the round trip byte-stable
// (R-TEAM-NEW-4).

// Sentinel errors of the team project list.
var (
	// ErrTeamMissing reports a folder that holds no team.yaml, so there is no
	// project list to change.
	ErrTeamMissing = errors.New("the folder holds no team repository")
	// ErrTeamProjectExists reports a project key the team already declares. The
	// list is keyed by project key alone (R-PROJ-1), so a second entry would
	// make every reference into it ambiguous.
	ErrTeamProjectExists = errors.New("the team already declares this project")
	// ErrTeamProjectUnknown reports a project key the team does not declare.
	ErrTeamProjectUnknown = errors.New("the team declares no such project")
	// ErrTeamProjectFields reports an entry the team file would refuse: a key
	// outside the grammar, or a missing required field.
	ErrTeamProjectFields = errors.New("invalid project entry")
)

// ProjectReference is one place a team artifact points at a project: a board
// column order, the scope of a board, the item list of a sprint or the task a
// retro action was promoted into. It is what removing a project would orphan.
type ProjectReference struct {
	// Kind is "board", "sprint" or "retro".
	Kind string `json:"kind"`
	// ID is the artifact id, and Path its file inside the team repository.
	ID   string `json:"id"`
	Path string `json:"path"`
	// Field is the front-matter field the reference sits in.
	Field string `json:"field"`
	// Ref is the reference itself, `<projectKey>/<itemId>`, or the bare project
	// key when the artifact names the project rather than an item of it.
	Ref string `json:"ref"`
}

// TeamArtifacts are the team artifacts a project reference can hide in. A nil
// slice simply contributes nothing, so a caller that has not loaded one of the
// three kinds still gets an answer about the other two.
type TeamArtifacts struct {
	Boards  []*Board
	Sprints []*Sprint
	Retros  []*Retro
}

// NormalizeTeamProject trims an entry and fills in what docs/04 section 3.3
// lets the file omit, so that what the tool writes is what the tool would have
// read back. It does not invent a repository URL or a name: those are required
// and their absence is a validation failure, not a default.
func NormalizeTeamProject(p TeamProject) TeamProject {
	p.Key = ProjectKey(strings.TrimSpace(string(p.Key)))
	p.Name = strings.TrimSpace(p.Name)
	p.Repo = strings.TrimSpace(p.Repo)
	p.DefaultBranch = strings.TrimSpace(p.DefaultBranch)
	p.Host = strings.TrimSpace(p.Host)
	p.WebURL = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p.WebURL), "/"))
	p.Color = strings.TrimSpace(p.Color)
	if docs := strings.TrimSpace(p.DocsPath); docs != "" {
		p.DocsPath = path.Clean(docs)
	}
	if p.Name == "" {
		p.Name = string(p.Key)
	}
	hints := make([]string, 0, len(p.LocalHints))
	for _, hint := range p.LocalHints {
		if trimmed := strings.TrimSpace(hint); trimmed != "" {
			hints = append(hints, trimmed)
		}
	}
	if len(hints) == 0 {
		p.LocalHints = nil
	} else {
		p.LocalHints = hints
	}
	return p
}

// validateTeamProject checks what the writer refuses outright, which is the
// subset of TeamConfig.Validate that no file can be saved in violation of: a
// key outside [A-Z][A-Z0-9]{1,9} and the three required fields of R-PROJ-1.
func validateTeamProject(p TeamProject) error {
	if !ValidProjectKey(p.Key) {
		return fmt.Errorf("%w: key %q does not match [A-Z][A-Z0-9]{1,9}", ErrTeamProjectFields, p.Key)
	}
	for _, field := range []struct {
		name  string
		value string
	}{{"name", p.Name}, {"repo", p.Repo}, {"docs_path", p.DocsPath}} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%w: project %s has no %s", ErrTeamProjectFields, p.Key, field.name)
		}
	}
	return nil
}

// AddTeamProject inserts a project into a team configuration.
//
// The entry lands in key order rather than at the end of the list: the position
// of an entry then depends on its key and not on the order the team connected
// its repositories, so two people connecting two different projects write two
// hunks in two different places and git merges them without a conflict. Entries
// already in the file are never reordered, which keeps the round trip of an
// existing team.yaml byte-stable.
//
// It fails with ErrTeamProjectFields on an entry no team.yaml may hold, and
// with ErrTeamProjectExists on a key the team already declares (R-PROJ-1).
func AddTeamProject(cfg *TeamConfig, entry TeamProject) (TeamProject, error) {
	if cfg == nil {
		return TeamProject{}, fmt.Errorf("%w: nil configuration", ErrTeamProjectFields)
	}
	p := NormalizeTeamProject(entry)
	if err := validateTeamProject(p); err != nil {
		return TeamProject{}, err
	}
	if _, exists := cfg.Project(p.Key); exists {
		return TeamProject{}, fmt.Errorf("%w: %s is already declared by team %s",
			ErrTeamProjectExists, p.Key, cfg.Key)
	}
	at := len(cfg.Projects)
	for i, existing := range cfg.Projects {
		if existing.Key > p.Key {
			at = i
			break
		}
	}
	out := make([]TeamProject, 0, len(cfg.Projects)+1)
	out = append(out, cfg.Projects[:at]...)
	out = append(out, p)
	out = append(out, cfg.Projects[at:]...)
	cfg.Projects = out
	return p, nil
}

// RemoveTeamProject drops a project from a team configuration and returns the
// entry that was removed. Every other entry keeps its position, so the diff is
// the removed block and nothing else.
//
// It fails with ErrTeamProjectUnknown when the team declares no such project.
// It says nothing about what referenced the project: that is
// TeamProjectReferences, which the caller decides what to do about.
func RemoveTeamProject(cfg *TeamConfig, key ProjectKey) (TeamProject, error) {
	if cfg == nil {
		return TeamProject{}, fmt.Errorf("%w: nil configuration", ErrTeamProjectFields)
	}
	key = ProjectKey(strings.TrimSpace(string(key)))
	removed, found := cfg.Project(key)
	if !found {
		return TeamProject{}, fmt.Errorf("%w: %s", ErrTeamProjectUnknown, key)
	}
	kept := make([]TeamProject, 0, len(cfg.Projects)-1)
	for _, p := range cfg.Projects {
		if p.Key != key {
			kept = append(kept, p)
		}
	}
	cfg.Projects = kept
	return removed, nil
}

// TeamProjectReferences lists every place the team artifacts point at a
// project: the scope of a board, the refs of its column order, the `items` and
// `committed` lists of a sprint and the task a retro action was promoted into.
//
// Nothing here reads a file: the caller loads the artifacts it already has and
// this decides, so the same answer is produced in the browser and in the
// companion. The result is sorted, so a message built from it is stable.
func TeamProjectReferences(key ProjectKey, artifacts TeamArtifacts) []ProjectReference {
	key = ProjectKey(strings.TrimSpace(string(key)))
	if key == "" {
		return nil
	}
	var out []ProjectReference
	add := func(kind, id, filePath, field, ref string) {
		out = append(out, ProjectReference{Kind: kind, ID: id, Path: filePath, Field: field, Ref: ref})
	}
	matches := func(raw string) bool {
		ref, err := ParseRef(raw)
		return err == nil && ref.Project == key
	}

	for _, b := range artifacts.Boards {
		if b == nil {
			continue
		}
		for _, scoped := range b.Projects {
			if scoped == key {
				add("board", b.ID, b.Path, "projects", string(key))
			}
		}
		order := b.Order
		for _, column := range order.Columns() {
			for _, ref := range order.Refs(column) {
				if matches(ref) {
					add("board", b.ID, b.Path, "order."+column, ref)
				}
			}
		}
	}
	for _, s := range artifacts.Sprints {
		if s == nil {
			continue
		}
		for _, field := range []struct {
			name string
			refs []string
		}{{"items", s.Items}, {"committed", s.Committed}} {
			for _, ref := range field.refs {
				if matches(ref) {
					add("sprint", s.ID, s.Path, field.name, ref)
				}
			}
		}
	}
	for _, r := range artifacts.Retros {
		if r == nil {
			continue
		}
		for _, a := range r.Actions {
			if a.Task != "" && matches(a.Task) {
				add("retro", r.ID, r.Path, "actions."+a.ID, a.Task)
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// ReadTeamConfig loads the team.yaml at root for editing. Unlike LoadTeamConfig
// it refuses a file that does not parse at all rather than returning a partial
// configuration, because writing that back would drop whatever failed to
// decode.
func ReadTeamConfig(fs FS, root string) (*TeamConfig, string, error) {
	if fs == nil {
		return nil, "", errors.New("read team: nil file system")
	}
	dir := path.Clean(root)
	if dir == "" {
		dir = "."
	}
	configPath := joinPath(dir, TeamFileName)
	data, err := fs.ReadFile(configPath)
	if errors.Is(err, ErrNotExist) {
		return nil, configPath, fmt.Errorf("%w: %s", ErrTeamMissing, configPath)
	}
	if err != nil {
		return nil, configPath, fmt.Errorf("read %s: %w", configPath, err)
	}
	// A validation failure is not fatal here: a team.yaml with no member and no
	// project is exactly what CreateTeam writes, and connecting a project to it
	// is how it stops being empty. Only a file that cannot be decoded at all is
	// refused, and LoadTeamConfig reports that as a nil configuration.
	cfg, loadErr := LoadTeamConfig(data)
	if cfg == nil {
		return nil, configPath, fmt.Errorf("read %s: %w", configPath, loadErr)
	}
	return cfg, configPath, nil
}

// WriteTeamConfig emits a team configuration back to the team.yaml at root
// through MarshalTeamConfig, the single emitter, so a file the product rewrites
// diffs against the one it wrote as a single block (R-TEAM-NEW-4).
func WriteTeamConfig(fs FS, root string, cfg TeamConfig) error {
	if fs == nil {
		return errors.New("write team: nil file system")
	}
	dir := path.Clean(root)
	if dir == "" {
		dir = "."
	}
	data, err := MarshalTeamConfig(cfg)
	if err != nil {
		return fmt.Errorf("write team: %w", err)
	}
	configPath := joinPath(dir, TeamFileName)
	if err := fs.WriteFile(configPath, data); err != nil {
		return fmt.Errorf("write %s: %w", configPath, err)
	}
	return nil
}

// UpdateTeamProjects reads the team.yaml at root, hands the parsed
// configuration to mutate and writes it back when mutate reports no error. It
// is the one path a project entry is added or removed through, so that the read
// and the write can never use two different parsers.
func UpdateTeamProjects(fs FS, root string, mutate func(*TeamConfig) error) error {
	cfg, _, err := ReadTeamConfig(fs, root)
	if err != nil {
		return err
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return WriteTeamConfig(fs, root, *cfg)
}
