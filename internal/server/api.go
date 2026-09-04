package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// maxRequestBody bounds a JSON request body. Markdown bodies are the largest
// thing the API accepts and a megabyte is far beyond any real one.
const maxRequestBody = 1 << 20

// wildcardRev is the If-Match value that skips the optimistic lock. RFC 9110
// allows it; docs/07 section 5.3 documents it as unsafe.
const wildcardRev = "*"

// mountAPI composes the versioned REST surface. Everything but the health probe
// sits behind the bearer token.
func (s *Server) mountAPI(api chi.Router) {
	api.Get("/health", s.handleHealth)

	api.Group(func(p chi.Router) {
		p.Use(s.bearerAuth)

		p.Get("/capabilities", s.handleCapabilities)
		p.Get("/events", s.handleEvents)

		// Workspaces and repositories.
		p.Get("/workspace", s.handleWorkspaceTree)
		p.Get("/workspaces", s.handleWorkspaces)
		p.Get("/workspaces/{name}", s.handleWorkspace)
		p.Post("/workspaces", s.notImplemented("Creating a workspace is a configuration change; use `gintrack config`."))
		p.Get("/repos", s.handleRepos)
		p.Post("/repos", s.notImplemented("Registering a repository is a configuration change; use `gintrack add`."))
		p.Get("/repos/{id}", s.handleRepo)
		p.Delete("/repos/{id}", s.notImplemented("Unregistering a repository is a configuration change; use `gintrack rm`."))
		p.Post("/repos/{id}/reindex", s.handleReindex)

		// Projects and their knowledge base.
		p.Get("/projects", s.handleProjects)
		p.Get("/projects/{key}", s.handleProject)
		p.Patch("/projects/{key}", s.notImplemented("Editing project.yaml over the API arrives with Phase 3."))
		p.Route("/projects/{key}/kb", s.mountKB)

		// Team repositories: team.yaml, its members and its project list.
		p.Get("/teams", s.handleTeams)
		p.Get("/teams/{key}", s.handleTeam)
		p.Route("/teams/{key}/kb", s.mountKB)
		p.Get("/refs", s.handleResolveRef)
		// The flat form addresses the knowledge base by vault path, with an
		// optional ?project= to disambiguate a multi-project repository.
		p.Route("/kb", s.mountKB)

		// Items.
		p.Route("/items", s.mountItems)

		p.Get("/search", s.handleSearch)
		p.Post("/validate", s.handleValidate)

		// Phases 3 and 4. The routes exist so that a client learns "not yet"
		// from the problem code instead of guessing from a 404.
		s.deferRoute(p, "/boards", "Boards arrive with Phase 3.")
		s.deferRoute(p, "/sprints", "Sprints arrive with Phase 3.")
		s.deferRoute(p, "/retros", "Retrospectives arrive with Phase 3.")
		s.deferRoute(p, "/sync", "Git synchronization arrives with Phase 4.")
		s.deferRoute(p, "/git", "Git inspection arrives with Phase 4.")
	})

	api.NotFound(s.handleAPINotFound)
	api.MethodNotAllowed(s.handleAPINotFound)
}

// deferRoute mounts a whole subtree that answers 501 with a stable code.
func (s *Server) deferRoute(r chi.Router, pattern, detail string) {
	h := s.notImplemented(detail)
	r.Handle(pattern, h)
	r.Handle(pattern+"/*", h)
}

// notImplemented answers with the documented problem for a route this phase
// does not serve yet.
func (s *Server) notImplemented(detail string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		failProblem(w, r, codeNotImplemented, detail)
	}
}

// call runs one method of the shared core against a mount and writes the
// problem document itself when it fails. It reports whether the call succeeded.
//
// The result is the value the vault returned: the API serves the very bytes the
// browser build serves, so that one contract cannot drift into two.
func (s *Server) call(w http.ResponseWriter, r *http.Request, m *mount, method string, params any) (any, bool) {
	if m == nil {
		failProblem(w, r, codeRepoNotRegistered,
			"No mounted repository serves this request. Register one with `gintrack add <path>` or pass --repo.")
		return nil, false
	}
	if !m.ready() {
		failProblem(w, r, codeIndexUnavailable,
			fmt.Sprintf("Repository %s is not indexed: %v", m.id, m.err))
		return nil, false
	}
	raw, err := json.Marshal(params)
	if err != nil {
		failProblem(w, r, codeInvalidRequest, fmt.Sprintf("The request could not be encoded: %v", err))
		return nil, false
	}
	result, err := m.vlt.Dispatch(r.Context(), method, raw)
	if err != nil {
		writeVaultError(w, r, err)
		return nil, false
	}
	return result, true
}

// decodeBody reads a JSON request body into dst, answering with a problem when
// it is malformed. It reports whether decoding succeeded.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err := dec.Decode(dst); err != nil {
		failProblem(w, r, codeInvalidRequest, fmt.Sprintf("The request body is not valid JSON: %v", err))
		return false
	}
	return true
}

// ifMatch reads the optimistic-lock header. It returns the revision to pass to
// the core, whether the header was present at all, and whether it was the
// wildcard that deliberately skips the check.
func ifMatch(r *http.Request) (rev string, present, wildcard bool) {
	raw := strings.TrimSpace(r.Header.Get("If-Match"))
	if raw == "" {
		return "", false, false
	}
	if raw == wildcardRev {
		return "", true, true
	}
	// Accept both the bare revision the UI sends and the quoted entity tag form
	// a strict HTTP client produces.
	raw = strings.TrimPrefix(raw, "W/")
	raw = strings.Trim(raw, `"`)
	return raw, true, false
}

// requireIfMatch enforces the strict precondition of docs/07 section 5.3: a
// write against something that already exists must carry the revision it read.
func requireIfMatch(w http.ResponseWriter, r *http.Request) (string, bool) {
	rev, present, wildcard := ifMatch(r)
	if !present {
		failProblem(w, r, codePreconditionRequired,
			"This write needs an If-Match header carrying the revision you read. Send If-Match: * to overwrite unconditionally.")
		return "", false
	}
	if wildcard {
		return "", true
	}
	return rev, true
}

// writeEntity writes a JSON entity together with its ETag when it carries a
// revision. The payload is whatever the vault returned, never a re-marshaled
// copy of it.
func writeEntity(w http.ResponseWriter, r *http.Request, status int, payload any, rev string) {
	if rev != "" {
		w.Header().Set("ETag", `"`+rev+`"`)
	}
	writeJSON(w, r, status, payload)
}
