package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/core/osfs"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The fixtures every test runs against, copied first so that no test ever
// writes into testdata/.
const (
	projectFixture = "../../testdata/fixtures/project-basic"
	teamFixture    = "../../testdata/fixtures/team-basic"
)

// copyTree copies a directory tree into a fresh temporary directory.
func copyTree(t *testing.T, src string) string {
	t.Helper()

	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy the fixture %s: %v", src, err)
	}
	return dst
}

// harness is one MCP server, the workspace behind it and the client session
// driving it over an in-memory transport. Every test goes through a real
// protocol session rather than calling the handlers directly, so what is
// asserted is what a client actually receives.
type harness struct {
	server  *Server
	space   *vault.Workspace
	session *sdk.ClientSession
	roots   []string
	writes  []WriteEvent
}

// newHarness mounts writable copies of the project and team fixtures and
// connects a client to a server over them.
func newHarness(t *testing.T, allowWrite bool) *harness {
	t.Helper()

	h := &harness{space: vault.NewWorkspace()}
	for _, mount := range []struct {
		id   string
		role string
		src  string
	}{
		{"demo-team", vault.RoleTeam, teamFixture},
		{"demo", vault.RoleProject, projectFixture},
	} {
		root := copyTree(t, mount.src)
		fsys, err := osfs.New(root)
		if err != nil {
			t.Fatalf("open %s: %v", root, err)
		}
		v, err := vault.Open(fsys, mount.id)
		if err != nil {
			t.Fatalf("index %s: %v", root, err)
		}
		if _, err := h.space.Attach(mount.id, mount.role, v); err != nil {
			t.Fatalf("attach %s: %v", mount.id, err)
		}
		h.roots = append(h.roots, fsys.Root())
	}

	srv, err := New(Options{
		Core:       h.space,
		Version:    "0.0.1-test",
		Agent:      "test-agent",
		AllowWrite: allowWrite,
		Roots:      h.roots,
		Now:        func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
		AfterWrite: func(_ context.Context, ev WriteEvent) { h.writes = append(h.writes, ev) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	h.server = srv
	h.session = connect(t, srv)
	return h
}

// connect opens a client session over an in-memory transport pair.
func connect(t *testing.T, srv *Server) *sdk.ClientSession {
	t.Helper()

	ctx := context.Background()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := srv.SDK().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the server: %v", err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect the client: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Wait()
	})
	return session
}

// call runs one tool and decodes its structured output into T. It fails the
// test when the tool reported an error.
func call[T any](t *testing.T, h *harness, name string, args any) T {
	t.Helper()

	res := rawCall(t, h, name, args)
	if res.IsError {
		t.Fatalf("%s failed: %s", name, textOf(res))
	}
	var out T
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-encode the result of %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode the result of %s: %v (%s)", name, err, raw)
	}
	return out
}

// callFails runs one tool and requires it to fail, returning the structured
// error the client received.
func callFails(t *testing.T, h *harness, name string, args any) toolError {
	t.Helper()

	res := rawCall(t, h, name, args)
	if !res.IsError {
		t.Fatalf("%s unexpectedly succeeded: %v", name, res.StructuredContent)
	}
	var wrapper struct {
		Error toolError `json:"error"`
	}
	text := textOf(res)
	if err := json.Unmarshal([]byte(text), &wrapper); err != nil {
		t.Fatalf("%s did not report a structured error: %s", name, text)
	}
	return wrapper.Error
}

// rawCall runs one tool and returns the whole result.
func rawCall(t *testing.T, h *harness, name string, args any) *sdk.CallToolResult {
	t.Helper()

	res, err := h.session.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

// textOf joins the text content blocks of a result.
func textOf(res *sdk.CallToolResult) string {
	out := ""
	for _, c := range res.Content {
		if text, ok := c.(*sdk.TextContent); ok {
			out += text.Text
		}
	}
	return out
}
