// Package hub is gintrack-hub: an optional self-hosted server that serves one
// organization's repositories, cloned from the team's existing forge, through
// the same vault, REST API, WebSocket events, MCP endpoint and web app as the
// companion (ADR-043).
//
// This file is the configuration: one YAML file naming the data directory, the
// listener and the repositories of one organization. Nothing here is state;
// the clones under the data directory are, and git is their source of truth.
package hub

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults of the optional settings.
const (
	DefaultPort         = 7420
	DefaultBind         = "127.0.0.1"
	DefaultPullInterval = time.Minute
	DefaultAuthorName   = "gintrack hub"
	DefaultAuthorEmail  = "hub@gintrack.invalid"
	// MinPullInterval keeps a misconfigured hub from hammering the forge.
	MinPullInterval = 10 * time.Second
)

// TokenEnv is the environment variable that overrides the configured bearer
// token, so the secret need not live in the file.
const TokenEnv = "GINTRACK_HUB_TOKEN"

// Config is the hub configuration file.
type Config struct {
	// DataDir holds the clones (repos/<id>), the SSH configuration and the
	// caches. Required; relative paths resolve against the config file.
	DataDir string `yaml:"dataDir"`
	// Bind and Port are the listener. Bind defaults to loopback.
	Bind string `yaml:"bind,omitempty"`
	Port int    `yaml:"port,omitempty"`
	// Token is the bearer token every route but /health requires until
	// per-principal authentication lands (GIT-US-0208). TokenEnv overrides it.
	Token string `yaml:"token,omitempty"`
	// PullInterval is how often every repository is synced with its forge.
	PullInterval Duration `yaml:"pullInterval,omitempty"`
	// Git is the hub's service identity: the committer of every commit, and
	// its author too when no principal is known (single-token mode).
	Git GitIdentity `yaml:"git,omitempty"`
	// Auth configures per-principal authentication (GIT-US-0208).
	Auth Auth `yaml:"auth,omitempty"`
	// OAuth makes the hub an OAuth 2.1 authorization server for /mcp, with
	// Google sign-in (GIT-US-0215). Nil leaves it off.
	OAuth *OAuthConfig `yaml:"oauth,omitempty"`
	// Organization is the one organization this hub serves.
	Organization Organization `yaml:"organization"`
}

// Auth configures authentication.
type Auth struct {
	// PrincipalsFile lists the principals and the hashes of their tokens.
	// When set, the single Token is ignored. Relative paths resolve against
	// the config file.
	PrincipalsFile string `yaml:"principalsFile,omitempty"`
}

// DefaultRedirects are the redirect URIs allowed when oauth.allowedRedirects
// is empty: claude.ai and claude.com connectors.
var DefaultRedirects = []string{
	"https://claude.ai/api/mcp/auth_callback",
	"https://claude.com/api/mcp/auth_callback",
}

// OAuthConfig is the `oauth:` section.
type OAuthConfig struct {
	// Issuer is the hub's public URL. Every published URL is built from it,
	// never from a request's Host. https, except on loopback.
	Issuer           string       `yaml:"issuer"`
	Google           GoogleConfig `yaml:"google"`
	AllowedRedirects []string     `yaml:"allowedRedirects,omitempty"`
	AccessTTL        Duration     `yaml:"accessTTL,omitempty"`
	RefreshTTL       Duration     `yaml:"refreshTTL,omitempty"`
	MaxClients       int          `yaml:"maxClients,omitempty"`
	ClientIdleTTL    Duration     `yaml:"clientIdleTTL,omitempty"`
}

// GoogleConfig is the identity provider people sign in with.
type GoogleConfig struct {
	// IssuerURL is the OIDC issuer; Google by default.
	IssuerURL string `yaml:"issuerURL,omitempty"`
	ClientID  string `yaml:"clientID"`
	// ClientSecretEnv names the variable holding the client secret, which is
	// never written in the file.
	ClientSecretEnv string `yaml:"clientSecretEnv"`
	// HostedDomains admits only accounts whose verified hd claim is listed.
	HostedDomains []string `yaml:"hostedDomains,omitempty"`
}

// GitIdentity is a commit identity.
type GitIdentity struct {
	AuthorName  string `yaml:"authorName,omitempty"`
	AuthorEmail string `yaml:"authorEmail,omitempty"`
}

// Organization is a team repository and its project repositories.
type Organization struct {
	ID       string     `yaml:"id"`
	Team     *RepoSpec  `yaml:"team,omitempty"`
	Projects []RepoSpec `yaml:"projects"`
}

// RepoSpec is one repository the hub clones and serves.
type RepoSpec struct {
	// ID names the clone directory and the mount; [a-z0-9][a-z0-9-]{0,62}.
	ID string `yaml:"id"`
	// URL is the forge remote: ssh (scp-like or ssh://), https, or a local
	// path (tests and single-machine setups).
	URL string `yaml:"url"`
	// DeployKey is the private key the hub authenticates to the forge with,
	// for ssh remotes. Relative paths resolve against the config file.
	DeployKey string `yaml:"deployKey,omitempty"`
	// Branch is checked out on clone; empty means the remote's default.
	Branch string `yaml:"branch,omitempty"`
}

// Duration is a time.Duration written as "90s" or "5m" in YAML.
type Duration time.Duration

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var text string
	if err := node.Decode(&text); err != nil {
		return fmt.Errorf("a duration such as 30s or 5m: %w", err)
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("a duration such as 30s or 5m: %w", err)
	}
	*d = Duration(parsed)
	return nil
}

var repoIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Load reads, defaults and validates a configuration file. Every validation
// failure names the field that caused it.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hub configuration: %w", err)
	}
	cfg, err := Parse(raw, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if env := strings.TrimSpace(os.Getenv(TokenEnv)); env != "" {
		cfg.Token = env
	}
	return cfg, nil
}

// Parse decodes a configuration, resolving relative paths against base.
// Unknown keys are errors: a misspelled setting must not be silently ignored.
func Parse(raw []byte, base string) (*Config, error) {
	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	cfg.applyDefaults(base)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults(base string) {
	if c.Bind == "" {
		c.Bind = DefaultBind
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.PullInterval == 0 {
		c.PullInterval = Duration(DefaultPullInterval)
	}
	if c.Git.AuthorName == "" {
		c.Git.AuthorName = DefaultAuthorName
	}
	if c.Git.AuthorEmail == "" {
		c.Git.AuthorEmail = DefaultAuthorEmail
	}
	c.DataDir = resolve(base, c.DataDir)
	c.Auth.PrincipalsFile = resolve(base, c.Auth.PrincipalsFile)
	if c.OAuth != nil && len(c.OAuth.AllowedRedirects) == 0 {
		c.OAuth.AllowedRedirects = append([]string(nil), DefaultRedirects...)
	}
	for _, r := range c.Repos() {
		r.DeployKey = resolve(base, r.DeployKey)
	}
}

// resolve makes a relative path absolute against base; empty stays empty.
func resolve(base, p string) string {
	if p == "" || filepath.IsAbs(p) || base == "" {
		return p
	}
	return filepath.Join(base, p)
}

// Repos returns pointers to every repository, the team first.
func (c *Config) Repos() []*RepoSpec {
	out := make([]*RepoSpec, 0, len(c.Organization.Projects)+1)
	if c.Organization.Team != nil {
		out = append(out, c.Organization.Team)
	}
	for i := range c.Organization.Projects {
		out = append(out, &c.Organization.Projects[i])
	}
	return out
}

// Validate reports every invalid field at once.
func (c *Config) Validate() error {
	var errs []error
	fail := func(field, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", field, fmt.Sprintf(format, args...)))
	}
	if c.DataDir == "" {
		fail("dataDir", "required")
	}
	if c.Port < 0 || c.Port > 65535 {
		fail("port", "%d is not a TCP port", c.Port)
	}
	if time.Duration(c.PullInterval) < MinPullInterval {
		fail("pullInterval", "%s is below the minimum of %s", time.Duration(c.PullInterval), MinPullInterval)
	}
	if !repoIDRE.MatchString(c.Organization.ID) {
		fail("organization.id", "%q must match %s", c.Organization.ID, repoIDRE)
	}
	if len(c.Organization.Projects) == 0 {
		fail("organization.projects", "at least one project is required")
	}
	seen := map[string]string{}
	check := func(field string, r *RepoSpec) {
		if !repoIDRE.MatchString(r.ID) {
			fail(field+".id", "%q must match %s", r.ID, repoIDRE)
		} else if prev, dup := seen[r.ID]; dup {
			fail(field+".id", "%q is already used by %s", r.ID, prev)
		} else {
			seen[r.ID] = field
		}
		if strings.TrimSpace(r.URL) == "" {
			fail(field+".url", "required")
		}
		if r.DeployKey != "" && !isSSHURL(r.URL) {
			fail(field+".deployKey", "only applies to an ssh remote, and %q is not one", r.URL)
		}
	}
	if c.Organization.Team != nil {
		check("organization.team", c.Organization.Team)
	}
	if o := c.OAuth; o != nil {
		if c.Auth.PrincipalsFile == "" {
			fail("oauth", "needs auth.principalsFile: people are matched by their oidc email there")
		}
		u, err := url.Parse(o.Issuer)
		switch {
		case err != nil || u.Host == "" || u.Path != "" && u.Path != "/":
			fail("oauth.issuer", "%q must be the hub's base URL, e.g. https://hub.acme.example", o.Issuer)
		case u.Scheme != "https" && (u.Scheme != "http" || !isLoopbackName(u.Hostname())):
			fail("oauth.issuer", "%q must use https (http only on loopback)", o.Issuer)
		}
		if strings.TrimSpace(o.Google.ClientID) == "" {
			fail("oauth.google.clientID", "required")
		}
		if strings.TrimSpace(o.Google.ClientSecretEnv) == "" {
			fail("oauth.google.clientSecretEnv", "required: the name of the variable holding the secret")
		}
		for i, r := range o.AllowedRedirects {
			ru, err := url.Parse(r)
			if err != nil || ru.Host == "" || ru.Fragment != "" ||
				(ru.Scheme != "https" && (ru.Scheme != "http" || !isLoopbackName(ru.Hostname()))) {
				fail(fmt.Sprintf("oauth.allowedRedirects[%d]", i), "%q must be an https URL, or http on loopback", r)
			}
		}
	}
	for i := range c.Organization.Projects {
		check(fmt.Sprintf("organization.projects[%d]", i), &c.Organization.Projects[i])
	}
	return errors.Join(errs...)
}

// RepoDir is where a repository is cloned.
func (c *Config) RepoDir(id string) string { return filepath.Join(c.DataDir, "repos", id) }

// CacheDir holds derived data that can be deleted at any time.
func (c *Config) CacheDir() string { return filepath.Join(c.DataDir, "cache") }

func isLoopbackName(h string) bool { return h == "127.0.0.1" || h == "::1" || h == "localhost" }
