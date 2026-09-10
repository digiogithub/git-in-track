package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/config"
)

// newMCPAPIServer mounts a copy of the fixture with the MCP endpoint enabled.
func newMCPAPIServer(t *testing.T, allowWrite bool) (*Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Token:         "test-token",
		Version:       "0.0.1-test",
		Workspace:     "test",
		Repos:         []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:           func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
		MCPHTTP:       true,
		MCPAllowWrite: allowWrite,
		MCPAgent:      "test-agent",
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s, root
}

// mcpSession connects a real MCP client to the server's /mcp endpoint over the
// streamable-HTTP transport, through an httptest listener so that the request
// really crosses a socket and really carries the bearer token.
func mcpSession(t *testing.T, s *Server, token string) *sdk.ClientSession {
	t.Helper()

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &sdk.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect over streamable HTTP: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// bearerTransport adds the local API's bearer token to every request, which is
// what an agent runner's MCP client configuration does.
type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	if b.token != "" {
		clone.Header.Set("Authorization", "Bearer "+b.token)
	}
	//nolint:wrapcheck // a RoundTripper must return the transport's error untouched
	return http.DefaultTransport.RoundTrip(clone)
}

func TestMCPEndpointIsOffByDefault(t *testing.T) {
	s, _ := newAPIServer(t)

	rec := send(t, s, request{method: http.MethodPost, target: "/mcp", body: map[string]any{}})
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("POST /mcp answered %d, want 501: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "gintrack mcp") {
		t.Errorf("body = %s, want it to point at the stdio transport", rec.Body.String())
	}

	var caps struct {
		Features map[string]any `json:"features"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/capabilities"}), http.StatusOK, &caps)
	if caps.Features["mcpHttp"] != false {
		t.Errorf("mcpHttp = %v, want false", caps.Features["mcpHttp"])
	}
}

func TestMCPEndpointRequiresTheToken(t *testing.T) {
	s, _ := newMCPAPIServer(t, false)

	tests := []struct {
		name   string
		header map[string]string
		want   int
	}{
		{name: "no token", want: http.StatusUnauthorized},
		{name: "a wrong token", header: map[string]string{"Authorization": "Bearer nope"}, want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{}`))
			for k, v := range tt.header {
				r.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, r)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestMCPCapabilitiesReportTheEndpoint(t *testing.T) {
	s, _ := newMCPAPIServer(t, true)
	var caps struct {
		Features map[string]any `json:"features"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/capabilities"}), http.StatusOK, &caps)
	if caps.Features["mcpHttp"] != true || caps.Features["mcpWrite"] != true {
		t.Errorf("features = %v, want the MCP endpoint enabled and writable", caps.Features)
	}
	tools, ok := caps.Features["mcpTools"].([]any)
	if !ok || len(tools) != 13 {
		t.Errorf("mcpTools = %v, want the thirteen tools", caps.Features["mcpTools"])
	}
}

// TestMCPOverStreamableHTTP is the end-to-end check of the story's second
// acceptance criterion: the same tools `gintrack mcp` serves over stdio,
// reached over the streamable-HTTP transport from `gintrack serve`.
func TestMCPOverStreamableHTTP(t *testing.T) {
	s, _ := newMCPAPIServer(t, true)
	session := mcpSession(t, s, "test-token")
	ctx := context.Background()

	t.Run("lists the same thirteen tools", func(t *testing.T) {
		listed, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if len(listed.Tools) != 13 {
			t.Fatalf("tools = %d, want 13", len(listed.Tools))
		}
	})

	t.Run("reads the backlog", func(t *testing.T) {
		page := callMCP[struct {
			Items []struct {
				ID  string `json:"id"`
				Rev string `json:"rev"`
			} `json:"items"`
			Total int `json:"total"`
		}](ctx, t, session, "list_items", map[string]any{"project": "DEMO", "limit": 2})
		if page.Total == 0 || len(page.Items) == 0 {
			t.Fatalf("page = %+v", page)
		}
		for _, it := range page.Items {
			if it.Rev == "" {
				t.Errorf("%s came back without a rev", it.ID)
			}
		}
	})

	t.Run("a write reaches the event stream and the working tree", func(t *testing.T) {
		created := callMCP[struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		}](ctx, t, session, "create_task", map[string]any{
			"project": "DEMO", "title": "Ship the agent surface", "parent": "DEMO-US-0001",
		})
		if created.Item.ID == "" {
			t.Fatal("the create returned no id")
		}
		// The REST API and the MCP endpoint read the same index, so the item is
		// visible through the other surface immediately.
		var read struct {
			ID string `json:"id"`
		}
		// send builds its request from this subtest's own context, which is
		// what an HTTP client would do; the MCP session's ctx belongs to the
		// other surface and deliberately does not cross over.
		//nolint:contextcheck // the request carries the subtest's context, not the session's
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/items/" + created.Item.ID,
		}), http.StatusOK, &read)
		if read.ID != created.Item.ID {
			t.Errorf("the REST API does not see %s", created.Item.ID)
		}
	})

	t.Run("a traversal attempt is refused", func(t *testing.T) {
		res, err := session.CallTool(ctx, &sdk.CallToolParams{
			Name: "get_kb_page", Arguments: map[string]any{"path": "../../../../etc/passwd"},
		})
		if err != nil {
			t.Fatalf("call get_kb_page: %v", err)
		}
		if !res.IsError {
			t.Fatal("the traversal was served")
		}
		if !strings.Contains(mcpText(res), "forbidden_path") {
			t.Errorf("refusal = %s, want forbidden_path", mcpText(res))
		}
	})
}

func TestMCPHTTPIsReadOnlyWithoutAllowWrite(t *testing.T) {
	s, _ := newMCPAPIServer(t, false)
	session := mcpSession(t, s, "test-token")

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range listed.Tools {
		if strings.HasPrefix(tool.Name, "create_") || tool.Name == "update_item" {
			t.Errorf("%s is advertised by a read-only endpoint", tool.Name)
		}
	}
}

// callMCP runs one tool over a session and decodes its structured output.
func callMCP[T any](ctx context.Context, t *testing.T, session *sdk.ClientSession, name string, args any) T {
	t.Helper()

	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s failed: %s", name, mcpText(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encode the result of %s: %v", name, err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode the result of %s: %v (%s)", name, err, raw)
	}
	return out
}

// mcpText joins the text content blocks of a result.
func mcpText(res *sdk.CallToolResult) string {
	out := ""
	for _, c := range res.Content {
		if text, ok := c.(*sdk.TextContent); ok {
			out += text.Text
		}
	}
	return out
}

// The settings surface of the write tools (PATCH /api/v1/mcp/settings): the
// switch a person flips in the UI instead of teaching every agent runtime to
// pass `--allow-write`.

func TestMCPSettingsPersistTheWriteMode(t *testing.T) {
	root := copyTree(t, fixtureRoot)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(configPath, config.Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s, err := New(Options{
		Token:      "test-token",
		Version:    "0.0.1-test",
		Workspace:  "test",
		Repos:      []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	var body mcpSettingsView
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/mcp/settings"}), http.StatusOK, &body)
	if !body.Supported || body.AllowWrite || body.HTTP {
		t.Fatalf("initial settings = %+v, want a supported, read-only, endpoint-less server", body)
	}

	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/mcp/settings",
		body:   map[string]any{"allowWrite": true},
	}), http.StatusOK, &body)

	if !body.AllowWrite || !body.Persisted {
		t.Fatalf("after the switch = %+v, want the write tools on and the file written", body)
	}
	saved, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !saved.MCP.AllowWrite {
		// This is the whole point: `gintrack mcp` reads this file at startup,
		// so a bare `gintrack mcp` in an agent's configuration picks the write
		// tools up on its next run.
		t.Error("mcp.allowWrite was not written to the configuration file")
	}
}

func TestMCPSettingsSwitchTheLiveEndpoint(t *testing.T) {
	s, _ := newMCPAPIServer(t, false)

	var body mcpSettingsView
	decode(t, send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/mcp/settings",
		body:   map[string]any{"allowWrite": true},
	}), http.StatusOK, &body)

	if !body.AllowWrite || !body.HTTP {
		t.Fatalf("settings = %+v, want the write tools on for a mounted endpoint", body)
	}
	if body.Persisted {
		t.Error("a server started without a configuration file cannot have persisted anything")
	}

	// The endpoint is rebuilt in place, so a client connecting now is offered
	// the write tools without the companion being restarted.
	session := mcpSession(t, s, "test-token")
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	found := false
	for _, tool := range listed.Tools {
		if tool.Name == "create_task" {
			found = true
		}
	}
	if !found {
		t.Errorf("create_task is missing from %d tools after the switch", len(listed.Tools))
	}
}

func TestMCPSettingsRefuseAChangeThatWouldDoNothing(t *testing.T) {
	// No configuration file and no endpoint: the switch would flip a boolean
	// nothing reads, so the server says so rather than reporting success.
	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	rec := send(t, s, request{
		method: http.MethodPatch,
		target: "/api/v1/mcp/settings",
		body:   map[string]any{"allowWrite": true},
	})
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotImplemented)
	}

	var view mcpSettingsView
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/mcp/settings"}), http.StatusOK, &view)
	if view.Supported || view.AllowWrite {
		t.Errorf("settings = %+v, want an unsupported, read-only surface", view)
	}
}
