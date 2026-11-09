package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/principal"
	"github.com/digiogithub/git-in-track/internal/vault"
)

const (
	acmeYAML = "schema: 2\nkey: ACME\nname: Acme\ndocs: {path: docs}\nworkflow:\n  initial: backlog\n  statuses:\n" +
		"    - {id: triage, category: triage}\n    - {id: backlog, category: todo}\n    - {id: done, category: done}\n"
	acmeStory = "---\nid: ACME-US-0001\ntype: story\ntitle: Acme secret story\nstatus: backlog\n" +
		"created: 2026-11-08T10:00:00Z\nupdated: 2026-11-08T10:00:00Z\nlinks:\n  - {kind: relates_to, target: WEB/WEB-US-0001}\n---\n\nAcme body.\n"
	webStory = "---\nid: WEB-US-0001\ntype: story\ntitle: Web hidden story\nstatus: backlog\n" +
		"created: 2026-11-08T10:00:00Z\nupdated: 2026-11-08T10:00:00Z\n---\n\nWeb body.\n"
	opsTeam = "schema: 1\nkey: OPS\nname: Ops team\nknowledge: {path: knowledge}\n"
)

// secrets are strings of the fixture no out-of-scope caller may see.
var secrets = []string{
	"Acme secret story", "Web hidden story", "ACME-US-0001", "WEB-US-0001",
	"docs/intro.md", "Acme intro page", "docs/web.md", "Web hidden page", "Ops runbook",
}

// grantFixture is a hub over two project repositories and a team, with
// principals of every shape.
type grantFixture struct {
	hub    *Hub
	tokens map[string]string
	root   string
	logs   *strings.Builder
}

func newGrantFixture(t *testing.T, revalidate ...time.Duration) *grantFixture {
	t.Helper()
	acme := bareRemote(t, map[string]string{
		"docs/.pmngr/project.yaml": acmeYAML, "docs/.pmngr/stories/ACME-US-0001-acme-secret-story.md": acmeStory,
		"docs/intro.md": "# Acme intro page\n\nSee [[WEB:web]].\n",
	})
	web := bareRemote(t, map[string]string{
		"docs/.pmngr/project.yaml":                            strings.ReplaceAll(acmeYAML, "ACME", "WEB"),
		"docs/.pmngr/stories/WEB-US-0001-web-hidden-story.md": webStory,
		"docs/web.md": "# Web hidden page\n",
	})
	ops := bareRemote(t, map[string]string{"team.yaml": opsTeam, "knowledge/runbook.md": "# Ops runbook\n"})

	dir := t.TempDir()
	tokens := map[string]string{"ana": "gth_ana", "proposer": "gth_prop", "outsider": "gth_out", "nobody": "gth_none", "admin": "gth_admin"}
	line := func(handle, grants string) string {
		return "  - {handle: " + handle + ", name: " + handle + ", email: " + handle + "@acme.example, kind: human, tokens: [\"" +
			HashToken(tokens[handle]) + "\"], grants: " + grants + "}\n"
	}
	text := "principals:\n" +
		line("ana", "[{scope: project/acme, perms: [read, write, delete]}]") +
		line("proposer", "[{scope: project/acme, perms: [read, propose]}]") +
		line("outsider", "[{scope: project/zed, perms: [read, write, delete]}, {scope: org/zzz, perms: [read, write]}]") +
		line("nobody", "[]") +
		line("admin", "[{scope: \"project/*\", perms: [read, write, delete, admin]}, {scope: \"org/*\", perms: [read, write, admin]}]")
	path := filepath.Join(dir, "principals.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := &strings.Builder{}
	cfg := &Config{
		DataDir: filepath.Join(dir, "data"), Bind: "127.0.0.1", PullInterval: Duration(time.Hour),
		Git:  GitIdentity{AuthorName: "gintrack-hub", AuthorEmail: "hub@acme.example"},
		Auth: Auth{PrincipalsFile: path},
		Organization: Organization{ID: "acme", Team: &RepoSpec{ID: "ops", URL: ops, Branch: "main"},
			Projects: []RepoSpec{{ID: "acme", URL: acme, Branch: "main"}, {ID: "web", URL: web, Branch: "main"}}},
	}
	deps := Deps{UI: fstest.MapFS{"index.html": {Data: []byte("x")}}, Logger: quietLog(), Git: gitBin(t)}
	if len(revalidate) > 0 {
		deps.RevalidateEvery = revalidate[0]
	}
	h, err := New(t.Context(), cfg, deps)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &grantFixture{hub: h, tokens: tokens, root: filepath.Join(dir, "data", "repos"), logs: logs}
}

func (f *grantFixture) as(t *testing.T, handle string) context.Context {
	t.Helper()
	auth, err := NewAuthenticator(f.hub.cfg.Auth.PrincipalsFile, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	p, err := auth.Authenticate(f.tokens[handle])
	if err != nil {
		t.Fatalf("authenticate %s: %v", handle, err)
	}
	return principal.With(t.Context(), p)
}

func (f *grantFixture) guard(t *testing.T, mount string) *Guard {
	t.Helper()
	auth, err := NewAuthenticator(f.hub.cfg.Auth.PrincipalsFile, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	space := f.hub.srv.Workspace()
	var next interface {
		Dispatch(context.Context, string, []byte) (any, error)
	} = space
	if mount != "" {
		m, _ := space.Lookup(mount)
		next = m.Vault
	}
	return NewGuard(next, f.hub.srv.Workspace, mount, auth.GrantsOf, quietLog())
}

// treeHash hashes every file of the clones, so a test can prove nothing was
// written.
func (f *grantFixture) treeHash(t *testing.T) string {
	t.Helper()
	h := sha256.New()
	_ = filepath.WalkDir(f.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(p, string(filepath.Separator)+".git"+string(filepath.Separator)) {
			return nil //nolint:nilerr // a walk error only skips that entry
		}
		data, _ := os.ReadFile(p)
		h.Write([]byte(p))
		h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func callAs(ctx context.Context, t *testing.T, g *Guard, method string, params map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(params)
	result, err := g.Dispatch(ctx, method, raw)
	out, _ := json.Marshal(result)
	return string(out), err
}

func errCode(err error) string {
	var coded *vault.Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

// TestGuardPolicyCoversEveryMethod parses the dispatch switches of the vault
// and the workspace: a method added there without a policy fails here.
func TestGuardPolicyCoversEveryMethod(t *testing.T) {
	t.Parallel()
	internal := map[string]bool{"create": true, "remove": true, "rename": true, "write": true} // vault.apply ops
	fset := token.NewFileSet()
	for _, file := range []string{"../vault/dispatch.go", "../vault/vault.go"} {
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			clause, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range clause.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				name, _ := strconv.Unquote(lit.Value)
				if !strings.Contains(name, ".") && !internal[name] {
					continue
				}
				if _, has := policy[name]; !has && !internal[name] && strings.Count(name, ".") >= 1 {
					t.Errorf("%s: method %q has no policy in guard.go", file, name)
				}
			}
			return true
		})
	}
}

// TestRoutePolicyCoversEveryRoute walks the router: a route added without a
// class fails here.
func TestRoutePolicyCoversEveryRoute(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t)
	routes := f.hub.Server().Handler().(chi.Routes)
	_ = chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/v1/") && classify(method, route) == routeUnknown {
			t.Errorf("route %s %s has no class in routes.go", method, route)
		}
		return nil
	})
}

// TestGuardOutsiderSeesAndWritesNothing is the property of GIT-US-0209: a
// principal with grants elsewhere calls every method, on the workspace and on
// each mount, with params naming every scope of the fixture, and learns
// nothing and changes nothing.
func TestGuardOutsiderSeesAndWritesNothing(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t)
	before := f.treeHash(t)
	methods := make([]string, 0, len(policy))
	for m := range policy {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	variants := []map[string]any{
		{"project": "ACME", "id": "ACME-US-0001", "itemId": "ACME-US-0001", "spec": "ACME-US-0001", "ref": "ACME-US-0001",
			"path": "docs/intro.md", "vaultId": "acme", "team": "OPS", "q": "secret", "text": "x", "title": "x",
			"patch": map[string]any{"title": "x"}, "status": "done", "items": []any{map[string]any{"key": "k", "content": "c"}}},
		{"project": "WEB", "id": "WEB-US-0001", "ref": "WEB-US-0001", "path": "docs/web.md", "vaultId": "web", "q": "hidden"},
		{"q": "story"}, {},
	}
	for _, who := range []string{"outsider", "nobody"} {
		ctx := f.as(t, who)
		for _, mount := range []string{"", "acme", "web", "ops"} {
			g := f.guard(t, mount)
			for _, method := range methods {
				for i, params := range variants {
					out, _ := callAs(ctx, t, g, method, params)
					echo, _ := json.Marshal(params)
					for _, s := range secrets {
						// A pure function may echo what it was given; that is
						// not a read of the vault.
						if policy[method].scope == scopeNone && strings.Contains(string(echo), s) {
							continue
						}
						if strings.Contains(out, s) {
							t.Errorf("%s via %q: %s (variant %d) leaked %q: %s", who, mount, method, i, s, out)
						}
					}
				}
			}
		}
	}
	if after := f.treeHash(t); after != before {
		t.Error("an out-of-scope principal changed files")
	}
}

func TestGuardScopes(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t)
	ana := f.as(t, "ana")
	proposer := f.as(t, "proposer")
	ws := f.guard(t, "")

	t.Run("fan-out reads see only the readable project", func(t *testing.T) {
		for _, c := range []struct {
			method string
			params map[string]any
		}{
			{"item.list", map[string]any{}}, {"search", map[string]any{"q": "story"}}, {"project.list", map[string]any{}},
			{"workspace.list", map[string]any{}}, {"inbox.list", map[string]any{}}, {"kb.tree", map[string]any{}},
		} {
			out, err := callAs(ana, t, ws, c.method, c.params)
			if err != nil && c.method != "inbox.list" {
				t.Errorf("%s: %v", c.method, err)
			}
			if strings.Contains(out, "WEB-US-0001") || strings.Contains(out, "Web hidden") {
				t.Errorf("%s leaked WEB: %s", c.method, out)
			}
		}
		out, _ := callAs(ana, t, ws, "item.list", map[string]any{})
		if !strings.Contains(out, "ACME-US-0001") {
			t.Errorf("item.list lost the readable story: %s", out)
		}
	})

	t.Run("reads out of scope are not_found, writes forbidden", func(t *testing.T) {
		for _, method := range []string{"item.get", "ref.resolve", "comment.list"} {
			if _, err := callAs(ana, t, ws, method, map[string]any{"id": "WEB-US-0001", "ref": "WEB-US-0001"}); errCode(err) != "not_found" {
				t.Errorf("%s on WEB = %v, want not_found", method, err)
			}
		}
		if _, err := callAs(ana, t, ws, "item.update", map[string]any{"id": "WEB-US-0001", "patch": map[string]any{"title": "x"}}); errCode(err) != "forbidden" {
			t.Errorf("item.update on WEB = %v, want forbidden", err)
		}
	})

	t.Run("a linked out-of-scope item is removed from the result", func(t *testing.T) {
		out, err := callAs(ana, t, ws, "item.get", map[string]any{"id": "ACME-US-0001"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "Acme secret story") || strings.Contains(out, "WEB-US-0001") {
			t.Errorf("item.get = %s", out)
		}
	})

	t.Run("an unresolvable scope is refused, never routed to the first mount", func(t *testing.T) {
		recorder := &recordingNext{}
		g := NewGuard(recorder, f.hub.srv.Workspace, "", f.guard(t, "").grants, quietLog())
		for _, method := range []string{"item.get", "item.update", "kb.page", "kb.write", "memory.put", "board.get"} {
			if _, err := callAs(ana, t, g, method, map[string]any{}); err == nil {
				t.Errorf("%s without a scope was admitted", method)
			}
		}
		if recorder.calls > 0 {
			t.Errorf("%d unscoped calls reached the workspace", recorder.calls)
		}
	})

	t.Run("author fields are forced to the principal", func(t *testing.T) {
		out, err := callAs(ana, t, ws, "item.create", map[string]any{"project": "ACME", "type": "story", "title": "Ana's story", "author": "mallory"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, `"author":"ana"`) {
			t.Errorf("author not forced: %s", out)
		}
		if _, err := callAs(ana, t, ws, "item.update", map[string]any{"id": "ACME-US-0001", "patch": map[string]any{"author": "mallory"}}); errCode(err) != "forbidden" {
			t.Errorf("patch.author = %v, want forbidden", err)
		}
	})

	t.Run("propose cannot write but may comment", func(t *testing.T) {
		prop := f.as(t, "proposer")
		if _, err := callAs(prop, t, ws, "item.create", map[string]any{"project": "ACME", "type": "story", "title": "x"}); errCode(err) != "forbidden" {
			t.Errorf("item.create with propose = %v", err)
		}
		if _, err := callAs(prop, t, ws, "kb.write", map[string]any{"path": "docs/x.md", "vaultId": "acme", "text": "# x\n"}); errCode(err) != "forbidden" {
			t.Errorf("kb.write with propose = %v", err)
		}
		if _, err := callAs(prop, t, ws, "comment.add", map[string]any{"id": "ACME-US-0001", "body": "hi", "author": "x"}); err != nil {
			t.Errorf("comment.add with propose = %v", err)
		}
	})

	t.Run("memory.put keeps a writer's status and forces a proposer's", func(t *testing.T) {
		out, err := callAs(ana, t, ws, "memory.put", map[string]any{"project": "ACME", "items": []any{
			map[string]any{"key": "writer-fact", "content": "Written.", "status": "active"},
		}})
		if err != nil || !strings.Contains(out, `"outcome":"created"`) {
			t.Fatalf("memory.put as a writer = %s, %v", out, err)
		}
		if got, _ := callAs(ana, t, ws, "memory.get", map[string]any{"project": "ACME", "key": "writer-fact"}); !strings.Contains(got, `"status":"active"`) {
			t.Errorf("a writer's memory = %s, want active", got)
		}
		if _, err := callAs(proposer, t, ws, "memory.put", map[string]any{"project": "ACME", "items": []any{
			map[string]any{"key": "proposed-fact", "content": "Proposed.", "status": "active"},
		}}); err != nil {
			t.Fatal(err)
		}
		if got, _ := callAs(ana, t, ws, "memory.get", map[string]any{"project": "ACME", "key": "proposed-fact"}); !strings.Contains(got, `"status":"proposed"`) {
			t.Errorf("a proposer's memory = %s, want proposed", got)
		}
	})

	t.Run("search narrows the semantic half too", func(t *testing.T) {
		got, err := ws.ScopeSearch(ana, nil)
		if err != nil || strings.Join(got, ",") != "ACME" {
			t.Fatalf("ScopeSearch = %v, %v", got, err)
		}
		if _, err := ws.ScopeSearch(ana, []string{"WEB"}); err == nil {
			t.Error("asking for WEB was admitted")
		}
	})
}

type recordingNext struct{ calls int }

func (r *recordingNext) Dispatch(context.Context, string, []byte) (any, error) {
	r.calls++
	return map[string]any{}, nil
}

func TestHubRoutesAndEvents(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t)
	ts := httptest.NewServer(f.hub.Server().Handler())
	t.Cleanup(ts.Close)
	get := func(token, path string) (int, string) {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := res.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return res.StatusCode, b.String()
	}

	if code, _ := get(f.tokens["ana"], "/api/v1/sync/status"); code != http.StatusForbidden {
		t.Errorf("an operator route for ana = %d, want 403", code)
	}
	if code, _ := get(f.tokens["admin"], "/api/v1/sync/status"); code == http.StatusForbidden {
		t.Errorf("an operator route for admin = %d", code)
	}
	code, body := get(f.tokens["ana"], "/api/v1/repos")
	if code != http.StatusOK || strings.Contains(body, `"web"`) || strings.Contains(body, "WEB") {
		t.Errorf("GET /repos for ana = %d %s", code, body)
	}
	if code, body := get(f.tokens["ana"], "/api/v1/search?q=hidden"); strings.Contains(body, "WEB-US-0001") {
		t.Errorf("search leaked WEB (%d): %s", code, body)
	}
	if code, _ := get(f.tokens["nobody"], "/api/v1/projects"); code != http.StatusForbidden {
		t.Errorf("a principal without grants = %d, want 403", code)
	}
}

// TestEventStreamIsScoped opens the event stream as a principal with grants
// elsewhere and as ana: a write ana makes reaches ana's stream and not the
// other one, and revoking the outsider's token closes its stream.
func TestEventStreamIsScoped(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t)
	ts := httptest.NewServer(f.hub.Server().Handler())
	t.Cleanup(ts.Close)
	dial := func(handle string) *websocket.Conn {
		t.Helper()
		conn, res, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/events?token="+f.tokens[handle], nil)
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
		if err != nil {
			t.Fatalf("dial as %s: %v", handle, err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		return conn
	}
	outsider, ana := dial("outsider"), dial("ana")

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPut, ts.URL+"/api/v1/projects/ACME/kb/page",
		strings.NewReader(`{"path":"note.md","content":"# Acme secret note\n"}`))
	req.Header.Set("Authorization", "Bearer "+f.tokens["ana"])
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("PUT page: %v %v", err, res)
	}
	_ = res.Body.Close()

	readOne := func(conn *websocket.Conn, wait time.Duration) (string, error) {
		ctx, cancel := context.WithTimeout(t.Context(), wait)
		defer cancel()
		_, data, err := conn.Read(ctx)
		return string(data), err
	}
	got, err := readOne(ana, 5*time.Second)
	if err != nil || !strings.Contains(got, "ACME") && !strings.Contains(got, "note.md") {
		t.Fatalf("ana's stream = %q, %v", got, err)
	}
	if got, err := readOne(outsider, 500*time.Millisecond); err == nil {
		t.Fatalf("the outsider's stream received %q", got)
	}
}

// TestEventStreamClosesWhenTheTokenIsRevoked revokes a token while its stream
// is open: the stream is closed at the next revalidation.
func TestEventStreamClosesWhenTheTokenIsRevoked(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t, 200*time.Millisecond)
	ts := httptest.NewServer(f.hub.Server().Handler())
	t.Cleanup(ts.Close)
	conn, res, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/events?token="+f.tokens["outsider"], nil)
	if res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	path := f.hub.cfg.Auth.PrincipalsFile
	text, _ := os.ReadFile(path)
	revoked := strings.Replace(string(text), HashToken(f.tokens["outsider"]), HashToken("gth_rotated"), 1)
	if err := os.WriteFile(path, []byte(revoked), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("the stream of a revoked token stayed open")
			}
			if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("closed with %v, want a policy violation", err)
			}
			return
		}
	}
}

// TestSemanticSearchIsScopedAndFresh indexes the fixture, then searches by
// meaning as ana: only ACME answers, and a page changed since it was indexed
// is dropped until the next commit range is applied.
func TestSemanticSearchIsScopedAndFresh(t *testing.T) {
	t.Parallel()
	f := newGrantFixture(t)
	f.hub.syncer.after(t.Context())
	ana := f.as(t, "ana")
	ws := f.guard(t, "")

	// A multi-word query matches any of its words (pando/rag v0.1.1), so it
	// reaches the visible story and would reach WEB's pages if scoping leaked.
	out, err := callAs(ana, t, ws, "search.semantic", map[string]any{"q": "hidden secret story page"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ACME-US-0001") {
		t.Fatalf("search.semantic missed the visible story: %s", out)
	}
	// ACME's own intro page links [[WEB:web]], so the bare key is not a leak;
	// WEB's ids, titles, project and vault are.
	for _, leak := range []string{"WEB-US-0001", "Web hidden", `"project":"WEB"`, `"vaultId":"web"`} {
		if strings.Contains(out, leak) {
			t.Fatalf("search.semantic leaked %s: %s", leak, out)
		}
	}

	if _, err := callAs(ana, t, ws, "kb.write", map[string]any{"vaultId": "acme", "path": "docs/intro.md",
		"text": "# Acme intro page\n\nRewritten after indexing.\n", "rev": revOf(ana, t, ws)}); err != nil {
		t.Fatal(err)
	}
	stale, _ := callAs(ana, t, ws, "search.semantic", map[string]any{"q": "intro"})
	if strings.Contains(stale, "docs/intro.md") {
		t.Errorf("a page changed since indexing was returned: %s", stale)
	}
	// The guard wrote the file; in the hub a REST or MCP write is committed by
	// commit-on-save, which this direct call bypasses, so commit it here.
	clone := filepath.Join(f.root, "acme")
	git(t, clone, "add", "-A")
	git(t, clone, "commit", "--quiet", "-m", "rewrite intro")
	f.hub.syncer.after(t.Context())
	fresh, _ := callAs(ana, t, ws, "search.semantic", map[string]any{"q": "rewritten"})
	if !strings.Contains(fresh, "docs/intro.md") {
		t.Errorf("after the commit range was applied the page is missing: %s", fresh)
	}
}

func revOf(ctx context.Context, t *testing.T, g *Guard) string {
	t.Helper()
	out, err := callAs(ctx, t, g, "kb.page", map[string]any{"vaultId": "acme", "path": "docs/intro.md"})
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Rev  string `json:"rev"`
		Page struct {
			Rev string `json:"rev"`
		} `json:"page"`
	}
	_ = json.Unmarshal([]byte(out), &page)
	if page.Rev != "" {
		return page.Rev
	}
	return page.Page.Rev
}
