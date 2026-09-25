package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/gitops"
)

// GET /api/v1/git/refs, the ref listing of GIT-US-0149.

// gitRefsBody is the documented shape of GET /api/v1/git/refs.
type gitRefsBody struct {
	Repo     string `json:"repo"`
	Backend  string `json:"backend"`
	Branches []struct {
		Name    string `json:"name"`
		Remote  string `json:"remote"`
		SHA     string `json:"sha"`
		Current bool   `json:"current"`
	} `json:"branches"`
	Commits []struct {
		SHA     string `json:"sha"`
		Subject string `json:"subject"`
		Date    string `json:"date"`
	} `json:"commits"`
}

func TestGitRefs(t *testing.T) {
	for _, backend := range []string{string(gitops.KindGoGit), string(gitops.KindSystem)} {
		t.Run(backend, func(t *testing.T) {
			settings := config.Default().Git
			settings.Backend = config.Backend(backend)
			s, root := newGitServer(t, settings)
			if got, ok := s.git.backendFor(testRepoID); !ok || got.Name() != backend {
				t.Skipf("the %s backend is not usable here", backend)
			}
			before := gitLog(t, root)

			var body gitRefsBody
			decode(t, send(t, s, request{method: http.MethodGet,
				target: "/api/v1/git/refs?repo=" + testRepoID}), http.StatusOK, &body)
			if body.Repo != testRepoID || body.Backend != backend {
				t.Errorf("repo, backend = %q, %q", body.Repo, body.Backend)
			}
			if len(body.Branches) != 1 || body.Branches[0].Name != "main" || !body.Branches[0].Current ||
				body.Branches[0].SHA == "" {
				t.Errorf("branches = %+v, want the current main", body.Branches)
			}
			if len(body.Commits) != 1 || body.Commits[0].Subject != "chore: seed the fixture" ||
				body.Commits[0].SHA != body.Branches[0].SHA || body.Commits[0].Date == "" {
				t.Errorf("commits = %+v, want the seed commit", body.Commits)
			}
			if after := gitLog(t, root); len(after) != len(before) {
				t.Errorf("listing refs committed: %v -> %v", before, after)
			}

			decode(t, send(t, s, request{method: http.MethodGet,
				target: "/api/v1/git/refs?repo=" + testRepoID + "&limit=0"}), http.StatusOK, &body)
			if len(body.Commits) != 0 || len(body.Branches) != 1 {
				t.Errorf("limit=0 lists branches only, got %+v", body)
			}
		})
	}

	t.Run("jj bookmarks are branches", func(t *testing.T) {
		s, _ := newJujutsuServer(t, config.Default().Git)
		var body gitRefsBody
		decode(t, send(t, s, request{method: http.MethodGet,
			target: "/api/v1/git/refs?repo=" + testRepoID + "&limit=5"}), http.StatusOK, &body)
		if body.Backend != string(gitops.KindJujutsu) {
			t.Fatalf("backend = %q, want jj", body.Backend)
		}
		if len(body.Branches) != 1 || body.Branches[0].Name != "main" || !body.Branches[0].Current {
			t.Errorf("branches = %+v, want the main bookmark", body.Branches)
		}
		if len(body.Commits) == 0 {
			t.Error("a jj repository lists its recent commits")
		}
	})

	problems := []struct {
		name   string
		target string
		want   int
	}{
		{name: "the repository is required", target: "/api/v1/git/refs", want: http.StatusBadRequest},
		{name: "an unknown repository", target: "/api/v1/git/refs?repo=nope", want: http.StatusNotFound},
		{name: "a limit that is not a number", target: "/api/v1/git/refs?repo=" + testRepoID + "&limit=x", want: http.StatusBadRequest},
		{name: "a limit out of range", target: "/api/v1/git/refs?repo=" + testRepoID + "&limit=1000", want: http.StatusBadRequest},
	}
	for _, tc := range problems {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newGitServer(t, config.Default().Git)
			decode(t, send(t, s, request{method: http.MethodGet, target: tc.target}), tc.want, nil)
		})
	}

	t.Run("a folder without git is unavailable", func(t *testing.T) {
		root := copyTree(t, fixtureRoot)
		s, err := New(Options{
			Token: "test-token", Version: "0.0.1-test", Workspace: "test",
			Repos: []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
			Now:   func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
			Git:   config.Default().Git,
		})
		if err != nil {
			t.Fatalf("New(): %v", err)
		}
		t.Cleanup(func() { s.git.close(t.Context()) })
		var problem struct {
			Code string `json:"code"`
		}
		decode(t, send(t, s, request{method: http.MethodGet,
			target: "/api/v1/git/refs?repo=" + testRepoID}), http.StatusServiceUnavailable, &problem)
		if problem.Code != "unavailable" {
			t.Errorf("code = %q, want unavailable", problem.Code)
		}
	})

	t.Run("the route needs the bearer token", func(t *testing.T) {
		s, _ := newGitServer(t, config.Default().Git)
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/git/refs?repo=" + testRepoID,
			header: map[string]string{"Authorization": "Bearer wrong"}})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}
