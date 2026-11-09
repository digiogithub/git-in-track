package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// This file is the OAuth state that must outlive a restart: registered
// clients and issued tokens. It is operational state, not knowledge, so it
// lives under <dataDir>/oauth/ and never in git. Tokens are stored as sha256
// hashes only; losing the file only means signing in again.

// The token kinds and their prefixes.
const (
	AccessPrefix  = "gto_"
	RefreshPrefix = "gtr_"
	kindAccess    = "access"
	kindRefresh   = "refresh"
)

type client struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Redirects []string  `json:"redirects"`
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"lastUsed"`
}

type token struct {
	Kind     string    `json:"kind"`
	ClientID string    `json:"clientId"`
	Handle   string    `json:"handle"`
	Scopes   []string  `json:"scopes"`
	Resource string    `json:"resource"`
	Expires  time.Time `json:"expires"`
	// Family groups a refresh token with the ones that rotated from it and
	// the access tokens they minted: presenting a rotated refresh token again
	// revokes the whole family.
	Family  string `json:"family"`
	Revoked bool   `json:"revoked,omitempty"`
	Rotated bool   `json:"rotated,omitempty"`
}

type stateFile struct {
	Clients map[string]*client `json:"clients"`
	Tokens  map[string]*token  `json:"tokens"`
}

// store is the persisted state behind a mutex.
type store struct {
	path string
	mu   sync.Mutex
	st   stateFile
}

func openStore(dir string) (*store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	s := &store{path: filepath.Join(dir, "state.json"),
		st: stateFile{Clients: map[string]*client{}, Tokens: map[string]*token{}}}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", s.path, err)
	default:
		if err := json.Unmarshal(raw, &s.st); err != nil {
			return nil, fmt.Errorf("decode %s: %w", s.path, err)
		}
		if s.st.Clients == nil {
			s.st.Clients = map[string]*client{}
		}
		if s.st.Tokens == nil {
			s.st.Tokens = map[string]*token{}
		}
	}
	return s, nil
}

// save writes the state atomically. The caller holds the lock.
func (s *store) save() error {
	raw, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode oauth state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write oauth state: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace oauth state: %w", err)
	}
	return nil
}

// prune drops expired tokens and clients idle past idle. The caller holds the
// lock.
func (s *store) prune(now time.Time, idle time.Duration) {
	for h, t := range s.st.Tokens {
		if now.After(t.Expires) {
			delete(s.st.Tokens, h)
		}
	}
	for id, c := range s.st.Clients {
		last := c.LastUsed
		if last.IsZero() {
			last = c.Created
		}
		if idle > 0 && now.Sub(last) > idle {
			delete(s.st.Clients, id)
		}
	}
}

// randomToken returns prefix plus 32 random bytes, base64url.
func randomToken(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate a token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
