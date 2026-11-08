package hub

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewToken(t *testing.T) {
	t.Parallel()
	token, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^gth_[A-Za-z0-9_-]{43}$`).MatchString(token) {
		t.Errorf("token %q has the wrong shape", token)
	}
	if hash != HashToken(token) || !tokenHashRE.MatchString(hash) {
		t.Errorf("hash %q", hash)
	}
	other, _, _ := NewToken()
	if other == token {
		t.Error("two tokens are equal")
	}
}

func TestParsePrincipals(t *testing.T) {
	t.Parallel()
	h1, h2 := HashToken("gth_one"), HashToken("gth_two")
	valid := `principals:
  - {handle: ana, name: Ana, email: ana@acme.example, kind: human, tokens: ["` + h1 + `"]}
  - {handle: pando-ana, name: Pando, email: p@acme.example, kind: agent, agent: pando, onBehalfOf: ana, tokens: ["` + h2 + `"]}
`
	if got, err := ParsePrincipals([]byte(valid)); err != nil || len(got) != 2 || got[h2].p.OnBehalfOf != "ana" {
		t.Fatalf("ParsePrincipals(valid) = %v, %v", got, err)
	}
	cases := []struct{ name, yaml, want string }{
		{"bad handle", `principals: [{handle: Ana, name: A, email: a@x, kind: human}]`, "principals[0].handle"},
		{"duplicate handle", `principals: [{handle: a, name: A, email: a@x, kind: human}, {handle: a, name: B, email: b@x, kind: human}]`, "listed twice"},
		{"missing email", `principals: [{handle: a, name: A, kind: human}]`, "principals[0].email"},
		{"bad kind", `principals: [{handle: a, name: A, email: a@x, kind: robot}]`, "principals[0].kind"},
		{"agent without agent name", `principals: [{handle: a, name: A, email: a@x, kind: agent}]`, "principals[0].agent"},
		{"onBehalfOf an unknown person", `principals: [{handle: a, name: A, email: a@x, kind: agent, agent: x, onBehalfOf: zoe}]`, "principals[0].onBehalfOf"},
		{"bad hash", `principals: [{handle: a, name: A, email: a@x, kind: human, tokens: ["md5:abc"]}]`, "principals[0].tokens[0]"},
		{"hash shared by two", `principals: [{handle: a, name: A, email: a@x, kind: human, tokens: ["` + h1 + `"]}, {handle: b, name: B, email: b@x, kind: human, tokens: ["` + h1 + `"]}]`, "the same hash"},
		{"unknown key", `principals: [{handle: a, name: A, email: a@x, kind: human, password: x}]`, "password"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParsePrincipals([]byte(c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want it to mention %q", err, c.want)
			}
			if strings.Contains(err.Error(), h1) {
				t.Errorf("the error quotes a hash: %v", err)
			}
		})
	}
}

// writePrincipals writes a principals file for ana (human), pando-ana (agent
// for ana) and old (disabled), and returns the path and their tokens.
func writePrincipals(t *testing.T, dir string) (path string, tokens map[string]string) {
	t.Helper()
	tokens = map[string]string{"ana": "gth_ana", "pando-ana": "gth_pando", "old": "gth_old"}
	text := `principals:
  - {handle: ana, name: Ana García, email: ana@acme.example, kind: human, tokens: ["` + HashToken(tokens["ana"]) + `"]}
  - {handle: pando-ana, name: Pando (Ana), email: pando-ana@agents.acme.example, kind: agent, agent: pando, onBehalfOf: ana, tokens: ["` + HashToken(tokens["pando-ana"]) + `"]}
  - {handle: old, name: Old, email: old@acme.example, kind: human, disabled: true, tokens: ["` + HashToken(tokens["old"]) + `"]}
`
	path = filepath.Join(dir, "principals.yaml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, tokens
}

func TestAuthenticatorReloads(t *testing.T) {
	t.Parallel()
	path, tokens := writePrincipals(t, t.TempDir())
	var logs bytes.Buffer
	a, err := NewAuthenticator(path, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	a.now = func() time.Time { return clock }

	if p, err := a.Authenticate(tokens["ana"]); err != nil || p.Handle != "ana" {
		t.Fatalf("ana = %+v, %v", p, err)
	}
	if _, err := a.Authenticate(tokens["old"]); err == nil {
		t.Error("a disabled principal authenticated")
	}

	// Revoke ana by editing the file; the next check after a second sees it.
	text, _ := os.ReadFile(path)
	revoked := strings.Replace(string(text), HashToken(tokens["ana"]), HashToken("gth_new"), 1)
	if err := os.WriteFile(path, []byte(revoked), 0o600); err != nil {
		t.Fatal(err)
	}
	future := clock.Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	if _, err := a.Authenticate(tokens["ana"]); err == nil {
		t.Error("a revoked token still authenticates")
	}
	if p, err := a.Authenticate("gth_new"); err != nil || p.Handle != "ana" {
		t.Errorf("the new token = %+v, %v", p, err)
	}

	// A broken file keeps the last good version.
	if err := os.WriteFile(path, []byte("principals: [{handle: BAD}]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := future.Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	if p, err := a.Authenticate("gth_new"); err != nil || p.Handle != "ana" {
		t.Errorf("after a broken edit = %+v, %v; want the previous version in force", p, err)
	}
	if !strings.Contains(logs.String(), "invalid") {
		t.Errorf("the broken file was not logged: %s", logs.String())
	}
	for _, tok := range tokens {
		if strings.Contains(logs.String(), tok) {
			t.Errorf("a token reached the log: %s", logs.String())
		}
	}
}

// authHub builds a hub with a principals file over a local forge.
func authHub(t *testing.T) (*Hub, map[string]string, *bytes.Buffer, string) {
	t.Helper()
	bare := bareRemote(t, map[string]string{"docs/.pmngr/project.yaml": projectYAML})
	dir := t.TempDir()
	path, tokens := writePrincipals(t, dir)
	cfg := &Config{
		DataDir: filepath.Join(dir, "data"), Bind: "127.0.0.1", Token: "ignored-single-token",
		PullInterval: Duration(time.Hour),
		Git:          GitIdentity{AuthorName: "gintrack-hub", AuthorEmail: "hub@acme.example"},
		Auth:         Auth{PrincipalsFile: path},
		Organization: Organization{ID: "acme", Projects: []RepoSpec{{ID: "web", URL: bare, Branch: "main"}}},
	}
	var logs bytes.Buffer
	h, err := New(t.Context(), cfg, Deps{
		UI:     fstest.MapFS{"index.html": {Data: []byte("<html></html>")}},
		Logger: slog.New(slog.NewTextHandler(&logs, nil)), Git: gitBin(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, tokens, &logs, cfg.RepoDir("web")
}

func send(h *Hub, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.Server().Handler().ServeHTTP(rec, req)
	return rec
}

func TestHubAuthentication(t *testing.T) {
	t.Parallel()
	h, tokens, logs, _ := authHub(t)
	bearer := func(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

	if rec := send(h, http.MethodGet, "/api/v1/projects", "", bearer(tokens["ana"])); rec.Code != http.StatusOK {
		t.Errorf("a valid token = %d %s", rec.Code, rec.Body)
	}
	if rec := send(h, http.MethodGet, "/api/v1/projects", "", bearer("ignored-single-token")); rec.Code != http.StatusUnauthorized {
		t.Errorf("the single token is ignored once principals are configured, got %d", rec.Code)
	}
	unknown := send(h, http.MethodGet, "/api/v1/projects", "", bearer("gth_nobody"))
	disabled := send(h, http.MethodGet, "/api/v1/projects", "", bearer(tokens["old"]))
	if unknown.Code != http.StatusUnauthorized || disabled.Code != http.StatusUnauthorized {
		t.Fatalf("unknown = %d, disabled = %d", unknown.Code, disabled.Code)
	}
	strip := func(s string) string {
		return regexp.MustCompile(`"(instance|requestId)":"[^"]*"`).ReplaceAllString(s, "")
	}
	if strip(unknown.Body.String()) != strip(disabled.Body.String()) {
		t.Errorf("a 401 tells an unknown token from a disabled principal:\n%s\n%s", unknown.Body, disabled.Body)
	}
	if rec := send(h, http.MethodGet, "/api/v1/projects?token="+tokens["ana"], "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a query-string token outside a WebSocket upgrade = %d, want 401", rec.Code)
	}
	if !strings.Contains(logs.String(), "disabled") || !strings.Contains(logs.String(), "no principal") {
		t.Errorf("the log does not tell the reasons apart:\n%s", logs)
	}
	for _, tok := range tokens {
		if strings.Contains(logs.String(), tok) {
			t.Errorf("a token reached the log:\n%s", logs)
		}
	}
}

// TestHubCommitsAsThePrincipal is the end-to-end check of GIT-US-0208: a page
// written with an agent's token is committed with the agent as author, the
// hub as committer, and the Agent and On-Behalf-Of trailers.
func TestHubCommitsAsThePrincipal(t *testing.T) {
	t.Parallel()
	h, tokens, _, clone := authHub(t)
	rec := send(h, http.MethodPut, "/api/v1/projects/ACME/kb/page",
		`{"path":"intro.md","content":"# Intro\n"}`, map[string]string{"Authorization": "Bearer " + tokens["pando-ana"]})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT page = %d: %s", rec.Code, rec.Body)
	}
	h.Server().SyncNow(t.Context())
	if got := git(t, clone, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got !=
		"Pando (Ana) <pando-ana@agents.acme.example>|gintrack-hub <hub@acme.example>" {
		t.Fatalf("author|committer = %q", got)
	}
	body := git(t, clone, "log", "-1", "--format=%B")
	for _, want := range []string{"Agent: pando", "On-Behalf-Of: ana"} {
		if !strings.Contains(body, want) {
			t.Errorf("message lacks %q:\n%s", want, body)
		}
	}
}

// TestHubMCPWritesCarryThePrincipal checks that the principal of the HTTP
// request reaches a write made through an MCP tool, over the real
// streamable-HTTP transport (design §3 of GIT-US-0208).
func TestHubMCPWritesCarryThePrincipal(t *testing.T) {
	t.Parallel()
	h, tokens, _, clone := authHub(t)
	ts := httptest.NewServer(h.Server().Handler())
	t.Cleanup(ts.Close)

	client := sdk.NewClient(&sdk.Implementation{Name: "test-agent", Version: "0.0.1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{
		Endpoint:   ts.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{token: tokens["pando-ana"]}},
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(t.Context(), &sdk.CallToolParams{
		Name: "create_epic", Arguments: map[string]any{"project": "ACME", "title": "Agent epic"},
	})
	if err != nil || res.IsError {
		t.Fatalf("create_epic: %v %+v", err, res)
	}
	h.Server().SyncNow(t.Context())
	if got := git(t, clone, "log", "-1", "--format=%an|%cn"); got != "Pando (Ana)|gintrack-hub" {
		t.Fatalf("author|committer of the MCP write = %q", got)
	}
}

// bearer adds a bearer token to every request.
type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone) //nolint:wrapcheck // a RoundTripper returns the transport's error untouched
}
