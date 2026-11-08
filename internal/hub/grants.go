package hub

import (
	"fmt"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// This file is the grant model of GIT-US-0209: what a principal may do in
// which scope. A scope is project/<key> or org/<team key>, lower-cased, or the
// wildcards project/* and org/*. Everything not granted is denied.

// Perm is one permission.
type Perm string

// The permissions, from weakest to strongest. write implies propose, and
// propose, write and delete need read in the same scope.
const (
	PermRead    Perm = "read"
	PermPropose Perm = "propose"
	PermWrite   Perm = "write"
	PermDelete  Perm = "delete"
	PermAdmin   Perm = "admin"
)

func (p Perm) valid() bool {
	switch p {
	case PermRead, PermPropose, PermWrite, PermDelete, PermAdmin:
		return true
	default:
		return false
	}
}

// GrantEntry is one scope and its permissions, as the principals file spells it.
type GrantEntry struct {
	Scope string `yaml:"scope"`
	Perms []Perm `yaml:"perms"`
}

// grant is one parsed entry.
type grant struct {
	org   bool   // org/ rather than project/
	key   string // lower-cased key, or "*"
	perms map[Perm]bool
}

// Grants is everything a principal may do.
type Grants struct {
	entries []grant
}

// parseGrant validates one entry; field names the entry for the message.
func parseGrant(field string, e GrantEntry) (grant, error) {
	kind, key, ok := strings.Cut(strings.TrimSpace(e.Scope), "/")
	if !ok || (kind != "project" && kind != "org") || key == "" {
		return grant{}, fmt.Errorf("%s.scope: %q must be project/<key>, org/<key>, project/* or org/*", field, e.Scope)
	}
	if key != "*" && !core.ValidProjectKey(core.ProjectKey(strings.ToUpper(key))) &&
		(kind != "org" || !core.ValidTeamKey(core.TeamKey(strings.ToUpper(key)))) {
		return grant{}, fmt.Errorf("%s.scope: %q does not name a valid key", field, e.Scope)
	}
	g := grant{org: kind == "org", key: strings.ToLower(key), perms: map[Perm]bool{}}
	for _, p := range e.Perms {
		if !p.valid() {
			return grant{}, fmt.Errorf("%s.perms: unknown permission %q", field, p)
		}
		g.perms[p] = true
	}
	if g.perms[PermWrite] {
		g.perms[PermPropose] = true
	}
	if (g.perms[PermPropose] || g.perms[PermDelete]) && !g.perms[PermRead] {
		return grant{}, fmt.Errorf("%s.perms: propose, write and delete need read in the same scope", field)
	}
	if len(g.perms) == 0 {
		return grant{}, fmt.Errorf("%s.perms: at least one permission is required", field)
	}
	return g, nil
}

// Allows reports whether the grants admit perm on a project (team false) or a
// team (team true) with that key.
func (g Grants) Allows(key string, team bool, perm Perm) bool {
	key = strings.ToLower(key)
	for _, e := range g.entries {
		if e.org == team && (e.key == "*" || e.key == key) && e.perms[perm] {
			return true
		}
	}
	return false
}

// Any reports whether the grants admit perm anywhere.
func (g Grants) Any(perm Perm) bool {
	for _, e := range g.entries {
		if e.perms[perm] {
			return true
		}
	}
	return false
}
