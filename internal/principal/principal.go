// Package principal is the authenticated identity a request runs as in
// gintrack-hub (ADR-043): a person or an agent, with the name and email its
// writes are committed under.
//
// It is plain Go with no dependencies, so the server, the vault and the MCP
// layer can all read it, and it compiles to WebAssembly next to them. The
// companion never sets a principal: without one every write keeps the git
// identity of the machine, exactly as before.
package principal

import (
	"context"
	"regexp"
	"strings"
)

// Kind tells a person from an agent.
type Kind string

// The principal kinds.
const (
	Human Kind = "human"
	Agent Kind = "agent"
)

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool { return k == Human || k == Agent }

// Principal is one authenticated identity.
type Principal struct {
	// Handle is the unique name of the principal in the hub.
	Handle string
	// Name and Email are the commit author of its writes.
	Name  string
	Email string
	// Kind is human or agent.
	Kind Kind
	// Agent names the agent for the `Agent:` commit trailer; agents only.
	Agent string
	// OnBehalfOf is the handle of the person an agent acts for, written as the
	// `On-Behalf-Of:` trailer. Optional.
	OnBehalfOf string
}

var handleRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidHandle reports whether h matches the handle grammar.
func ValidHandle(h string) bool { return handleRE.MatchString(h) }

// Valid reports whether p carries everything a commit needs.
func (p Principal) Valid() bool {
	if !ValidHandle(p.Handle) || strings.TrimSpace(p.Name) == "" || !strings.Contains(p.Email, "@") || !p.Kind.Valid() {
		return false
	}
	return p.Kind != Agent || strings.TrimSpace(p.Agent) != ""
}

type contextKey struct{}

// With returns a context that carries p.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// From returns the principal ctx carries, if any.
func From(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return Principal{}, false
	}
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}
