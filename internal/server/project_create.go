package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// createProjectBody is the request of POST /api/v1/repos/{id}/projects
// (docs/07 section 5.4).
type createProjectBody struct {
	// DocsFolder is the documentation folder, relative to the repository root.
	// An empty value and "." both mean the repository root.
	DocsFolder string `json:"docsFolder,omitempty"`
	// Key is the ID prefix, matching [A-Z][A-Z0-9]{1,9}.
	Key string `json:"key"`
	// Name is the human name; it defaults to the key.
	Name string `json:"name,omitempty"`
	// Description is one optional paragraph shown in project pickers.
	Description string `json:"description,omitempty"`
	// Timezone is an IANA name; it defaults to UTC.
	Timezone string `json:"timezone,omitempty"`
}

// handleCreateProject serves POST /api/v1/repos/{id}/projects: it scaffolds a
// backlog in a registered repository that has none yet.
//
// It is the companion half of the "create a project" flow the browser runs
// through "project.create": both go through the same vault method, so the file
// a companion writes and the file a browser writes are the same bytes.
//
// The registration is not rewritten here — the configuration file belongs to
// the CLI (`gintrack add`, `gintrack init`) — but the vault declares the folder
// for the rest of the process's life, so the new project stays discoverable
// even when it sits deeper than the bounded discovery rule reaches.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, ok := s.repos.lookup(id)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+id+".")
		return
	}
	var body createProjectBody
	if !decodeBody(w, r, &body) {
		return
	}
	result, ok := s.call(w, r, m, "project.create", map[string]any{
		"docsFolder":  body.DocsFolder,
		"key":         body.Key,
		"name":        body.Name,
		"description": body.Description,
		"timezone":    body.Timezone,
	})
	if !ok {
		return
	}
	m.touch(s.now())
	s.publishIndexUpdated(m, indexCounts{Full: true}, requestIDOf(r))
	writeJSON(w, r, http.StatusCreated, result)
}
