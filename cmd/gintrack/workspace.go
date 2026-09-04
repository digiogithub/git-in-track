package main

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// repoView is one registered repository opened and indexed on its own. It is
// what the commands that report per repository — ls, index, doctor — work with.
type repoView struct {
	Repo     config.Repo
	FS       *osfs.FS
	Projects []core.ProjectRef
	Index    *core.Index
	Stats    core.IndexStats
}

// openRepo opens a registered repository, discovers its projects and builds its
// index.
func openRepo(ctx context.Context, repo config.Repo, full bool) (*repoView, error) {
	fsys, err := osfs.New(repo.Path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", repo.ID, err)
	}
	projects, err := core.DiscoverProjects(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("discover the projects of %s: %w", repo.ID, err)
	}
	index := core.NewIndex(fsys, projects)
	stats, err := index.Build(ctx, full)
	if err != nil {
		return nil, fmt.Errorf("index %s: %w", repo.ID, err)
	}
	return &repoView{Repo: repo, FS: fsys, Projects: projects, Index: index, Stats: stats}, nil
}

// Keys returns the project keys the repository holds.
func (r *repoView) Keys() []string {
	keys := make([]string, 0, len(r.Projects))
	for _, p := range r.Projects {
		keys = append(keys, string(p.Key))
	}
	return keys
}

// projectView is one project backlog inside the vault, with the repository it
// belongs to.
type projectView struct {
	Ref  core.ProjectRef
	Repo config.Repo
}

// vault is every repository of a workspace mounted side by side and indexed
// together, so that a query sorts and paginates across all of them at once.
type vault struct {
	Workspace string
	Repos     []config.Repo
	FS        *mountFS
	Index     *core.Index
	Stats     core.IndexStats
	Projects  []projectView

	// dry is the in-memory overlay a --dry-run invocation writes to. One vault
	// keeps one overlay, so that a command touching two items reports both.
	dry *overlayFS
}

// openVault mounts the repositories of a workspace and builds one index over
// them.
func openVault(ctx context.Context, res *config.Resolution, full bool) (*vault, error) {
	repos := res.Config.WorkspaceRepos(res.Workspace)
	if len(repos) == 0 {
		return nil, notFoundf("no repository is registered in workspace %q: run `gintrack add <path>`", res.Workspace)
	}
	mounts := newMountFS()
	byMount := make(map[string]config.Repo, len(repos))
	for _, repo := range repos {
		fsys, err := osfs.New(repo.Path)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", repo.ID, err)
		}
		if err := mounts.mount(repo.ID, fsys); err != nil {
			return nil, fmt.Errorf("open %s: %w", repo.ID, err)
		}
		byMount[repo.ID] = repo
	}
	refs, err := core.DiscoverProjects(mounts, ".")
	if err != nil {
		return nil, fmt.Errorf("discover the projects: %w", err)
	}
	index := core.NewIndex(mounts, refs)
	stats, err := index.Build(ctx, full)
	if err != nil {
		return nil, fmt.Errorf("index the workspace: %w", err)
	}

	v := &vault{Workspace: res.Workspace, Repos: repos, FS: mounts, Index: index, Stats: stats}
	for _, ref := range refs {
		mount, _ := mountNameOf(ref.DocsPath)
		v.Projects = append(v.Projects, projectView{Ref: ref, Repo: byMount[mount]})
	}
	return v, nil
}

// project returns the project with this key.
func (v *vault) project(key core.ProjectKey) (projectView, error) {
	for _, p := range v.Projects {
		if p.Ref.Key == key {
			return p, nil
		}
	}
	return projectView{}, notFoundf("no project %q in workspace %q", key, v.Workspace)
}

// only returns the single registered project, so that a command may omit
// --project when there is nothing to disambiguate.
func (v *vault) only() (projectView, error) {
	switch len(v.Projects) {
	case 0:
		return projectView{}, notFoundf("no project is registered in workspace %q", v.Workspace)
	case 1:
		return v.Projects[0], nil
	default:
		return projectView{}, usagef("--project is required: the workspace holds %s", strings.Join(v.keys(), ", "))
	}
}

// keys returns every project key of the vault.
func (v *vault) keys() []string {
	out := make([]string, 0, len(v.Projects))
	for _, p := range v.Projects {
		out = append(out, string(p.Ref.Key))
	}
	return out
}

// projectOf returns the project an item id belongs to.
func (v *vault) projectOf(id core.ItemID) (projectView, error) {
	key, _, _, err := core.ParseItemID(string(id))
	if err != nil {
		return projectView{}, failf(exitValidation, "%q is not an item id: %v", id, err)
	}
	return v.project(key)
}

// store returns a file store writing into a project of the vault.
func (v *vault) store(p projectView) (*core.FileStore, error) {
	if p.Ref.Config == nil {
		return nil, failf(exitValidation, "%s: %s is invalid, fix it before writing", p.Ref.Key, p.Ref.ConfigPath)
	}
	return core.NewStore(v.FS, p.Ref.BacklogPath, p.Ref.Config), nil
}

// repoPath splits a vault path into the repository that holds it and the path
// inside that repository, which is what users and the data model call a path.
func repoPath(p string) (repo, rel string) {
	name, rest := mountNameOf(p)
	return name, path.Clean(rest)
}

// displayPath renders a vault path as "<repo>:<path inside the repository>".
func displayPath(p string) string {
	repo, rel := repoPath(p)
	if repo == "" {
		return rel
	}
	return repo + ":" + rel
}
