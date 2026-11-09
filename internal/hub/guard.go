package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path"
	"reflect"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/mcp"
	"github.com/digiogithub/git-in-track/internal/principal"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// This file is the one authorization point of gintrack-hub (ADR-043 §4,
// GIT-US-0209): a Dispatcher that admits a core call only when the
// principal of the request holds the permission it needs in every scope the
// call touches, and that removes from what a read returns anything outside
// the principal's scopes.
//
// The rules, decided with the supervisor:
//   - deny by default: a method missing from the policy, a call without a
//     principal, or a call whose scope cannot be resolved is refused. The
//     guard never lets a call reach the vault's route fallback (mounts[0]);
//   - a read out of scope answers not_found, so a guessable id does not
//     reveal that it exists; a write out of scope answers forbidden. The real
//     reason goes to the hub's log;
//   - fan-out reads get the reachable projects injected and are post-filtered;
//   - author fields are forced to the principal.

// scopeKind is how a method's scope is found in its params.
type scopeKind int

const (
	scopeNone    scopeKind = iota // touches no repository data
	scopeProject                  // "project" (and "projects")
	scopeItem                     // an id, itemId, spec or ref
	scopePath                     // a KB path, inside a mount
	scopeTeam                     // a team, plus the items a call names
	scopeFanOut                   // every reachable project; params rewritten
	scopeDenied                   // never available through the hub
)

// rule is the policy of one method.
type rule struct {
	perm  Perm
	scope scopeKind
	// forceAuthor names the params that are overwritten with the principal.
	forceAuthor []string
	// refuse names params a caller may not set at all.
	refuse []string
}

func read(scope scopeKind) rule         { return rule{perm: PermRead, scope: scope} }
func write(scope scopeKind) rule        { return rule{perm: PermWrite, scope: scope} }
func denied() rule                      { return rule{perm: PermAdmin, scope: scopeDenied} }
func authored(r rule, f ...string) rule { r.forceAuthor = f; return r }
func refusing(r rule, f ...string) rule { r.refuse = f; return r }
func withPerm(r rule, p Perm) rule      { r.perm = p; return r }

// policy is the permission and scope of every method of the CoreApi. A
// method that is not here is refused; TestGuardPolicyCoversEveryMethod keeps
// it complete.
var policy = map[string]rule{
	"ping": {perm: PermRead, scope: scopeNone}, "version": {perm: PermRead, scope: scopeNone},
	"item.parse": {perm: PermRead, scope: scopeNone}, "item.serialize": {perm: PermRead, scope: scopeNone},
	"item.validate": {perm: PermRead, scope: scopeNone},

	"workspace.list": read(scopeFanOut), "project.list": read(scopeFanOut),
	"workspace.mount": denied(), "workspace.unmount": denied(),
	"vault.load": denied(), "vault.apply": denied(), "vault.stats": denied(),
	"project.create": denied(), "project.inbox.enable": denied(), "team.create": denied(),
	"team.project.add": denied(), "team.project.remove": denied(),
	"snapshot.load": denied(), "snapshot.refresh": denied(),
	"youtrack.import.preview": denied(), "youtrack.import.run": denied(), "youtrack.comment.push": denied(),
	"youtrack.kb.status": denied(), "youtrack.kb.publish": denied(), "youtrack.kb.pull": denied(),

	"project.kpis": read(scopeProject),
	"team.get":     read(scopeTeam), "team.list": read(scopeFanOut),

	"item.list": read(scopeFanOut), "item.get": read(scopeItem),
	"item.children": read(scopeItem), "item.references": read(scopeItem),
	"item.create":   authored(write(scopeProject), "author"),
	"item.update":   refusing(write(scopeItem), "patch.author"),
	"item.task.set": write(scopeItem), "item.move": write(scopeItem),
	"item.delete": withPerm(write(scopeItem), PermDelete),

	"inbox.list": read(scopeFanOut), "inbox.triage": write(scopeItem),

	"comment.list":     read(scopeItem),
	"comment.add":      authored(withPerm(write(scopeItem), PermPropose), "author"),
	"comment.update":   write(scopeItem),
	"comment.task.set": write(scopeItem),

	"kb.tree": read(scopeFanOut), "kb.page": read(scopePath),
	"kb.write":        write(scopePath),
	"kb.feedback.add": authored(withPerm(write(scopePath), PermPropose), "author", "authorName", "authorEmail"),
	"conflict.merge":  write(scopePath),

	"search": read(scopeFanOut), "search.semantic": read(scopeFanOut), "ref.resolve": read(scopeItem),

	"board.list": read(scopeTeam), "board.get": read(scopeTeam),
	"board.create": write(scopeTeam), "board.update": write(scopeTeam), "board.delete": withPerm(write(scopeTeam), PermDelete),
	"board.move": write(scopeTeam),

	"sprint.list": read(scopeTeam), "sprint.get": read(scopeTeam), "sprint.metrics": read(scopeTeam),
	"sprint.create": authored(write(scopeTeam), "author"), "sprint.update": write(scopeTeam),
	"sprint.start": write(scopeTeam), "sprint.close": write(scopeTeam), "sprint.transfer": write(scopeTeam),

	"retro.list": read(scopeTeam), "retro.get": read(scopeTeam),
	"retro.create": authored(write(scopeTeam), "author"), "retro.update": write(scopeTeam),
	"retro.promote": write(scopeTeam),

	"snapshot.list": read(scopeTeam), "snapshot.export": read(scopeTeam),

	"requirement.list": read(scopeItem), "requirement.get": read(scopeItem),
	"requirement.create": write(scopeItem), "requirement.update": write(scopeItem),
	"requirement.stamp": write(scopeItem),
	"spec.context":      read(scopeItem), "spec.lint": read(scopeItem), "spec.delta.preview": read(scopeItem),
	"spec.templates": read(scopeProject),
	"coverage.list":  read(scopeFanOut), "trace.requirement": read(scopeItem), "trace.touching": read(scopeFanOut),
	"impact.query": read(scopeFanOut), "impact.report": read(scopeFanOut),

	"memory.get": read(scopeProject), "memory.list": read(scopeProject), "memory.changes": read(scopeProject),
	"memory.put": authored(withPerm(write(scopeProject), PermPropose), "items[].author"),
}

// GrantSource gives the grants of a principal.
type GrantSource func(handle string) Grants

// Guard is the authorizing Dispatcher.
type Guard struct {
	next   mcp.Dispatcher
	space  func() *vault.Workspace
	mount  string // the repository a per-mount guard serves; "" for the workspace
	grants GrantSource
	log    *slog.Logger
}

// NewGuard wraps next. mount is the repository id when next is one mount's
// vault, and "" when it is the workspace.
// space is read lazily: the server builds its MCP surface before New returns.
func NewGuard(next mcp.Dispatcher, space func() *vault.Workspace, mount string, grants GrantSource, log *slog.Logger) *Guard {
	if log == nil {
		log = slog.Default()
	}
	return &Guard{next: next, space: space, mount: mount, grants: grants, log: log}
}

// Dispatch authorizes, rewrites, forwards and filters one call.
func (g *Guard) Dispatch(ctx context.Context, method string, raw []byte) (any, error) {
	p, ok := principal.From(ctx)
	if !ok {
		return nil, g.deny(method, "", PermRead, "no principal on the call")
	}
	r, known := policy[method]
	if !known {
		return nil, g.deny(method, p.Handle, PermAdmin, "the method has no policy")
	}
	grants := g.grants(p.Handle)
	if r.scope == scopeDenied {
		return nil, g.deny(method, p.Handle, PermAdmin, "not available through the hub")
	}
	params := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, vaultError("invalid_request", "the params must be a JSON object")
		}
	}
	for _, f := range r.refuse {
		if hasPath(params, f) {
			return nil, g.deny(method, p.Handle, r.perm, f+" may not be set through the hub")
		}
	}

	// A principal with no read anywhere is anonymous in effect: even the pure
	// functions are refused (condition d).
	if !grants.Any(PermRead) {
		return nil, g.deny(method, p.Handle, r.perm, "the principal holds no grant")
	}

	need := r.perm
	if method == "memory.put" && !grants.Allows(projectParam(params), false, PermWrite) {
		need = PermPropose
	}
	switch r.scope {
	case scopeNone:
	case scopeFanOut:
		reach := g.reachable(grants, PermRead)
		if err := g.narrow(method, p.Handle, params, reach); err != nil {
			return nil, err
		}
	default:
		scopes, err := g.resolve(r.scope, params)
		if err != nil || len(scopes) == 0 {
			reason := "the scope of the call cannot be resolved"
			if err != nil {
				reason = err.Error()
			}
			return nil, g.deny(method, p.Handle, need, reason)
		}
		for _, s := range scopes {
			if !grants.Allows(s.key, s.team, PermRead) || !grants.Allows(s.key, s.team, need) {
				return nil, g.deny(method, p.Handle, need, "no "+string(need)+" in "+s.String())
			}
		}
	}

	if method == "memory.put" && need == PermPropose {
		forceProposals(params)
	}
	if method == "memory.put" {
		for _, it := range itemsOf(params) {
			if op, _ := it["op"].(string); op == "forget" && !grants.Allows(projectParam(params), false, PermDelete) {
				return nil, g.deny(method, p.Handle, PermDelete, "forgetting a memory needs delete")
			}
		}
	}
	for _, f := range r.forceAuthor {
		setPath(params, f, p)
	}
	rewritten, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("re-encode %s params: %w", method, err)
	}

	result, err := g.next.Dispatch(ctx, method, rewritten)
	if err != nil || r.perm != PermRead {
		return result, err //nolint:wrapcheck // the core's coded error goes through untouched
	}
	return redactResult(result, func(key string, team bool) bool { return grants.Allows(key, team, PermRead) }), nil
}

// deny logs the real reason and answers with the code the rules give: a read
// is not_found, anything else forbidden.
func (g *Guard) deny(method, handle string, perm Perm, reason string) error {
	g.log.Info("call refused", "method", method, "principal", handle, "need", string(perm), "reason", reason)
	if perm == PermRead {
		return vaultError("not_found", "not found")
	}
	return vaultError("forbidden", "not permitted in this scope")
}

func vaultError(code, msg string) error { return &vault.Error{Code: code, Message: msg} }

// scope is one project or team a call touches.
type scope struct {
	key  string
	team bool
}

func (s scope) String() string {
	if s.team {
		return "org/" + strings.ToLower(s.key)
	}
	return "project/" + strings.ToLower(s.key)
}

// refs is every project and team the guarded repositories hold.
func (g *Guard) refs() []core.ProjectRef {
	var out []core.ProjectRef
	for _, m := range g.space().Mounts() {
		if g.mount != "" && m.ID != g.mount {
			continue
		}
		out = append(out, m.Vault.Projects()...)
	}
	return out
}

// reachable lists the project keys the grants admit perm on.
func (g *Guard) reachable(grants Grants, perm Perm) []string {
	var out []string
	for _, ref := range g.refs() {
		if !ref.Team && grants.Allows(string(ref.Key), false, perm) {
			out = append(out, string(ref.Key))
		}
	}
	return out
}

// narrow rewrites a fan-out read to the reachable projects. A caller that
// names projects gets the ones it may read; naming one it may not read is a
// not_found.
func (g *Guard) narrow(method, handle string, params map[string]any, reach []string) error {
	asked := append(stringsOf(params["projects"]), stringsOf(params["project"])...)
	allowed := map[string]bool{}
	for _, k := range reach {
		allowed[k] = true
	}
	for _, k := range asked {
		if !allowed[k] {
			return g.deny(method, handle, PermRead, "project "+k+" is out of scope")
		}
	}
	if singleProject[method] {
		// These take one project. Without it the call would be routed to the
		// first repository: inject the only reachable project, or refuse.
		switch {
		case len(asked) == 1:
			params["project"] = asked[0]
			delete(params, "projects")
			return nil
		case len(asked) == 0 && len(reach) == 1:
			params["project"] = reach[0]
			return nil
		default:
			return g.deny(method, handle, PermRead, "the call must name one project")
		}
	}
	if len(asked) > 0 {
		return nil
	}
	if len(reach) == 0 {
		// Nothing reachable: an empty list would mean "every project".
		reach = []string{"\x00none"}
	}
	params["projects"] = reach
	return nil
}

// singleProject are the fan-out reads whose params name one project only.
var singleProject = map[string]bool{
	"item.list": true, "inbox.list": true, "kb.tree": true,
	"coverage.list": true, "trace.touching": true, "impact.query": true, "impact.report": true,
}

// resolve finds the scopes of a call. It never guesses: what it cannot place
// is an error, which the caller turns into a refusal.
func (g *Guard) resolve(kind scopeKind, params map[string]any) ([]scope, error) {
	var out []scope
	add := func(s scope) {
		for _, have := range out {
			if have == s {
				return
			}
		}
		out = append(out, s)
	}
	switch kind {
	case scopeProject:
		key := projectParam(params)
		if key == "" {
			return nil, fmt.Errorf("the call names no project")
		}
		add(scope{key: key, team: g.isTeam(key)})
	case scopeItem:
		for _, f := range []string{"id", "itemId", "spec", "ref", "target"} {
			s, _ := params[f].(string)
			if s == "" {
				continue
			}
			key, err := keyOfRef(s)
			if err != nil {
				return nil, err
			}
			add(scope{key: key, team: g.isTeam(key)})
		}
		if len(out) == 0 {
			if key := projectParam(params); key != "" {
				add(scope{key: key})
			}
		}
	case scopePath:
		p, _ := params["path"].(string)
		if p == "" {
			if key := projectParam(params); key != "" {
				add(scope{key: key})
				break
			}
			return nil, fmt.Errorf("the call names no path")
		}
		ref, ok := g.ownerOf(stringParam(params, "vaultId"), p)
		if !ok {
			return nil, fmt.Errorf("path %q belongs to no project", p)
		}
		add(scope{key: string(ref.Key), team: ref.Team})
	case scopeTeam:
		team := stringParam(params, "team")
		if team == "" {
			teams := g.teams()
			if len(teams) != 1 {
				return nil, fmt.Errorf("the call names no team")
			}
			team = teams[0]
		}
		add(scope{key: team, team: true})
		for _, f := range []string{"id", "itemId", "item"} {
			if s, _ := params[f].(string); s != "" {
				if key, err := keyOfRef(s); err == nil && !g.isTeam(key) {
					add(scope{key: key})
				}
			}
		}
	}
	return out, nil
}

// projectParam is the "project" param.
func projectParam(params map[string]any) string { return stringParam(params, "project") }

func stringParam(params map[string]any, name string) string {
	s, _ := params[name].(string)
	return strings.TrimSpace(s)
}

// keyOfRef returns the project key of an item id, a requirement ref or a
// "<KEY>/<ID>" reference.
func keyOfRef(s string) (string, error) {
	bare := s
	if before, after, found := strings.Cut(s, "/"); found && core.ValidProjectKey(core.ProjectKey(before)) {
		bare = after
	}
	if req, err := core.ParseRequirementRef(bare); err == nil {
		bare = string(req.Spec)
	}
	key, _, _, err := core.ParseItemID(bare)
	if err != nil {
		return "", fmt.Errorf("%q is not an item reference", s)
	}
	return string(key), nil
}

// isTeam reports whether key is a team of the guarded repositories.
func (g *Guard) isTeam(key string) bool {
	for _, t := range g.teams() {
		if t == key {
			return true
		}
	}
	return false
}

func (g *Guard) teams() []string {
	var out []string
	for _, ref := range g.refs() {
		if ref.Team {
			out = append(out, string(ref.Key))
		}
	}
	return out
}

// ownerOf finds the project or team whose documentation folder holds a path,
// the longest folder winning, in one mount or in the guard's own.
func (g *Guard) ownerOf(vaultID, p string) (core.ProjectRef, bool) {
	clean := path.Clean(p)
	var best core.ProjectRef
	found := false
	for _, m := range g.space().Mounts() {
		if (g.mount != "" && m.ID != g.mount) || (vaultID != "" && m.ID != vaultID) {
			continue
		}
		if g.mount == "" && vaultID == "" && len(g.space().Mounts()) > 1 {
			// A path without its repository is ambiguous at the workspace.
			return core.ProjectRef{}, false
		}
		for _, ref := range m.Vault.Projects() {
			if ref.DocsPath == "." || clean == ref.DocsPath || strings.HasPrefix(clean, ref.DocsPath+"/") {
				if !found || len(ref.DocsPath) > len(best.DocsPath) {
					best, found = ref, true
				}
			}
		}
	}
	return best, found
}

// forceProposals makes every upsert of a memory.put a proposal and drops its
// supersedes: replacing an accepted memory is a write.
func forceProposals(params map[string]any) {
	for _, it := range itemsOf(params) {
		if op, _ := it["op"].(string); op == "forget" {
			continue
		}
		it["status"] = "proposed"
		delete(it, "supersedes")
	}
}

func itemsOf(params map[string]any) []map[string]any {
	list, _ := params["items"].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, v := range list {
		if m, ok := v.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringsOf(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// hasPath reports whether a dotted param path is set, e.g. "patch.author".
func hasPath(params map[string]any, dotted string) bool {
	parts := strings.Split(dotted, ".")
	var cur any = params
	for _, part := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = m[part]; !ok {
			return false
		}
	}
	return cur != nil
}

// setPath overwrites an author param with the principal: a handle, or for
// authorName and authorEmail the name and email. "items[].author" sets the
// author of every entry of a list.
func setPath(params map[string]any, dotted string, p principal.Principal) {
	value := p.Handle
	switch {
	case strings.HasSuffix(dotted, "authorName"):
		value = p.Name
	case strings.HasSuffix(dotted, "authorEmail"):
		value = p.Email
	}
	if list, field, ok := strings.Cut(dotted, "[]."); ok {
		for _, it := range itemsOf(map[string]any{"items": params[list]}) {
			it[field] = value
		}
		return
	}
	params[dotted] = value
}

// ScopeSearch narrows GET /search to the projects the request may read: the
// ones asked for, all of them readable, or every reachable project. It is
// what makes the semantic half filter in its backend rather than after it
// (condition e).
func (g *Guard) ScopeSearch(ctx context.Context, asked []string) ([]string, error) {
	p, ok := principal.From(ctx)
	if !ok {
		return nil, g.deny("search", "", PermRead, "no principal on the call")
	}
	params := map[string]any{"projects": toAny(asked)}
	if err := g.narrow("search", p.Handle, params, g.reachable(g.grants(p.Handle), PermRead)); err != nil {
		return nil, err
	}
	return stringsOf(params["projects"]), nil
}

func toAny(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

// redactResult removes from a read's result every object and id that belongs to a
// project or team out of scope. A value it does not change is returned as
// is; a changed one is rebuilt in its own Go type, so that the server's
// handlers keep reading it as before.
func redactResult(result any, readable func(key string, team bool) bool) any {
	if m, ok := result.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = redactResult(v, readable)
		}
		return out
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return result
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return result
	}
	filtered, changed := walk(generic, readable)
	if !changed {
		return result
	}
	again, err := json.Marshal(filtered)
	if err != nil {
		return nil
	}
	t := reflect.TypeOf(result)
	if t == nil {
		return filtered
	}
	if t.Kind() == reflect.Pointer {
		ptr := reflect.New(t.Elem())
		if err := json.Unmarshal(again, ptr.Interface()); err != nil {
			return filtered
		}
		return ptr.Interface()
	}
	ptr := reflect.New(t)
	if err := json.Unmarshal(again, ptr.Interface()); err != nil {
		return filtered
	}
	return ptr.Elem().Interface()
}

// walk filters a decoded JSON value. An object out of scope is dropped from
// its array, or nulled in its parent; an item id out of scope is dropped from
// a string array.
func walk(v any, readable func(string, bool) bool) (any, bool) {
	switch t := v.(type) {
	case map[string]any:
		if !objectInScope(t, readable) {
			return nil, true
		}
		changed := false
		for k, e := range t {
			if k == "projects" {
				if keys, ok := e.([]any); ok {
					kept := make([]any, 0, len(keys))
					for _, key := range keys {
						if s, isString := key.(string); isString && core.ValidProjectKey(core.ProjectKey(s)) && !readable(s, false) {
							changed = true
							continue
						}
						kept = append(kept, key)
					}
					if len(keys) > 0 && len(kept) == 0 {
						// Everything it summarized is out of scope: a repository
						// or a team none of whose projects is readable.
						return nil, true
					}
					t[k] = kept
				}
			}
			nv, c := walk(t[k], readable)
			if c {
				changed = true
				t[k] = nv
			}
		}
		return t, changed
	case []any:
		out := make([]any, 0, len(t))
		changed := false
		for _, e := range t {
			if s, ok := e.(string); ok {
				if key, err := keyOfRef(s); err == nil && !readable(key, false) {
					changed = true
					continue
				}
			}
			nv, c := walk(e, readable)
			if c {
				changed = true
				if nv == nil {
					continue
				}
			}
			out = append(out, nv)
		}
		return out, changed
	default:
		return v, false
	}
}

// objectInScope reads the fields that place an object: an item id, a
// reference target, a project key or a team key.
func objectInScope(m map[string]any, readable func(string, bool) bool) bool {
	for _, f := range []string{"id", "target", "itemId", "item"} {
		if s, ok := m[f].(string); ok {
			if key, err := keyOfRef(s); err == nil && !readable(key, false) {
				return false
			}
		}
	}
	keys := []string{"project", "projectKey"}
	if _, external := m["system"]; !external {
		// "key" places a project summary; on an external reference it is the
		// tracker's own project key, and a memory key is never a project key.
		keys = append(keys, "key")
	}
	for _, f := range keys {
		if s, ok := m[f].(string); ok && core.ValidProjectKey(core.ProjectKey(s)) && !readable(s, false) {
			return false
		}
	}
	if s, ok := m["teamKey"].(string); ok && s != "" && !readable(s, true) {
		return false
	}
	return true
}
