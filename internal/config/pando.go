package config

import (
	"net"
	"net/url"
	"os"
	"strings"
)

// The Pando credentials the environment may supply. Each one is a distinct
// name on purpose: GINTRACK_TOKEN already carries two meanings — the
// companion's own bearer token and the HTTP password go-git authenticates a
// remote with — and one leaked variable must never hand out four credentials.
const (
	// EnvPandoToken overrides the AG-UI bearer token of every upstream.
	EnvPandoToken = "GINTRACK_PANDO_TOKEN"
	// EnvPandoMCPToken overrides `search.pando.mcpToken`.
	EnvPandoMCPToken = "GINTRACK_PANDO_MCP_TOKEN"
	// EnvPandoRESTToken overrides `search.pando.restToken`.
	EnvPandoRESTToken = "GINTRACK_PANDO_REST_TOKEN"
)

// SetPandoTokenOverride installs the AG-UI token supplied by the environment.
// It is held in an unexported field, so no Save can write it back to the file.
func (c *Config) SetPandoTokenOverride(token string) { c.pandoToken = strings.TrimSpace(token) }

// SetPandoMCPTokenOverride installs the `search.pando.mcpToken` override.
func (c *Config) SetPandoMCPTokenOverride(token string) { c.pandoMCPToken = strings.TrimSpace(token) }

// SetPandoRESTTokenOverride installs the `search.pando.restToken` override.
func (c *Config) SetPandoRESTTokenOverride(token string) {
	c.pandoRESTToken = strings.TrimSpace(token)
}

// ResolvedPandoToken returns the bearer token to authenticate the AG-UI adapter
// serving this repository with, and where it came from.
//
// The precedence is the documented one — environment over file — and within the
// file half the row of `agent.pando.repos` that names the repository beats the
// section-wide value, with `token` beating `tokenFile`. An empty repoID asks for
// the section default.
//
// It is the only way out of this package for the value: nothing copies a Pando
// token into a struct that anything marshals.
func (c *Config) ResolvedPandoToken(repoID string) (string, TokenSource) {
	if c.pandoToken != "" {
		return c.pandoToken, TokenSourceEnv
	}
	if row, ok := c.Agent.Pando.repo(repoID); ok {
		if token := fileToken(row.Token, row.TokenFile); token != "" {
			return token, TokenSourceFile
		}
	}
	if token := fileToken(c.Agent.Pando.Token, c.Agent.Pando.TokenFile); token != "" {
		return token, TokenSourceFile
	}
	return "", TokenSourceNone
}

// ResolvedPandoMCPToken returns the token of the Pando MCP endpoint.
func (c *Config) ResolvedPandoMCPToken() (string, TokenSource) {
	if c.pandoMCPToken != "" {
		return c.pandoMCPToken, TokenSourceEnv
	}
	if token := strings.TrimSpace(c.Search.Pando.MCPToken); token != "" {
		return token, TokenSourceFile
	}
	return "", TokenSourceNone
}

// ResolvedPandoRESTToken returns the token of the Pando REST API.
func (c *Config) ResolvedPandoRESTToken() (string, TokenSource) {
	if c.pandoRESTToken != "" {
		return c.pandoRESTToken, TokenSourceEnv
	}
	if token := strings.TrimSpace(c.Search.Pando.RESTToken); token != "" {
		return token, TokenSourceFile
	}
	return "", TokenSourceNone
}

// fileToken returns the inline token, or the contents of the file that holds
// one. A file that cannot be read yields no token rather than an error: the
// feature reports itself unconfigured, which is what an operator who has not
// provisioned the credential yet should see.
func fileToken(inline, path string) string {
	if token := strings.TrimSpace(inline); token != "" {
		return token
	}
	if path = strings.TrimSpace(path); path == "" {
		return ""
	}
	raw, err := os.ReadFile(path) //nolint:gosec // the path is an operator-supplied credential file
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// repo returns the routing-table row for a repository id.
func (p Pando) repo(repoID string) (PandoRepo, bool) {
	repoID = strings.TrimSpace(repoID)
	if repoID == "" {
		return PandoRepo{}, false
	}
	for _, row := range p.Repos {
		if row.Repo == repoID {
			return row, true
		}
	}
	return PandoRepo{}, false
}

// PandoTarget is the non-secret half of one resolved upstream: everything a
// caller needs to build a request, and nothing it needs to authenticate one.
// It is safe to log and safe to marshal, which is exactly why the token is not
// in it.
type PandoTarget struct {
	// URL is the base URL of the adapter.
	URL string
	// Path is the AG-UI mount point under it, with no trailing slash.
	Path string
	// Agent is the AG-UI agent or profile name.
	Agent string
	// InsecureTLS skips certificate verification for this upstream.
	InsecureTLS bool
}

// PandoTargets is a resolved snapshot of the AG-UI routing table: the default
// upstream, the per-repository overrides and the tokens, with the tokens in
// unexported fields so the snapshot can be handed to a component that has no
// business holding the whole configuration (the companion's agent proxy) and
// still cannot leak one.
type PandoTargets struct {
	def     PandoTarget
	defTok  string
	byRepo  map[string]PandoTarget
	tokens  map[string]string
	maxRuns int
}

// PandoTargets resolves the `agent.pando` section into a snapshot.
func (c *Config) PandoTargets() PandoTargets {
	p := c.Agent.Pando
	out := PandoTargets{maxRuns: p.MaxRuns}
	if out.maxRuns <= 0 {
		out.maxRuns = DefaultPandoMaxRuns
	}
	out.def = PandoTarget{
		URL:         strings.TrimRight(strings.TrimSpace(p.URL), "/"),
		Path:        pandoPath(p.Path),
		Agent:       pandoAgent(p.Agent),
		InsecureTLS: p.InsecureTLS,
	}
	out.defTok, _ = c.ResolvedPandoToken("")
	if len(p.Repos) == 0 {
		return out
	}
	out.byRepo = make(map[string]PandoTarget, len(p.Repos))
	out.tokens = make(map[string]string, len(p.Repos))
	for _, row := range p.Repos {
		id := strings.TrimSpace(row.Repo)
		if id == "" {
			continue
		}
		target := out.def
		if u := strings.TrimSpace(row.URL); u != "" {
			target.URL = strings.TrimRight(u, "/")
		}
		if row.Agent != "" {
			target.Agent = row.Agent
		}
		target.InsecureTLS = row.InsecureTLS || p.InsecureTLS
		out.byRepo[id] = target
		token, _ := c.ResolvedPandoToken(id)
		out.tokens[id] = token
	}
	return out
}

// Configured reports whether an upstream URL is set at all. With none there is
// nothing to proxy to and the feature reports itself absent.
func (t PandoTargets) Configured() bool { return t.def.URL != "" || len(t.byRepo) > 0 }

// MaxRuns is the global in-flight run cap.
func (t PandoTargets) MaxRuns() int { return t.maxRuns }

// Repos lists the repository ids the table names, for the startup banner.
func (t PandoTargets) Repos() []string {
	if len(t.byRepo) == 0 {
		return nil
	}
	out := make([]string, 0, len(t.byRepo))
	for id := range t.byRepo {
		out = append(out, id)
	}
	return out
}

// Target returns the upstream serving a repository. An empty id, or one the
// table does not name, resolves to the default upstream; it reports false only
// when there is no upstream at all.
func (t PandoTargets) Target(repoID string) (PandoTarget, bool) {
	if target, ok := t.byRepo[strings.TrimSpace(repoID)]; ok && target.URL != "" {
		return target, true
	}
	if t.def.URL == "" {
		return PandoTarget{}, false
	}
	return t.def, true
}

// Known reports whether the table names this repository explicitly. The proxy
// uses it to tell "no override, use the default" from "this id is a typo",
// which must never fall through to another repository's agent.
func (t PandoTargets) Known(repoID string) bool {
	_, ok := t.byRepo[strings.TrimSpace(repoID)]
	return ok
}

// Token returns the bearer token of a repository's upstream. It is the one
// accessor that yields a secret, and its result goes into an Authorization
// header and nowhere else.
func (t PandoTargets) Token(repoID string) string {
	if token, ok := t.tokens[strings.TrimSpace(repoID)]; ok && token != "" {
		return token
	}
	return t.defTok
}

// String renders the snapshot without any of its tokens.
func (t PandoTargets) String() string {
	return "PandoTargets{url: " + t.def.URL + ", path: " + t.def.Path +
		", agent: " + t.def.Agent + ", repos: " + strings.Join(t.Repos(), ",") + "}"
}

// pandoPath normalises an AG-UI mount point: leading slash, no trailing one.
func pandoPath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return DefaultPandoPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.TrimRight(path, "/")
}

// pandoAgent returns the AG-UI agent name or the shipped default.
func pandoAgent(raw string) string {
	if name := strings.TrimSpace(raw); name != "" {
		return name
	}
	return DefaultPandoAgent
}

// LoopbackURL reports whether a URL names a loopback interface. It is what
// `allowRemote` guards: dialing a host on the network with a token attached is
// a deliberate act, never a default.
func LoopbackURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
