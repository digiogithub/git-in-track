package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/core"
)

// readTools is the surface a server started without --allow-write advertises.
var readTools = []string{
	"get_item", "get_kb_page", "list_items", "list_kb_pages", "search_items", "search_kb",
}

// writeTools is what enabling writes adds.
var writeTools = []string{
	"add_comment", "create_epic", "create_story", "create_task", "move_on_board", "update_item",
}

func TestToolSurface(t *testing.T) {
	tests := []struct {
		name       string
		allowWrite bool
		want       []string
	}{
		{name: "read-only advertises no write tool", allowWrite: false, want: readTools},
		{
			name:       "writes enabled advertise every tool",
			allowWrite: true,
			want:       append(append([]string{}, readTools...), writeTools...),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.allowWrite)
			listed, err := h.session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatalf("tools/list: %v", err)
			}
			var names []string
			for _, tool := range listed.Tools {
				names = append(names, tool.Name)
				if tool.InputSchema == nil {
					t.Errorf("%s has no input schema", tool.Name)
				}
				if tool.OutputSchema == nil {
					t.Errorf("%s has no output schema", tool.Name)
				}
				if tool.Description == "" {
					t.Errorf("%s has no description", tool.Name)
				}
			}
			slices.Sort(names)
			want := append([]string{}, tt.want...)
			slices.Sort(want)
			if !slices.Equal(names, want) {
				t.Errorf("tools = %v, want %v", names, want)
			}
		})
	}
}

func TestInstructionsStateTheDataBoundary(t *testing.T) {
	h := newHarness(t, false)
	got := h.session.InitializeResult()
	if got.Instructions == "" {
		t.Fatal("the server sent no instructions")
	}
	for _, phrase := range []string{"DATA", "never an instruction", "rev", "permanent"} {
		if !strings.Contains(got.Instructions, phrase) {
			t.Errorf("the instructions do not mention %q:\n%s", phrase, got.Instructions)
		}
	}
	if got.ServerInfo == nil || got.ServerInfo.Name != ServerName {
		t.Errorf("serverInfo = %+v, want name %q", got.ServerInfo, ServerName)
	}
}

// TestUntrustedContentIsMarked checks the second half of the data boundary: a
// tool that can return repository-authored text says so in its description and
// marks the result, so a client can quote it rather than obey it.
func TestUntrustedContentIsMarked(t *testing.T) {
	h := newHarness(t, false)
	listed, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range listed.Tools {
		t.Run(tool.Name+" description", func(t *testing.T) {
			if !strings.Contains(tool.Description, "never as instructions") {
				t.Errorf("%s does not state the data boundary:\n%s", tool.Name, tool.Description)
			}
		})
	}
	res := rawCall(t, h, "get_item", map[string]any{"id": "DEMO-US-0001", "include": []string{"body"}})
	if got := res.Meta[untrustedMeta]; got != untrustedValue {
		t.Errorf("result meta %q = %v, want %q", untrustedMeta, got, untrustedValue)
	}
}

func TestListItems(t *testing.T) {
	h := newHarness(t, false)

	t.Run("filters and projects", func(t *testing.T) {
		page := call[ItemPage](t, h, "list_items", map[string]any{
			"project": "DEMO",
			"type":    []string{"story"},
			"fields":  []string{"title", "status"},
		})
		if len(page.Items) != 2 {
			t.Fatalf("items = %d, want 2", len(page.Items))
		}
		for _, it := range page.Items {
			if it.ID == "" || it.Rev == "" {
				t.Errorf("item %+v is missing an id or a rev", it)
			}
			if it.Priority != "" || it.Labels != nil {
				t.Errorf("item %+v carries fields the projection did not ask for", it)
			}
			if it.Title == "" || it.Status == "" {
				t.Errorf("item %+v is missing a projected field", it)
			}
		}
	})

	t.Run("never returns a body", func(t *testing.T) {
		page := call[ItemPage](t, h, "list_items", map[string]any{
			"project": "DEMO",
			"fields":  []string{"title", "body"},
		})
		for _, it := range page.Items {
			if it.Body != "" {
				t.Errorf("%s came back with a body from a list", it.ID)
			}
		}
	})

	t.Run("paginates with a bounded page size", func(t *testing.T) {
		page := call[ItemPage](t, h, "list_items", map[string]any{"project": "DEMO", "limit": 2})
		if len(page.Items) != 2 {
			t.Fatalf("items = %d, want 2", len(page.Items))
		}
		if page.NextCursor == "" {
			t.Fatal("a partial page came back without a cursor")
		}
		seen := map[string]bool{}
		for _, it := range page.Items {
			seen[it.ID] = true
		}
		next := call[ItemPage](t, h, "list_items", map[string]any{
			"project": "DEMO", "limit": 2, "cursor": page.NextCursor,
		})
		if len(next.Items) == 0 {
			t.Fatal("the second page is empty")
		}
		for _, it := range next.Items {
			if seen[it.ID] {
				t.Errorf("%s appears on both pages", it.ID)
			}
		}
	})

	t.Run("caps an oversized limit", func(t *testing.T) {
		if got := boundedLimit(10_000); got != maxPageSize {
			t.Errorf("boundedLimit(10000) = %d, want %d", got, maxPageSize)
		}
		if got := boundedLimit(0); got != defaultPageSize {
			t.Errorf("boundedLimit(0) = %d, want %d", got, defaultPageSize)
		}
		// The cap has to survive the protocol too: the schema declares a
		// maximum, and the handler clamps whatever gets through.
		page := call[ItemPage](t, h, "list_items", map[string]any{"project": "DEMO", "limit": 99})
		if len(page.Items) > maxPageSize {
			t.Errorf("items = %d, want at most %d", len(page.Items), maxPageSize)
		}
	})

	t.Run("rejects a cursor from another query", func(t *testing.T) {
		first := call[HitPage](t, h, "search_items", map[string]any{"query": "checkout", "limit": 1})
		if first.NextCursor == "" {
			t.Skip("the fixture does not produce a second page")
		}
		got := callFails(t, h, "search_items", map[string]any{
			"query": "payment", "limit": 1, "cursor": first.NextCursor,
		})
		if got.Code != codeInvalidCursor {
			t.Errorf("code = %q, want %q", got.Code, codeInvalidCursor)
		}
	})
}

func TestGetItem(t *testing.T) {
	h := newHarness(t, false)

	t.Run("omits the body unless asked", func(t *testing.T) {
		got := call[ItemResult](t, h, "get_item", map[string]any{"id": "DEMO-US-0001"})
		if got.Item.Body != "" {
			t.Errorf("body = %q, want none", got.Item.Body)
		}
		if got.Item.Rev == "" {
			t.Error("the item came back without a rev")
		}
		if got.Comments != nil || got.Children != nil {
			t.Error("sections nobody asked for came back")
		}
	})

	t.Run("returns the body, the thread and the children on request", func(t *testing.T) {
		got := call[ItemResult](t, h, "get_item", map[string]any{
			"id":      "DEMO-US-0001",
			"include": []string{"body", "comments", "children"},
		})
		if !strings.Contains(got.Item.Body, "## Description") {
			t.Errorf("body = %q, want the Markdown body", got.Item.Body)
		}
		if len(got.Comments) == 0 {
			t.Error("the thread came back empty")
		}
		for _, c := range got.Comments {
			if c.Rev == "" || c.Author == "" {
				t.Errorf("comment %+v is missing a rev or an author", c)
			}
		}
		if len(got.Children) == 0 {
			t.Error("the children came back empty")
		}
		for _, kid := range got.Children {
			if kid.Rev == "" {
				t.Errorf("child %s came back without a rev", kid.ID)
			}
			if kid.Body != "" {
				t.Errorf("child %s came back with a body", kid.ID)
			}
		}
	})

	t.Run("reports an unknown id", func(t *testing.T) {
		got := callFails(t, h, "get_item", map[string]any{"id": "DEMO-US-9999"})
		if got.Code != codeNotFound {
			t.Errorf("code = %q, want %q (%s)", got.Code, codeNotFound, got.Message)
		}
	})

	t.Run("rejects a missing id", func(t *testing.T) {
		got := callFails(t, h, "get_item", map[string]any{"id": "  "})
		if got.Code != codeInvalidRequest || got.Field != "id" {
			t.Errorf("error = %+v, want an invalid_request on `id`", got)
		}
	})
}

func TestSearch(t *testing.T) {
	h := newHarness(t, false)

	t.Run("search_items returns items with a rev", func(t *testing.T) {
		got := call[HitPage](t, h, "search_items", map[string]any{"query": "checkout"})
		if len(got.Results) == 0 {
			t.Fatal("no hits")
		}
		for _, hit := range got.Results {
			if hit.Kind != "item" {
				t.Errorf("kind = %q, want item", hit.Kind)
			}
			if hit.ID == "" || hit.Rev == "" {
				t.Errorf("hit %+v is missing an id or a rev", hit)
			}
		}
	})

	t.Run("search_kb returns pages with a rev", func(t *testing.T) {
		got := call[HitPage](t, h, "search_kb", map[string]any{"query": "checkout"})
		for _, hit := range got.Results {
			if hit.Kind != "page" {
				t.Errorf("kind = %q, want page", hit.Kind)
			}
			if hit.Path == "" || hit.Rev == "" {
				t.Errorf("hit %+v is missing a path or a rev", hit)
			}
		}
	})

	t.Run("an empty query is refused", func(t *testing.T) {
		for _, tool := range []string{"search_items", "search_kb"} {
			got := callFails(t, h, tool, map[string]any{"query": "   "})
			if got.Code != codeInvalidRequest || got.Field != "query" {
				t.Errorf("%s: error = %+v, want an invalid_request on `query`", tool, got)
			}
		}
	})
}

func TestKnowledgeBase(t *testing.T) {
	h := newHarness(t, false)

	t.Run("lists pages under a prefix", func(t *testing.T) {
		all := call[PageList](t, h, "list_kb_pages", nil)
		if all.Total == 0 {
			t.Fatal("no pages")
		}
		scoped := call[PageList](t, h, "list_kb_pages", map[string]any{"prefix": "docs/architecture"})
		if scoped.Total == 0 || scoped.Total >= all.Total {
			t.Fatalf("prefixed total = %d, unfiltered = %d", scoped.Total, all.Total)
		}
		for _, p := range scoped.Pages {
			if !strings.HasPrefix(p.Path, "docs/architecture") {
				t.Errorf("page %s is outside the prefix", p.Path)
			}
		}
	})

	t.Run("paginates", func(t *testing.T) {
		first := call[PageList](t, h, "list_kb_pages", map[string]any{"limit": 1})
		if len(first.Pages) != 1 || first.NextCursor == "" {
			t.Fatalf("first page = %+v", first)
		}
		second := call[PageList](t, h, "list_kb_pages", map[string]any{"limit": 1, "cursor": first.NextCursor})
		if len(second.Pages) != 1 {
			t.Fatalf("second page = %+v", second)
		}
		if second.Pages[0].Path == first.Pages[0].Path {
			t.Error("the cursor returned the same page twice")
		}
	})

	t.Run("omits the body unless asked", func(t *testing.T) {
		got := call[Page](t, h, "get_kb_page", map[string]any{"path": "docs/architecture/overview.md"})
		if got.Body != "" {
			t.Errorf("body = %q, want none", got.Body)
		}
		if got.Rev == "" {
			t.Error("the page came back without a rev")
		}
		withBody := call[Page](t, h, "get_kb_page", map[string]any{
			"path": "docs/architecture/overview.md", "body": true,
		})
		if withBody.Body == "" {
			t.Error("the body was requested and did not come back")
		}
	})

	t.Run("reports an unknown page", func(t *testing.T) {
		got := callFails(t, h, "get_kb_page", map[string]any{"path": "docs/nothing-here.md"})
		if got.Code != codeNotFound {
			t.Errorf("code = %q, want %q", got.Code, codeNotFound)
		}
	})
}

func TestWriteToolsAreAbsentWhenReadOnly(t *testing.T) {
	h := newHarness(t, false)
	for _, name := range writeTools {
		t.Run(name, func(t *testing.T) {
			_, err := h.session.CallTool(context.Background(),
				&sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
			if err == nil {
				t.Fatalf("%s answered on a read-only server", name)
			}
			if !strings.Contains(err.Error(), "unknown tool") && !strings.Contains(err.Error(), name) {
				t.Errorf("error = %v, want an unknown-tool failure", err)
			}
		})
	}
}

func TestCreateTools(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		args     map[string]any
		wantType core.ItemType
	}{
		{
			name:     "create_epic",
			tool:     "create_epic",
			args:     map[string]any{"project": "DEMO", "title": "Checkout analytics"},
			wantType: core.TypeEpic,
		},
		{
			name: "create_story",
			tool: "create_story",
			args: map[string]any{
				"project": "DEMO", "title": "Remember the delivery address",
				"parent": "DEMO-EP-0001", "labels": []string{"frontend"},
			},
			wantType: core.TypeStory,
		},
		{
			name: "create_task",
			tool: "create_task",
			args: map[string]any{
				"project": "DEMO", "title": "Cache the address lookup",
				"parent": "DEMO-US-0001", "body": "## Description\n\nCache it.\n",
			},
			wantType: core.TypeTask,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, true)
			got := call[WriteResult](t, h, tt.tool, tt.args)
			if got.Item.Type != string(tt.wantType) {
				t.Errorf("type = %q, want %q", got.Item.Type, tt.wantType)
			}
			if got.Item.ID == "" || got.Item.Rev == "" {
				t.Errorf("result %+v is missing an id or a rev", got.Item)
			}
			if len(got.Changed) == 0 {
				t.Error("the write reported no changed file")
			}
			if len(h.writes) != 1 || h.writes[0].Op != "created" {
				t.Errorf("write events = %+v, want one creation", h.writes)
			}
			// The item is really there: the same session can read it back.
			read := call[ItemResult](t, h, "get_item", map[string]any{"id": got.Item.ID})
			if read.Item.Title != tt.args["title"] {
				t.Errorf("title = %q, want %q", read.Item.Title, tt.args["title"])
			}
		})
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	h := newHarness(t, true)

	tests := []struct {
		name      string
		args      map[string]any
		wantCode  string
		wantField string
	}{
		{
			name:      "no title",
			args:      map[string]any{"project": "DEMO", "title": "  "},
			wantCode:  codeInvalidRequest,
			wantField: "title",
		},
		{
			name:     "a status the project does not declare",
			args:     map[string]any{"project": "DEMO", "title": "Nope", "status": "shipped"},
			wantCode: "validation_failed",
		},
		{
			name:     "an unknown project",
			args:     map[string]any{"project": "NOPE", "title": "Nope"},
			wantCode: codeNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callFails(t, h, "create_story", tt.args)
			if got.Code != tt.wantCode {
				t.Errorf("code = %q, want %q (%s)", got.Code, tt.wantCode, got.Message)
			}
			if tt.wantField != "" && got.Field != tt.wantField {
				t.Errorf("field = %q, want %q", got.Field, tt.wantField)
			}
		})
	}
}

func TestUpdateItem(t *testing.T) {
	t.Run("applies a sparse patch", func(t *testing.T) {
		h := newHarness(t, true)
		before := call[ItemResult](t, h, "get_item", map[string]any{"id": "DEMO-US-0002"})
		got := call[WriteResult](t, h, "update_item", map[string]any{
			"id": "DEMO-US-0002", "rev": before.Item.Rev,
			"priority": "high", "labels": []string{"backend", "payments", "urgent"},
		})
		if got.Item.Priority != "high" {
			t.Errorf("priority = %q, want high", got.Item.Priority)
		}
		if got.Item.Rev == before.Item.Rev {
			t.Error("the rev did not change after a write")
		}
		if got.Item.Title != before.Item.Title {
			t.Errorf("title changed to %q; a sparse patch must not touch it", got.Item.Title)
		}
	})

	t.Run("moves through the workflow", func(t *testing.T) {
		h := newHarness(t, true)
		before := call[ItemResult](t, h, "get_item", map[string]any{"id": "DEMO-US-0002"})
		got := call[WriteResult](t, h, "update_item", map[string]any{
			"id": "DEMO-US-0002", "rev": before.Item.Rev, "status": "in_progress",
		})
		if got.Item.Status != "in_progress" {
			t.Errorf("status = %q, want in_progress", got.Item.Status)
		}
		if len(h.writes) == 0 || h.writes[len(h.writes)-1].Op != "moved" {
			t.Errorf("write events = %+v, want a move last", h.writes)
		}
	})

	t.Run("refuses a transition the workflow does not declare", func(t *testing.T) {
		h := newHarness(t, true)
		before := call[ItemResult](t, h, "get_item", map[string]any{"id": "DEMO-US-0002"})
		got := callFails(t, h, "update_item", map[string]any{
			"id": "DEMO-US-0002", "rev": before.Item.Rev, "status": "done",
		})
		if got.Code == "" || got.Code == codeNotFound {
			t.Errorf("error = %+v, want the workflow refusal", got)
		}
	})

	t.Run("refuses an empty patch", func(t *testing.T) {
		h := newHarness(t, true)
		got := callFails(t, h, "update_item", map[string]any{"id": "DEMO-US-0002"})
		if got.Code != codeInvalidRequest {
			t.Errorf("code = %q, want %q", got.Code, codeInvalidRequest)
		}
	})

	t.Run("refuses a stale rev", func(t *testing.T) {
		h := newHarness(t, true)
		got := callFails(t, h, "update_item", map[string]any{
			"id": "DEMO-US-0002", "rev": "sha256:0000000000000000", "priority": "low",
		})
		if got.Code != "stale_revision" {
			t.Errorf("code = %q, want stale_revision (%s)", got.Code, got.Message)
		}
	})
}

func TestAddComment(t *testing.T) {
	h := newHarness(t, true)

	t.Run("writes a comment attributed to the agent", func(t *testing.T) {
		got := call[CommentResult](t, h, "add_comment", map[string]any{
			"id": "DEMO-US-0001", "body": "Picked this up.",
		})
		if got.Comment.Author != "test-agent" {
			t.Errorf("author = %q, want the agent name", got.Comment.Author)
		}
		if got.Comment.Rev == "" || got.Comment.Body != "Picked this up." {
			t.Errorf("comment = %+v", got.Comment)
		}
		if len(got.Changed) == 0 {
			t.Error("the write reported no changed file")
		}
	})

	t.Run("refuses an empty body", func(t *testing.T) {
		got := callFails(t, h, "add_comment", map[string]any{"id": "DEMO-US-0001", "body": " "})
		if got.Code != codeInvalidRequest || got.Field != "body" {
			t.Errorf("error = %+v, want an invalid_request on `body`", got)
		}
	})
}

func TestMoveOnBoard(t *testing.T) {
	h := newHarness(t, true)

	t.Run("moves a card and the item behind it", func(t *testing.T) {
		got := call[MoveResult](t, h, "move_on_board", map[string]any{
			"board": "delivery", "ref": "DEMO/DEMO-US-0001",
			"toColumn": "todo", "status": "todo", "position": 0,
		})
		if got.ToColumn != "todo" || !got.StatusChanged {
			t.Errorf("move = %+v, want a status-changing move into todo", got)
		}
		if got.Item == nil || got.Item.Rev == "" {
			t.Errorf("item = %+v, want the moved item with its rev", got.Item)
		}
		if len(got.Changed) < 2 {
			t.Errorf("changed = %v, want a write in each repository", got.Changed)
		}
	})

	t.Run("refuses a card whose project is not cloned", func(t *testing.T) {
		got := callFails(t, h, "move_on_board", map[string]any{
			"board": "delivery", "ref": "WEB/WEB-US-0031", "toColumn": "done",
		})
		if got.Code == "" {
			t.Errorf("error = %+v, want a refusal", got)
		}
	})

	t.Run("rejects a call with no target column", func(t *testing.T) {
		got := callFails(t, h, "move_on_board", map[string]any{
			"board": "delivery", "ref": "DEMO/DEMO-US-0002", "toColumn": "",
		})
		if got.Code != codeInvalidRequest || got.Field != "toColumn" {
			t.Errorf("error = %+v, want an invalid_request on `toColumn`", got)
		}
	})
}

// TestResultsAreCompact guards the shape agents pay for: no nulls, no empty
// arrays, no decorative wrappers.
func TestResultsAreCompact(t *testing.T) {
	h := newHarness(t, false)
	res := rawCall(t, h, "list_items", map[string]any{"project": "DEMO", "limit": 3})
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("encode the result: %v", err)
	}
	body := string(raw)
	for _, unwanted := range []string{"null", "\"\"", "[]"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the result carries %s:\n%s", unwanted, body)
		}
	}
}
