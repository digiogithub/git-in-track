package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createdTeamBody is the documented shape of POST /repos/{id}/team.
type createdTeamBody struct {
	Team struct {
		Key           string `json:"key"`
		Name          string `json:"name"`
		KnowledgePath string `json:"knowledgePath"`
	} `json:"team"`
	Writes struct {
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
}

func TestCreateTeamRoute(t *testing.T) {
	t.Run("scaffolds a team repository and indexes it", func(t *testing.T) {
		s, root := newEmptyAPIServer(t)
		var body createdTeamBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/repos/" + testRepoID + "/team",
			body:   map[string]any{"key": "ACME-TEAM", "name": "ACME Delivery Team"},
		}), http.StatusCreated, &body)

		if body.Team.Key != "ACME-TEAM" || body.Team.Name != "ACME Delivery Team" {
			t.Errorf("team = %+v", body.Team)
		}
		for _, path := range []string{"team.yaml", filepath.Join("knowledge", "index.md")} {
			if _, err := os.Stat(filepath.Join(root, path)); err != nil {
				t.Errorf("%s was not written: %v", path, err)
			}
		}
	})

	t.Run("refusals carry the documented problem", func(t *testing.T) {
		tests := []struct {
			name   string
			repo   string
			body   map[string]any
			status int
		}{
			{name: "an unknown repository", repo: "ghost", body: map[string]any{"key": "ACME"}, status: http.StatusNotFound},
			{name: "an invalid key", repo: testRepoID, body: map[string]any{"key": "acme"}, status: http.StatusUnprocessableEntity},
			{name: "no key at all", repo: testRepoID, body: map[string]any{}, status: http.StatusUnprocessableEntity},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				s, _ := newEmptyAPIServer(t)
				rec := send(t, s, request{
					method: http.MethodPost,
					target: "/api/v1/repos/" + tc.repo + "/team",
					body:   tc.body,
				})
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body)
				}
			})
		}
	})

	t.Run("an existing team repository is never overwritten", func(t *testing.T) {
		s, root := newEmptyAPIServer(t)
		send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/repos/" + testRepoID + "/team",
			body:   map[string]any{"key": "ACME-TEAM"},
		})
		before, err := os.ReadFile(filepath.Join(root, "team.yaml"))
		if err != nil {
			t.Fatalf("read team.yaml: %v", err)
		}
		rec := send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/repos/" + testRepoID + "/team",
			body:   map[string]any{"key": "OTHER"},
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body)
		}
		after, err := os.ReadFile(filepath.Join(root, "team.yaml"))
		if err != nil || string(after) != string(before) {
			t.Errorf("the existing team.yaml was rewritten")
		}
	})
}

func TestRegisterRepoRouteReportsTheCommand(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "a project repository",
			body: map[string]any{"path": "/src/acme", "role": "project"},
			want: "gintrack add /src/acme",
		},
		{
			name: "a team repository",
			body: map[string]any{"path": "/src/acme-team", "role": "team"},
			want: "gintrack add /src/acme-team --team",
		},
		{
			name: "a documentation folder",
			body: map[string]any{"path": "/src/acme", "docs": "documentation"},
			want: "gintrack add /src/acme --docs documentation",
		},
		{
			// The detail is JSON, so the quotes the shell needs are escaped.
			name: "a path with a space",
			body: map[string]any{"path": "/src/my repo"},
			want: `gintrack add \"/src/my repo\"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEmptyAPIServer(t)
			rec := send(t, s, request{method: http.MethodPost, target: "/api/v1/repos", body: tc.body})
			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNotImplemented, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("the problem does not carry %q: %s", tc.want, rec.Body)
			}
		})
	}
}
