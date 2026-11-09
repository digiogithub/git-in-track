// Package oauth makes gintrack-hub an OAuth 2.1 authorization server for its
// MCP endpoint, as the MCP authorization specification describes
// (GIT-US-0215): protected-resource and authorization-server metadata
// (RFC 9728, RFC 8414), dynamic client registration (RFC 7591), the
// authorization-code grant with mandatory PKCE S256 and a resource
// indicator (RFC 8707), rotating refresh tokens and revocation (RFC 7009).
// People sign in with Google; the hub never sees a password.
//
// Every URL it publishes is built from the configured issuer, never from the
// request's Host or forwarding headers.
package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"github.com/digiogithub/git-in-track/internal/principal"
)

// The scopes a client may ask for.
const (
	ScopeRead  = "mcp:read"
	ScopeWrite = "mcp:write"
)

// Config is the `oauth:` section of hub.yaml, resolved.
type Config struct {
	// Issuer is the hub's public URL, e.g. https://hub.acme.example.
	Issuer string
	// GoogleIssuer is the OIDC issuer people sign in with; Google by default.
	GoogleIssuer       string
	GoogleClientID     string
	GoogleClientSecret string
	// HostedDomains, when set, admits only Google accounts whose verified hd
	// claim is one of them.
	HostedDomains []string
	// AllowedRedirects are the redirect URIs a client may register. A loopback
	// entry (127.0.0.1, [::1] or localhost, no port) admits any port with the
	// same scheme, host and exact path.
	AllowedRedirects []string
	AccessTTL        time.Duration
	RefreshTTL       time.Duration
	// MaxClients caps dynamic registration; ClientIdleTTL forgets a client
	// unused for that long.
	MaxClients    int
	ClientIdleTTL time.Duration
}

// People resolves the principal a Google account signs in as.
type People interface {
	// HumanByEmail finds the enabled human principal whose oidc email this is.
	HumanByEmail(email string) (principal.Principal, bool)
}

// Server is the authorization server.
type Server struct {
	cfg    Config
	people People
	log    *slog.Logger
	now    func() time.Time
	client *http.Client
	store  *store

	mu       sync.Mutex
	pending  map[string]*pendingAuth // by internal id
	byState  map[string]string       // Google state -> pending id
	codes    map[string]*grantCode   // by hash
	limiter  map[string][]time.Time  // per IP and endpoint
	provider *oidc.Provider
}

type pendingAuth struct {
	clientID, redirect, challenge, state, resource, nonce, csrf string
	scopes                                                      []string
	handle                                                      string
	expires                                                     time.Time
}

type grantCode struct {
	clientID, redirect, challenge, resource, handle string
	scopes                                          []string
	expires                                         time.Time
}

// New opens the persisted state under dir.
func New(cfg Config, dir string, people People, log *slog.Logger) (*Server, error) {
	if cfg.GoogleIssuer == "" {
		cfg.GoogleIssuer = "https://accounts.google.com"
	}
	if cfg.AccessTTL <= 0 {
		cfg.AccessTTL = time.Hour
	}
	if cfg.RefreshTTL <= 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	if cfg.MaxClients <= 0 {
		cfg.MaxClients = 500
	}
	if cfg.ClientIdleTTL <= 0 {
		cfg.ClientIdleTTL = 30 * 24 * time.Hour
	}
	cfg.Issuer = strings.TrimSuffix(cfg.Issuer, "/")
	st, err := openStore(dir)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		cfg: cfg, people: people, log: log, now: time.Now, client: http.DefaultClient, store: st,
		pending: map[string]*pendingAuth{}, byState: map[string]string{},
		codes: map[string]*grantCode{}, limiter: map[string][]time.Time{},
	}, nil
}

// SetHTTPClient sets the client used to reach Google (tests point it at a fake).
func (s *Server) SetHTTPClient(c *http.Client) { s.client = c }

// Resource is the protected resource tokens are bound to: the MCP endpoint.
func (s *Server) Resource() string { return s.cfg.Issuer + "/mcp" }

// Challenge is the WWW-Authenticate value of a 401 on the hub.
func (s *Server) Challenge() string {
	return `Bearer realm="gintrack", resource_metadata="` + s.cfg.Issuer + `/.well-known/oauth-protected-resource"`
}

// Routes mounts the endpoints.
func (s *Server) Routes(r chi.Router) {
	r.Get("/.well-known/oauth-protected-resource", s.protectedResource)
	r.Get("/.well-known/oauth-protected-resource/mcp", s.protectedResource)
	r.Get("/.well-known/oauth-authorization-server", s.authorizationServer)
	r.Post("/oauth/register", s.register)
	r.Get("/oauth/authorize", s.authorize)
	r.Get("/oauth/google/callback", s.googleCallback)
	r.Get("/oauth/consent", s.consentPage)
	r.Post("/oauth/consent", s.consentDecision)
	r.Post("/oauth/token", s.token)
	r.Post("/oauth/revoke", s.revoke)
}

func (s *Server) protectedResource(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.Resource(),
		"authorization_servers":    []string{s.cfg.Issuer},
		"scopes_supported":         []string{ScopeRead, ScopeWrite},
		"bearer_methods_supported": []string{"header"},
	})
}

func (s *Server) authorizationServer(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.cfg.Issuer,
		"authorization_endpoint":                s.cfg.Issuer + "/oauth/authorize",
		"token_endpoint":                        s.cfg.Issuer + "/oauth/token",
		"registration_endpoint":                 s.cfg.Issuer + "/oauth/register",
		"revocation_endpoint":                   s.cfg.Issuer + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{ScopeRead, ScopeWrite},
	})
}

// ---------------------------------------------------------------- register

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, "register", 10) {
		oauthError(w, http.StatusTooManyRequests, "slow_down", "too many registrations from this address")
		return
	}
	var req struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
		AuthMethod   string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the body is not JSON")
		return
	}
	if req.AuthMethod != "" && req.AuthMethod != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only public clients (token_endpoint_auth_method none) are supported")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 5 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "one to five redirect_uris are required")
		return
	}
	for _, u := range req.RedirectURIs {
		if !s.redirectAllowed(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "a redirect_uri is not allowed by this hub")
			return
		}
	}
	id, err := randomToken("gtc_")
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	now := s.now()
	c := &client{ID: id, Name: SanitizeName(req.ClientName), Redirects: req.RedirectURIs, Created: now, LastUsed: now}
	s.store.mu.Lock()
	s.store.prune(now, s.cfg.ClientIdleTTL)
	if len(s.store.st.Clients) >= s.cfg.MaxClients {
		s.store.mu.Unlock()
		oauthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "the client registry is full")
		return
	}
	s.store.st.Clients[id] = c
	err = s.store.save()
	s.store.mu.Unlock()
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id": id, "client_id_issued_at": now.Unix(), "client_name": c.Name,
		"redirect_uris": c.Redirects, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
	})
}

// SanitizeName bounds a client name to 64 printable characters of a safe
// set, because it reaches a commit trailer and the consent page.
func SanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if b.Len() >= 64 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" .-_()", r) {
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return "mcp-client"
	}
	return out
}

// redirectAllowed matches a redirect URI against the allowlist.
func (s *Server) redirectAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.User != nil {
		return false
	}
	for _, allowed := range s.cfg.AllowedRedirects {
		if raw == allowed {
			return true
		}
		a, err := url.Parse(allowed)
		if err != nil || a.Port() != "" || !isLoopbackHost(a.Hostname()) {
			continue
		}
		if u.Scheme == a.Scheme && u.Hostname() == a.Hostname() && u.Path == a.Path && u.RawQuery == "" {
			return true
		}
	}
	return false
}

func isLoopbackHost(h string) bool { return h == "127.0.0.1" || h == "::1" || h == "localhost" }

// redirectRegistered reports whether redirect is one of the client's, with
// any port for a loopback one (RFC 8252 §7.3).
func redirectRegistered(c *client, redirect string) bool {
	u, err := url.Parse(redirect)
	if err != nil {
		return false
	}
	for _, reg := range c.Redirects {
		if reg == redirect {
			return true
		}
		r, err := url.Parse(reg)
		if err == nil && isLoopbackHost(r.Hostname()) && r.Scheme == u.Scheme && r.Hostname() == u.Hostname() && r.Path == u.Path {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- authorize

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c := s.client_(q.Get("client_id"))
	redirect := q.Get("redirect_uri")
	if c == nil || !redirectRegistered(c, redirect) {
		// Never redirect to an unverified URI: answer here instead.
		http.Error(w, "unknown client or redirect_uri", http.StatusBadRequest)
		return
	}
	fail := func(code, desc string) {
		redirectWith(w, r, redirect, url.Values{"error": {code}, "error_description": {desc}, "state": {q.Get("state")}})
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only code is supported")
		return
	}
	if q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) < 43 {
		fail("invalid_request", "PKCE with S256 is required")
		return
	}
	resource := q.Get("resource")
	if resource == "" {
		resource = s.Resource()
	}
	if resource != s.Resource() {
		fail("invalid_target", "the only resource of this hub is its MCP endpoint")
		return
	}
	scopes, ok := parseScopes(q.Get("scope"))
	if !ok {
		fail("invalid_scope", "scopes are mcp:read and mcp:write")
		return
	}
	provider, err := s.google(r.Context())
	if err != nil {
		s.log.Warn("google discovery failed", "error", err)
		fail("temporarily_unavailable", "the identity provider is unreachable")
		return
	}
	p := &pendingAuth{clientID: c.ID, redirect: redirect, challenge: q.Get("code_challenge"), state: q.Get("state"),
		resource: resource, scopes: scopes, expires: s.now().Add(10 * time.Minute)}
	id, err1 := randomToken("")
	googleState, err2 := randomToken("")
	nonce, err3 := randomToken("")
	csrf, err4 := randomToken("")
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		fail("server_error", "")
		return
	}
	p.nonce, p.csrf = nonce, csrf
	s.mu.Lock()
	s.gc()
	s.pending[id] = p
	s.byState[googleState] = id
	s.mu.Unlock()
	http.Redirect(w, r, s.googleConfig(provider).AuthCodeURL(googleState, oidc.Nonce(nonce)), http.StatusFound)
}

func parseScopes(raw string) ([]string, bool) {
	if strings.TrimSpace(raw) == "" {
		return []string{ScopeRead, ScopeWrite}, true
	}
	var out []string
	for _, sc := range strings.Fields(raw) {
		if sc != ScopeRead && sc != ScopeWrite {
			return nil, false
		}
		out = append(out, sc)
	}
	return out, true
}

func (s *Server) google(ctx context.Context) (*oidc.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != nil {
		return s.provider, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, s.client), s.cfg.GoogleIssuer)
	if err != nil {
		return nil, fmt.Errorf("discover %s: %w", s.cfg.GoogleIssuer, err)
	}
	s.provider = p
	return p, nil
}

func (s *Server) googleConfig(p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID: s.cfg.GoogleClientID, ClientSecret: s.cfg.GoogleClientSecret,
		Endpoint: p.Endpoint(), RedirectURL: s.cfg.Issuer + "/oauth/google/callback",
		Scopes: []string{oidc.ScopeOpenID, "email"},
	}
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	id, ok := s.byState[q.Get("state")]
	delete(s.byState, q.Get("state"))
	p := s.pending[id]
	s.mu.Unlock()
	if !ok || p == nil || s.now().After(p.expires) {
		http.Error(w, "this sign-in expired or is unknown; start again from your client", http.StatusBadRequest)
		return
	}
	deny := func(reason string) {
		s.log.Info("oauth sign-in refused", "reason", reason)
		redirectWith(w, r, p.redirect, url.Values{"error": {"access_denied"}, "state": {p.state}})
	}
	if q.Get("error") != "" {
		deny("the identity provider refused")
		return
	}
	provider, err := s.google(r.Context())
	if err != nil {
		deny("the identity provider is unreachable")
		return
	}
	ctx := oidc.ClientContext(r.Context(), s.client)
	tok, err := s.googleConfig(provider).Exchange(ctx, q.Get("code"))
	if err != nil {
		deny("the code exchange with the identity provider failed")
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	idt, err := provider.Verifier(&oidc.Config{ClientID: s.cfg.GoogleClientID}).Verify(ctx, raw)
	if err != nil || idt.Nonce != p.nonce {
		deny("the identity token did not verify")
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		HD            string `json:"hd"`
	}
	if err := idt.Claims(&claims); err != nil || !claims.EmailVerified || claims.Email == "" {
		deny("the account has no verified email")
		return
	}
	if len(s.cfg.HostedDomains) > 0 && !contains(s.cfg.HostedDomains, claims.HD) {
		deny("the account is outside the allowed domains")
		return
	}
	who, ok := s.people.HumanByEmail(claims.Email)
	if !ok {
		deny("no enabled principal has this email")
		return
	}
	s.mu.Lock()
	p.handle = who.Handle
	s.mu.Unlock()
	http.Redirect(w, r, s.cfg.Issuer+"/oauth/consent?request="+url.QueryEscape(id), http.StatusFound)
}

// ---------------------------------------------------------------- consent

var consentTemplate = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Allow access to gintrack hub</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body style="font-family: system-ui, sans-serif; max-width: 32rem; margin: 3rem auto; padding: 0 1rem">
<h1>Allow {{.Client}}?</h1>
<p>Signed in as <strong>{{.Handle}}</strong>. {{.Client}} asks to act for you on this hub with:</p>
<ul>{{range .Scopes}}<li>{{.}}</li>{{end}}</ul>
<p>It sees and changes only what your grants allow. You can revoke it at any time.</p>
<form method="post" action="{{.Action}}">
<input type="hidden" name="request" value="{{.Request}}">
<input type="hidden" name="csrf" value="{{.CSRF}}">
<button name="decision" value="approve" type="submit">Allow</button>
<button name="decision" value="deny" type="submit">Deny</button>
</form></body></html>`))

func (s *Server) consentPage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("request")
	s.mu.Lock()
	p := s.pending[id]
	s.mu.Unlock()
	if p == nil || p.handle == "" || s.now().After(p.expires) {
		http.Error(w, "this request expired or is unknown", http.StatusBadRequest)
		return
	}
	c := s.client_(p.clientID)
	if c == nil {
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = consentTemplate.Execute(w, map[string]any{
		"Client": c.Name, "Handle": p.handle, "Scopes": p.scopes,
		"Action": s.cfg.Issuer + "/oauth/consent", "Request": id, "CSRF": p.csrf,
	})
}

func (s *Server) consentDecision(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := r.PostForm.Get("request")
	s.mu.Lock()
	p := s.pending[id]
	delete(s.pending, id)
	s.mu.Unlock()
	if p == nil || p.handle == "" || s.now().After(p.expires) ||
		subtle.ConstantTimeCompare([]byte(p.csrf), []byte(r.PostForm.Get("csrf"))) != 1 {
		http.Error(w, "this request expired, is unknown or was not submitted from its page", http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("decision") != "approve" {
		redirectWith(w, r, p.redirect, url.Values{"error": {"access_denied"}, "state": {p.state}})
		return
	}
	code, err := randomToken("")
	if err != nil {
		redirectWith(w, r, p.redirect, url.Values{"error": {"server_error"}, "state": {p.state}})
		return
	}
	s.mu.Lock()
	s.codes[hash(code)] = &grantCode{clientID: p.clientID, redirect: p.redirect, challenge: p.challenge,
		resource: p.resource, handle: p.handle, scopes: p.scopes, expires: s.now().Add(time.Minute)}
	s.mu.Unlock()
	redirectWith(w, r, p.redirect, url.Values{"code": {code}, "state": {p.state}})
}

// ---------------------------------------------------------------- token

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.allow(r, "token", 60) {
		oauthError(w, http.StatusTooManyRequests, "slow_down", "too many token requests from this address")
		return
	}
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "bad form")
		return
	}
	f := r.PostForm
	switch f.Get("grant_type") {
	case "authorization_code":
		s.mu.Lock()
		g := s.codes[hash(f.Get("code"))]
		delete(s.codes, hash(f.Get("code"))) // single use, whatever happens next
		s.mu.Unlock()
		resource := f.Get("resource")
		if resource == "" {
			resource = s.Resource()
		}
		switch {
		case g == nil || s.now().After(g.expires):
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code is unknown, used or expired")
		case g.clientID != f.Get("client_id") || g.redirect != f.Get("redirect_uri"):
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code was issued to another client or redirect")
		case !pkceMatches(g.challenge, f.Get("code_verifier")):
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the code_verifier does not match")
		case resource != g.resource:
			oauthError(w, http.StatusBadRequest, "invalid_target", "the resource differs from the authorization request")
		default:
			family, _ := randomToken("")
			s.issue(w, g.clientID, g.handle, g.scopes, g.resource, family)
		}
	case "refresh_token":
		s.refresh(w, f.Get("refresh_token"), f.Get("client_id"))
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func pkceMatches(challenge, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

func (s *Server) issue(w http.ResponseWriter, clientID, handle string, scopes []string, resource, family string) {
	access, err1 := randomToken(AccessPrefix)
	refresh, err2 := randomToken(RefreshPrefix)
	if errors.Join(err1, err2) != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	now := s.now()
	s.store.mu.Lock()
	s.store.prune(now, s.cfg.ClientIdleTTL)
	s.store.st.Tokens[hash(access)] = &token{Kind: kindAccess, ClientID: clientID, Handle: handle, Scopes: scopes,
		Resource: resource, Expires: now.Add(s.cfg.AccessTTL), Family: family}
	s.store.st.Tokens[hash(refresh)] = &token{Kind: kindRefresh, ClientID: clientID, Handle: handle, Scopes: scopes,
		Resource: resource, Expires: now.Add(s.cfg.RefreshTTL), Family: family}
	if c := s.store.st.Clients[clientID]; c != nil {
		c.LastUsed = now
	}
	err := s.store.save()
	s.store.mu.Unlock()
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": int(s.cfg.AccessTTL.Seconds()),
		"refresh_token": refresh, "scope": strings.Join(scopes, " "),
	})
}

// refresh rotates a refresh token. A refresh token presented after it already
// rotated is a sign it was stolen: the whole family is revoked.
func (s *Server) refresh(w http.ResponseWriter, presented, clientID string) {
	h := hash(presented)
	s.store.mu.Lock()
	t := s.store.st.Tokens[h]
	switch {
	case t == nil || t.Kind != kindRefresh || t.Revoked || s.now().After(t.Expires) || t.ClientID != clientID:
		s.store.mu.Unlock()
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, revoked or expired")
		return
	case t.Rotated:
		for _, other := range s.store.st.Tokens {
			if other.Family == t.Family {
				other.Revoked = true
			}
		}
		_ = s.store.save()
		s.store.mu.Unlock()
		s.log.Warn("a rotated refresh token was presented again: its family is revoked", "principal", t.Handle)
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token was already used")
		return
	}
	t.Rotated = true
	handle, scopes, resource, family := t.Handle, t.Scopes, t.Resource, t.Family
	s.store.mu.Unlock()
	s.issue(w, clientID, handle, scopes, resource, family)
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err == nil {
		s.store.mu.Lock()
		if t := s.store.st.Tokens[hash(r.PostForm.Get("token"))]; t != nil {
			t.Revoked = true
			if t.Kind == kindRefresh {
				for _, other := range s.store.st.Tokens {
					if other.Family == t.Family {
						other.Revoked = true
					}
				}
			}
			_ = s.store.save()
		}
		s.store.mu.Unlock()
	}
	// RFC 7009: the answer is 200 whether or not the token existed.
	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------- resolve

// errUnknown and friends are logged by the server, never sent.
var (
	errUnknown  = errors.New("the oauth token is unknown")
	errExpired  = errors.New("the oauth token expired or was revoked")
	errNobody   = errors.New("the oauth token's principal is no longer enabled")
	errNotOAuth = errors.New("not an oauth access token")
)

// Lookup resolves the principal of a handle as the principals file now has
// it; it is how a token follows a principal being disabled.
type Lookup func(handle string) (principal.Principal, bool)

// Authenticate resolves an access token to the person it was issued to,
// acting through the client (Agent is the client's name), bound to the MCP
// endpoint, and read-only without mcp:write.
func (s *Server) Authenticate(tokenValue string, lookup Lookup) (principal.Principal, error) {
	if !strings.HasPrefix(tokenValue, AccessPrefix) {
		return principal.Principal{}, errNotOAuth
	}
	s.store.mu.Lock()
	t := s.store.st.Tokens[hash(tokenValue)]
	var name string
	if t != nil {
		if c := s.store.st.Clients[t.ClientID]; c != nil {
			name = c.Name
		}
	}
	s.store.mu.Unlock()
	switch {
	case t == nil || t.Kind != kindAccess:
		return principal.Principal{}, errUnknown
	case t.Revoked || s.now().After(t.Expires):
		return principal.Principal{}, errExpired
	}
	p, ok := lookup(t.Handle)
	if !ok {
		return principal.Principal{}, errNobody
	}
	if name == "" {
		name = "mcp-client"
	}
	p.Agent = name
	if u, err := url.Parse(t.Resource); err == nil {
		p.Audience = u.Path
	}
	p.ReadOnly = !contains(t.Scopes, ScopeWrite)
	return p, nil
}

// ---------------------------------------------------------------- helpers

func (s *Server) client_(id string) *client { //nolint:revive // client is the type
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	return s.store.st.Clients[id]
}

// gc drops expired pending requests and codes. The caller holds mu.
func (s *Server) gc() {
	now := s.now()
	for id, p := range s.pending {
		if now.After(p.expires) {
			delete(s.pending, id)
		}
	}
	for h, c := range s.codes {
		if now.After(c.expires) {
			delete(s.codes, h)
		}
	}
}

// allow is a per-IP, per-endpoint limit of perMinute requests. The address is
// the peer's, never a forwarding header a client can set.
func (s *Server) allow(r *http.Request, endpoint string, perMinute int) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	key := endpoint + "|" + ip
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.limiter[key][:0]
	for _, t := range s.limiter[key] {
		if now.Sub(t) < time.Minute {
			kept = append(kept, t)
		}
	}
	if len(kept) >= perMinute {
		s.limiter[key] = kept
		return false
	}
	s.limiter[key] = append(kept, now)
	return true
}

func redirectWith(w http.ResponseWriter, r *http.Request, base string, params url.Values) {
	u, err := url.Parse(base)
	if err != nil {
		http.Error(w, "bad redirect", http.StatusBadRequest)
		return
	}
	q := u.Query()
	for k, v := range params {
		if len(v) > 0 && v[0] != "" {
			q.Set(k, v[0])
		}
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

// SecretFromEnv reads the Google client secret from the named variable.
func SecretFromEnv(name string) string { return strings.TrimSpace(os.Getenv(name)) }
