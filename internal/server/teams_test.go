package server

import (
	"net/http"
	"testing"
	"time"
)

// teamFixtureRoot is the team repository mounted next to the project one.
const teamFixtureRoot = "../../testdata/fixtures/team-basic"

// teamRepoID is the id the team fixture is mounted under.
const teamRepoID = "demo-team"

// secondTeamFixtureRoot is the other team repository of the multi-team tests,
// and secondTeamRepoID the id it is registered under.
const (
	secondTeamFixtureRoot = "../../testdata/fixtures/team-second"
	secondTeamRepoID      = "platform-team"
)

// teamBody is the documented shape of GET /api/v1/teams/{key}.
type teamBody struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	KnowledgePath string `json:"knowledgePath"`
	VaultID       string `json:"vaultId"`
	Members       []struct {
		Handle string `json:"handle"`
		Active bool   `json:"active"`
	} `json:"members"`
	Projects []struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Cloned  bool   `json:"cloned"`
		VaultID string `json:"vaultId"`
	} `json:"projects"`
	Diagnostics []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
	} `json:"diagnostics"`
}

// refBody is the documented shape of GET /api/v1/refs?ref=…
type refBody struct {
	Ref      string `json:"ref"`
	Project  string `json:"project"`
	Item     string `json:"item"`
	Declared bool   `json:"declared"`
	Cloned   bool   `json:"cloned"`
	VaultID  string `json:"vaultId"`
	Reason   string `json:"reason"`
	Found    *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"found"`
}

// newTeamServer mounts the team fixture and one project clone, which is the
// workspace of docs/04: a team repository plus the projects the user has.
func newTeamServer(t *testing.T) *Server {
	t.Helper()

	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos: []Repo{
			{ID: teamRepoID, Path: copyTree(t, teamFixtureRoot), Role: "team", DocsFolder: "knowledge"},
			{ID: testRepoID, Path: copyTree(t, fixtureRoot), Role: "project", DocsFolder: "docs"},
		},
		Now: func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

func TestTeamEndpoints(t *testing.T) {
	s := newTeamServer(t)

	t.Run("list", func(t *testing.T) {
		var body struct {
			Teams []teamBody `json:"teams"`
			Total int        `json:"total"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/teams"}), http.StatusOK, &body)
		if body.Total != 1 || len(body.Teams) != 1 {
			t.Fatalf("teams = %d, want 1", body.Total)
		}
		if body.Teams[0].Key != "DEMO-TEAM" {
			t.Errorf("key = %q, want DEMO-TEAM", body.Teams[0].Key)
		}
	})

	t.Run("get", func(t *testing.T) {
		var team teamBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/teams/DEMO-TEAM"}), http.StatusOK, &team)

		if team.Name != "Demo Delivery Team" || team.KnowledgePath != "knowledge" {
			t.Errorf("team = %+v", team)
		}
		if len(team.Members) != 4 {
			t.Fatalf("members = %d, want 4", len(team.Members))
		}
		for _, d := range team.Diagnostics {
			if d.Severity == "error" {
				t.Errorf("unexpected error diagnostic %s", d.Code)
			}
		}
		cloned := map[string]bool{}
		for _, p := range team.Projects {
			cloned[p.Key] = p.Cloned
		}
		if !cloned["DEMO"] {
			t.Error("DEMO is mounted and must be marked cloned")
		}
		if cloned["WEB"] {
			t.Error("WEB is not mounted and must be marked not cloned")
		}
	})

	t.Run("unknown team", func(t *testing.T) {
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/teams/NOPE"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("team knowledge base", func(t *testing.T) {
		var tree []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		}
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/teams/DEMO-TEAM/kb/tree",
		}), http.StatusOK, &tree)
		names := map[string]bool{}
		for _, n := range tree {
			names[n.Name] = true
		}
		for _, want := range []string{"decisions", "index.md", "ways-of-working"} {
			if !names[want] {
				t.Errorf("the team knowledge base is missing %q; got %v", want, names)
			}
		}
	})
}

// newTwoTeamServer registers both team fixtures next to the project clone: the
// workspace GIT-US-0036 made legal, where the client says which team it means.
func newTwoTeamServer(t *testing.T) *Server {
	t.Helper()

	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos: []Repo{
			{ID: teamRepoID, Path: copyTree(t, teamFixtureRoot), Role: "team", DocsFolder: "knowledge"},
			{
				ID: secondTeamRepoID, Path: copyTree(t, secondTeamFixtureRoot),
				Role: "team", DocsFolder: "knowledge",
			},
			{ID: testRepoID, Path: copyTree(t, fixtureRoot), Role: "project", DocsFolder: "docs"},
		},
		Now: func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

func TestTeamEndpointsWithSeveralTeams(t *testing.T) {
	s := newTwoTeamServer(t)

	t.Run("the list carries every mounted team", func(t *testing.T) {
		var body struct {
			Teams []teamBody `json:"teams"`
			Total int        `json:"total"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/teams"}), http.StatusOK, &body)
		if body.Total != 2 || len(body.Teams) != 2 {
			t.Fatalf("teams = %d, want 2", body.Total)
		}
		if body.Teams[0].Key != "DEMO-TEAM" || body.Teams[1].Key != "PLATFORM-TEAM" {
			t.Errorf("keys = %q, %q", body.Teams[0].Key, body.Teams[1].Key)
		}
	})

	t.Run("the key in the path picks the team", func(t *testing.T) {
		for _, tc := range []struct{ target, want string }{
			{target: "/api/v1/teams/DEMO-TEAM", want: "Demo Delivery Team"},
			{target: "/api/v1/teams/PLATFORM-TEAM", want: "Platform Team"},
			{target: "/api/v1/teams/" + secondTeamRepoID, want: "Platform Team"},
		} {
			t.Run(tc.target, func(t *testing.T) {
				var team teamBody
				decode(t, send(t, s, request{method: http.MethodGet, target: tc.target}), http.StatusOK, &team)
				if team.Name != tc.want {
					t.Errorf("name = %q, want %q", team.Name, tc.want)
				}
			})
		}
	})

	t.Run("team-scoped routes take the team they act on", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			target string
			want   string
			count  int
		}{
			{name: "delivery", target: "/api/v1/boards?team=DEMO-TEAM", want: "delivery", count: 2},
			{name: "platform", target: "/api/v1/boards?team=PLATFORM-TEAM", want: "platform", count: 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var body struct {
					Boards []struct {
						ID string `json:"id"`
					} `json:"boards"`
				}
				decode(t, send(t, s, request{method: http.MethodGet, target: tc.target}), http.StatusOK, &body)
				if len(body.Boards) != tc.count {
					t.Fatalf("boards = %d, want %d", len(body.Boards), tc.count)
				}
				if body.Boards[0].ID != tc.want {
					t.Errorf("first board = %q, want %q", body.Boards[0].ID, tc.want)
				}
			})
		}
	})

	t.Run("naming no team is refused rather than guessed", func(t *testing.T) {
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/boards"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("each team knowledge base is reachable by its own key", func(t *testing.T) {
		for _, tc := range []struct{ target, want string }{
			{target: "/api/v1/teams/DEMO-TEAM/kb/tree", want: "ways-of-working"},
			{target: "/api/v1/teams/PLATFORM-TEAM/kb/tree", want: "index.md"},
		} {
			t.Run(tc.target, func(t *testing.T) {
				var tree []struct {
					Name string `json:"name"`
				}
				decode(t, send(t, s, request{method: http.MethodGet, target: tc.target}), http.StatusOK, &tree)
				found := false
				for _, n := range tree {
					if n.Name == tc.want {
						found = true
					}
				}
				if !found {
					t.Errorf("the tree of %s is missing %q: %+v", tc.target, tc.want, tree)
				}
			})
		}
	})
}

func TestResolveRefEndpoint(t *testing.T) {
	s := newTeamServer(t)

	tests := []struct {
		name         string
		ref          string
		status       int
		wantCloned   bool
		wantDeclared bool
		wantFound    bool
	}{
		{
			name: "cloned", ref: "DEMO/DEMO-US-0001", status: http.StatusOK,
			wantCloned: true, wantDeclared: true, wantFound: true,
		},
		{
			name: "declared but not cloned", ref: "WEB/WEB-US-0031", status: http.StatusOK,
			wantDeclared: true,
		},
		{name: "undeclared", ref: "GONE/GONE-US-0001", status: http.StatusOK},
		{name: "malformed", ref: "not-a-ref", status: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/refs?ref=" + tt.ref})
			if tt.status != http.StatusOK {
				if rec.Code != tt.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
				}
				return
			}
			var body refBody
			decode(t, rec, http.StatusOK, &body)
			if body.Cloned != tt.wantCloned {
				t.Errorf("cloned = %v, want %v (%s)", body.Cloned, tt.wantCloned, body.Reason)
			}
			if body.Declared != tt.wantDeclared {
				t.Errorf("declared = %v, want %v", body.Declared, tt.wantDeclared)
			}
			if (body.Found != nil) != tt.wantFound {
				t.Errorf("found = %v, want %v", body.Found, tt.wantFound)
			}
		})
	}
}

func TestSearchSpansEveryRepository(t *testing.T) {
	s := newTeamServer(t)

	type hit struct {
		Kind    string `json:"kind"`
		Path    string `json:"path"`
		Project string `json:"project"`
		VaultID string `json:"vaultId"`
	}

	t.Run("across repositories", func(t *testing.T) {
		var hits []hit
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/search?q=done&limit=20",
		}), http.StatusOK, &hits)
		sources := map[string]bool{}
		for _, h := range hits {
			if h.VaultID == "" {
				t.Errorf("hit %s does not name its repository", h.Path)
			}
			sources[h.VaultID] = true
		}
		if !sources[teamRepoID] {
			t.Errorf("search must reach the team knowledge base; sources were %v", sources)
		}
	})

	t.Run("scoped to a project", func(t *testing.T) {
		var hits []hit
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/search?q=checkout&project=DEMO&limit=20",
		}), http.StatusOK, &hits)
		if len(hits) == 0 {
			t.Fatal("search found nothing in the DEMO project")
		}
		for _, h := range hits {
			if h.Project != "DEMO" {
				t.Errorf("a scoped search returned a hit from %q", h.Project)
			}
		}
	})

	t.Run("unknown project", func(t *testing.T) {
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/search?q=x&project=NOPE"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestWorkspaceEndpoint(t *testing.T) {
	s := newTeamServer(t)

	var body struct {
		Vaults []struct {
			ID       string   `json:"id"`
			Role     string   `json:"role"`
			Team     bool     `json:"team"`
			Projects []string `json:"projects"`
		} `json:"vaults"`
		Team *teamBody `json:"team"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/workspace"}), http.StatusOK, &body)

	if len(body.Vaults) != 2 {
		t.Fatalf("vaults = %d, want 2", len(body.Vaults))
	}
	if body.Team == nil || body.Team.Key != "DEMO-TEAM" {
		t.Fatal("the workspace must report its team repository")
	}
	for _, v := range body.Vaults {
		switch v.ID {
		case teamRepoID:
			if !v.Team || len(v.Projects) != 0 {
				t.Errorf("the team repository holds no project: %+v", v)
			}
		case testRepoID:
			if len(v.Projects) != 1 || v.Projects[0] != "DEMO" {
				t.Errorf("the clone must expose DEMO: %+v", v)
			}
		default:
			t.Errorf("unexpected repository %q", v.ID)
		}
	}
}

// snapshotRow is the documented shape of one entry of /api/v1/snapshots.
type snapshotRow struct {
	Project string `json:"project"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Items   int    `json:"items"`
	Reason  string `json:"reason"`
	Info    struct {
		Present   bool   `json:"present"`
		Freshness string `json:"freshness"`
		Stale     bool   `json:"stale"`
		Items     int    `json:"items"`
	} `json:"info"`
}

// snapshotBody is the answer of both snapshot routes.
type snapshotBody struct {
	Snapshots []snapshotRow `json:"snapshots"`
	Writes    []struct {
		VaultID string `json:"vaultId"`
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
	DryRun bool `json:"dryRun"`
}

// rowOf returns the entry of a project key.
func rowOf(t *testing.T, body snapshotBody, key string) snapshotRow {
	t.Helper()
	for _, row := range body.Snapshots {
		if row.Project == key {
			return row
		}
	}
	t.Fatalf("no entry for %s in %+v", key, body.Snapshots)
	return snapshotRow{}
}

func TestSnapshotEndpoints(t *testing.T) {
	s := newTeamServer(t)

	t.Run("the listing reports the committed snapshot of every project", func(t *testing.T) {
		var body snapshotBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/snapshots"}), http.StatusOK, &body)
		if len(body.Snapshots) != 2 {
			t.Fatalf("snapshots = %+v", body.Snapshots)
		}
		web := rowOf(t, body, "WEB")
		if !web.Info.Present || web.Info.Items != 4 || web.Info.Freshness != "ageing" {
			t.Fatalf("WEB = %+v", web.Info)
		}
		if demo := rowOf(t, body, "DEMO"); demo.Info.Present {
			t.Errorf("the fixture commits no DEMO snapshot: %+v", demo.Info)
		}
	})

	t.Run("a dry run reports what would change and writes nothing", func(t *testing.T) {
		var body snapshotBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/snapshots",
			body: map[string]any{"dryRun": true, "generatedBy": "jose"},
		}), http.StatusOK, &body)
		if !body.DryRun || len(body.Writes) != 0 {
			t.Fatalf("body = %+v", body)
		}
		if row := rowOf(t, body, "DEMO"); row.Status != "written" {
			t.Fatalf("DEMO = %+v", row)
		}
	})

	t.Run("a refresh writes the snapshot of the cloned project", func(t *testing.T) {
		var body snapshotBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/snapshots",
			body: map[string]any{"generatedBy": "jose"},
		}), http.StatusOK, &body)
		if row := rowOf(t, body, "DEMO"); row.Status != "written" || !row.Info.Present {
			t.Fatalf("DEMO = %+v", row)
		}
		if row := rowOf(t, body, "WEB"); row.Status != "skipped" {
			t.Fatalf("WEB = %+v", row)
		}
		if len(body.Writes) != 1 || body.Writes[0].VaultID != teamRepoID {
			t.Fatalf("writes = %+v", body.Writes)
		}
	})

	t.Run("regenerating an unchanged snapshot writes nothing", func(t *testing.T) {
		var body snapshotBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/snapshots", body: map[string]any{},
		}), http.StatusOK, &body)
		if row := rowOf(t, body, "DEMO"); row.Status != "unchanged" {
			t.Fatalf("DEMO = %+v", row)
		}
		if len(body.Writes) != 0 {
			t.Fatalf("writes = %+v", body.Writes)
		}
	})
}
