package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/core/osfs"
)

// teamFixtureRoot is the team repository the workspace tests open next to the
// project fixture.
const teamFixtureRoot = "../../testdata/fixtures/team-basic"

// openFixture mounts a fixture directory read-only, the way the companion does.
func openFixture(t *testing.T, root string) *Vault {
	t.Helper()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolve %s: %v", root, err)
	}
	fsys, err := osfs.New(abs)
	if err != nil {
		t.Fatalf("mount %s: %v", abs, err)
	}
	v, err := Open(fsys, filepath.Base(abs))
	if err != nil {
		t.Fatalf("open %s: %v", abs, err)
	}
	return v
}

// loadFixture pushes a fixture directory into an in-memory vault of a
// workspace, which is exactly what the browser does with a picked folder.
func loadFixture(t *testing.T, w *Workspace, vaultID, role, root string) {
	t.Helper()
	type file struct {
		Path string `json:"path"`
		Text string `json:"text"`
	}
	var files []file
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(p) //nolint:gosec // fixture paths come from the walk itself
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		files = append(files, file{Path: filepath.ToSlash(rel), Text: string(data)})
		return nil
	})
	if err != nil {
		t.Fatalf("read fixture %s: %v", root, err)
	}
	wsCall(t, w, "workspace.mount", map[string]any{
		"vaultId": vaultID, "role": role, "rootLabel": filepath.Base(root),
	})
	wsCall(t, w, "vault.load", map[string]any{
		"vaultId": vaultID, "files": files, "rootLabel": filepath.Base(root),
	})
}

// wsCall runs one method against the workspace and fails on an error envelope.
func wsCall(t *testing.T, w *Workspace, method string, params any) json.RawMessage {
	t.Helper()
	encoded := "null"
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("encode params for %s: %v", method, err)
		}
		encoded = string(data)
	}
	var env envelope
	if err := json.Unmarshal([]byte(w.Call(method, encoded)), &env); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", method, err)
	}
	if !env.OK {
		t.Fatalf("%s: %s: %s", method, env.Error.Code, env.Error.Message)
	}
	return env.Result
}

// nativeWorkspace attaches the two fixtures the way the companion process does.
func nativeWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w := NewWorkspace()
	if _, err := w.Attach("demo-team", RoleTeam, openFixture(t, teamFixtureRoot)); err != nil {
		t.Fatalf("attach the team repository: %v", err)
	}
	if _, err := w.Attach("demo", RoleProject, openFixture(t, fixtureRoot)); err != nil {
		t.Fatalf("attach the project repository: %v", err)
	}
	return w
}

// browserWorkspace pushes the same two fixtures in as the browser does.
func browserWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w := NewWorkspace()
	loadFixture(t, w, "demo-team", RoleTeam, teamFixtureRoot)
	loadFixture(t, w, "demo", RoleProject, fixtureRoot)
	return w
}

// modes runs a subtest against both operating modes, which is what "the
// team-basic fixture loads end to end in both operating modes" means.
func modes(t *testing.T, run func(t *testing.T, w *Workspace)) {
	t.Helper()
	t.Run("companion", func(t *testing.T) { run(t, nativeWorkspace(t)) })
	t.Run("browser", func(t *testing.T) { run(t, browserWorkspace(t)) })
}

func TestWorkspaceTeamFixture(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		summary := decode[teamSummary](t, wsCall(t, w, "team.get", nil))

		if summary.Key != "DEMO-TEAM" {
			t.Errorf("key = %q, want DEMO-TEAM", summary.Key)
		}
		if summary.Name != "Demo Delivery Team" {
			t.Errorf("name = %q", summary.Name)
		}
		if len(summary.Members) != 4 {
			t.Fatalf("members = %d, want 4", len(summary.Members))
		}
		if summary.Members[0].Handle != "jose" || !summary.Members[0].Active {
			t.Errorf("first member = %+v", summary.Members[0])
		}
		for _, m := range summary.Members {
			if m.Handle == "laura" && m.Active {
				t.Error("laura declares active: false and must not be reported active")
			}
			if m.Handle == "bot-ci" && !m.Active {
				t.Error("a member that omits `active` is active (R-MEM-3)")
			}
		}
		for _, d := range summary.Diagnostics {
			if d.Severity == core.SeverityError {
				t.Errorf("unexpected error diagnostic: %s", d)
			}
		}

		if len(summary.Projects) != 2 {
			t.Fatalf("projects = %d, want 2", len(summary.Projects))
		}
		byKey := map[core.ProjectKey]teamProjectSummary{}
		for _, p := range summary.Projects {
			byKey[p.Key] = p
		}
		demo, ok := byKey["DEMO"]
		if !ok {
			t.Fatal("DEMO is missing from the project list")
		}
		if !demo.Cloned {
			t.Error("DEMO is open in this workspace and must be marked cloned")
		}
		if demo.VaultID != "demo" {
			t.Errorf("DEMO vaultId = %q, want demo", demo.VaultID)
		}
		web, ok := byKey["WEB"]
		if !ok {
			t.Fatal("WEB is missing from the project list")
		}
		if web.Cloned {
			t.Error("WEB is not open in this workspace and must be marked not cloned")
		}
		if web.Name != "Marketing Website" || web.Repo == "" {
			t.Errorf("a remote project must still carry its metadata: %+v", web)
		}
	})
}

func TestWorkspaceProjectsAcrossRepositories(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		projects := decode[[]projectSummary](t, wsCall(t, w, "project.list", nil))
		if len(projects) != 1 {
			t.Fatalf("projects = %d, want the one project of the clone", len(projects))
		}
		if projects[0].Key != "DEMO" || projects[0].VaultID != "demo" {
			t.Errorf("project = %+v, want DEMO served by the demo repository", projects[0])
		}

		vaults := decode[workspaceSummary](t, wsCall(t, w, "workspace.list", nil))
		if len(vaults.Vaults) != 2 {
			t.Fatalf("vaults = %d, want 2", len(vaults.Vaults))
		}
		if vaults.Team == nil || vaults.Team.Key != "DEMO-TEAM" {
			t.Error("workspace.list must report the team repository")
		}
		for _, d := range vaults.Diagnostics {
			if d.Severity == core.SeverityError {
				t.Errorf("unexpected workspace diagnostic: %s", d)
			}
		}
	})
}

func TestWorkspaceResolveRef(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		tests := []struct {
			name         string
			ref          string
			wantCloned   bool
			wantDeclared bool
			wantFound    bool
		}{
			{name: "cloned project", ref: "DEMO/DEMO-US-0001", wantCloned: true, wantDeclared: true, wantFound: true},
			{name: "cloned project, unknown item", ref: "DEMO/DEMO-US-9999", wantCloned: true, wantDeclared: true},
			{name: "declared but not cloned", ref: "WEB/WEB-US-0031", wantDeclared: true},
			{name: "undeclared project", ref: "GONE/GONE-US-0001"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := decode[refResolution](t, wsCall(t, w, "ref.resolve", map[string]string{"ref": tt.ref}))
				if got.Cloned != tt.wantCloned {
					t.Errorf("cloned = %v, want %v (%s)", got.Cloned, tt.wantCloned, got.Reason)
				}
				if got.Declared != tt.wantDeclared {
					t.Errorf("declared = %v, want %v", got.Declared, tt.wantDeclared)
				}
				if (got.Found != nil) != tt.wantFound {
					t.Errorf("found = %v, want %v", got.Found, tt.wantFound)
				}
				if !tt.wantFound && got.Reason == "" {
					t.Error("an unresolved reference must explain itself")
				}
				if tt.wantFound && got.Found.Title == "" {
					t.Error("a resolved reference must carry the item title")
				}
			})
		}

		env := rawWorkspaceCall(t, w, "ref.resolve", map[string]string{"ref": "not-a-ref"})
		if env.OK || env.Error.Code != "invalid_request" {
			t.Errorf("a malformed reference must fail with invalid_request, got %+v", env)
		}
	})
}

func TestWorkspaceSearchSpansEveryRepository(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		hits := decode[[]searchHit](t, wsCall(t, w, "search", map[string]any{"q": "checkout", "limit": 20}))
		if len(hits) == 0 {
			t.Fatal("search found nothing in the project repository")
		}
		for _, h := range hits {
			if h.VaultID == "" {
				t.Errorf("hit %s does not say which repository it came from", h.Path)
			}
		}

		team := decode[[]searchHit](t, wsCall(t, w, "search", map[string]any{"q": "done", "limit": 20}))
		sources := map[string]bool{}
		for _, h := range team {
			sources[h.VaultID] = true
		}
		if !sources["demo-team"] {
			t.Errorf("search must reach the team knowledge base; sources were %v", sources)
		}
		for _, h := range team {
			if h.VaultID == "demo-team" && h.Project != "DEMO-TEAM" {
				t.Errorf("a team knowledge-base hit must be labelled with the team key, got %q", h.Project)
			}
		}

		scoped := decode[[]searchHit](t, wsCall(t, w,
			"search", map[string]any{"q": "checkout", "project": "DEMO", "limit": 20}))
		for _, h := range scoped {
			if h.Project != "DEMO" {
				t.Errorf("a scoped search returned a hit from %q", h.Project)
			}
		}
	})
}

func TestWorkspaceTeamKnowledgeBase(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		tree := decode[[]kbNode](t, wsCall(t, w, "kb.tree", map[string]any{"project": "DEMO-TEAM"}))
		names := map[string]bool{}
		for _, n := range tree {
			names[n.Name] = true
		}
		for _, want := range []string{"decisions", "index.md", "ways-of-working"} {
			if !names[want] {
				t.Errorf("the team knowledge base is missing %q; got %v", want, names)
			}
		}

		page := decode[kbPageResult](t, wsCall(t, w,
			"kb.page", map[string]any{"vaultId": "demo-team", "path": "knowledge/index.md"}))
		if !strings.Contains(page.Body, "DEMO/DEMO-US-0001") {
			t.Errorf("the team index page did not load: %q", page.Body)
		}
	})
}

func TestWorkspaceRoutesItemsToTheirRepository(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		item := decode[core.Item](t, wsCall(t, w, "item.get", map[string]string{"id": "DEMO-US-0001"}))
		if item.ID != "DEMO-US-0001" {
			t.Fatalf("item = %+v", item)
		}

		env := rawWorkspaceCall(t, w, "item.get", map[string]string{"id": "WEB-US-0031"})
		if env.OK {
			t.Error("an item of a project nobody cloned cannot be read")
		}
	})
}

func TestWorkspaceDuplicateProjectKey(t *testing.T) {
	w := NewWorkspace()
	if _, err := w.Attach("one", RoleProject, openFixture(t, fixtureRoot)); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := w.Attach("two", RoleProject, openFixture(t, fixtureRoot)); err != nil {
		t.Fatalf("attach: %v", err)
	}
	diags := w.Diagnostics()
	found := false
	for _, d := range diags {
		if d.Code == core.CodeTeamKeyDup {
			found = true
		}
	}
	if !found {
		t.Fatalf("two repositories serving DEMO must be reported, got %v", diags)
	}
}

func TestWorkspaceUnmount(t *testing.T) {
	w := nativeWorkspace(t)
	wsCall(t, w, "workspace.unmount", map[string]string{"vaultId": "demo"})
	summary := decode[teamSummary](t, wsCall(t, w, "team.get", nil))
	for _, p := range summary.Projects {
		if p.Cloned {
			t.Errorf("project %s must be remote once its repository is unmounted", p.Key)
		}
	}
	if _, ok := w.Lookup("demo"); ok {
		t.Error("the repository is still mounted")
	}
}

// rawWorkspaceCall runs one method and returns the envelope, error or not.
func rawWorkspaceCall(t *testing.T, w *Workspace, method string, params any) envelope {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode params for %s: %v", method, err)
	}
	var env envelope
	if err := json.Unmarshal([]byte(w.Call(method, string(data))), &env); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", method, err)
	}
	return env
}
