package config

import (
	"strings"
)

// EnvYouTrackToken overrides the YouTrack permanent token of every project.
//
// It is deliberately not GINTRACK_TOKEN: that name already carries two other
// meanings — the companion's own bearer token and the HTTP password go-git
// authenticates a remote with — and overloading it a third time would make one
// leaked variable hand out three different credentials.
const EnvYouTrackToken = "GINTRACK_YOUTRACK_TOKEN"

// TokenSource says where a credential came from, so a surface can report its
// provenance without ever rendering its value (ADR-032).
type TokenSource string

// The provenances a credential can have, in precedence order.
const (
	// TokenSourceNone means no credential is configured at all.
	TokenSourceNone TokenSource = "none"
	// TokenSourceFile means the credential comes from the machine-local
	// configuration file, which is written with mode 0600.
	TokenSourceFile TokenSource = "file"
	// TokenSourceEnv means it comes from EnvYouTrackToken.
	TokenSourceEnv TokenSource = "env"
	// TokenSourceFlag means a command-line flag supplied it for this process
	// only; nothing is written to disk.
	TokenSourceFlag TokenSource = "flag"
)

// Integrations is the machine-local half of the external-tracker configuration:
// the credentials, and nothing else. The committed half — instance URL, remote
// project and field mapping — lives in project.yaml, so that a clone knows where
// its items came from without knowing the token (see YouTrackLink).
//
// The section is excluded from JSON on purpose: the configuration is rendered as
// JSON by `gintrack config show --json` and by nothing else, and a credential has
// no business in either that output or any API response.
type Integrations struct {
	// YouTrack maps a gintrack project key onto the credential of the YouTrack
	// instance that project is linked to.
	YouTrack map[string]YouTrackCredential `json:"-" yaml:"youtrack,omitempty"`
}

// YouTrackCredential is what the companion stores for one project. It is a
// struct rather than a bare string so that a later field — a token expiry, say —
// does not force a schema migration.
type YouTrackCredential struct {
	// Token is the permanent token, "perm:" prefix included.
	Token string `json:"-" yaml:"token"`
}

// String renders the credential without any of its bytes, so that a %v of a
// Config, a Repo listing or a log attribute cannot leak it.
func (c YouTrackCredential) String() string {
	if strings.TrimSpace(c.Token) == "" {
		return "YouTrackCredential{token: <unset>}"
	}
	return "YouTrackCredential{token: [redacted]}"
}

// normalizeProjectKey folds a project key the way lookups compare them. Project
// keys are upper case by the data model (docs/03 section 4.1); accepting the
// lower-case spelling a user types into a flag costs nothing.
func normalizeProjectKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}

// SetYouTrackTokenOverride installs a token that beats the file for every
// project, which is what an environment variable and a --token flag are. The
// value is held in an unexported field: it is never marshaled and therefore can
// never be written back to the file by a later Save.
func (c *Config) SetYouTrackTokenOverride(token string, source TokenSource) {
	token = strings.TrimSpace(token)
	if token == "" {
		c.youtrackToken, c.youtrackTokenSource = "", ""
		return
	}
	c.youtrackToken, c.youtrackTokenSource = token, source
}

// YouTrackToken returns the token to authenticate this project's YouTrack
// instance with and where it came from, applying the documented precedence
// chain flag > env > file > none.
func (c *Config) YouTrackToken(projectKey string) (string, TokenSource) {
	return c.YouTrackTokens().For(projectKey)
}

// HasYouTrackToken reports whether a token resolves for this project.
func (c *Config) HasYouTrackToken(projectKey string) bool {
	token, _ := c.YouTrackToken(projectKey)
	return token != ""
}

// SetYouTrackToken stores a token for a project in the file half of the
// configuration. The caller persists it with Save, which writes mode 0600.
func (c *Config) SetYouTrackToken(projectKey, token string) {
	key := normalizeProjectKey(projectKey)
	token = strings.TrimSpace(token)
	if key == "" {
		return
	}
	if token == "" {
		c.ClearYouTrackToken(projectKey)
		return
	}
	if c.Integrations.YouTrack == nil {
		c.Integrations.YouTrack = map[string]YouTrackCredential{}
	}
	c.Integrations.YouTrack[key] = YouTrackCredential{Token: token}
}

// ClearYouTrackToken forgets the stored token of a project. An override from
// the environment or a flag is untouched: this package does not control the
// process environment and pretending otherwise would report a lie.
func (c *Config) ClearYouTrackToken(projectKey string) {
	key := normalizeProjectKey(projectKey)
	if key == "" || c.Integrations.YouTrack == nil {
		return
	}
	delete(c.Integrations.YouTrack, key)
	if len(c.Integrations.YouTrack) == 0 {
		c.Integrations.YouTrack = nil
	}
}

// YouTrackTokens returns a snapshot of the resolved credentials. It is the value
// to hand to a component that needs to authenticate but has no business holding
// the whole configuration — the companion's YouTrack endpoints, for instance.
func (c *Config) YouTrackTokens() YouTrackTokens {
	out := YouTrackTokens{override: c.youtrackToken, overrideSource: c.youtrackTokenSource}
	if len(c.Integrations.YouTrack) > 0 {
		out.byProject = make(map[string]string, len(c.Integrations.YouTrack))
		for key, cred := range c.Integrations.YouTrack {
			if token := strings.TrimSpace(cred.Token); token != "" {
				out.byProject[normalizeProjectKey(key)] = token
			}
		}
	}
	return out
}

// YouTrackTokens is a resolved view of the machine-local YouTrack credentials:
// the per-project tokens read from the file plus the single override an
// environment variable or a flag installs over all of them.
//
// It has no exported field and no marshaler, so it cannot be serialized into a
// response, a log line or a configuration file by accident.
type YouTrackTokens struct {
	byProject      map[string]string
	override       string
	overrideSource TokenSource
}

// For resolves the token of one project and its provenance.
func (t YouTrackTokens) For(projectKey string) (string, TokenSource) {
	if t.override != "" {
		source := t.overrideSource
		if source == "" {
			source = TokenSourceEnv
		}
		return t.override, source
	}
	if token, ok := t.byProject[normalizeProjectKey(projectKey)]; ok && token != "" {
		return token, TokenSourceFile
	}
	return "", TokenSourceNone
}

// Source reports where this project's token comes from without handing the
// value out at all.
func (t YouTrackTokens) Source(projectKey string) TokenSource {
	_, source := t.For(projectKey)
	return source
}

// Has reports whether a token resolves for this project.
func (t YouTrackTokens) Has(projectKey string) bool {
	token, _ := t.For(projectKey)
	return token != ""
}

// Set records a token for a project in this snapshot. The companion calls it
// after a settings write so the running process honors the new credential even
// when there is no configuration file to persist it to.
func (t *YouTrackTokens) Set(projectKey, token string) {
	key := normalizeProjectKey(projectKey)
	token = strings.TrimSpace(token)
	if key == "" {
		return
	}
	if token == "" {
		t.Clear(projectKey)
		return
	}
	if t.byProject == nil {
		t.byProject = map[string]string{}
	}
	t.byProject[key] = token
}

// Clear forgets a project's token in this snapshot.
func (t *YouTrackTokens) Clear(projectKey string) {
	delete(t.byProject, normalizeProjectKey(projectKey))
}

// Clone returns an independent copy, so that one goroutine's write cannot be
// seen through another's snapshot.
func (t YouTrackTokens) Clone() YouTrackTokens {
	out := YouTrackTokens{override: t.override, overrideSource: t.overrideSource}
	if len(t.byProject) > 0 {
		out.byProject = make(map[string]string, len(t.byProject))
		for k, v := range t.byProject {
			out.byProject[k] = v
		}
	}
	return out
}

// String renders the snapshot as counts, never as values.
func (t YouTrackTokens) String() string {
	override := "<unset>"
	if t.override != "" {
		override = "[redacted]"
	}
	return "config.YouTrackTokens{projects: " + itoa(len(t.byProject)) + ", override: " + override + "}"
}

// itoa keeps String free of a fmt dependency, which would invite a %v of the
// struct fields in a future edit.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
