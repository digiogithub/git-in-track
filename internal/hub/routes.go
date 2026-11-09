package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/principal"
	"github.com/digiogithub/git-in-track/internal/server"
)

// This file is the HTTP half of the hub's authorization (GIT-US-0209): which
// /api/v1 routes a principal may reach at all, the filtering of the responses
// that do not come from the core, and the filtering of the event stream. The
// core calls themselves are authorized by the Guard.

// routeClass is what a route needs.
type routeClass int

const (
	routeUnknown  routeClass = iota // not classified: refused
	routeCore                       // reaches only guarded core calls
	routeFiltered                   // served from the mount registry: post-filtered
	routeAdmin                      // operator surface: needs admin somewhere
)

// adminFamilies are the operator surfaces: sync and git plumbing, the
// tracker connector, the agent proxy, the tunnel, the managed search and the
// server settings. None of them goes through the core.
var adminFamilies = []string{
	"/api/v1/sync/", "/api/v1/git/", "/api/v1/agent/", "/api/v1/youtrack/",
	"/api/v1/search/managed/", "/youtrack/",
}

// routes classifies every other /api/v1 route. A route that is neither here
// nor in adminFamilies is refused; TestRoutePolicyCoversEveryRoute keeps the
// table complete.
var routes = map[string]routeClass{
	"/api/v1/health": routeCore, "/api/v1/version": routeCore, "/api/v1/events": routeCore,
	"/api/v1/capabilities": routeFiltered, "/api/v1/workspace": routeCore,
	"/api/v1/workspaces": routeFiltered, "/api/v1/workspaces/{name}": routeFiltered,
	"/api/v1/repos": routeFiltered, "/api/v1/repos/{id}": routeFiltered,
	"/api/v1/repos/{id}/projects": routeAdmin, "/api/v1/repos/{id}/reindex": routeAdmin,
	"/api/v1/repos/{id}/team": routeAdmin,
	"/api/v1/search":          routeCore, "/api/v1/search/reindex": routeAdmin, "/api/v1/search/settings": routeAdmin,
	"/api/v1/mcp/settings": routeAdmin, "/api/v1/tunnel": routeAdmin,
	"/api/v1/refs": routeCore, "/api/v1/snapshots": routeCore, "/api/v1/validate": routeCore,
	"/api/v1/inbox":  routeCore,
	"/api/v1/items/": routeCore, "/api/v1/items/validate": routeCore, "/api/v1/items/{id}": routeCore,
	"/api/v1/items/{id}/comments": routeCore, "/api/v1/items/{id}/comments/tasks": routeCore,
	"/api/v1/items/{id}/links": routeCore, "/api/v1/items/{id}/links/*": routeCore,
	"/api/v1/items/{id}/move": routeCore, "/api/v1/items/{id}/references": routeCore,
	"/api/v1/items/{id}/tasks": routeCore, "/api/v1/items/{id}/triage": routeCore,
	"/api/v1/projects": routeCore, "/api/v1/projects/{key}": routeCore,
	"/api/v1/projects/{key}/inbox": routeCore, "/api/v1/projects/{key}/kpis": routeCore,
	"/api/v1/teams": routeCore, "/api/v1/teams/{key}": routeCore,
	"/api/v1/teams/{key}/projects": routeCore, "/api/v1/teams/{key}/projects/{project}": routeCore,
	"/api/v1/boards/": routeCore, "/api/v1/boards/{slug}": routeCore, "/api/v1/boards/{slug}/cards/move": routeCore,
	"/api/v1/sprints/": routeCore, "/api/v1/sprints/{id}": routeCore, "/api/v1/sprints/{id}/burndown": routeCore,
	"/api/v1/sprints/{id}/close": routeCore, "/api/v1/sprints/{id}/start": routeCore,
	"/api/v1/sprints/{id}/transfer": routeCore,
	"/api/v1/retros/":               routeCore, "/api/v1/retros/{id}": routeCore, "/api/v1/retros/{id}/actions/promote": routeCore,
}

// kbRoutes and specRoutes are the knowledge-base and spec routes, mounted
// under several prefixes; all of them reach the core only.
var (
	kbPrefixes = []string{"/api/v1/kb/", "/api/v1/projects/{key}/kb/", "/api/v1/teams/{key}/kb/"}
	kbLeaves   = map[string]bool{"asset": true, "feedback": true, "page": true, "tree": true}
)

// classify returns what a route needs. Methods do not change the class: a
// write on a core route is authorized by the Guard, and the filtered routes
// only list; their other methods are admin.
func classify(method, pattern string) routeClass {
	for _, family := range adminFamilies {
		if strings.Contains(pattern, family) {
			return routeAdmin
		}
	}
	for _, prefix := range kbPrefixes {
		if leaf, ok := strings.CutPrefix(pattern, prefix); ok && kbLeaves[leaf] {
			return routeCore
		}
	}
	if strings.HasPrefix(pattern, "/api/v1/projects/{key}/specs/") {
		return routeCore
	}
	class := routes[pattern]
	if class == routeFiltered && method != http.MethodGet && method != http.MethodHead {
		return routeAdmin
	}
	return class
}

// routeGuard is the server Middleware of the hub.
type routeGuard struct {
	routes func() chi.Routes
	grants GrantSource
	filter func(ctx context.Context, generic any) any
}

func (g *routeGuard) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := principal.From(r.Context())
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		rctx := chi.NewRouteContext()
		pattern := ""
		if g.routes().Match(rctx, r.Method, r.URL.Path) {
			pattern = rctx.RoutePattern()
		}
		grants := g.grants(p.Handle)
		switch classify(r.Method, pattern) {
		case routeCore:
			if !grants.Any(PermRead) && pattern != "/api/v1/health" && pattern != "/api/v1/version" {
				writeForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		case routeFiltered:
			if !grants.Any(PermRead) {
				writeForbidden(w)
				return
			}
			g.filtered(next, w, r)
		case routeAdmin:
			if !grants.Any(PermAdmin) {
				writeForbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		default:
			writeForbidden(w)
		}
	})
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"type":"about:blank","title":"Forbidden","status":403,"code":"forbidden"}`))
}

// filtered serves a route into a buffer and passes its JSON body through the
// scope filter before it reaches the client.
func (g *routeGuard) filtered(next http.Handler, w http.ResponseWriter, r *http.Request) {
	rec := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
	next.ServeHTTP(rec, r)
	body := rec.body.Bytes()
	if rec.status >= 200 && rec.status < 300 && strings.Contains(rec.header.Get("Content-Type"), "json") {
		var generic any
		if err := json.Unmarshal(body, &generic); err == nil {
			if out, err := json.Marshal(g.filter(r.Context(), generic)); err == nil {
				body = out
			}
		}
	}
	for k, v := range rec.header {
		w.Header()[k] = v
	}
	w.Header().Del("Content-Length")
	w.Header().Del("ETag")
	w.WriteHeader(rec.status)
	_, _ = w.Write(body)
}

// bufferedResponse is the minimal ResponseWriter filtered needs.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header         { return b.header }
func (b *bufferedResponse) WriteHeader(status int)      { b.status = status }
func (b *bufferedResponse) Write(p []byte) (int, error) { return b.body.Write(p) } //nolint:wrapcheck // an in-memory buffer

// scopeFilter is the response and event filter of a principal.
type scopeFilter struct {
	grants GrantSource
	// repoProjects maps a repository id to the projects and teams it holds.
	repoProjects func(repo string) []scope
}

// readable builds the predicate of a principal.
func (f *scopeFilter) readable(ctx context.Context) (func(key string, team bool) bool, bool) {
	p, ok := principal.From(ctx)
	if !ok {
		return nil, false
	}
	grants := f.grants(p.Handle)
	return func(key string, team bool) bool { return grants.Allows(key, team, PermRead) }, true
}

// response filters a decoded JSON body.
func (f *scopeFilter) response(ctx context.Context, generic any) any {
	readable, ok := f.readable(ctx)
	if !ok {
		return nil
	}
	out, _ := walk(generic, readable)
	return out
}

// event decides whether one event reaches a stream: only when everything it
// names is readable, and something is named at all. An event that names no
// project, item or repository — a stream control frame — passes when it
// carries no data.
func (f *scopeFilter) event(ctx context.Context, ev server.Event) bool {
	readable, ok := f.readable(ctx)
	if !ok {
		return false
	}
	if ev.Data == nil {
		return true
	}
	raw, err := json.Marshal(ev.Data)
	if err != nil {
		return false
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return false
	}
	named := false
	allowed := true
	var visit func(v any)
	visit = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for _, field := range []string{"repo", "vaultId", "repoId"} {
				if repo, isString := t[field].(string); isString && repo != "" {
					scopes := f.repoProjects(repo)
					named = true
					if len(scopes) == 0 {
						allowed = false
					}
					for _, s := range scopes {
						if !readable(s.key, s.team) {
							allowed = false
						}
					}
				}
			}
			if !objectInScope(t, readable) {
				allowed = false
			}
			for _, field := range []string{"id", "itemId", "item", "target", "project", "projectKey"} {
				if _, has := t[field].(string); has {
					named = true
				}
			}
			for _, e := range t {
				visit(e)
			}
		case []any:
			for _, e := range t {
				if s, isString := e.(string); isString {
					if key, err := keyOfRef(s); err == nil {
						named = true
						if !readable(key, false) {
							allowed = false
						}
					}
				}
				visit(e)
			}
		}
	}
	visit(generic)
	return named && allowed
}
