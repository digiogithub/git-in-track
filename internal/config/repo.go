package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// Sentinel errors of repository registration.
var (
	// ErrDuplicateRepo reports a path that is already registered.
	ErrDuplicateRepo = errors.New("repository already registered")
	// ErrNotGitRepo reports a folder that is not a git working tree.
	ErrNotGitRepo = errors.New("not a git repository")
	// ErrNotDir reports a path that is not a directory.
	ErrNotDir = errors.New("not a directory")
	// ErrNoBacklog reports a repository with no discoverable documentation
	// folder, that is one with no .pmngr/project.yaml and no team.yaml.
	ErrNoBacklog = errors.New("no .pmngr/project.yaml or team.yaml found")
)

// TeamFileName is the discovery marker of a team repository (R-TEAM-LOC-1).
const TeamFileName = "team.yaml"

// detectDepth bounds the search for a documentation folder. Four levels is
// deep enough for docs/, packages/<x>/docs/ and friends, and shallow enough to
// stay instant on a large working tree.
const detectDepth = 4

// preferredDocsFolder is the folder name the detector picks when several
// candidates are equally deep.
const preferredDocsFolder = "docs"

// AddOptions tunes AddRepoWithOptions.
type AddOptions struct {
	// Role forces the role instead of detecting it from team.yaml.
	Role Role
	// DocsFolder forces the documentation folder, relative to the repository
	// root, instead of detecting it.
	DocsFolder string
	// DocsFolders forces the whole list of declared documentation folders,
	// instead of recording every candidate detection found. The first entry is
	// the primary one when DocsFolder is empty.
	DocsFolders []string
	// NoGit registers a folder that is not a git working tree.
	NoGit bool
	// Workspace is the workspace the repository joins; empty means the active
	// one. It is created when it does not exist.
	Workspace string
	// ID forces the repository id instead of deriving it from the folder name.
	ID string
	// Env resolves ~ in the path; it defaults to the process environment.
	Env Reader
}

// AddRepo registers a repository with the role and documentation folder given,
// detecting whatever is left empty. It is the shape docs/07 section 4.2
// describes; AddRepoWithOptions carries the remaining flags.
func (c *Config) AddRepo(repoPath, role, docsFolder string) (Repo, error) {
	return c.AddRepoWithOptions(repoPath, AddOptions{Role: Role(role), DocsFolder: docsFolder})
}

// AddRepoWithOptions resolves the path, verifies it is a git working tree,
// detects the documentation folder and the role, assigns a stable id and
// appends the registration to the active workspace.
func (c *Config) AddRepoWithOptions(repoPath string, opts AddOptions) (Repo, error) {
	env := opts.Env
	if env == nil {
		env = Env()
	}
	abs, err := Expand(repoPath, env)
	if err != nil {
		return Repo{}, fmt.Errorf("add %s: %w", repoPath, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Repo{}, fmt.Errorf("add %s: %w", repoPath, err)
	}
	if !info.IsDir() {
		return Repo{}, fmt.Errorf("add %s: %w", abs, ErrNotDir)
	}
	if !opts.NoGit && !IsGitRepo(abs) {
		return Repo{}, fmt.Errorf("add %s: %w (pass --no-git to register it anyway)", abs, ErrNotGitRepo)
	}
	for _, existing := range c.Repos {
		if canonical(existing.Path) == canonical(abs) {
			return Repo{}, fmt.Errorf("add %s: %w as %q", abs, ErrDuplicateRepo, existing.ID)
		}
	}

	det := Detect(abs)
	declared := cleanFolders(opts.DocsFolders)
	docs := strings.TrimSpace(opts.DocsFolder)
	switch {
	case docs != "":
		docs = path.Clean(filepath.ToSlash(docs))
	case len(declared) > 0:
		docs = declared[0]
	case det.DocsFolder != "":
		docs = det.DocsFolder
	default:
		docs = preferredDocsFolder
	}
	// Only the primary folder is declared automatically. Detection offers the
	// deeper candidates it found (Detection.Candidates) but declaring one is a
	// deliberate act: auto-declaring every `.pmngr/` a working tree happens to
	// carry is exactly the unbounded discovery ADR-018 replaced — a repository
	// with test fixtures under it would import them as projects.
	declared = withFolder(declared, docs)

	role := opts.Role
	if role == "" {
		role = det.Role
	}
	if !role.Valid() {
		return Repo{}, fmt.Errorf("add %s: unknown role %q: use project or team", abs, role)
	}

	repo := Repo{
		ID:          c.freeID(opts.ID, abs),
		Path:        abs,
		Role:        role,
		DocsFolder:  docs,
		DocsFolders: declared,
		Enabled:     true,
	}
	c.Repos = append(c.Repos, repo)

	workspace := opts.Workspace
	if workspace == "" {
		workspace = c.ActiveWorkspace()
	}
	c.EnsureWorkspace(workspace)
	c.attach(workspace, repo.ID)
	return repo, nil
}

// attach adds a repository id to a workspace that lists its members. A
// workspace with an empty list stands for every registered repository, so it is
// left alone.
func (c *Config) attach(workspace, id string) {
	for i, w := range c.Workspaces {
		if w.Name != workspace || len(w.Repos) == 0 {
			continue
		}
		for _, existing := range w.Repos {
			if existing == id {
				return
			}
		}
		c.Workspaces[i].Repos = append(c.Workspaces[i].Repos, id)
		return
	}
}

// freeID returns a repository id that is not taken yet: the slug of the folder
// name, or the requested one, with "-2", "-3" appended until it is free.
func (c *Config) freeID(want, repoPath string) string {
	base := strings.TrimSpace(want)
	if base == "" {
		base = core.Slugify(filepath.Base(repoPath))
	}
	if base == "" {
		base = "repo"
	}
	taken := make(map[string]bool, len(c.Repos))
	for _, r := range c.Repos {
		taken[r.ID] = true
	}
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// DeclaredDocsFolders returns every documentation folder this registration
// declares, in preference order and with the primary one first. It is what a
// caller hands to core.DiscoverProjectsWith so that a folder deeper than the
// bounded rule is still found (ADR-018).
func (r Repo) DeclaredDocsFolders() []string {
	return withFolder(cleanFolders(r.DocsFolders), strings.TrimSpace(r.DocsFolder))
}

// cleanFolders normalizes repository-relative folders and drops the empty and
// the duplicated ones, keeping the given order.
func cleanFolders(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		folder := strings.TrimSpace(raw)
		if folder == "" {
			continue
		}
		folder = path.Clean(filepath.ToSlash(folder))
		if folder == ".." || strings.HasPrefix(folder, "../") || path.IsAbs(folder) || seen[folder] {
			continue
		}
		seen[folder] = true
		out = append(out, folder)
	}
	return out
}

// withFolder puts folder at the head of the list, without duplicating it.
func withFolder(folders []string, folder string) []string {
	head := cleanFolders([]string{folder})
	if len(head) == 0 {
		return folders
	}
	out := head
	for _, existing := range folders {
		if existing != head[0] {
			out = append(out, existing)
		}
	}
	return out
}

// Detection is what Detect found out about a folder.
type Detection struct {
	// Path is the absolute folder that was inspected.
	Path string
	// Git reports whether the folder is a git working tree.
	Git bool
	// Team reports whether a team.yaml sits at the root (R-TEAM-LOC-1).
	Team bool
	// Role is the role the markers imply.
	Role Role
	// DocsFolder is the repository-relative documentation folder holding
	// .pmngr/project.yaml, empty when none was found.
	DocsFolder string
	// Candidates lists every documentation folder found, in preference order.
	Candidates []string
}

// Detect inspects a folder: is it a git working tree, is it a team repository,
// and where does its backlog live.
func Detect(repoPath string) Detection {
	det := Detection{Path: repoPath, Git: IsGitRepo(repoPath), Role: RoleProject}
	if _, err := os.Stat(filepath.Join(repoPath, TeamFileName)); err == nil {
		det.Team = true
		det.Role = RoleTeam
	}
	det.Candidates = DocsCandidates(repoPath)
	if len(det.Candidates) > 0 {
		det.DocsFolder = det.Candidates[0]
	}
	return det
}

// IsGitRepo reports whether a folder is a git working tree: it holds a .git
// directory, or a .git file pointing at one (a worktree or a submodule).
func IsGitRepo(repoPath string) bool {
	_, err := os.Stat(filepath.Join(repoPath, ".git"))
	return err == nil
}

// DocsCandidates returns every documentation folder under a repository that
// holds a .pmngr/project.yaml, in preference order: `docs` first, then the
// shallowest, then alphabetically. Paths are repository-relative with forward
// slashes; "." is the repository root itself.
func DocsCandidates(repoPath string) []string {
	var found []string
	var walk func(dir, rel string, depth int)
	walk = func(dir, rel string, depth int) {
		if _, err := os.Stat(filepath.Join(dir, core.BacklogDirName, core.ProjectFileName)); err == nil {
			found = append(found, rel)
		}
		if depth >= detectDepth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() || skipDir(e.Name()) {
				continue
			}
			child := rel + "/" + e.Name()
			if rel == "." {
				child = e.Name()
			}
			walk(filepath.Join(dir, e.Name()), child, depth+1)
		}
	}
	walk(repoPath, ".", 0)

	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if pa, pb := a == preferredDocsFolder, b == preferredDocsFolder; pa != pb {
			return pa
		}
		if da, db := strings.Count(a, "/"), strings.Count(b, "/"); da != db {
			return da < db
		}
		return a < b
	})
	return found
}

// skipDir reports whether the detector refuses to descend into a folder.
func skipDir(name string) bool {
	switch name {
	case core.BacklogDirName, "node_modules", "vendor", "dist", "build", "target":
		return true
	}
	return strings.HasPrefix(name, ".")
}
