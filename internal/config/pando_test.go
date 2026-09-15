package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// pandoYAML is a file that spells out both stanzas the way docs/07 documents
// them, so the round trip is exercised against the documented spelling rather
// than against whatever the structs happen to marshal.
const pandoYAML = `version: 1
agent:
  enabled: true
  pando:
    url: http://127.0.0.1:8090
    path: /api/v1/agui
    token: file-token
    agent: backlog-assistant
    insecureTls: false
    maxRuns: 4
    repos:
      - repo: git-in-track
        url: http://127.0.0.1:8091
        token: repo-token
search:
  pando:
    mcpUrl: http://127.0.0.1:9777/mcp
    mcpToken: mcp-token
    restUrl: http://127.0.0.1:7788
    restToken: rest-token
    projectId: git-in-track
    corpusDir: /tmp/pando-kb
`

func TestParsePandoSections(t *testing.T) {
	cfg, err := Parse([]byte(pandoYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !cfg.Agent.Enabled {
		t.Fatal("agent.enabled should be true")
	}
	p := cfg.Agent.Pando
	if p.URL != "http://127.0.0.1:8090" || p.Path != "/api/v1/agui" || p.Agent != "backlog-assistant" {
		t.Fatalf("unexpected agent.pando: %+v", PandoTarget{URL: p.URL, Path: p.Path, Agent: p.Agent})
	}
	if p.MaxRuns != 4 {
		t.Fatalf("maxRuns = %d, want 4", p.MaxRuns)
	}
	if len(p.Repos) != 1 || p.Repos[0].Repo != "git-in-track" || p.Repos[0].URL != "http://127.0.0.1:8091" {
		t.Fatalf("unexpected routing table: %v", p.Repos)
	}
	s := cfg.Search.Pando
	if s.MCPURL != "http://127.0.0.1:9777/mcp" || s.RESTURL != "http://127.0.0.1:7788" {
		t.Fatalf("unexpected search.pando: %v", s)
	}
	if s.ProjectID != "git-in-track" || s.CorpusDir != "/tmp/pando-kb" {
		t.Fatalf("unexpected search.pando ids: %v", s)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestPandoSectionRoundTrips(t *testing.T) {
	cfg, err := Parse([]byte(pandoYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	again, err := Parse(raw)
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	if again.Agent.Enabled != cfg.Agent.Enabled || again.Agent.Pando.URL != cfg.Agent.Pando.URL ||
		again.Agent.Pando.Path != cfg.Agent.Pando.Path || again.Agent.Pando.Agent != cfg.Agent.Pando.Agent ||
		again.Agent.Pando.MaxRuns != cfg.Agent.Pando.MaxRuns || again.Agent.Pando.Token != cfg.Agent.Pando.Token {
		t.Fatalf("agent section did not round trip: %v", again.Agent.Pando)
	}
	if len(again.Agent.Pando.Repos) != 1 || again.Agent.Pando.Repos[0].Token != "repo-token" {
		t.Fatalf("routing table did not round trip: %v", again.Agent.Pando.Repos)
	}
	if again.Search.Pando != cfg.Search.Pando {
		t.Fatalf("search section did not round trip: %v", again.Search.Pando)
	}
}

func TestPandoDefaults(t *testing.T) {
	cfg := Default()
	p := cfg.Agent.Pando
	switch {
	case cfg.Agent.Enabled:
		t.Fatal("the agent feature must ship disabled")
	case p.Path != DefaultPandoPath:
		t.Fatalf("path = %q, want %q", p.Path, DefaultPandoPath)
	case p.Agent != DefaultPandoAgent:
		t.Fatalf("agent = %q, want %q", p.Agent, DefaultPandoAgent)
	case p.MaxRuns != DefaultPandoMaxRuns:
		t.Fatalf("maxRuns = %d, want %d", p.MaxRuns, DefaultPandoMaxRuns)
	case cfg.Search.Pando.MCPURL != "":
		t.Fatal("semantic search must ship off")
	}
}

func TestResolvedPandoTokenPrecedence(t *testing.T) {
	cfg, err := Parse([]byte(pandoYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	token, source := cfg.ResolvedPandoToken("")
	if token != "file-token" || source != TokenSourceFile {
		t.Fatalf("default token = %q/%s", token, source)
	}
	token, _ = cfg.ResolvedPandoToken("git-in-track")
	if token != "repo-token" {
		t.Fatalf("repo token = %q, want the row's own", token)
	}
	// An id the table does not name falls back to the section token rather
	// than to some other repository's credential.
	token, _ = cfg.ResolvedPandoToken("other")
	if token != "file-token" {
		t.Fatalf("unknown repo token = %q, want the section's", token)
	}

	cfg.SetPandoTokenOverride("env-token")
	for _, id := range []string{"", "git-in-track", "other"} {
		token, source = cfg.ResolvedPandoToken(id)
		if token != "env-token" || source != TokenSourceEnv {
			t.Fatalf("%s overrides every row: got %q/%s for %q", EnvPandoToken, token, source, id)
		}
	}
}

func TestPandoTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agui.token")
	if err := os.WriteFile(path, []byte("  from-file\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := Default()
	cfg.Agent.Pando.TokenFile = path
	token, source := cfg.ResolvedPandoToken("")
	if token != "from-file" || source != TokenSourceFile {
		t.Fatalf("tokenFile = %q/%s, want the trimmed contents", token, source)
	}
	// An inline token beats the file: it is the more specific of the two.
	cfg.Agent.Pando.Token = "inline"
	if token, _ = cfg.ResolvedPandoToken(""); token != "inline" {
		t.Fatalf("token = %q, want the inline value", token)
	}
	// A file that is not there leaves the feature unconfigured rather than
	// failing the whole configuration.
	cfg.Agent.Pando.Token = ""
	cfg.Agent.Pando.TokenFile = filepath.Join(dir, "missing")
	if token, source = cfg.ResolvedPandoToken(""); token != "" || source != TokenSourceNone {
		t.Fatalf("missing tokenFile = %q/%s, want none", token, source)
	}
}

func TestPandoEnvOverridesFromResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(pandoYAML), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	env := func(key string) string {
		switch key {
		case EnvPandoToken:
			return "env-agui"
		case EnvPandoMCPToken:
			return "env-mcp"
		case EnvPandoRESTToken:
			return "env-rest"
		default:
			return ""
		}
	}
	res, err := Resolve(Flags{ConfigPath: path}, env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if token, _ := res.Config.ResolvedPandoToken(""); token != "env-agui" {
		t.Fatalf("agui token = %q", token)
	}
	if token, _ := res.Config.ResolvedPandoMCPToken(); token != "env-mcp" {
		t.Fatalf("mcp token = %q", token)
	}
	if token, _ := res.Config.ResolvedPandoRESTToken(); token != "env-rest" {
		t.Fatalf("rest token = %q", token)
	}
	// The override lives in an unexported field, so saving the file back must
	// not write any of the three environment secrets into it.
	if err := Save(path, res.Config); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // the path is this test's own temp file
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, secret := range []string{"env-agui", "env-mcp", "env-rest"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("Save wrote the environment secret %q into the file", secret)
		}
	}
}

func TestPandoRedaction(t *testing.T) {
	cfg, err := Parse([]byte(pandoYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	rendered := cfg.Agent.Pando.String() + cfg.Agent.Pando.Repos[0].String() +
		cfg.Search.Pando.String() + cfg.PandoTargets().String()
	for _, secret := range []string{"file-token", "repo-token", "mcp-token", "rest-token"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("a String() rendered the secret %q", secret)
		}
	}
	if !strings.Contains(rendered, "[redacted]") {
		t.Fatalf("a configured token should render as [redacted]: %s", rendered)
	}
}

func TestPandoTargets(t *testing.T) {
	cfg, err := Parse([]byte(pandoYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	targets := cfg.PandoTargets()
	if !targets.Configured() {
		t.Fatal("a configured section should report itself configured")
	}
	if targets.MaxRuns() != 4 {
		t.Fatalf("MaxRuns = %d, want 4", targets.MaxRuns())
	}
	target, ok := targets.Target("git-in-track")
	if !ok || target.URL != "http://127.0.0.1:8091" {
		t.Fatalf("Target(git-in-track) = %+v, %v", target, ok)
	}
	if !targets.Known("git-in-track") || targets.Known("nope") {
		t.Fatal("Known must distinguish a declared row from anything else")
	}
	if target, ok = targets.Target("nope"); !ok || target.URL != "http://127.0.0.1:8090" {
		t.Fatalf("an undeclared id falls back to the default upstream: %+v", target)
	}
	if targets.Token("git-in-track") != "repo-token" || targets.Token("") != "file-token" {
		t.Fatal("Token must follow the same precedence as ResolvedPandoToken")
	}

	// No URL anywhere means the feature is absent, not broken.
	empty := Default()
	empty.Agent.Pando.URL = ""
	if empty.PandoTargets().Configured() {
		t.Fatal("an empty URL must report the feature as unconfigured")
	}
	if _, ok := empty.PandoTargets().Target(""); ok {
		t.Fatal("there is no target without a URL")
	}
	// A section that names no maxRuns gets the shipped one.
	empty.Agent.Pando.MaxRuns = 0
	if empty.PandoTargets().MaxRuns() != DefaultPandoMaxRuns {
		t.Fatalf("MaxRuns = %d, want the default", empty.PandoTargets().MaxRuns())
	}
}

func TestValidatePandoRefusals(t *testing.T) {
	tests := []struct {
		name  string
		mut   func(c *Config)
		field string
	}{
		{
			name:  "malformed url",
			mut:   func(c *Config) { c.Agent.Pando.URL = "http://[::1" },
			field: "agent.pando.url",
		},
		{
			name:  "unsupported scheme",
			mut:   func(c *Config) { c.Agent.Pando.URL = "ftp://127.0.0.1:8090" },
			field: "agent.pando.url",
		},
		{
			name:  "remote host without allowRemote",
			mut:   func(c *Config) { c.Agent.Pando.URL = "http://pando.example.com:8090" },
			field: "agent.pando.url",
		},
		{
			name:  "credentials in the url",
			mut:   func(c *Config) { c.Agent.Pando.URL = "http://user:pass@127.0.0.1:8090" },
			field: "agent.pando.url",
		},
		{
			name:  "relative path",
			mut:   func(c *Config) { c.Agent.Pando.Path = "api/v1/agui" },
			field: "agent.pando.path",
		},
		{
			name:  "maxRuns out of range",
			mut:   func(c *Config) { c.Agent.Pando.MaxRuns = MaxPandoMaxRuns + 1 },
			field: "agent.pando.maxRuns",
		},
		{
			name: "duplicate routing rows",
			mut: func(c *Config) {
				c.Agent.Pando.Repos = []PandoRepo{{Repo: "a"}, {Repo: "a"}}
			},
			field: "agent.pando.repos[1].repo",
		},
		{
			name: "remote row without allowRemote",
			mut: func(c *Config) {
				c.Agent.Pando.Repos = []PandoRepo{{Repo: "a", URL: "https://pando.example.com"}}
			},
			field: "agent.pando.repos[0].url",
		},
		{
			name:  "enabled without a url",
			mut:   func(c *Config) { c.Agent.Enabled = true; c.Agent.Pando.URL = "" },
			field: "agent.pando.url",
		},
		{
			name:  "remote mcp url",
			mut:   func(c *Config) { c.Search.Pando.MCPURL = "http://pando.example.com:9777/mcp" },
			field: "search.pando.mcpUrl",
		},
		{
			name:  "relative corpus dir",
			mut:   func(c *Config) { c.Search.Pando.CorpusDir = "pando-kb" },
			field: "search.pando.corpusDir",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			tc.mut(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected a validation failure")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error does not classify as ErrInvalid: %v", err)
			}
			var fields FieldErrors
			if !errors.As(err, &fields) {
				t.Fatalf("error is not a FieldErrors: %v", err)
			}
			for _, fe := range fields {
				if fe.Field == tc.field {
					return
				}
			}
			t.Fatalf("no failure on %s: %v", tc.field, err)
		})
	}
}

func TestValidatePandoAllowRemote(t *testing.T) {
	cfg := Default()
	cfg.Agent.Pando.URL = "https://pando.example.com"
	cfg.Agent.Pando.AllowRemote = true
	cfg.Search.Pando.MCPURL = "https://pando.example.com/mcp"
	cfg.Search.Pando.AllowRemote = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("allowRemote should permit a remote host: %v", err)
	}
	// The section-wide opt-in covers the rows underneath it.
	cfg.Agent.Pando.Repos = []PandoRepo{{Repo: "a", URL: "https://other.example.com"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a row inherits the section's allowRemote: %v", err)
	}
}

func TestLoopbackURL(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:8090", "http://localhost:1/x", "https://[::1]:9"} {
		if !LoopbackURL(raw) {
			t.Fatalf("%s should be loopback", raw)
		}
	}
	for _, raw := range []string{"http://10.0.0.1", "http://example.com", "://nope"} {
		if LoopbackURL(raw) {
			t.Fatalf("%s should not be loopback", raw)
		}
	}
}
