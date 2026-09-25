package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/config"
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
			want: []string{
				"list_items", "get_item", "search_items", "search_semantic",
				"get_kb_page", "search_kb", "list_kb_pages", "list_requirements",
				"spec_impact", "trace_requirement", "spec_context", "spec_coverage",
			},
			absent: []string{
				"create_epic", "create_story", "create_task", "create_milestone",
				"update_item", "add_comment", "move_on_board",
				"create_spec", "create_requirement", "update_requirement", "verify_requirement",
			},
		},
		{
			name: "writes enabled",
			args: []string{"mcp", "--allow-write", "--list-tools"},
			want: []string{
				"create_epic", "create_story", "create_task", "create_milestone",
				"update_item", "add_comment", "move_on_board", "list_items",
				"create_spec", "create_requirement", "update_requirement", "verify_requirement",
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

// TestMCPListToolsMatchesTheDocumentedCounts keeps the agent-facing docs honest
// (GIT-US-0126): the numbers AGENTS.md and docs/08 §4 give for
// `gintrack mcp --list-tools`, with and without --allow-write, must be the
// numbers the binary prints, and every tool it prints must be named in both.
func TestMCPListToolsMatchesTheDocumentedCounts(t *testing.T) {
	h := newHarness(t)
	h.register()
	readOnly := strings.Fields(h.mustRun("mcp", "--list-tools", "--allow-write=false"))
	all := strings.Fields(h.mustRun("mcp", "--list-tools", "--allow-write"))

	agents := readDoc(t, "AGENTS.md")
	mcpDoc := readDoc(t, filepath.Join("docs", "08-mcp-server.md"))

	tests := []struct {
		name    string
		doc     string
		pattern string
		want    []int
	}{
		{
			name:    "AGENTS.md connect-a-client counts",
			doc:     agents,
			pattern: "\\((\\d+) tools read-only, (\\d+) with `--allow-write`\\)",
			want:    []int{len(readOnly), len(all)},
		},
		{
			name:    "AGENTS.md tool list heading",
			doc:     agents,
			pattern: `The ([a-z-]+) tools, ([a-z-]+) read and ([a-z-]+) write:`,
			want:    []int{len(all), len(readOnly), len(all) - len(readOnly)},
		},
		{
			name:    "docs/08 section 4 total",
			doc:     mcpDoc,
			pattern: `([A-Za-z-]+) tools ship:`,
			want:    []int{len(all)},
		},
		{
			name: "docs/08 section 4 read and write split",
			doc:  mcpDoc,
			pattern: "([A-Za-z-]+) are read tools and ([a-z-]+) are write tools; " +
				"`gintrack mcp --list-tools` prints ([a-z-]+), and with `--allow-write` ([a-z-]+)",
			want: []int{len(readOnly), len(all) - len(readOnly), len(readOnly), len(all)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := regexp.MustCompile(tt.pattern).FindStringSubmatch(tt.doc)
			if m == nil {
				t.Fatalf("no sentence matches %q", tt.pattern)
			}
			for i, want := range tt.want {
				got, ok := documentedCount(m[i+1])
				if !ok {
					t.Fatalf("cannot read %q as a number", m[i+1])
				}
				if got != want {
					t.Errorf("documented count %q = %d, `gintrack mcp --list-tools` gives %d", m[i+1], got, want)
				}
			}
		})
	}

	for _, name := range all {
		if !strings.Contains(agents, "`"+name+"`") {
			t.Errorf("AGENTS.md does not name the tool %s", name)
		}
		if !strings.Contains(mcpDoc, "| `"+name+"`") {
			t.Errorf("the docs/08 section 4 table has no row for %s", name)
		}
	}
}

// readDoc reads a repository document with its lines joined, so a sentence
// wrapped across lines still matches a one-line pattern.
func readDoc(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return strings.Join(strings.Fields(string(data)), " ")
}

// documentedCount reads a count the docs spell as digits or as English words
// ("thirteen", "thirty-two").
func documentedCount(s string) (int, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	units := map[string]int{
		"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7,
		"eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13,
		"fourteen": 14, "fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18,
		"nineteen": 19,
	}
	tens := map[string]int{
		"twenty": 20, "thirty": 30, "forty": 40, "fifty": 50, "sixty": 60,
	}
	s = strings.ToLower(s)
	if n, ok := units[s]; ok {
		return n, true
	}
	head, tail, hyphen := strings.Cut(s, "-")
	n, ok := tens[head]
	if !ok {
		return 0, false
	}
	if !hyphen {
		return n, true
	}
	u, ok := units[tail]
	if !ok || u > 9 {
		return 0, false
	}
	return n + u, true
}

// TestMCPTakesWritesFromTheConfiguration covers the other half of the posture:
// an agent runtime that spawns `gintrack mcp` with no arguments still gets the
// write tools when the user enabled them once in the configuration file, and
// --allow-write=false still turns them off from the command line.
func TestMCPTakesWritesFromTheConfiguration(t *testing.T) {
	h := newHarness(t)
	h.register()

	enableWrites(t, h.Config)

	listed := strings.Fields(h.mustRun("mcp", "--list-tools"))
	for _, name := range []string{"create_task", "update_item", "add_comment"} {
		if !contains(listed, name) {
			t.Errorf("%s is missing from %v with mcp.allowWrite enabled", name, listed)
		}
	}

	listed = strings.Fields(h.mustRun("mcp", "--list-tools", "--allow-write=false"))
	if contains(listed, "create_task") {
		t.Errorf("--allow-write=false did not override the configuration: %v", listed)
	}
}

// enableWrites turns on mcp.allowWrite in a configuration file on disk, the way
// a user editing it would.
func enableWrites(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the configuration: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "allowWrite: false") {
		text = strings.Replace(text, "allowWrite: false", "allowWrite: true", 1)
	} else {
		text += "\nmcp:\n    allowWrite: true\n"
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
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
		if len(listed.Tools) != 32 {
			t.Errorf("tools = %d, want 32", len(listed.Tools))
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

	t.Run("the requirement seams are installed over stdio", func(t *testing.T) {
		spec := callStdio[struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		}](ctx, t, session, "create_spec", map[string]any{"project": "DEMO", "title": "Checkout addresses"})
		req := callStdio[struct {
			Requirement struct {
				Ref string `json:"ref"`
			} `json:"requirement"`
		}](ctx, t, session, "create_requirement", map[string]any{
			"spec": spec.Item.ID, "title": "Trim input", "text": "The checkout SHALL trim input.",
		})
		// Without the trace engine the stdio server would answer
		// unavailable here, as a browser-only session does.
		trace := callStdio[struct {
			Ref string `json:"ref"`
		}](ctx, t, session, "trace_requirement", map[string]any{"ref": req.Requirement.Ref})
		if trace.Ref != req.Requirement.Ref {
			t.Errorf("trace ref = %q, want %q", trace.Ref, req.Requirement.Ref)
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

// TestMCPInstallsTraceSeams checks the wiring behind spec_impact over stdio:
// a repository with git history gets the trace, coverage and impact seams
// the companion installs, so the impact report answers with tier 1 while the
// Pando tiers report unavailable; a repository git cannot open keeps trace
// and coverage and answers the impact query unavailable.
func TestMCPInstallsTraceSeams(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	withGit := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "testdata", "fixtures", fixtureName), withGit)
	gitIn(t, withGit, "init", "--initial-branch=main")
	identify(t, withGit)
	gitIn(t, withGit, "add", "-A")
	gitIn(t, withGit, "commit", "-m", "chore: seed")

	space, mounts, err := mountWorkspace([]config.Repo{{ID: "demo", Path: withGit, Role: config.RoleProject}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	installMCPTraceSeams(mounts, "", nil, slog.New(slog.DiscardHandler))
	v := mounts[0].vlt
	if !v.TraceAvailable() || !v.CoverageAvailable() || !v.ImpactAvailable() {
		t.Fatalf("seams: trace %v, coverage %v, impact %v", v.TraceAvailable(), v.CoverageAvailable(), v.ImpactAvailable())
	}
	got, err := space.Dispatch(context.Background(), "impact.report", []byte(`{"project":"DEMO","base":"HEAD"}`))
	if err != nil {
		t.Fatalf("impact.report: %v", err)
	}
	raw, _ := json.Marshal(got)
	for _, want := range []string{`"tier":1,"status":"ok"`, `"tier":2,"status":"unavailable"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("impact report lacks %s: %s", want, raw)
		}
	}

	noGit := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "testdata", "fixtures", fixtureName), noGit)
	_, mounts, err = mountWorkspace([]config.Repo{{ID: "demo", Path: noGit, Role: config.RoleProject}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	installMCPTraceSeams(mounts, "", nil, slog.New(slog.DiscardHandler))
	if v := mounts[0].vlt; !v.TraceAvailable() || !v.CoverageAvailable() || v.ImpactAvailable() {
		t.Errorf("without git: trace %v, coverage %v, impact %v, want true, true, false",
			v.TraceAvailable(), v.CoverageAvailable(), v.ImpactAvailable())
	}
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
