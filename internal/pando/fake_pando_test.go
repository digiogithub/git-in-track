package pando

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakePando is an in-process Pando: a real MCP server built with the same
// go-sdk the client speaks to, served over the same streamable HTTP handler,
// fronted by the bearer-token check Pando's transport performs. Faking the
// tools rather than the protocol is what makes these tests worth having: the
// handshake, the session id and the framing are the SDK's own.
type fakePando struct {
	srv   *httptest.Server
	token string

	mu      sync.Mutex
	tools   map[string]toolFunc
	calls   []recordedCall
	handler http.Handler
}

type toolFunc func(ctx context.Context, args map[string]any) (*mcpsdk.CallToolResult, error)

type recordedCall struct {
	name string
	args map[string]any
}

// fakeToolNames are the tools this package calls. They are registered up
// front; their behavior is swapped per test with setTool.
var fakeToolNames = []string{toolKBSearch, toolCodeSearch, toolCodeProjects, toolCodeIndex}

func newFakePando(t *testing.T, token string) *fakePando {
	t.Helper()
	f := &fakePando{token: token, tools: map[string]toolFunc{}}
	f.handler = f.buildHandler()
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePando) serve(w http.ResponseWriter, r *http.Request) {
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	h := f.handler
	f.mu.Unlock()
	h.ServeHTTP(w, r)
}

func (f *fakePando) buildHandler() http.Handler {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "pando-fake", Version: "test"}, nil)
	for _, name := range fakeToolNames {
		srv.AddTool(
			&mcpsdk.Tool{
				Name:        name,
				Description: name,
				InputSchema: map[string]any{"type": "object"},
			},
			func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				args := map[string]any{}
				if len(req.Params.Arguments) > 0 {
					_ = json.Unmarshal(req.Params.Arguments, &args)
				}
				f.mu.Lock()
				f.calls = append(f.calls, recordedCall{name: req.Params.Name, args: args})
				fn := f.tools[req.Params.Name]
				f.mu.Unlock()
				if fn == nil {
					return textResult("No documents found matching the query."), nil
				}
				return fn(ctx, args)
			},
		)
	}
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)
}

// restart throws away every session the way a Pando restart does: the client's
// session id is then unknown and the server answers 404.
func (f *fakePando) restart() {
	h := f.buildHandler()
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
}

func (f *fakePando) setTool(name string, fn toolFunc) {
	f.mu.Lock()
	f.tools[name] = fn
	f.mu.Unlock()
}

func (f *fakePando) mcpURL() string { return f.srv.URL + "/mcp" }

func (f *fakePando) callsFor(name string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, c := range f.calls {
		if c.name == name {
			out = append(out, c.args)
		}
	}
	return out
}

func (f *fakePando) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func textResult(s string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: s}}}
}

func errorResult(s string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: s}},
		IsError: true,
	}
}

// metadataResult mirrors how Pando's MCP server exposes a tool's structured
// metadata: a JSON string under structuredContent.metadata, beside the compact
// human-readable text.
func metadataResult(text string, metadata any) *mcpsdk.CallToolResult {
	raw, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return &mcpsdk.CallToolResult{
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: text}},
		StructuredContent: map[string]any{"metadata": string(raw)},
	}
}

// newTestClient builds a client against the fake, with a short deadline so a
// hung test fails fast.
func newTestClient(t *testing.T, f *fakePando, mutate func(*Options)) *Client {
	t.Helper()
	opts := Options{MCPURL: f.mcpURL(), Token: f.token, Timeout: 5 * timeUnit}
	if mutate != nil {
		mutate(&opts)
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// timeUnit keeps the deadlines in these tests in one place.
const timeUnit = time.Second
