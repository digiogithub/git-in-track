package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPListTools(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []string
		absent  []string
		wantErr bool
	}{
		{
			name: "read-only by default",
			args: []string{"mcp", "--list-tools"},
			want: []string{"list_items", "get_item", "search_items", "get_kb_page", "search_kb", "list_kb_pages"},
			absent: []string{
				"create_epic", "create_story", "create_task", "create_milestone",
				"update_item", "add_comment", "move_on_board",
			},
		},
		{
			name: "writes enabled",
			args: []string{"mcp", "--allow-write", "--list-tools"},
			want: []string{
				"create_epic", "create_story", "create_task", "create_milestone",
				"update_item", "add_comment", "move_on_board", "list_items",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.register()
			out := h.mustRun(tt.args...)
			listed := strings.Fields(out)
			for _, name := range tt.want {
				if !contains(listed, name) {
					t.Errorf("%s is missing from %v", name, listed)
				}
			}
			for _, name := range tt.absent {
				if contains(listed, name) {
					t.Errorf("%s is advertised by a read-only server", name)
				}
			}
		})
	}
}

func TestMCPWithoutARepository(t *testing.T) {
	h := newHarness(t)
	_, stderr, code := h.run("mcp", "--list-tools")
	if code == exitOK {
		t.Fatal("mcp succeeded with no repository registered")
	}
	if !strings.Contains(stderr, "gintrack add") {
		t.Errorf("stderr = %q, want it to suggest `gintrack add`", stderr)
	}
}

// TestMCPOverStdio is the end-to-end check of the story's first acceptance
// criterion: a real `gintrack mcp` process, spoken to by a real MCP client over
// stdin and stdout, doing the work an agent would do in its first session.
func TestMCPOverStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("building the binary is too slow for -short")
	}
	binary := buildGintrack(t)
	h := newHarness(t)
	h.register()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "mcp", "--allow-write", "--agent", "test-agent") //nolint:gosec // the path is one this test built
	cmd.Env = append(os.Environ(), "GINTRACK_CONFIG="+h.Config)
	cmd.Stderr = os.Stderr

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to `gintrack mcp` over stdio: %v", err)
	}
	defer func() { _ = session.Close() }()

	t.Run("the handshake carries the agent instructions", func(t *testing.T) {
		init := session.InitializeResult()
		if init.ServerInfo == nil || init.ServerInfo.Name != "git-in-track" {
			t.Fatalf("serverInfo = %+v", init.ServerInfo)
		}
		if !strings.Contains(init.Instructions, "DATA") {
			t.Errorf("instructions = %q, want the data boundary", init.Instructions)
		}
	})

	t.Run("tools are listed with schemas", func(t *testing.T) {
		listed, err := session.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		if len(listed.Tools) != 13 {
			t.Errorf("tools = %d, want 13", len(listed.Tools))
		}
		for _, tool := range listed.Tools {
			if tool.InputSchema == nil || tool.OutputSchema == nil {
				t.Errorf("%s is missing a schema", tool.Name)
			}
		}
	})

	var storyID, storyRev string

	t.Run("an agent picks up work, creates a task and reports on it", func(t *testing.T) {
		page := callStdio[struct {
			Items []struct {
				ID   string `json:"id"`
				Rev  string `json:"rev"`
				Type string `json:"type"`
			} `json:"items"`
			Total int `json:"total"`
		}](ctx, t, session, "list_items", map[string]any{"project": "DEMO", "type": []string{"story"}})
		if page.Total == 0 {
			t.Fatal("no stories in the fixture")
		}
		storyID, storyRev = page.Items[0].ID, page.Items[0].Rev
		if storyRev == "" {
			t.Fatal("the list returned an item without a rev")
		}

		created := callStdio[struct {
			Item struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"item"`
			Changed []string `json:"changed"`
		}](ctx, t, session, "create_task", map[string]any{
			"project": "DEMO", "title": "Wire the address lookup", "parent": storyID,
		})
		if created.Item.Type != "task" || created.Item.ID == "" {
			t.Fatalf("created = %+v", created)
		}
		if len(created.Changed) == 0 {
			t.Error("the create reported no changed file")
		}
		// The file really is on disk, in the working tree the agent shares with
		// the human.
		matches, err := filepath.Glob(filepath.Join(h.Repo, "docs", ".pmngr", "tasks", created.Item.ID+"-*.md"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("the created task is not on disk: %v %v", matches, err)
		}

		comment := callStdio[struct {
			Comment struct {
				Author string `json:"author"`
			} `json:"comment"`
		}](ctx, t, session, "add_comment", map[string]any{
			"id": storyID, "body": "Broke this down into one task.", "rev": storyRev,
		})
		if comment.Comment.Author != "test-agent" {
			t.Errorf("author = %q, want the --agent name", comment.Comment.Author)
		}
	})

	t.Run("a traversal attempt is refused over the wire", func(t *testing.T) {
		res, err := session.CallTool(ctx, &sdk.CallToolParams{
			Name: "get_kb_page", Arguments: map[string]any{"path": "../../../../etc/passwd"},
		})
		if err != nil {
			t.Fatalf("call get_kb_page: %v", err)
		}
		if !res.IsError {
			t.Fatal("the traversal was served")
		}
		var wrapper struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		text := stdioText(res)
		if err := json.Unmarshal([]byte(text), &wrapper); err != nil {
			t.Fatalf("the refusal is not structured: %s", text)
		}
		if wrapper.Error.Code != "forbidden_path" {
			t.Errorf("code = %q, want forbidden_path", wrapper.Error.Code)
		}
	})
}

// buildGintrack compiles the binary the stdio test spawns.
func buildGintrack(t *testing.T) string {
	t.Helper()

	name := "gintrack"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build gintrack: %v\n%s", err, out)
	}
	return binary
}

// callStdio runs one tool over the session and decodes its structured output.
func callStdio[T any](ctx context.Context, t *testing.T, session *sdk.ClientSession, name string, args any) T {
	t.Helper()

	res, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s failed: %s", name, stdioText(res))
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

// stdioText joins the text content blocks of a result.
func stdioText(res *sdk.CallToolResult) string {
	out := ""
	for _, c := range res.Content {
		if text, ok := c.(*sdk.TextContent); ok {
			out += text.Text
		}
	}
	return out
}
