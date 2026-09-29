package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
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
				fake.usePando(t, h.Config)
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
// the semantic searcher calls with Pando's own "nothing found" text, and
// code_impact_analysis and code_find_symbol — the call graph of impact tier
// 2 — with no callers and no definitions, and code_related_files — the probe
// that tells a real "no callers" from a project without call edges
// (GIT-US-0167) — with one coupled file.
// It counts the calls of each tool. With noCallEdges set it answers the graph
// tools the way a project indexed with BuildCodeGraph = false does.
type fakeSemanticPando struct {
	srv         *httptest.Server
	requests    atomic.Int64
	mu          sync.Mutex
	calls       map[string]int
	noCallEdges atomic.Bool
	// kbArgs are the arguments of every kb_search_documents call, in order.
	kbArgs []map[string]any
}

func newFakeSemanticPando(t *testing.T) *fakeSemanticPando {
	t.Helper()
	f := &fakeSemanticPando{calls: map[string]int{}}
	srv := sdk.NewServer(&sdk.Implementation{Name: "pando-fake", Version: "test"}, nil)
	for _, name := range []string{
		"kb_search_documents", "code_hybrid_search", "code_list_projects", "code_index_project",
		"code_impact_analysis", "code_find_symbol", "code_related_files",
	} {
		text := "No documents found matching the query."
		switch name {
		case "code_impact_analysis":
			text = `{"symbol":"NextID","count":0,"truncated":false,"callers":[]}`
		case "code_find_symbol":
			text = "No symbols found matching the pattern."
		case "code_related_files":
			text = "count: 1\nrelated[1]:\n  - file_path: src/alloc_test.go\n    reasons[1]: calls\n    score: 0.8\ntruncated: false"
		}
		srv.AddTool(
			&sdk.Tool{Name: name, Description: name, InputSchema: map[string]any{"type": "object"}},
			func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				var args map[string]any
				if req.Params != nil && len(req.Params.Arguments) > 0 {
					_ = json.Unmarshal(req.Params.Arguments, &args)
				}
				f.mu.Lock()
				f.calls[name]++
				if name == "kb_search_documents" {
					f.kbArgs = append(f.kbArgs, args)
				}
				f.mu.Unlock()
				answer := text
				if f.noCallEdges.Load() {
					// Pando's own sentences (internal/llm/tools/graph_tools.go).
					switch name {
					case "code_impact_analysis":
						answer = `No callers found for symbol "NextID" (nothing in the indexed graph depends on it, or the project lacks call edges for its language).`
					case "code_related_files":
						answer = `No related files found for "src/alloc.go" (no resolved imports or call coupling in the indexed graph).`
					}
				}
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: answer}}}, nil
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

// called reports how many times a tool was called.
func (f *fakeSemanticPando) called(tool string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[tool]
}

// kbSearches returns the arguments of every kb_search_documents call.
func (f *fakeSemanticPando) kbSearches() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.kbArgs...)
}

// usePando points the configuration at the fake.
func (f *fakeSemanticPando) usePando(t *testing.T, configPath string) {
	t.Helper()
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	cfg.Search.Pando.MCPURL = f.url()
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("save the configuration: %v", err)
	}
}
