package server

import (
	"net/http"
	"testing"
)

// teamProjectBodyOut is the documented shape of the two project-list routes.
type teamProjectBodyOut struct {
	Team    teamBody `json:"team"`
	Project struct {
		Key           string `json:"key"`
		Name          string `json:"name"`
		Repo          string `json:"repo"`
		DefaultBranch string `json:"defaultBranch"`
		DocsPath      string `json:"docsPath"`
	} `json:"project"`
	References []struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Field string `json:"field"`
		Ref   string `json:"ref"`
	} `json:"references"`
	Writes []struct {
		VaultID string `json:"vaultId"`
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
}

// toolsProject is the entry the tests connect to the fixture team.
func toolsProject() map[string]any {
	return map[string]any{
		"key":           "TOOLS",
		"name":          "Internal Tools",
		"repo":          "https://github.com/example/tools.git",
		"defaultBranch": "trunk",
		"docsPath":      "docs",
	}
}

func TestTeamProjectRoutes(t *testing.T) {
	t.Run("a project is added to the team named in the path", func(t *testing.T) {
		s := newTeamServer(t)
		var body teamProjectBodyOut
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/teams/DEMO-TEAM/projects",
			body:   toolsProject(),
		}), http.StatusCreated, &body)

		if body.Project.Key != "TOOLS" || body.Project.DefaultBranch != "trunk" {
			t.Errorf("project = %+v", body.Project)
		}
		if len(body.Team.Projects) != 3 {
			t.Fatalf("projects = %d, want 3", len(body.Team.Projects))
		}
		if len(body.Writes) != 1 || len(body.Writes[0].Written) != 1 {
			t.Fatalf("writes = %+v", body.Writes)
		}
		if got := body.Writes[0].Written[0].Path; got != "team.yaml" {
			t.Errorf("written = %q, want team.yaml", got)
		}
		if body.Writes[0].VaultID != teamRepoID {
			t.Errorf("vaultId = %q, want %q", body.Writes[0].VaultID, teamRepoID)
		}

		// The ordinary read path sees it.
		var team teamBody
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/teams/DEMO-TEAM",
		}), http.StatusOK, &team)
		found := false
		for _, p := range team.Projects {
			if p.Key == "TOOLS" {
				found = true
				if p.Cloned {
					t.Error("no registered repository serves TOOLS; it cannot be cloned")
				}
			}
		}
		if !found {
			t.Errorf("TOOLS is missing from %+v", team.Projects)
		}
	})

	t.Run("a project nothing references is removed", func(t *testing.T) {
		s := newTeamServer(t)
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/teams/DEMO-TEAM/projects",
			body:   toolsProject(),
		}), http.StatusCreated, nil)

		var body teamProjectBodyOut
		decode(t, send(t, s, request{
			method: http.MethodDelete,
			target: "/api/v1/teams/DEMO-TEAM/projects/TOOLS",
		}), http.StatusOK, &body)
		if body.Project.Key != "TOOLS" {
			t.Errorf("removed = %+v", body.Project)
		}
		if len(body.Team.Projects) != 2 {
			t.Errorf("projects = %d, want 2", len(body.Team.Projects))
		}
	})

	t.Run("refusals carry the documented problem", func(t *testing.T) {
		tests := []struct {
			name   string
			method string
			target string
			body   map[string]any
			status int
			code   string
		}{
			{
				name: "a duplicate project key", method: http.MethodPost,
				target: "/api/v1/teams/DEMO-TEAM/projects",
				body: map[string]any{
					"key": "DEMO", "name": "Demo again",
					"repo": "https://github.com/example/other.git", "docsPath": "docs",
				},
				status: http.StatusConflict, code: "team_project_exists",
			},
			{
				name: "a key outside the grammar", method: http.MethodPost,
				target: "/api/v1/teams/DEMO-TEAM/projects",
				body: map[string]any{
					"key": "demo-shop", "repo": "https://github.com/example/x.git", "docsPath": "docs",
				},
				status: http.StatusUnprocessableEntity, code: "validation_failed",
			},
			{
				name: "no key at all", method: http.MethodPost,
				target: "/api/v1/teams/DEMO-TEAM/projects",
				body:   map[string]any{"repo": "https://github.com/example/x.git", "docsPath": "docs"},
				status: http.StatusBadRequest, code: "invalid_request",
			},
			{
				name: "an unknown team", method: http.MethodPost,
				target: "/api/v1/teams/NOPE-TEAM/projects", body: toolsProject(),
				status: http.StatusNotFound, code: "not_found",
			},
			{
				name: "a project the team does not declare", method: http.MethodDelete,
				target: "/api/v1/teams/DEMO-TEAM/projects/NOPE",
				status: http.StatusNotFound, code: "not_found",
			},
			{
				name: "a project a board and a sprint reference", method: http.MethodDelete,
				target: "/api/v1/teams/DEMO-TEAM/projects/WEB",
				status: http.StatusConflict, code: "team_project_referenced",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s := newTeamServer(t)
				var doc problemBody
				decode(t, send(t, s, request{method: tt.method, target: tt.target, body: tt.body}),
					tt.status, &doc)
				if doc.Code != tt.code {
					t.Errorf("code = %q (%s), want %q", doc.Code, doc.Detail, tt.code)
				}
			})
		}
	})

	t.Run("force accepts the references it breaks", func(t *testing.T) {
		s := newTeamServer(t)
		var body teamProjectBodyOut
		decode(t, send(t, s, request{
			method: http.MethodDelete,
			target: "/api/v1/teams/DEMO-TEAM/projects/WEB?force=true",
		}), http.StatusOK, &body)

		if len(body.References) == 0 {
			t.Fatal("a forced removal must report what it broke")
		}
		kinds := map[string]bool{}
		for _, ref := range body.References {
			kinds[ref.Kind] = true
		}
		for _, kind := range []string{"board", "sprint", "retro"} {
			if !kinds[kind] {
				t.Errorf("the fixture references WEB from a %s and none was reported: %+v",
					kind, body.References)
			}
		}
		if len(body.Team.Projects) != 1 || body.Team.Projects[0].Key != "DEMO" {
			t.Errorf("projects = %+v", body.Team.Projects)
		}
	})
}
