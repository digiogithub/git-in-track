package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
)

// ytToken is the credential every assertion in this file hunts for in the
// output. If it ever appears in a response, a problem document or a log, the
// promise of ADR-032 is broken.
const ytToken = "perm:jose.gintrack.s3cr3t-value"

// ytProjectKey is the project key the fixture backlog declares.
const ytProjectKey = "DEMO"

// ytStub is a fake YouTrack instance: it records what it was asked for and
// answers with whatever the test set.
type ytStub struct {
	server *httptest.Server
	// status is the status code every endpoint answers with; zero means 200.
	status int
	// paths records the request paths, so a test can prove a call was made.
	paths []string
	// authorizations records the Authorization headers the stub received.
	authorizations []string
}

// newYTStub starts a stub YouTrack instance and stops it with the test.
func newYTStub(t *testing.T) *ytStub {
	t.Helper()

	stub := &ytStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.paths = append(stub.paths, r.URL.Path)
		stub.authorizations = append(stub.authorizations, r.Header.Get("Authorization"))
		if stub.status != 0 && stub.status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(stub.status)
			// A real instance echoes a body; a stray echo of the credential in
			// it is exactly what the redaction assertions must survive.
			_, _ = w.Write([]byte(`{"error":"denied","error_description":"Bearer ` + ytToken + ` was refused"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/users/me"):
			_, _ = w.Write([]byte(`{"id":"1-1","login":"jose","fullName":"Jose F. Rives","email":"jose@example.com"}`))
		case strings.Contains(r.URL.Path, "/customFieldSettings"):
			_, _ = w.Write([]byte(`[{"id":"f1","field":{"id":"d1","name":"State","fieldType":{"id":"state[1]"}},` +
				`"bundle":{"id":"b1","$type":"StateBundle"},"canBeEmpty":false}]`))
		case strings.HasSuffix(r.URL.Path, "/api/admin/projects"):
			_, _ = w.Write([]byte(`[{"id":"0-1","shortName":"ACME","name":"ACME API","archived":false}]`))
		default:
			_, _ = w.Write([]byte(`{"id":"0-1","shortName":"ACME","name":"ACME API"}`))
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// URL is the base URL of the stub instance.
func (s *ytStub) URL() string { return s.server.URL }

// newYouTrackServer mounts the fixture, optionally writes a YouTrack block into
// its project.yaml and optionally stores a token, and returns the server, the
// served directory and the companion configuration file.
func newYouTrackServer(t *testing.T, link *config.YouTrackLink, token string, withConfigFile bool) (*Server, string, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	if link != nil {
		if _, err := config.SaveYouTrackLink(ytProjectYAML(root), *link); err != nil {
			t.Fatalf("write the link: %v", err)
		}
	}
	opts := Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC) },
	}
	configPath := ""
	if withConfigFile {
		configPath = filepath.Join(t.TempDir(), "config.yaml")
		if err := config.Save(configPath, config.Default()); err != nil {
			t.Fatalf("write the configuration: %v", err)
		}
		opts.ConfigPath = configPath
	}
	if token != "" {
		cfg := config.Default()
		cfg.SetYouTrackToken(ytProjectKey, token)
		opts.YouTrack = cfg.YouTrackTokens()
		if configPath != "" {
			if err := config.Save(configPath, cfg); err != nil {
				t.Fatalf("write the configuration: %v", err)
			}
		}
	}
	s, err := New(opts)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s, root, configPath
}

// ytProjectYAML is the fixture's project.yaml.
func ytProjectYAML(root string) string {
	return filepath.Join(root, "docs", ".pmngr", "project.yaml")
}

// ytSettingsBody is the documented shape of /api/v1/youtrack/settings.
type ytSettingsBody struct {
	ProjectKey      string          `json:"projectKey"`
	Configured      bool            `json:"configured"`
	URL             string          `json:"url"`
	Project         string          `json:"project"`
	ProjectID       string          `json:"projectId"`
	FieldMap        config.FieldMap `json:"fieldMap"`
	PushComments    string          `json:"pushComments"`
	KBSync          string          `json:"kbSync"`
	KBSyncDirection string          `json:"kbSyncDirection"`
	HasToken        bool            `json:"hasToken"`
	TokenSource     string          `json:"tokenSource"`
	Persisted       bool            `json:"persisted"`
	ProjectPath     string          `json:"projectPath"`
	Repo            string          `json:"repo"`
}

// TestYouTrackRoutesRequireTheBearerToken proves the whole subtree sits inside
// the authenticated group.
func TestYouTrackRoutesRequireTheBearerToken(t *testing.T) {
	t.Parallel()

	s, _, _ := newYouTrackServer(t, nil, "", false)
	for _, target := range []string{
		"/api/v1/youtrack/settings",
		"/api/v1/youtrack/projects",
		"/api/v1/youtrack/fields",
	} {
		resp := do(t, s, http.MethodGet, target, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", target, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	resp := do(t, s, http.MethodPost, "/api/v1/youtrack/test", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /test without a token = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestYouTrackSettingsUnconfigured is what a fresh project answers.
func TestYouTrackSettingsUnconfigured(t *testing.T) {
	t.Parallel()

	s, _, _ := newYouTrackServer(t, nil, "", false)
	var body ytSettingsBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/settings"}), http.StatusOK, &body)

	if body.Configured {
		t.Error("an untouched project reported a connection")
	}
	if body.HasToken || body.TokenSource != string(config.TokenSourceNone) {
		t.Errorf("token = %v from %q, want none", body.HasToken, body.TokenSource)
	}
	if body.ProjectKey != ytProjectKey {
		t.Errorf("projectKey = %q, want %q", body.ProjectKey, ytProjectKey)
	}
}

// TestYouTrackSettingsPatch covers the write path: the committed half reaches
// project.yaml, the token reaches the 0600 file, and neither comes back.
func TestYouTrackSettingsPatch(t *testing.T) {
	t.Parallel()

	s, root, configPath := newYouTrackServer(t, nil, "", true)
	rec := send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body: map[string]any{
			"url":          "https://yt.example.com/youtrack",
			"project":      "ACME",
			"fieldMap":     map[string]string{"status": "State"},
			"pushComments": "auto",
			"token":        ytToken,
		},
	})
	var body ytSettingsBody
	decode(t, rec, http.StatusOK, &body)

	if !body.Configured || body.URL != "https://yt.example.com/youtrack" || body.Project != "ACME" {
		t.Fatalf("settings = %+v", body)
	}
	if body.PushComments != "auto" || body.KBSync != "manual" || body.KBSyncDirection != "push" {
		t.Errorf("modes = %q/%q/%q", body.PushComments, body.KBSync, body.KBSyncDirection)
	}
	if !body.HasToken || body.TokenSource != string(config.TokenSourceFile) {
		t.Errorf("token = %v from %q, want one from the file", body.HasToken, body.TokenSource)
	}
	if !body.Persisted {
		t.Error("persisted = false although the server has a configuration path")
	}
	if strings.Contains(rec.Body.String(), "s3cr3t-value") {
		t.Errorf("the response carried the token: %s", rec.Body.String())
	}

	// The committed half landed in project.yaml, the secret half did not.
	yaml, err := os.ReadFile(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	if !strings.Contains(string(yaml), "yt.example.com") {
		t.Errorf("project.yaml has no link:\n%s", yaml)
	}
	if strings.Contains(string(yaml), "s3cr3t-value") {
		t.Fatalf("the token was committed to project.yaml:\n%s", yaml)
	}
	if !strings.Contains(string(yaml), "# Fixture project used by the internal/core tests.") {
		t.Errorf("the write lost the file's comments:\n%s", yaml)
	}

	// The secret half landed in the machine-local file, which stays 0600.
	stored, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	if token, source := stored.YouTrackToken(ytProjectKey); token != ytToken || source != config.TokenSourceFile {
		t.Errorf("stored token = %q from %q", token, source)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("configuration mode = %o, want 600", perm)
	}
}

// TestYouTrackSettingsPatchRecordsTheEntityID covers what the picker saves: the
// short name a person reads and the entity id a write is addressed by travel
// together, and moving the project without a new id drops the old one rather
// than leaving a create pointed at the previous project.
func TestYouTrackSettingsPatchRecordsTheEntityID(t *testing.T) {
	t.Parallel()

	s, root, _ := newYouTrackServer(t, nil, "", false)
	var body ytSettingsBody
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body: map[string]any{
			"url": "https://yt.example.com/youtrack", "project": "ACME", "projectId": "0-17",
		},
	}), http.StatusOK, &body)
	if body.Project != "ACME" || body.ProjectID != "0-17" {
		t.Fatalf("settings = %+v", body)
	}
	yaml, err := os.ReadFile(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	if !strings.Contains(string(yaml), "project_id: 0-17") {
		t.Errorf("project.yaml does not record the entity id:\n%s", yaml)
	}

	// A different project, and no id with it: the old id must not survive. The
	// answer omits an empty id, so it is decoded into a fresh value rather than
	// over the one that is already there.
	body = ytSettingsBody{}
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body:   map[string]any{"project": "OTHER"},
	}), http.StatusOK, &body)
	if body.ProjectID != "" {
		t.Errorf("projectId = %q, want it cleared with the project it belonged to", body.ProjectID)
	}
	if yaml, err = os.ReadFile(ytProjectYAML(root)); err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	if strings.Contains(string(yaml), "project_id") {
		t.Errorf("project.yaml kept the previous project's entity id:\n%s", yaml)
	}

	// An id that is not one is refused rather than written and failed later.
	var problem problemBody
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body:   map[string]any{"projectId": "DIGIO"},
	}), http.StatusBadRequest, &problem)
	if problem.Code == "" {
		t.Error("a short name was accepted as an entity id")
	}
}

// TestYouTrackSettingsPatchWithoutAConfigFile is the contract the git settings
// already set: the running process takes the change, the file does not exist to
// take it, and the answer says so.
func TestYouTrackSettingsPatchWithoutAConfigFile(t *testing.T) {
	t.Parallel()

	s, _, _ := newYouTrackServer(t, nil, "", false)
	var body ytSettingsBody
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body:   map[string]any{"url": "https://yt.example.com", "project": "ACME", "token": ytToken},
	}), http.StatusOK, &body)

	if body.Persisted {
		t.Error("persisted = true although the server was started without a configuration path")
	}
	if !body.HasToken {
		t.Error("the running process did not take the token")
	}
}

// TestYouTrackSettingsPatchRejectsAnInvalidLink proves a refused write leaves
// project.yaml alone.
func TestYouTrackSettingsPatchRejectsAnInvalidLink(t *testing.T) {
	t.Parallel()

	s, root, _ := newYouTrackServer(t, nil, "", false)
	before, err := os.ReadFile(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	rec := send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/youtrack/settings",
		body:   map[string]any{"url": "not-a-url", "project": "ACME"},
	})
	var problem problemBody
	decode(t, rec, http.StatusBadRequest, &problem)
	if problem.Code != codeInvalidRequest {
		t.Errorf("code = %q, want %q", problem.Code, codeInvalidRequest)
	}
	after, err := os.ReadFile(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	if string(after) != string(before) {
		t.Error("a refused write still edited project.yaml")
	}
}

// TestYouTrackTest covers the probe: the success shape and every failure mode
// mapping onto its own problem code, with the token never rendered.
func TestYouTrackTest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		wantStatus int
		wantCode   string
	}{
		{name: "success", wantStatus: http.StatusOK},
		{name: "bad token", status: http.StatusUnauthorized, wantStatus: http.StatusBadGateway, wantCode: codeYouTrackUnauthorized},
		{name: "no permission", status: http.StatusForbidden, wantStatus: http.StatusBadGateway, wantCode: codeYouTrackForbidden},
		{name: "missing context path", status: http.StatusNotFound, wantStatus: http.StatusBadGateway, wantCode: codeYouTrackNotFound},
		{name: "server error", status: http.StatusInternalServerError, wantStatus: http.StatusBadGateway, wantCode: codeYouTrackUnreachable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stub := newYTStub(t)
			stub.status = tc.status
			link := &config.YouTrackLink{URL: stub.URL(), Project: "ACME"}
			s, _, _ := newYouTrackServer(t, link, ytToken, false)

			rec := send(t, s, request{method: http.MethodPost, target: "/api/v1/youtrack/test"})
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "s3cr3t-value") {
				t.Fatalf("the response leaked the token: %s", rec.Body.String())
			}
			if tc.wantCode == "" {
				var out struct {
					OK       bool   `json:"ok"`
					Login    string `json:"login"`
					FullName string `json:"fullName"`
					Project  string `json:"project"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if !out.OK || out.Login != "jose" || out.FullName != "Jose F. Rives" {
					t.Errorf("result = %+v", out)
				}
				if out.Project != "ACME" {
					t.Errorf("project = %q, want the linked one", out.Project)
				}
				if got := stub.authorizations[0]; got != "Bearer "+ytToken {
					t.Errorf("the stub saw %q", got)
				}
				return
			}
			var problem problemBody
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if problem.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", problem.Code, tc.wantCode)
			}
			if problem.Detail == "" {
				t.Error("the problem carries no actionable detail")
			}
		})
	}
}

// TestYouTrackTestWithAnUnsavedConnection proves a user can test before saving.
func TestYouTrackTestWithAnUnsavedConnection(t *testing.T) {
	t.Parallel()

	stub := newYTStub(t)
	s, root, _ := newYouTrackServer(t, nil, "", false)

	rec := send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/test",
		body:   map[string]any{"url": stub.URL(), "token": ytToken},
	})
	decode(t, rec, http.StatusOK, nil)

	yaml, err := os.ReadFile(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	if strings.Contains(string(yaml), stub.URL()) {
		t.Error("a connection test wrote the connection to project.yaml")
	}
	var body ytSettingsBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/settings"}), http.StatusOK, &body)
	if body.HasToken {
		t.Error("a connection test stored the token")
	}
}

// TestYouTrackNotConfigured proves the discovery endpoints refuse cleanly
// rather than reaching for a nil client.
func TestYouTrackNotConfigured(t *testing.T) {
	t.Parallel()

	s, _, _ := newYouTrackServer(t, nil, "", false)
	for _, target := range []string{"/api/v1/youtrack/projects", "/api/v1/youtrack/fields"} {
		var problem problemBody
		decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusConflict, &problem)
		if problem.Code != codeYouTrackNotConfigured {
			t.Errorf("GET %s: code = %q, want %q", target, problem.Code, codeYouTrackNotConfigured)
		}
	}
}

// TestYouTrackDiscovery covers the two endpoints the settings UI calls on a
// keystroke.
func TestYouTrackDiscovery(t *testing.T) {
	t.Parallel()

	stub := newYTStub(t)
	link := &config.YouTrackLink{URL: stub.URL(), Project: "ACME"}
	s, _, _ := newYouTrackServer(t, link, ytToken, false)

	var projects struct {
		Projects []struct {
			ID        string `json:"id"`
			ShortName string `json:"shortName"`
		} `json:"projects"`
		Limit int `json:"limit"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/projects?q=ac"}), http.StatusOK, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].ShortName != "ACME" {
		t.Fatalf("projects = %+v", projects)
	}
	if projects.Limit != youtrackMaxProjects {
		t.Errorf("limit = %d, want the documented cap %d", projects.Limit, youtrackMaxProjects)
	}

	var fields struct {
		Project        string `json:"project"`
		GintrackFields []string
		Fields         []struct {
			Name       string `json:"name"`
			Type       string `json:"type"`
			BundleType string `json:"bundleType"`
		} `json:"fields"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/fields"}), http.StatusOK, &fields)
	if len(fields.Fields) != 1 || fields.Fields[0].Name != "State" {
		t.Fatalf("fields = %+v", fields)
	}
	if fields.Project != "ACME" {
		t.Errorf("project = %q, want the linked one", fields.Project)
	}

	// Several calls in a row share one client, and therefore one rate limiter,
	// rather than opening a fresh bucket per keystroke.
	before, _, _ := s.youtrack.clientFor(ytProjectKey)
	after, _, _ := s.youtrack.clientFor(ytProjectKey)
	if before == nil || before != after {
		t.Error("the client is rebuilt on every call, so the rate limit is not shared")
	}
}

// TestYouTrackProjectsWithAnUnsavedConnection covers the picker the settings
// card drives while it is being filled in: choosing the remote project is part
// of connecting, so the list has to be readable with a URL and a token that
// have been typed and not yet saved — and reading it must store neither.
func TestYouTrackProjectsWithAnUnsavedConnection(t *testing.T) {
	t.Parallel()

	stub := newYTStub(t)
	s, root, _ := newYouTrackServer(t, nil, "", false)

	var projects struct {
		Projects []struct {
			ID        string `json:"id"`
			ShortName string `json:"shortName"`
		} `json:"projects"`
	}
	rec := send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/youtrack/projects",
		body:   map[string]any{"url": stub.URL(), "token": ytToken, "q": "ac"},
	})
	decode(t, rec, http.StatusOK, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].ShortName != "ACME" {
		t.Fatalf("projects = %+v", projects)
	}
	// The entity id is the whole point of the picker: it is what a create is
	// addressed by, and typing a short name cannot produce it.
	if projects.Projects[0].ID != "0-1" {
		t.Errorf("project id = %q, want the entity id 0-1", projects.Projects[0].ID)
	}

	yaml, err := os.ReadFile(ytProjectYAML(root))
	if err != nil {
		t.Fatalf("read project.yaml: %v", err)
	}
	if strings.Contains(string(yaml), stub.URL()) {
		t.Error("listing projects wrote the connection to project.yaml")
	}
	var body ytSettingsBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/settings"}), http.StatusOK, &body)
	if body.HasToken {
		t.Error("listing projects stored the token")
	}
	if rec.Body.String() != "" && strings.Contains(rec.Body.String(), ytToken) {
		t.Error("the answer echoed the token")
	}
}

// TestYouTrackCapability proves features.youtrack answers the configured
// question and nothing else.
func TestYouTrackCapability(t *testing.T) {
	t.Parallel()

	read := func(t *testing.T, s *Server) map[string]any {
		t.Helper()
		var body struct {
			Features map[string]any `json:"features"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/capabilities"}), http.StatusOK, &body)
		return body.Features
	}

	t.Run("unconfigured", func(t *testing.T) {
		t.Parallel()
		s, _, _ := newYouTrackServer(t, nil, "", false)
		features := read(t, s)
		if features["youtrack"] != false {
			t.Errorf("features.youtrack = %v, want false", features["youtrack"])
		}
		if features["youtrackSupported"] != true {
			t.Errorf("features.youtrackSupported = %v, want true in companion mode", features["youtrackSupported"])
		}
	})

	t.Run("configured", func(t *testing.T) {
		t.Parallel()
		link := &config.YouTrackLink{URL: "https://yt.example.com", Project: "ACME"}
		s, _, _ := newYouTrackServer(t, link, ytToken, false)
		if features := read(t, s); features["youtrack"] != true {
			t.Errorf("features.youtrack = %v, want true", features["youtrack"])
		}
	})
}

// TestYouTrackTokenSourceReportsTheEnvironment proves the provenance a user
// sees matches where the token actually came from.
func TestYouTrackTokenSourceReportsTheEnvironment(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.SetYouTrackTokenOverride(ytToken, config.TokenSourceEnv)

	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Token:     "test-token",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		YouTrack:  cfg.YouTrackTokens(),
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	var body ytSettingsBody
	rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/settings"})
	decode(t, rec, http.StatusOK, &body)
	if !body.HasToken || body.TokenSource != string(config.TokenSourceEnv) {
		t.Errorf("token = %v from %q, want one from the environment", body.HasToken, body.TokenSource)
	}
	if strings.Contains(rec.Body.String(), "s3cr3t-value") {
		t.Errorf("the response leaked the token: %s", rec.Body.String())
	}
}
