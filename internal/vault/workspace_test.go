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
				t.Errorf("a team knowledge-base hit must be labeled with the team key, got %q", h.Project)
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

// secondTeamFixtureRoot is the other team repository of the multi-team tests.
const secondTeamFixtureRoot = "../../testdata/fixtures/team-second"

// twoTeamWorkspace opens both team fixtures next to the project fixture, in
// both operating modes: the workspace GIT-US-0036 made legal.
func twoTeamWorkspace(t *testing.T, browser bool) *Workspace {
	t.Helper()
	if browser {
		w := NewWorkspace()
		loadFixture(t, w, "demo-team", RoleTeam, teamFixtureRoot)
		loadFixture(t, w, "platform-team", RoleTeam, secondTeamFixtureRoot)
		loadFixture(t, w, "demo", RoleProject, fixtureRoot)
		return w
	}
	w := NewWorkspace()
	for _, m := range []struct{ id, role, root string }{
		{"demo-team", RoleTeam, teamFixtureRoot},
		{"platform-team", RoleTeam, secondTeamFixtureRoot},
		{"demo", RoleProject, fixtureRoot},
	} {
		if _, err := w.Attach(m.id, m.role, openFixture(t, m.root)); err != nil {
			t.Fatalf("attach %s: %v", m.id, err)
		}
	}
	return w
}

// twoTeamModes runs a subtest against a two-team workspace in both modes.
func twoTeamModes(t *testing.T, run func(t *testing.T, w *Workspace)) {
	t.Helper()
	t.Run("companion", func(t *testing.T) { run(t, twoTeamWorkspace(t, false)) })
	t.Run("browser", func(t *testing.T) { run(t, twoTeamWorkspace(t, true)) })
}

func TestWorkspaceHoldsSeveralTeams(t *testing.T) {
	twoTeamModes(t, func(t *testing.T, w *Workspace) {
		t.Run("every team is mounted, not just the first", func(t *testing.T) {
			mounts := w.TeamMounts()
			if len(mounts) != 2 {
				t.Fatalf("team mounts = %d, want 2", len(mounts))
			}
			if mounts[0].ID != "demo-team" || mounts[1].ID != "platform-team" {
				t.Errorf("mount order = %s, %s", mounts[0].ID, mounts[1].ID)
			}
		})

		t.Run("a second team is no longer a diagnostic", func(t *testing.T) {
			for _, d := range w.Diagnostics() {
				if d.Code == core.CodeTeamKey {
					t.Errorf("a second team repository is legal, got %+v", d)
				}
			}
		})

		t.Run("team.list answers with both", func(t *testing.T) {
			list := decode[TeamListResult](t, wsCall(t, w, "team.list", nil))
			if list.Total != 2 || len(list.Teams) != 2 {
				t.Fatalf("team.list = %+v", list)
			}
			if list.Teams[0].Key != "DEMO-TEAM" || list.Teams[1].Key != "PLATFORM-TEAM" {
				t.Errorf("keys = %s, %s", list.Teams[0].Key, list.Teams[1].Key)
			}
		})

		t.Run("a team is resolved by key and by repository id", func(t *testing.T) {
			for _, name := range []string{"PLATFORM-TEAM", "platform-team"} {
				summary := decode[teamSummary](t, wsCall(t, w, "team.get", map[string]string{"team": name}))
				if summary.Key != "PLATFORM-TEAM" {
					t.Errorf("team.get %q = %s", name, summary.Key)
				}
			}
		})

		t.Run("naming no team is refused while two are open", func(t *testing.T) {
			env := rawWorkspaceCall(t, w, "team.get", map[string]string{})
			if env.OK || env.Error.Code != "invalid_request" {
				t.Fatalf("team.get without a team = %+v", env)
			}
		})

		t.Run("an unknown team is not found", func(t *testing.T) {
			env := rawWorkspaceCall(t, w, "team.get", map[string]string{"team": "NOPE"})
			if env.OK || env.Error.Code != "not_found" {
				t.Fatalf("team.get NOPE = %+v", env)
			}
		})

		t.Run("boards follow the team they are asked for", func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				team  string
				first string
				count int
			}{
				{name: "the delivery team", team: "DEMO-TEAM", first: "delivery", count: 2},
				{name: "the platform team", team: "PLATFORM-TEAM", first: "platform", count: 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					result := decode[BoardListResult](t,
						wsCall(t, w, "board.list", map[string]string{"team": tc.team}))
					if len(result.Boards) != tc.count {
						t.Fatalf("boards = %d, want %d", len(result.Boards), tc.count)
					}
					if result.Boards[0].ID != tc.first {
						t.Errorf("first board = %s, want %s", result.Boards[0].ID, tc.first)
					}
				})
			}
		})

		t.Run("a board of the other team is not on this one", func(t *testing.T) {
			env := rawWorkspaceCall(t, w, "board.get",
				map[string]string{"board": "platform", "team": "DEMO-TEAM"})
			if env.OK {
				t.Error("the delivery team does not hold the platform board")
			}
		})

		t.Run("retros and sprints follow the same team", func(t *testing.T) {
			sprints := decode[SprintListResult](t,
				wsCall(t, w, "sprint.list", map[string]string{"team": "DEMO-TEAM"}))
			if len(sprints.Sprints) == 0 {
				t.Error("the delivery team holds a sprint")
			}
			empty := decode[SprintListResult](t,
				wsCall(t, w, "sprint.list", map[string]string{"team": "PLATFORM-TEAM"}))
			if len(empty.Sprints) != 0 {
				t.Errorf("the platform team holds no sprint, got %d", len(empty.Sprints))
			}
			retros := decode[RetroListResult](t,
				wsCall(t, w, "retro.list", map[string]string{"team": "DEMO-TEAM"}))
			if len(retros.Retros) == 0 {
				t.Error("the delivery team holds a retro")
			}
		})

		t.Run("a board call naming no team is refused rather than guessed", func(t *testing.T) {
			env := rawWorkspaceCall(t, w, "board.list", map[string]string{})
			if env.OK || env.Error.Code != "invalid_request" {
				t.Fatalf("board.list without a team = %+v", env)
			}
		})

		t.Run("workspace.list carries every team", func(t *testing.T) {
			summary := decode[workspaceSummary](t, wsCall(t, w, "workspace.list", nil))
			if len(summary.Teams) != 2 {
				t.Fatalf("workspace teams = %d, want 2", len(summary.Teams))
			}
			if summary.Team == nil || summary.Team.Key != "DEMO-TEAM" {
				t.Errorf("the legacy single-team field must repeat the first team, got %+v", summary.Team)
			}
		})
	})
}

func TestWorkspaceSingleTeamNeedsNoName(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		summary := decode[teamSummary](t, wsCall(t, w, "team.get", nil))
		if summary.Key != "DEMO-TEAM" {
			t.Fatalf("team.get = %s", summary.Key)
		}
		boards := decode[BoardListResult](t, wsCall(t, w, "board.list", nil))
		if len(boards.Boards) == 0 {
			t.Error("a workspace with one team lists its boards with no team named")
		}
	})
}

func TestWorkspaceDuplicateTeamKey(t *testing.T) {
	w := NewWorkspace()
	for _, id := range []string{"one", "two"} {
		if _, err := w.Attach(id, RoleTeam, openFixture(t, teamFixtureRoot)); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	found := false
	for _, d := range w.Diagnostics() {
		if d.Code == core.CodeTeamKey && d.Severity == core.SeverityError {
			found = true
		}
	}
	if !found {
		t.Fatalf("two repositories declaring DEMO-TEAM must be reported, got %v", w.Diagnostics())
	}
}
