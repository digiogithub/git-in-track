package oauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func newTestServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	if cfg.Issuer == "" {
		cfg.Issuer = "https://hub.acme.example"
	}
	s, err := New(cfg, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRedirectAllowlist(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, Config{AllowedRedirects: []string{
		"https://claude.ai/api/mcp/auth_callback", "https://claude.com/api/mcp/auth_callback",
		"http://127.0.0.1/callback", "http://localhost/cb", "http://[::1]/cb",
	}})
	cases := map[string]bool{
		"https://claude.ai/api/mcp/auth_callback":              true,
		"https://claude.com/api/mcp/auth_callback":             true,
		"https://claude.ai/api/mcp/auth_callback/x":            false,
		"https://claude.ai.evil.example/api/mcp/auth_callback": false,
		"http://127.0.0.1:53682/callback":                      true,
		"http://127.0.0.1/callback":                            true,
		"http://127.0.0.1:53682/callback/other":                false,
		"http://127.0.0.1:53682/callback?x=1":                  false,
		"https://127.0.0.1:53682/callback":                     false,
		"http://localhost:9000/cb":                             true,
		"http://[::1]:9000/cb":                                 true,
		"http://10.0.0.1:9000/callback":                        false,
		"https://claude.ai/api/mcp/auth_callback#frag":         false,
		"https://user@claude.ai/api/mcp/auth_callback":         false,
	}
	for raw, want := range cases {
		if got := s.redirectAllowed(raw); got != want {
			t.Errorf("redirectAllowed(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Claude":                               "Claude",
		"Claude\n<script>x</script>":           "Claudescriptxscript",
		"":                                     "mcp-client",
		"   ":                                  "mcp-client",
		"Pando (agent) v1.2_beta-3":            "Pando (agent) v1.2_beta-3",
		string(bytes.Repeat([]byte("a"), 100)): string(bytes.Repeat([]byte("a"), 64)),
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPKCE(t *testing.T) {
	t.Parallel()
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if !pkceMatches(challenge, verifier) {
		t.Error("the RFC 7636 verifier does not match its challenge")
	}
	if pkceMatches(challenge, verifier+"x") || pkceMatches(challenge, "short") {
		t.Error("a wrong verifier matched")
	}
}

func register(t *testing.T, s *Server, ip string) int {
	t.Helper()
	r := chi.NewRouter()
	s.Routes(r)
	body, _ := json.Marshal(map[string]any{"redirect_uris": []string{"https://claude.ai/api/mcp/auth_callback"}, "client_name": "c"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/oauth/register", bytes.NewReader(body))
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func TestRegistrationLimits(t *testing.T) {
	t.Parallel()
	t.Run("the registry is capped", func(t *testing.T) {
		s := newTestServer(t, Config{AllowedRedirects: []string{"https://claude.ai/api/mcp/auth_callback"}, MaxClients: 2})
		codes := []int{register(t, s, "10.0.0.1"), register(t, s, "10.0.0.2"), register(t, s, "10.0.0.3")}
		if codes[0] != http.StatusCreated || codes[1] != http.StatusCreated || codes[2] != http.StatusServiceUnavailable {
			t.Errorf("codes = %v", codes)
		}
	})
	t.Run("registration is rate limited per address", func(t *testing.T) {
		s := newTestServer(t, Config{AllowedRedirects: []string{"https://claude.ai/api/mcp/auth_callback"}})
		last := 0
		for range 11 {
			last = register(t, s, "10.0.0.9")
		}
		if last != http.StatusTooManyRequests {
			t.Errorf("the 11th registration in a minute = %d", last)
		}
		if register(t, s, "10.0.0.10") != http.StatusCreated {
			t.Error("another address was limited too")
		}
	})
	t.Run("idle clients are forgotten", func(t *testing.T) {
		s := newTestServer(t, Config{AllowedRedirects: []string{"https://claude.ai/api/mcp/auth_callback"}, ClientIdleTTL: time.Hour})
		register(t, s, "10.0.0.1")
		s.store.mu.Lock()
		s.store.prune(time.Now().Add(2*time.Hour), s.cfg.ClientIdleTTL)
		left := len(s.store.st.Clients)
		s.store.mu.Unlock()
		if left != 0 {
			t.Errorf("%d idle clients survived", left)
		}
	})
}
