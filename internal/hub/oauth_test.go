package hub

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/digiogithub/git-in-track/internal/principal"
)

type principalT = principal.Principal

// fakeGoogle is an OIDC provider that signs whatever identity the test sets.
type fakeGoogle struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	clientID string
	mu       sync.Mutex
	nonce    string
	email    string
	verified bool
	hd       string
}

func newFakeGoogle(t *testing.T, clientID string) *fakeGoogle {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGoogle{key: key, clientID: clientID, email: "ana@acme.example", verified: true, hd: "acme.example"}
	mux := http.NewServeMux()
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": g.srv.URL, "authorization_endpoint": g.srv.URL + "/auth", "token_endpoint": g.srv.URL + "/token",
			"jwks_uri": g.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		e := big.NewInt(int64(key.E)).Bytes()
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(e),
		}}})
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		g.mu.Lock()
		g.nonce = q.Get("nonce")
		g.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=google-code&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		claims := map[string]any{"iss": g.srv.URL, "aud": g.clientID, "sub": "1234", "email": g.email,
			"email_verified": g.verified, "nonce": g.nonce, "hd": g.hd,
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "g", "token_type": "Bearer", "id_token": g.sign(t, claims)})
	})
	return g
}

func (g *fakeGoogle) sign(t *testing.T, claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	payload, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, g.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// oauthHub is a hub with OAuth on, listening on a real loopback address so
// that its issuer is known before it is built.
type oauthHub struct {
	hub    *Hub
	url    string
	google *fakeGoogle
	logs   *bytes.Buffer
	clone  string
}

const claudeRedirect = "https://claude.ai/api/mcp/auth_callback"

func newOAuthHub(t *testing.T) *oauthHub {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://" + ln.Addr().String()
	google := newFakeGoogle(t, "hub-client")
	t.Setenv("TEST_GOOGLE_SECRET", "s3cret")

	acme := bareRemote(t, map[string]string{"docs/.pmngr/project.yaml": acmeYAML})
	dir := t.TempDir()
	principals := "principals:\n" +
		"  - {handle: ana, name: Ana García, email: ana@acme.example, kind: human, tokens: [\"" + HashToken("gth_ana") +
		"\"], oidc: {email: ana@acme.example}, grants: [{scope: project/acme, perms: [read, write]}]}\n"
	path := filepath.Join(dir, "principals.yaml")
	if err := os.WriteFile(path, []byte(principals), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	cfg := &Config{
		DataDir: filepath.Join(dir, "data"), Bind: "127.0.0.1", PullInterval: Duration(time.Hour),
		Git:  GitIdentity{AuthorName: "gintrack-hub", AuthorEmail: "hub@acme.example"},
		Auth: Auth{PrincipalsFile: path},
		OAuth: &OAuthConfig{Issuer: issuer, Google: GoogleConfig{IssuerURL: google.srv.URL, ClientID: "hub-client",
			ClientSecretEnv: "TEST_GOOGLE_SECRET", HostedDomains: []string{"acme.example"}},
			AllowedRedirects: []string{claudeRedirect, "https://claude.com/api/mcp/auth_callback", "http://127.0.0.1/callback"}},
		Organization: Organization{ID: "acme", Projects: []RepoSpec{{ID: "acme", URL: acme, Branch: "main"}}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config: %v", err)
	}
	h, err := New(t.Context(), cfg, Deps{UI: fstest.MapFS{"index.html": {Data: []byte("x")}},
		Logger: slog.New(slog.NewTextHandler(logs, nil)), Git: gitBin(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewUnstartedServer(h.Server().Handler())
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return &oauthHub{hub: h, url: issuer, google: google, logs: logs, clone: cfg.RepoDir("acme")}
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// reply is a response read in full, so no body is left open.
type reply struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func (o *oauthHub) do(t *testing.T, method, target string, form url.Values) reply {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, _ := http.NewRequestWithContext(t.Context(), method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return reply{StatusCode: res.StatusCode, Header: res.Header, Body: data}
}

func (o *oauthHub) register(t *testing.T, redirect, name string) (string, int) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"redirect_uris": []string{redirect}, "client_name": name, "token_endpoint_auth_method": "none"})
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, o.url+"/oauth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		ClientID string `json:"client_id"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	return out.ClientID, res.StatusCode
}

// authorizeCode walks the browser half of the flow and returns where the hub
// finally redirected, which carries the code on success.
func (o *oauthHub) authorizeCode(t *testing.T, clientID, redirect, challenge, scope string) *url.URL {
	t.Helper()
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"}, "state": {"xyz"}, "scope": {scope},
		"resource": {o.url + "/mcp"}}
	res := o.do(t, http.MethodGet, o.url+"/oauth/authorize?"+q.Encode(), nil)
	for hops := 0; res.StatusCode == http.StatusFound && hops < 5; hops++ {
		next := res.Header.Get("Location")
		if strings.HasPrefix(next, redirect) {
			u, _ := url.Parse(next)
			return u
		}
		res = o.do(t, http.MethodGet, next, nil)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("authorize flow stopped at %d", res.StatusCode)
	}
	page := string(res.Body)
	field := func(name string) string {
		m := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(page)
		if m == nil {
			t.Fatalf("consent page lacks %s:\n%s", name, page)
		}
		return m[1]
	}
	decision := o.do(t, http.MethodPost, o.url+"/oauth/consent",
		url.Values{"request": {field("request")}, "csrf": {field("csrf")}, "decision": {"approve"}})
	u, _ := url.Parse(decision.Header.Get("Location"))
	return u
}

func pkce(t *testing.T) (verifier, challenge string) {
	t.Helper()
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func (o *oauthHub) exchange(t *testing.T, form url.Values) (map[string]any, int) {
	t.Helper()
	res := o.do(t, http.MethodPost, o.url+"/oauth/token", form)
	out := map[string]any{}
	_ = json.Unmarshal(res.Body, &out)
	return out, res.StatusCode
}

func TestOAuthFlowWithClaudeRedirect(t *testing.T) {
	o := newOAuthHub(t)

	meta := o.do(t, http.MethodGet, o.url+"/.well-known/oauth-protected-resource", nil)
	var pr map[string]any
	_ = json.Unmarshal(meta.Body, &pr)
	if pr["resource"] != o.url+"/mcp" {
		t.Fatalf("protected resource metadata = %v", pr)
	}
	spoofed, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, o.url+"/.well-known/oauth-authorization-server", nil)
	spoofed.Host = "evil.example"
	spoofed.Header.Set("X-Forwarded-Host", "evil.example")
	asRes, _ := http.DefaultClient.Do(spoofed)
	var as map[string]any
	_ = json.NewDecoder(asRes.Body).Decode(&as)
	_ = asRes.Body.Close()
	if as["issuer"] != o.url || !strings.HasPrefix(as["token_endpoint"].(string), o.url) {
		t.Errorf("metadata follows the request's host: %v", as)
	}

	if res := o.do(t, http.MethodPost, o.url+"/mcp", url.Values{}); res.StatusCode != http.StatusUnauthorized ||
		!strings.Contains(res.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Errorf("/mcp without a token = %d %q", res.StatusCode, res.Header.Get("WWW-Authenticate"))
	}

	if _, code := o.register(t, "https://evil.example/cb", "x"); code != http.StatusBadRequest {
		t.Errorf("an unlisted redirect registered: %d", code)
	}
	if _, code := o.register(t, "http://127.0.0.1:5555/other", "x"); code != http.StatusBadRequest {
		t.Errorf("a loopback redirect with another path registered: %d", code)
	}
	if _, code := o.register(t, "http://127.0.0.1:5555/callback", "local"); code != http.StatusCreated {
		t.Errorf("a loopback redirect with a variable port was refused: %d", code)
	}

	clientID, code := o.register(t, claudeRedirect, "Claude\n<script>alert(1)</script>")
	if code != http.StatusCreated {
		t.Fatalf("register = %d", code)
	}
	verifier, challenge := pkce(t)
	back := o.authorizeCode(t, clientID, claudeRedirect, challenge, "mcp:read mcp:write")
	if back.Scheme+"://"+back.Host+back.Path != claudeRedirect || back.Query().Get("state") != "xyz" || back.Query().Get("code") == "" {
		t.Fatalf("redirect back = %s", back)
	}
	authCode := back.Query().Get("code")

	if _, status := o.exchange(t, url.Values{"grant_type": {"authorization_code"}, "code": {authCode}, "client_id": {clientID},
		"redirect_uri": {claudeRedirect}, "code_verifier": {"wrong-" + verifier}}); status != http.StatusBadRequest {
		t.Errorf("a wrong verifier = %d", status)
	}
	// The code was spent by the failed attempt: codes are single use.
	if _, status := o.exchange(t, url.Values{"grant_type": {"authorization_code"}, "code": {authCode}, "client_id": {clientID},
		"redirect_uri": {claudeRedirect}, "code_verifier": {verifier}}); status != http.StatusBadRequest {
		t.Errorf("a reused code = %d", status)
	}

	back = o.authorizeCode(t, clientID, claudeRedirect, challenge, "mcp:read mcp:write")
	tokens, status := o.exchange(t, url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")},
		"client_id": {clientID}, "redirect_uri": {claudeRedirect}, "code_verifier": {verifier}, "resource": {o.url + "/mcp"}})
	if status != http.StatusOK {
		t.Fatalf("token = %d %v", status, tokens)
	}
	access, refresh := tokens["access_token"].(string), tokens["refresh_token"].(string)

	// The token reaches /mcp and writes as Ana, through the client.
	client := sdk.NewClient(&sdk.Implementation{Name: "claude", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: o.url + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{token: access}}}, nil)
	if err != nil {
		t.Fatalf("mcp connect: %v", err)
	}
	defer session.Close()
	if _, errText := tool(t, session, "add_memory", map[string]any{"project": "ACME", "key": "via-oauth", "content": "Written through OAuth."}); errText != "" {
		t.Fatalf("add_memory = %s", errText)
	}
	o.hub.Server().SyncNow(t.Context())
	if got := git(t, o.clone, "log", "-1", "--format=%an|%cn"); got != "Ana García|gintrack-hub" {
		t.Errorf("author|committer = %q", got)
	}
	trailer := git(t, o.clone, "log", "-1", "--format=%B")
	// The newline and the angle brackets are gone: only a safe character set
	// reaches the trailer and the consent page.
	if !strings.Contains(trailer, "Agent: Claudescriptalert(1)script") {
		t.Errorf("the client name was not sanitized into the trailer:\n%s", trailer)
	}

	// The token is bound to /mcp.
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, o.url+"/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the MCP token reached the REST API: %d", res.StatusCode)
	} else {
		_ = res.Body.Close()
	}

	// Refresh rotates; presenting the old refresh token again revokes the family.
	again, status := o.exchange(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}})
	if status != http.StatusOK || again["refresh_token"] == refresh {
		t.Fatalf("refresh = %d %v", status, again)
	}
	if _, status := o.exchange(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}); status != http.StatusBadRequest {
		t.Errorf("a reused refresh token = %d", status)
	}
	if _, err := o.hub.oauth.Authenticate(again["access_token"].(string), func(string) (principalT, bool) { return principalT{}, true }); err == nil {
		t.Error("the family survived a reused refresh token")
	}

	for _, secret := range []string{access, refresh, authCode, "s3cret"} {
		if strings.Contains(o.logs.String(), secret) {
			t.Errorf("a secret reached the log")
		}
	}
}

func TestOAuthReadOnlyAndRefusedAccounts(t *testing.T) {
	o := newOAuthHub(t)
	clientID, _ := o.register(t, claudeRedirect, "Claude")
	verifier, challenge := pkce(t)

	back := o.authorizeCode(t, clientID, claudeRedirect, challenge, "mcp:read")
	tokens, _ := o.exchange(t, url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")},
		"client_id": {clientID}, "redirect_uri": {claudeRedirect}, "code_verifier": {verifier}})
	client := sdk.NewClient(&sdk.Implementation{Name: "claude", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: o.url + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{token: tokens["access_token"].(string)}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, errText := tool(t, session, "add_memory", map[string]any{"project": "ACME", "key": "ro", "content": "x"}); errText == "" {
		t.Error("a read-only token wrote")
	}

	for name, set := range map[string]func(){
		"unverified email": func() { o.google.verified = false },
		"unknown email":    func() { o.google.email = "mallory@acme.example" },
		"other domain":     func() { o.google.hd = "evil.example" },
	} {
		o.google.mu.Lock()
		o.google.email, o.google.verified, o.google.hd = "ana@acme.example", true, "acme.example"
		set()
		o.google.mu.Unlock()
		back := o.authorizeCode(t, clientID, claudeRedirect, challenge, "mcp:read")
		if back.Query().Get("error") != "access_denied" {
			t.Errorf("%s: redirect back = %s", name, back)
		}
	}
}
