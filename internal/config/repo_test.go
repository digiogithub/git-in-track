package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newRepoDir builds a folder tree for the detector: a fake .git marker plus the
// files listed, each relative to the folder.
func newRepoDir(t *testing.T, git bool, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	if git {
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("create .git: %v", err)
		}
	}
	for _, rel := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("create %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte("key: DEMO\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

func TestAddRepoDetectsTheDocsFolder(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "docs/.pmngr/project.yaml")
	c := Default()
	repo, err := c.AddRepo(dir, "", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if repo.DocsFolder != "docs" {
		t.Errorf("docs folder = %q, want docs", repo.DocsFolder)
	}
	if repo.Role != RoleProject {
		t.Errorf("role = %q, want project", repo.Role)
	}
	if !repo.Enabled {
		t.Error("a fresh registration is disabled")
	}
	if repo.Path != dir {
		t.Errorf("path = %q, want the absolute %q", repo.Path, dir)
	}
	if repo.ID == "" {
		t.Error("no id was assigned")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestAddRepoPrefersDocsOverOtherCandidates(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true,
		"apps/api/.pmngr/project.yaml",
		"docs/.pmngr/project.yaml",
		"knowledge/.pmngr/project.yaml",
	)
	if got := DocsCandidates(dir); len(got) != 3 || got[0] != "docs" {
		t.Errorf("candidates = %v, want docs first", got)
	}

	c := Default()
	repo, err := c.AddRepo(dir, "", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if repo.DocsFolder != "docs" {
		t.Errorf("docs folder = %q, want docs", repo.DocsFolder)
	}
}

func TestAddRepoDetectsANestedDocsFolder(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "packages/web/documentation/.pmngr/project.yaml")
	c := Default()
	repo, err := c.AddRepo(dir, "", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if repo.DocsFolder != "packages/web/documentation" {
		t.Errorf("docs folder = %q", repo.DocsFolder)
	}
}

func TestDocsCandidatesStopsAtTheDepthLimit(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "a/b/c/d/e/.pmngr/project.yaml")
	if got := DocsCandidates(dir); len(got) != 0 {
		t.Errorf("candidates = %v, want nothing beyond depth %d", got, detectDepth)
	}
}

func TestAddRepoDetectsATeamRepository(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "team.yaml", "knowledge/.pmngr/project.yaml")
	c := Default()
	repo, err := c.AddRepo(dir, "", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if repo.Role != RoleTeam {
		t.Errorf("role = %q, want team", repo.Role)
	}
	if repo.DocsFolder != "knowledge" {
		t.Errorf("docs folder = %q, want knowledge", repo.DocsFolder)
	}
}

func TestAddRepoHonorsExplicitRoleAndDocs(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "docs/.pmngr/project.yaml")
	c := Default()
	repo, err := c.AddRepo(dir, string(RoleTeam), "knowledge")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if repo.Role != RoleTeam || repo.DocsFolder != "knowledge" {
		t.Errorf("repo = %+v, want the explicit role and docs folder", repo)
	}
}

func TestAddRepoRejectsAnUnknownRole(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "docs/.pmngr/project.yaml")
	if _, err := Default().AddRepo(dir, "boss", ""); err == nil {
		t.Error("an unknown role was accepted")
	}
}

func TestAddRepoRequiresAGitRepository(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, false, "docs/.pmngr/project.yaml")
	c := Default()
	_, err := c.AddRepo(dir, "", "")
	if !errors.Is(err, ErrNotGitRepo) {
		t.Fatalf("add = %v, want ErrNotGitRepo", err)
	}
	if len(c.Repos) != 0 {
		t.Errorf("a refused repository was registered: %+v", c.Repos)
	}

	repo, err := c.AddRepoWithOptions(dir, AddOptions{NoGit: true})
	if err != nil {
		t.Fatalf("add --no-git: %v", err)
	}
	if repo.DocsFolder != "docs" {
		t.Errorf("docs folder = %q", repo.DocsFolder)
	}
}

func TestAddRepoAcceptsAGitFile(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, false, "docs/.pmngr/project.yaml", ".git")
	if !IsGitRepo(dir) {
		t.Fatal("a .git file was not recognized as a working tree")
	}
	if _, err := Default().AddRepo(dir, "", ""); err != nil {
		t.Errorf("add: %v", err)
	}
}

func TestAddRepoRefusesDuplicates(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "docs/.pmngr/project.yaml")
	c := Default()
	if _, err := c.AddRepo(dir, "", ""); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err := c.AddRepo(filepath.Join(dir, "docs", ".."), "", "")
	if !errors.Is(err, ErrDuplicateRepo) {
		t.Fatalf("second add = %v, want ErrDuplicateRepo", err)
	}
	if len(c.Repos) != 1 {
		t.Errorf("repos = %+v, want the duplicate refused", c.Repos)
	}
}

func TestAddRepoRefusesAFile(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "README.md")
	if _, err := Default().AddRepo(filepath.Join(dir, "README.md"), "", ""); !errors.Is(err, ErrNotDir) {
		t.Errorf("add a file = %v, want ErrNotDir", err)
	}
	if _, err := Default().AddRepo(filepath.Join(dir, "absent"), "", ""); err == nil {
		t.Error("adding a missing folder succeeded")
	}
}

func TestAddRepoDeduplicatesIDs(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	var ids []string
	c := Default()
	for i := range 3 {
		dir := filepath.Join(base, string(rune('a'+i)), "acme-api")
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		repo, err := c.AddRepo(dir, "", "")
		if err != nil {
			t.Fatalf("add %s: %v", dir, err)
		}
		ids = append(ids, repo.ID)
	}
	want := []string{"acme-api", "acme-api-2", "acme-api-3"}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("ids = %v, want %v", ids, want)
			break
		}
	}
	if err := c.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestAddRepoJoinsTheWorkspace(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "docs/.pmngr/project.yaml")
	c := Default()
	c.Workspaces = []Workspace{{Name: DefaultWorkspaceName, Repos: []string{"other"}}}
	c.Repos = []Repo{{ID: "other", Path: filepath.Join(dir, ".."), Role: RoleProject, Enabled: true}}

	repo, err := c.AddRepoWithOptions(dir, AddOptions{Workspace: "oss"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	ws, ok := c.WorkspaceNamed("oss")
	if !ok {
		t.Fatal("the workspace was not created")
	}
	if len(ws.Repos) != 0 {
		t.Errorf("a new workspace should list nothing and mean everything, got %v", ws.Repos)
	}
	if got := c.WorkspaceRepos("oss"); len(got) != 2 {
		t.Errorf("workspace repos = %+v", got)
	}
	if got := c.WorkspaceRepos(DefaultWorkspaceName); len(got) != 1 || got[0].ID != "other" {
		t.Errorf("the default workspace changed: %+v", got)
	}
	_ = repo
}

func TestDetect(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, "team.yaml", "docs/.pmngr/project.yaml")
	det := Detect(dir)
	if !det.Git || !det.Team || det.Role != RoleTeam || det.DocsFolder != "docs" {
		t.Errorf("detection = %+v", det)
	}
}

func TestDetectARootBacklog(t *testing.T) {
	t.Parallel()

	dir := newRepoDir(t, true, ".pmngr/project.yaml")
	det := Detect(dir)
	if det.DocsFolder != "." {
		t.Errorf("docs folder = %q, want the repository root", det.DocsFolder)
	}
}
