// Package config reads, writes and resolves the gintrack configuration file.
//
// The file lives next to the rest of the companion's state, at the per-platform
// location documented in docs/07-cli-and-api.md section 3.1, and holds the list
// of registered repositories plus the settings of the server, the git backend,
// the indexer, the MCP server and the logger.
//
// The effective configuration is the result of the precedence chain
// flags > environment (GINTRACK_*) > file > built-in defaults, which Resolve
// applies. Nothing in this package touches a repository: it only describes
// which repositories exist and how the companion should behave.
package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the configuration schema this build understands.
const SchemaVersion = 1

// DefaultWorkspaceName is the workspace a fresh configuration starts with.
const DefaultWorkspaceName = "default"

// Defaults of the server section.
const (
	DefaultBind = "127.0.0.1"
	DefaultPort = 7317
)

// DefaultCommitMessageTemplate is the shipped commit-message template
// (docs/06-git-sync.md section 4).
const DefaultCommitMessageTemplate = `pmngr: update {{.ItemID}} "{{.Title}}"`

// DefaultDebounce is how long the watcher coalesces file events for.
const DefaultDebounce = 250 * time.Millisecond

// DefaultCommitDebounce is how long commit-on-save coalesces rapid saves of the
// same item for, so a burst of keystrokes becomes one commit
// (docs/06-git-sync.md section 3.3, `commitDebounceMs`).
const DefaultCommitDebounce = 2 * time.Second

// Role is what a registered repository holds.
type Role string

// The repository roles.
const (
	// RoleProject is a repository whose documentation folder holds a .pmngr
	// backlog: epics, stories, tasks and milestones.
	RoleProject Role = "project"
	// RoleTeam is a team repository: boards, sprints, retrospectives and the
	// shared knowledge base.
	RoleTeam Role = "team"
)

// Valid reports whether the role is one this build knows.
func (r Role) Valid() bool { return r == RoleProject || r == RoleTeam }

// Backend selects the git implementation.
type Backend string

// The git backends of docs/07 section 3.4.
const (
	// BackendAuto uses the system git when one is on PATH and go-git otherwise.
	BackendAuto Backend = "auto"
	// BackendGoGit is the pure-Go implementation.
	BackendGoGit Backend = "go-git"
	// BackendSystem shells out to the git executable.
	BackendSystem Backend = "system"
)

// Valid reports whether the backend is one this build knows.
func (b Backend) Valid() bool {
	return b == BackendAuto || b == BackendGoGit || b == BackendSystem
}

// Config is the whole configuration file.
type Config struct {
	Version          int         `json:"version"                    yaml:"version"`
	DefaultWorkspace string      `json:"defaultWorkspace,omitempty" yaml:"defaultWorkspace,omitempty"`
	Workspaces       []Workspace `json:"workspaces,omitempty"       yaml:"workspaces,omitempty"`
	Repos            []Repo      `json:"repos,omitempty"            yaml:"repos,omitempty"`
	Server           Server      `json:"server"                     yaml:"server"`
	Git              Git         `json:"git"                        yaml:"git"`
	Index            Index       `json:"index"                      yaml:"index"`
	MCP              MCP         `json:"mcp"                        yaml:"mcp"`
	Log              Log         `json:"log"                        yaml:"log"`
}

// Workspace is a named group of registered repositories. Repos holds repository
// ids, which resolve against the top-level repos list.
type Workspace struct {
	Name  string   `json:"name"            yaml:"name"`
	Repos []string `json:"repos,omitempty" yaml:"repos,omitempty"`
}

// Repo is one registered repository.
type Repo struct {
	// ID is the stable handle the command line addresses the repository by. It
	// is the slug of the folder name, deduplicated across the file.
	ID string `json:"id" yaml:"id"`
	// Path is the absolute path of the git working tree.
	Path string `json:"path" yaml:"path"`
	// Role says whether the repository holds a project backlog or a team space.
	Role Role `json:"role" yaml:"role"`
	// DocsFolder is the documentation folder relative to the repository root,
	// "." when the backlog sits at the root.
	DocsFolder string `json:"docsFolder,omitempty" yaml:"docsFolder,omitempty"`
	// Enabled is false for a registration the user keeps but does not want
	// indexed. It defaults to true.
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// Server is the local HTTP server section.
type Server struct {
	Bind        string        `json:"bind"            yaml:"bind"`
	Port        int           `json:"port"            yaml:"port"`
	Token       string        `json:"token,omitempty" yaml:"token,omitempty"`
	IdleTimeout time.Duration `json:"idleTimeout"     yaml:"idleTimeout"`
	OpenBrowser bool          `json:"openBrowser"     yaml:"openBrowser"`
}

// Git is the git backend section.
type Git struct {
	Backend      Backend `json:"backend"      yaml:"backend"`
	CommitOnSave bool    `json:"commitOnSave" yaml:"commitOnSave"`
	// CommitDebounce coalesces rapid saves of the same item into one commit.
	// The configuration file spells it as a Go duration (`2s`), which is the
	// `commitDebounceMs: 2000` of docs/06 section 13 in this file's units.
	CommitDebounce  time.Duration `json:"commitDebounce"        yaml:"commitDebounce"`
	MessageTemplate string        `json:"messageTemplate"       yaml:"messageTemplate"`
	AuthorName      string        `json:"authorName,omitempty"  yaml:"authorName,omitempty"`
	AuthorEmail     string        `json:"authorEmail,omitempty" yaml:"authorEmail,omitempty"`
	// SignCommits asks for gpg or ssh signed commits. It is honoured by the
	// system backend only; go-git refuses it with git_unsupported.
	SignCommits bool `json:"signCommits" yaml:"signCommits"`
}

// Index is the indexer and watcher section.
type Index struct {
	// CacheDir is where the index snapshot is persisted; empty means the
	// directory the configuration file lives in.
	CacheDir string        `json:"cacheDir,omitempty" yaml:"cacheDir,omitempty"`
	Watch    bool          `json:"watch"              yaml:"watch"`
	Debounce time.Duration `json:"debounce"           yaml:"debounce"`
}

// MCP is the Model Context Protocol section.
type MCP struct {
	Enabled    bool `json:"enabled"    yaml:"enabled"`
	AllowWrite bool `json:"allowWrite" yaml:"allowWrite"`
}

// Log is the logging section.
type Log struct {
	Level  string `json:"level"  yaml:"level"`
	Format string `json:"format" yaml:"format"`
}

// Default returns the built-in configuration: no repositories, one empty
// workspace, and the documented defaults for every section.
func Default() *Config {
	return &Config{
		Version:          SchemaVersion,
		DefaultWorkspace: DefaultWorkspaceName,
		Workspaces:       []Workspace{{Name: DefaultWorkspaceName}},
		Server: Server{
			Bind:        DefaultBind,
			Port:        DefaultPort,
			OpenBrowser: true,
		},
		Git: Git{
			Backend:         BackendAuto,
			CommitDebounce:  DefaultCommitDebounce,
			MessageTemplate: DefaultCommitMessageTemplate,
		},
		Index: Index{
			Watch:    true,
			Debounce: DefaultDebounce,
		},
		Log: Log{Level: "info", Format: "text"},
	}
}

// Clone returns a deep copy, so that a caller may layer overrides on a loaded
// configuration without mutating it.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	out := *c
	out.Workspaces = make([]Workspace, len(c.Workspaces))
	for i, w := range c.Workspaces {
		out.Workspaces[i] = Workspace{Name: w.Name, Repos: append([]string(nil), w.Repos...)}
	}
	out.Repos = append([]Repo(nil), c.Repos...)
	return &out
}

// UnmarshalYAML decodes a configuration on top of the defaults, so that a file
// that omits a key keeps the built-in value instead of the zero value. Without
// it a file with no `server:` block would ask for port 0.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	type plain Config
	seeded := plain(*Default())
	if err := node.Decode(&seeded); err != nil {
		return fmt.Errorf("decode configuration: %w", err)
	}
	*c = Config(seeded)
	return nil
}

// UnmarshalYAML decodes a repository entry, defaulting `enabled` to true.
func (r *Repo) UnmarshalYAML(node *yaml.Node) error {
	type plain Repo
	seeded := plain{Enabled: true}
	if err := node.Decode(&seeded); err != nil {
		return fmt.Errorf("decode repository: %w", err)
	}
	*r = Repo(seeded)
	return nil
}

// Repo returns the registered repository with this id.
func (c *Config) Repo(id string) (Repo, bool) {
	for _, r := range c.Repos {
		if r.ID == id {
			return r, true
		}
	}
	return Repo{}, false
}

// RemoveRepo unregisters a repository by id, dropping it from every workspace.
// It reports whether anything was removed; the files on disk are never touched.
func (c *Config) RemoveRepo(id string) (Repo, bool) {
	removed, found := Repo{}, false
	kept := c.Repos[:0]
	for _, r := range c.Repos {
		if r.ID == id {
			removed, found = r, true
			continue
		}
		kept = append(kept, r)
	}
	if !found {
		return Repo{}, false
	}
	c.Repos = kept
	for i, w := range c.Workspaces {
		ids := w.Repos[:0]
		for _, ref := range w.Repos {
			if ref != id {
				ids = append(ids, ref)
			}
		}
		c.Workspaces[i].Repos = ids
	}
	return removed, true
}

// EnsureWorkspace adds a workspace with this name when it is missing, which is
// what `--workspace <new>` and GINTRACK_WORKSPACE do: naming a workspace is
// enough to create it.
func (c *Config) EnsureWorkspace(name string) {
	if name == "" {
		return
	}
	if _, ok := c.WorkspaceNamed(name); ok {
		return
	}
	c.Workspaces = append(c.Workspaces, Workspace{Name: name})
}

// WorkspaceNamed returns the workspace with this name.
func (c *Config) WorkspaceNamed(name string) (Workspace, bool) {
	for _, w := range c.Workspaces {
		if w.Name == name {
			return w, true
		}
	}
	return Workspace{}, false
}

// ActiveWorkspace returns the name of the workspace commands act on: the
// configured default, or the first workspace, or DefaultWorkspaceName.
func (c *Config) ActiveWorkspace() string {
	if c.DefaultWorkspace != "" {
		return c.DefaultWorkspace
	}
	if len(c.Workspaces) > 0 {
		return c.Workspaces[0].Name
	}
	return DefaultWorkspaceName
}

// WorkspaceRepos returns the enabled repositories of a workspace, in
// registration order. An empty name means the active workspace. A workspace
// that lists no ids stands for every registered repository, which is what a
// single-workspace configuration wants.
func (c *Config) WorkspaceRepos(name string) []Repo {
	if name == "" {
		name = c.ActiveWorkspace()
	}
	ws, ok := c.WorkspaceNamed(name)
	if !ok || len(ws.Repos) == 0 {
		out := make([]Repo, 0, len(c.Repos))
		for _, r := range c.Repos {
			if r.Enabled {
				out = append(out, r)
			}
		}
		return out
	}
	members := make(map[string]bool, len(ws.Repos))
	for _, id := range ws.Repos {
		members[id] = true
	}
	out := make([]Repo, 0, len(ws.Repos))
	for _, r := range c.Repos {
		if r.Enabled && members[r.ID] {
			out = append(out, r)
		}
	}
	return out
}
