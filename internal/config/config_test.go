package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	t.Parallel()

	c := Default()
	if c.Version != SchemaVersion {
		t.Errorf("version = %d, want %d", c.Version, SchemaVersion)
	}
	if c.Server.Bind != "127.0.0.1" || c.Server.Port != 7317 || !c.Server.OpenBrowser {
		t.Errorf("server = %+v", c.Server)
	}
	if c.Git.Backend != BackendAuto || c.Git.MessageTemplate == "" {
		t.Errorf("git = %+v", c.Git)
	}
	if !c.Index.Watch || c.Index.Debounce != DefaultDebounce {
		t.Errorf("index = %+v", c.Index)
	}
	if c.MCP.Enabled || c.MCP.AllowWrite {
		t.Errorf("mcp = %+v, want the MCP server off by default", c.MCP)
	}
	if c.Log.Level != "info" || c.Log.Format != "text" {
		t.Errorf("log = %+v", c.Log)
	}
	if c.ActiveWorkspace() != DefaultWorkspaceName {
		t.Errorf("active workspace = %q", c.ActiveWorkspace())
	}
	if err := c.Validate(); err != nil {
		t.Errorf("the defaults do not validate: %v", err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")

	want := Default()
	want.Server.Token = "s3cret"
	want.Server.IdleTimeout = 30 * time.Minute
	want.Git.CommitOnSave = true
	want.Git.AuthorName = "Jose Ruiz"
	want.Git.AuthorEmail = "jose@example.com"
	want.MCP = MCP{Enabled: true, AllowWrite: true}
	want.Index.CacheDir = filepath.Join(dir, "cache")
	want.Repos = []Repo{{ID: "acme", Path: filepath.Join(dir, "acme"), Role: RoleProject, DocsFolder: "docs", Enabled: true}}
	want.Workspaces = []Workspace{{Name: DefaultWorkspaceName, Repos: []string{"acme"}}}

	if err := Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Server != want.Server || got.Git != want.Git || got.Index != want.Index || got.MCP != want.MCP || got.Log != want.Log {
		t.Errorf("round trip lost a section:\ngot  %+v\nwant %+v", got, want)
	}
	if len(got.Repos) != 1 || got.Repos[0] != want.Repos[0] {
		t.Errorf("repos = %+v, want %+v", got.Repos, want.Repos)
	}
	if len(got.Workspaces) != 1 || got.Workspaces[0].Name != DefaultWorkspaceName {
		t.Errorf("workspaces = %+v", got.Workspaces)
	}
}

func TestLoadMissingFileYieldsDefaults(t *testing.T) {
	t.Parallel()

	got, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Server.Port != DefaultPort {
		t.Errorf("port = %d, want the default %d", got.Server.Port, DefaultPort)
	}
}

func TestParsePartialFileKeepsDefaults(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte("version: 1\nserver:\n  port: 9000\nrepos:\n  - id: demo\n    path: /tmp/demo\n    role: project\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Server.Port != 9000 {
		t.Errorf("port = %d, want 9000", got.Server.Port)
	}
	if got.Server.Bind != DefaultBind {
		t.Errorf("bind = %q, want the default %q", got.Server.Bind, DefaultBind)
	}
	if !got.Server.OpenBrowser || !got.Index.Watch {
		t.Errorf("a partial file dropped a boolean default: %+v %+v", got.Server, got.Index)
	}
	if len(got.Repos) != 1 || !got.Repos[0].Enabled {
		t.Errorf("repos = %+v, want enabled to default to true", got.Repos)
	}
}

func TestParseDisabledRepoStaysDisabled(t *testing.T) {
	t.Parallel()

	got, err := Parse([]byte("repos:\n  - id: demo\n    path: /tmp/demo\n    role: project\n    enabled: false\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Repos[0].Enabled {
		t.Error("enabled: false was ignored")
	}
	if repos := got.WorkspaceRepos(""); len(repos) != 0 {
		t.Errorf("workspace repos = %+v, want the disabled repository skipped", repos)
	}
}

func TestParseRejectsBrokenYAML(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]byte("server: [1, 2\n")); err == nil {
		t.Error("broken YAML was accepted")
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	t.Parallel()

	c := Default()
	c.Version = 7
	c.Server.Bind = ""
	c.Server.Port = 70000
	c.Git.Backend = "magic"
	c.Log.Level = "loud"
	c.Log.Format = "xml"
	c.Repos = []Repo{
		{ID: "", Path: "relative/path", Role: "boss"},
		{ID: "dup", Path: "/tmp/a", Role: RoleProject},
		{ID: "dup", Path: "/tmp/b", Role: RoleTeam},
	}
	c.Workspaces = []Workspace{{Name: DefaultWorkspaceName, Repos: []string{"ghost"}}}

	err := c.Validate()
	if err == nil {
		t.Fatal("an invalid configuration validated")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("errors.Is(err, ErrInvalid) = false for %v", err)
	}
	var fields FieldErrors
	if !errors.As(err, &fields) {
		t.Fatalf("errors.As(err, &FieldErrors) = false for %v", err)
	}
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		seen[f.Field] = true
	}
	for _, want := range []string{
		"version", "server.bind", "server.port", "git.backend", "log.level", "log.format",
		"repos[0].id", "repos[0].path", "repos[0].role", "repos[2].id", "workspaces[0].repos",
	} {
		if !seen[want] {
			t.Errorf("no finding for %q; got %v", want, err)
		}
	}
}

func TestValidateAcceptsAGoodConfiguration(t *testing.T) {
	t.Parallel()

	c := Default()
	c.Repos = []Repo{{ID: "demo", Path: filepath.FromSlash("/tmp/demo"), Role: RoleProject, DocsFolder: "docs", Enabled: true}}
	c.Workspaces = []Workspace{{Name: DefaultWorkspaceName, Repos: []string{"demo"}}}
	if err := c.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestRemoveRepo(t *testing.T) {
	t.Parallel()

	c := Default()
	c.Repos = []Repo{
		{ID: "a", Path: "/tmp/a", Role: RoleProject, Enabled: true},
		{ID: "b", Path: "/tmp/b", Role: RoleTeam, Enabled: true},
	}
	c.Workspaces = []Workspace{{Name: DefaultWorkspaceName, Repos: []string{"a", "b"}}}

	if _, ok := c.RemoveRepo("ghost"); ok {
		t.Error("removing an unknown id reported success")
	}
	removed, ok := c.RemoveRepo("a")
	if !ok || removed.ID != "a" {
		t.Fatalf("remove a = %+v, %v", removed, ok)
	}
	if len(c.Repos) != 1 || c.Repos[0].ID != "b" {
		t.Errorf("repos = %+v", c.Repos)
	}
	if got := c.Workspaces[0].Repos; len(got) != 1 || got[0] != "b" {
		t.Errorf("workspace members = %v", got)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("validate after removal: %v", err)
	}
}

func TestWorkspaceReposFallsBackToEveryRepository(t *testing.T) {
	t.Parallel()

	c := Default()
	c.Repos = []Repo{{ID: "a", Path: "/tmp/a", Role: RoleProject, Enabled: true}}
	if got := c.WorkspaceRepos(""); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("workspace repos = %+v, want every repository when the workspace lists none", got)
	}
	if got := c.WorkspaceRepos("unknown"); len(got) != 1 {
		t.Errorf("unknown workspace = %+v", got)
	}
}

func TestCloneIsDeep(t *testing.T) {
	t.Parallel()

	c := Default()
	c.Repos = []Repo{{ID: "a", Path: "/tmp/a", Role: RoleProject, Enabled: true}}
	c.Workspaces = []Workspace{{Name: "w", Repos: []string{"a"}}}

	clone := c.Clone()
	clone.Repos[0].ID = "changed"
	clone.Workspaces[0].Repos[0] = "changed"
	if c.Repos[0].ID != "a" || c.Workspaces[0].Repos[0] != "a" {
		t.Error("Clone shares memory with the original")
	}
}

func TestSaveRefusesEmptyPath(t *testing.T) {
	t.Parallel()

	if err := Save("", Default()); err == nil {
		t.Error("saving to an empty path succeeded")
	}
	if err := Save(filepath.Join(t.TempDir(), "c.yaml"), nil); err == nil {
		t.Error("saving a nil configuration succeeded")
	}
}

func TestSavedFileCarriesAHeader(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, Default()); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(data), "# gintrack configuration") {
		t.Errorf("the file has no header:\n%s", data)
	}
}
