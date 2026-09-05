package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newEmptyAPIServer mounts a repository that holds no backlog at all, which is
// the state POST /repos/{id}/projects exists for.
func newEmptyAPIServer(t *testing.T) (*Server, string) {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# repo\n"), 0o600); err != nil {
		t.Fatalf("seed the repository: %v", err)
	}
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project"}},
		Now:       func() time.Time { return time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s, root
}

// createdProjectBody is the documented shape of POST /repos/{id}/projects.
type createdProjectBody struct {
	Project struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		DocsPath    string `json:"docsPath"`
		BacklogPath string `json:"backlogPath"`
		Writable    bool   `json:"writable"`
	} `json:"project"`
	Writes struct {
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
}

func TestCreateProjectRoute(t *testing.T) {
	t.Run("scaffolds a backlog and indexes it", func(t *testing.T) {
		s, root := newEmptyAPIServer(t)
		var body createdProjectBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/repos/" + testRepoID + "/projects",
			body:   map[string]any{"docsFolder": "docs", "key": "ACME", "name": "ACME Platform"},
		}), http.StatusCreated, &body)

		if body.Project.Key != "ACME" || body.Project.Name != "ACME Platform" {
			t.Errorf("project = %+v", body.Project)
		}
		if !body.Project.Writable {
			t.Error("a freshly created project must be writable")
		}
		if _, err := os.Stat(filepath.Join(root, "docs", ".pmngr", "project.yaml")); err != nil {
			t.Fatalf("project.yaml was not written: %v", err)
		}

		// The project is served by the very next request.
		var repo struct {
			Projects []string `json:"projects"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/repos/" + testRepoID}),
			http.StatusOK, &repo)
		if len(repo.Projects) != 1 || repo.Projects[0] != "ACME" {
			t.Errorf("projects = %v, want [ACME]", repo.Projects)
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
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				s, _ := newEmptyAPIServer(t)
				rec := send(t, s, request{
					method: http.MethodPost,
					target: "/api/v1/repos/" + tc.repo + "/projects",
					body:   tc.body,
				})
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body)
				}
			})
		}
	})

	t.Run("an existing project is never overwritten", func(t *testing.T) {
		s, _ := newAPIServer(t)
		rec := send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/repos/" + testRepoID + "/projects",
			body:   map[string]any{"docsFolder": "docs", "key": "OTHER"},
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body)
		}
	})
}
