package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/config"
)

// TestMCPStdioSemanticSearch is the regression test of GIT-US-0121: a stdio
// `gintrack mcp` must install the same semantic searcher `gintrack serve`
// does, so that search_semantic reaches the configured Pando instead of
// answering `unavailable`; and with no Pando configured it must still answer
// `unavailable` naming search_items as the fallback.
func TestMCPStdioSemanticSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("building the binary is too slow for -short")
	}
	binary := buildGintrack(t)

	tests := []struct {
		name  string
		pando bool
	}{
		{name: "pando configured: the searcher is installed", pando: true},
		{name: "no pando: unavailable naming search_items", pando: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.register()

			var fake *fakeSemanticPando
			if tt.pando {
				fake = newFakeSemanticPando(t)
				cfg, err := config.Load(h.Config)
				if err != nil {
					t.Fatalf("load the configuration: %v", err)
				}
				cfg.Search.Pando.MCPURL = fake.url()
				if err := config.Save(h.Config, cfg); err != nil {
					t.Fatalf("save the configuration: %v", err)
				}
			}

			res := callSemanticOverStdio(t, binary, h.Config)
			code, retry := toolErrorOf(t, res)
			if tt.pando {
				if code == "unavailable" {
					t.Fatalf("search_semantic answered unavailable although Pando is configured: %s",
						stdioText(res))
				}
				if fake.requests.Load() == 0 {
					t.Error("the configured Pando was never contacted")
				}
				return
			}
			if !res.IsError || code != "unavailable" {
				t.Fatalf("search_semantic without Pando = %s, want an unavailable error", stdioText(res))
			}
			if !strings.Contains(retry, "search_items") {
				t.Errorf("retry = %q, want it to name search_items", retry)
			}
		})
	}
}

// callSemanticOverStdio spawns `gintrack mcp` and runs one search_semantic.
func callSemanticOverStdio(t *testing.T, binary, configPath string) *sdk.CallToolResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "mcp") //nolint:gosec // the path is one this test built
	cmd.Env = append(os.Environ(), "GINTRACK_CONFIG="+configPath)
	cmd.Stderr = os.Stderr

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to `gintrack mcp` over stdio: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "search_semantic", Arguments: map[string]any{"query": "how do we sign in"},
	})
	if err != nil {
		t.Fatalf("call search_semantic: %v", err)
	}
	return res
}

// toolErrorOf decodes the structured refusal of a failed call; a successful
// call answers two empty strings.
func toolErrorOf(t *testing.T, res *sdk.CallToolResult) (code, retry string) {
	t.Helper()
	if !res.IsError {
		return "", ""
	}
	var wrapper struct {
		Error struct {
			Code  string `json:"code"`
			Retry string `json:"retry"`
		} `json:"error"`
	}
	text := stdioText(res)
	if err := json.Unmarshal([]byte(text), &wrapper); err != nil {
		t.Fatalf("the refusal is not structured: %s", text)
	}
	return wrapper.Error.Code, wrapper.Error.Retry
}

// fakeSemanticPando is an in-process Pando MCP endpoint answering every tool
// the semantic searcher calls with Pando's own "nothing found" text.
type fakeSemanticPando struct {
	srv      *httptest.Server
	requests atomic.Int64
}

func newFakeSemanticPando(t *testing.T) *fakeSemanticPando {
	t.Helper()
	f := &fakeSemanticPando{}
	srv := sdk.NewServer(&sdk.Implementation{Name: "pando-fake", Version: "test"}, nil)
	for _, name := range []string{
		"kb_search_documents", "code_hybrid_search", "code_list_projects", "code_index_project",
	} {
		srv.AddTool(
			&sdk.Tool{Name: name, Description: name, InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				return &sdk.CallToolResult{Content: []sdk.Content{
					&sdk.TextContent{Text: "No documents found matching the query."},
				}}, nil
			},
		)
	}
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSemanticPando) url() string { return f.srv.URL + "/mcp" }
