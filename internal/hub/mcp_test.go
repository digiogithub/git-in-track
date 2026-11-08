package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpAs opens an MCP session on the hub as one principal.
func mcpAs(t *testing.T, f *grantFixture, handle string) *sdk.ClientSession {
	t.Helper()
	ts := httptest.NewServer(f.hub.Server().Handler())
	t.Cleanup(ts.Close)
	client := sdk.NewClient(&sdk.Implementation{Name: "test-agent", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{token: f.tokens[handle]}},
	}, nil)
	if err != nil {
		t.Fatalf("connect as %s: %v", handle, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// tool calls one tool and returns its structured answer, or the error text.
func tool(t *testing.T, s *sdk.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := s.CallTool(t.Context(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	if res.IsError {
		return nil, text.String()
	}
	out := map[string]any{}
	raw, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(raw, &out)
	return out, ""
}

func TestHubMemoryAndKBTools(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t)
	ana, prop := mcpAs(t, f, "ana"), mcpAs(t, f, "proposer")
	clone := f.root + "/acme"

	t.Run("the hub tools are listed", func(t *testing.T) {
		list, err := ana.ListTools(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, tl := range list.Tools {
			names[tl.Name] = true
		}
		for _, want := range []string{"write_kb_page", "add_memory", "propose_memory", "update_memory", "forget_memory",
			"accept_memory", "reject_memory", "search_memory", "project_context"} {
			if !names[want] {
				t.Errorf("%s is not listed", want)
			}
		}
	})

	t.Run("a writer's memory is active and committed under its name", func(t *testing.T) {
		out, errText := tool(t, ana, "add_memory", map[string]any{"project": "ACME", "key": "invoicing", "content": "Customer X is invoiced quarterly."})
		if errText != "" || out["status"] != "active" || out["outcome"] != "created" {
			t.Fatalf("add_memory = %v %s", out, errText)
		}
		f.hub.Server().SyncNow(t.Context())
		if got := git(t, clone, "log", "-1", "--format=%an|%cn"); got != "ana|gintrack-hub" {
			t.Errorf("author|committer = %q", got)
		}
	})

	t.Run("a proposer's memory is a proposal, accepted by a writer", func(t *testing.T) {
		out, errText := tool(t, prop, "add_memory", map[string]any{"project": "ACME", "key": "vat", "content": "VAT is 21%."})
		if errText != "" || out["status"] != "proposed" || !strings.Contains(out["note"].(string), "proposal") {
			t.Fatalf("add_memory as proposer = %v %s", out, errText)
		}
		if found, _ := tool(t, ana, "search_memory", map[string]any{"project": "ACME", "query": "vat"}); found["total"] != float64(0) {
			t.Errorf("a proposal is visible before review: %v", found)
		}
		if _, errText := tool(t, prop, "accept_memory", map[string]any{"id": out["id"], "rev": out["rev"]}); errText == "" {
			t.Error("a proposer accepted its own proposal")
		}
		accepted, errText := tool(t, ana, "accept_memory", map[string]any{"id": out["id"], "rev": out["rev"]})
		if errText != "" || accepted["status"] != "active" {
			t.Fatalf("accept_memory = %v %s", accepted, errText)
		}
		found, _ := tool(t, ana, "search_memory", map[string]any{"project": "ACME", "query": "vat"})
		if found["total"] != float64(1) {
			t.Errorf("search after accept = %v", found)
		}
	})

	t.Run("update needs the rev and forget withdraws", func(t *testing.T) {
		found, _ := tool(t, ana, "search_memory", map[string]any{"project": "ACME", "query": "invoiced"})
		row := found["memories"].([]any)[0].(map[string]any)
		if _, errText := tool(t, ana, "update_memory", map[string]any{"project": "ACME", "key": "invoicing", "rev": "sha256:stale", "content": "x"}); !strings.Contains(errText, "stale") {
			t.Errorf("a stale rev = %q", errText)
		}
		out, errText := tool(t, ana, "update_memory", map[string]any{"project": "ACME", "key": "invoicing", "rev": row["rev"], "content": "Customer X is invoiced monthly."})
		if errText != "" || out["outcome"] != "updated" {
			t.Fatalf("update_memory = %v %s", out, errText)
		}
		if _, errText := tool(t, prop, "forget_memory", map[string]any{"project": "ACME", "key": "invoicing"}); errText == "" {
			t.Error("a proposer forgot a memory")
		}
		gone, errText := tool(t, ana, "forget_memory", map[string]any{"project": "ACME", "key": "invoicing"})
		if errText != "" || gone["status"] != "forgotten" {
			t.Fatalf("forget_memory = %v %s", gone, errText)
		}
	})

	t.Run("write_kb_page creates, and refuses to overwrite without a rev", func(t *testing.T) {
		out, errText := tool(t, ana, "write_kb_page", map[string]any{"project": "ACME", "path": "guides/onboarding.md", "text": "# Onboarding\n"})
		if errText != "" || out["path"] != "docs/guides/onboarding.md" {
			t.Fatalf("write_kb_page = %v %s", out, errText)
		}
		if _, errText := tool(t, ana, "write_kb_page", map[string]any{"project": "ACME", "path": "intro.md", "text": "# Overwritten\n"}); !strings.Contains(errText, "already_exists") && !strings.Contains(errText, "already exists") {
			t.Errorf("overwrite without rev = %q", errText)
		}
		if _, errText := tool(t, prop, "write_kb_page", map[string]any{"project": "ACME", "path": "x.md", "text": "# x\n"}); errText == "" {
			t.Error("a proposer wrote a page")
		}
		if _, errText := tool(t, ana, "write_kb_page", map[string]any{"project": "WEB", "path": "x.md", "text": "# x\n"}); errText == "" {
			t.Error("ana wrote a page in WEB")
		}
	})

	t.Run("project_context fits the budget and stays in scope", func(t *testing.T) {
		out, errText := tool(t, ana, "project_context", map[string]any{"project": "ACME", "budget": 200})
		if errText != "" {
			t.Fatal(errText)
		}
		raw, _ := json.Marshal(out)
		if strings.Contains(string(raw), "WEB") || !strings.Contains(string(raw), "vat") {
			t.Errorf("project_context = %s", raw)
		}
		if _, errText := tool(t, ana, "project_context", map[string]any{"project": "WEB"}); errText == "" {
			t.Error("project_context of WEB answered ana")
		}
	})
}
