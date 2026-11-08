package hub

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/digiogithub/git-in-track/internal/principal"
)

// This file is the hub's authentication for the proof of concept (ADR-043,
// GIT-US-0208): opaque bearer tokens, one or more per principal, whose sha256
// hashes are listed in a principals file. The hub never writes that file and
// never stores a token: `gintrack-hub token new` prints a fresh one once, with
// the hash line to paste.

// TokenPrefix starts every token the hub issues, so a leaked one is easy to
// recognize and scan for.
const TokenPrefix = "gth_"

// tokenHashRE is a stored token hash.
var tokenHashRE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// reloadEvery bounds how often the principals file is stat'ed for changes.
const reloadEvery = time.Second

// PrincipalsFile is the on-disk shape of the principals file.
type PrincipalsFile struct {
	Principals []PrincipalEntry `yaml:"principals"`
}

// PrincipalEntry is one principal and the hashes of its tokens.
type PrincipalEntry struct {
	Handle     string   `yaml:"handle"`
	Name       string   `yaml:"name"`
	Email      string   `yaml:"email"`
	Kind       string   `yaml:"kind"`
	Agent      string   `yaml:"agent,omitempty"`
	OnBehalfOf string   `yaml:"onBehalfOf,omitempty"`
	Tokens     []string `yaml:"tokens"`
	Disabled   bool     `yaml:"disabled,omitempty"`
	// Grants are the scopes the principal may act in (GIT-US-0209). None
	// means nothing is visible.
	Grants []GrantEntry `yaml:"grants,omitempty"`
	// OIDC is how a person signs in through OAuth (GIT-US-0215).
	OIDC *OIDCEntry `yaml:"oidc,omitempty"`
}

// OIDCEntry matches a verified identity-provider account to a principal.
type OIDCEntry struct {
	Email string `yaml:"email"`
}

// NewToken returns a fresh token and the hash line that stores it.
func NewToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate a token: %w", err)
	}
	token = TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken is the stored form of a token: "sha256:<hex>". A token carries 256
// bits of entropy, so a fast hash is enough; there is nothing to brute-force.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ParsePrincipals decodes and validates a principals file, reporting every
// problem at once. It never echoes a hash back in a message.
func ParsePrincipals(raw []byte) (map[string]principalRecord, error) {
	var file PrincipalsFile
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	var errs []error
	fail := func(i int, field, format string, args ...any) {
		errs = append(errs, fmt.Errorf("principals[%d].%s: %s", i, field, fmt.Sprintf(format, args...)))
	}
	byHash := map[string]principalRecord{}
	handles := map[string]PrincipalEntry{}
	for i, e := range file.Principals {
		if !principal.ValidHandle(e.Handle) {
			fail(i, "handle", "%q is not a handle ([a-z0-9][a-z0-9._-]{0,63})", e.Handle)
		} else if _, dup := handles[e.Handle]; dup {
			fail(i, "handle", "%q is listed twice", e.Handle)
		}
		handles[e.Handle] = e
		if strings.TrimSpace(e.Name) == "" {
			fail(i, "name", "required")
		}
		if !strings.Contains(e.Email, "@") {
			fail(i, "email", "required, and must be an email address")
		}
		kind := principal.Kind(e.Kind)
		if !kind.Valid() {
			fail(i, "kind", "%q must be human or agent", e.Kind)
		}
		if kind == principal.Agent && strings.TrimSpace(e.Agent) == "" {
			fail(i, "agent", "required for an agent")
		}
		if e.OIDC != nil && (kind != principal.Human || !strings.Contains(e.OIDC.Email, "@")) {
			fail(i, "oidc.email", "only a human signs in, with a full email address")
		}
		if kind == principal.Human && (e.Agent != "" || e.OnBehalfOf != "") {
			fail(i, "agent", "agent and onBehalfOf only apply to an agent")
		}
		var grants Grants
		for j, ge := range e.Grants {
			g, err := parseGrant(fmt.Sprintf("principals[%d].grants[%d]", i, j), ge)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			grants.entries = append(grants.entries, g)
		}
		for j, h := range e.Tokens {
			switch {
			case !tokenHashRE.MatchString(h):
				fail(i, fmt.Sprintf("tokens[%d]", j), "must be sha256: followed by 64 lowercase hex digits")
			case byHash[h].p.Handle != "":
				fail(i, fmt.Sprintf("tokens[%d]", j), "the same hash is listed for %s", byHash[h].p.Handle)
			default:
				byHash[h] = principalRecord{
					p: principal.Principal{
						Handle: e.Handle, Name: e.Name, Email: e.Email, Kind: kind,
						Agent: e.Agent, OnBehalfOf: e.OnBehalfOf,
					},
					disabled: e.Disabled,
					grants:   grants,
					email:    oidcEmail(e),
				}
			}
		}
	}
	for i, e := range file.Principals {
		if e.OnBehalfOf == "" {
			continue
		}
		if who, ok := handles[e.OnBehalfOf]; !ok || who.Kind != string(principal.Human) {
			fail(i, "onBehalfOf", "%q is not a human principal of this file", e.OnBehalfOf)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return byHash, nil
}

// principalRecord is one token's principal.
type principalRecord struct {
	p        principal.Principal
	disabled bool
	grants   Grants
	email    string
}

// Authenticator resolves tokens through a principals file that it reloads
// when the file changes, so revoking a token is an edit, not a restart. A file
// that stops parsing keeps the last good version in force and is logged.
type Authenticator struct {
	path string
	log  *slog.Logger
	now  func() time.Time

	mu      sync.Mutex
	byHash  map[string]principalRecord
	modTime time.Time
	size    int64
	checked time.Time
}

// The refusal reasons. The server logs them and answers every one with the
// same 401, so a client cannot tell an unknown token from a disabled one.
var (
	errUnknownToken  = errors.New("the token matches no principal")
	errDisabled      = errors.New("the principal is disabled")
	errPrincipalFile = errors.New("the principals file could not be read")
)

// NewAuthenticator loads path, failing when the first load does.
func NewAuthenticator(path string, log *slog.Logger) (*Authenticator, error) {
	if log == nil {
		log = slog.Default()
	}
	a := &Authenticator{path: path, log: log, now: time.Now}
	if err := a.reload(true); err != nil {
		return nil, err
	}
	return a, nil
}

// Authenticate resolves a token to its principal. The token itself is never
// logged or returned in an error.
func (a *Authenticator) Authenticate(token string) (principal.Principal, error) {
	a.mu.Lock()
	if a.now().Sub(a.checked) >= reloadEvery {
		if err := a.reload(false); err != nil {
			a.log.Warn("the principals file is invalid; keeping the previous version", "path", a.path, "error", err)
		}
	}
	byHash := a.byHash
	a.mu.Unlock()
	if byHash == nil {
		return principal.Principal{}, errPrincipalFile
	}

	// The lookup is keyed by the hash of what was presented, so its timing
	// says nothing about any stored token: a caller cannot steer the hash of
	// a guess towards a stored one.
	rec, ok := byHash[HashToken(token)]
	if !ok {
		return principal.Principal{}, errUnknownToken
	}
	if rec.disabled {
		return principal.Principal{}, fmt.Errorf("%w: %s", errDisabled, rec.p.Handle)
	}
	return rec.p, nil
}

func oidcEmail(e PrincipalEntry) string {
	if e.OIDC == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(e.OIDC.Email))
}

// HumanByEmail finds the enabled human whose oidc email this is.
func (a *Authenticator) HumanByEmail(email string) (principal.Principal, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, rec := range a.byHash {
		if rec.email != "" && rec.email == email && !rec.disabled && rec.p.Kind == principal.Human {
			return rec.p, true
		}
	}
	return principal.Principal{}, false
}

// Lookup returns the enabled principal with this handle, as the file now has
// it, so an OAuth token stops working once its principal is disabled.
func (a *Authenticator) Lookup(handle string) (principal.Principal, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, rec := range a.byHash {
		if rec.p.Handle == handle && !rec.disabled {
			return rec.p, true
		}
	}
	return principal.Principal{}, false
}

// GrantsOf returns the grants of a principal, as the file currently in force
// lists them; an unknown or disabled handle has none.
func (a *Authenticator) GrantsOf(handle string) Grants {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, rec := range a.byHash {
		if rec.p.Handle == handle && !rec.disabled {
			return rec.grants
		}
	}
	return Grants{}
}

// reload re-reads the file when its size or modification time changed. The
// caller holds the lock, except on the first load.
func (a *Authenticator) reload(first bool) error {
	a.checked = a.now()
	info, err := os.Stat(a.path)
	if err != nil {
		return fmt.Errorf("principals file: %w", err)
	}
	if !first && info.ModTime().Equal(a.modTime) && info.Size() == a.size {
		return nil
	}
	raw, err := os.ReadFile(a.path)
	if err != nil {
		return fmt.Errorf("principals file: %w", err)
	}
	byHash, err := ParsePrincipals(raw)
	if err != nil {
		if !first {
			// Remember the bad version so it is reported once, not every second.
			a.modTime, a.size = info.ModTime(), info.Size()
		}
		return fmt.Errorf("principals file %s: %w", a.path, err)
	}
	a.byHash, a.modTime, a.size = byHash, info.ModTime(), info.Size()
	return nil
}
