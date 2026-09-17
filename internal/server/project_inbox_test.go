package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// legacyProjectYAML is a project from before the inbox: no status in the
// triage category, and a comment the edit must keep.
const legacyProjectYAML = `schema: 1
key: OLD
name: Legacy Fixture
workflow:
  initial: backlog
  statuses:
    # Ordinary work only.
    - {id: backlog, name: Backlog, category: todo}
    - {id: done, name: Done, category: done, terminal: true}
`

// newLegacyServer mounts one project whose project.yaml is config, and returns
// the server and the path of that file.
func newLegacyServer(t *testing.T, config string) (*Server, string) {
	t.Helper()

	root := t.TempDir()
	backlog := filepath.Join(root, "docs", ".pmngr")
	if err := os.MkdirAll(backlog, 0o755); err != nil {
		t.Fatalf("scaffold the fixture: %v", err)
	}
	configPath := filepath.Join(backlog, "project.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write project.yaml: %v", err)
	}
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: "legacy", Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s, configPath
}

// projectInboxBody is the documented shape of POST /projects/{key}/inbox.
type projectInboxBody struct {
	Project struct {
		Key       string `json:"key"`
		ConfigRev string `json:"configRev"`
		Statuses  []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
		} `json:"statuses"`
	} `json:"project"`
	Writes struct {
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
}

func TestProjectInboxEnableRoute(t *testing.T) {
	t.Parallel()

	t.Run("adds the triage status and announces the write", func(t *testing.T) {
		t.Parallel()
		s, configPath := newLegacyServer(t, legacyProjectYAML)
		client := newHubClient()
		client.subscribe([]string{eventFileChanged})
		s.hub.register(client)

		var project struct {
			ConfigRev string `json:"configRev"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/projects/OLD"}),
			http.StatusOK, &project)
		if project.ConfigRev == "" {
			t.Fatal("the project listing carries no configRev")
		}

		var body projectInboxBody
		rec := send(t, s, request{
			method: http.MethodPost, target: "/api/v1/projects/OLD/inbox",
			header: map[string]string{"If-Match": project.ConfigRev},
		})
		decode(t, rec, http.StatusOK, &body)
		if len(body.Project.Statuses) != 3 || body.Project.Statuses[0].Category != "triage" {
			t.Fatalf("statuses = %+v", body.Project.Statuses)
		}
		if rec.Header().Get("ETag") != `"`+body.Project.ConfigRev+`"` {
			t.Errorf("ETag = %q, configRev = %q", rec.Header().Get("ETag"), body.Project.ConfigRev)
		}
		if len(body.Writes.Written) != 1 || body.Writes.Written[0].Path != "docs/.pmngr/project.yaml" {
			t.Errorf("writes = %+v", body.Writes)
		}
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if !strings.Contains(string(data),
			"    # Ordinary work only.\n    - {id: triage, name: Triage, category: triage}\n") {
			t.Errorf("project.yaml on disk:\n%s", data)
		}
		select {
		case ev := <-client.events:
			if changed, ok := ev.Data.(fileChangedData); !ok || changed.Path != "docs/.pmngr/project.yaml" {
				t.Errorf("payload = %+v", ev.Data)
			}
		default:
			t.Error("enabling the inbox published no file.changed")
		}

		// The project now has an inbox: a submission is accepted.
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/items",
			body: map[string]any{"type": "story", "title": "Report", "inbox": map[string]any{"source": "web"}},
		}), http.StatusCreated, nil)

		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodPost, target: "/api/v1/projects/OLD/inbox"}),
			http.StatusConflict, &doc)
		if doc.Code != "inbox_already_enabled" {
			t.Errorf("second enable code = %q", doc.Code)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		clash := strings.Replace(legacyProjectYAML, "{id: backlog, name: Backlog, category: todo}",
			"{id: backlog, name: Backlog, category: todo}\n    - {id: triage, name: Later, category: todo}", 1)
		tests := []struct {
			name   string
			config string
			target string
			header map[string]string
			status int
			code   string
		}{
			{
				name: "a stale revision", config: legacyProjectYAML, target: "/api/v1/projects/OLD/inbox",
				header: map[string]string{"If-Match": "sha256:0000000000000000"},
				status: http.StatusPreconditionFailed, code: "stale_revision",
			},
			{
				name: "an id clash", config: clash, target: "/api/v1/projects/OLD/inbox",
				status: http.StatusConflict, code: "triage_status_id_taken",
			},
			{
				name: "an unknown project", config: legacyProjectYAML, target: "/api/v1/projects/NOPE/inbox",
				status: http.StatusNotFound, code: codeNotFound,
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				s, configPath := newLegacyServer(t, tc.config)
				var doc problemBody
				decode(t, send(t, s, request{method: http.MethodPost, target: tc.target, header: tc.header}),
					tc.status, &doc)
				if doc.Code != tc.code {
					t.Errorf("code = %q, want %q", doc.Code, tc.code)
				}
				data, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatalf("read back: %v", err)
				}
				if string(data) != tc.config {
					t.Errorf("a refused call rewrote project.yaml:\n%s", data)
				}
			})
		}
	})
}
