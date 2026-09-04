package server

import (
	"net/http"
	"reflect"

	"github.com/go-chi/chi/v5"
)

// handleRepos serves GET /api/v1/repos.
func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	role := r.URL.Query().Get("role")
	repos := make([]map[string]any, 0, len(s.repos.all()))
	for _, m := range s.repos.all() {
		if role != "" && m.role != role {
			continue
		}
		repos = append(repos, m.info())
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"repos": repos, "total": len(repos)})
}

// handleRepo serves GET /api/v1/repos/{id}.
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, ok := s.repos.lookup(id)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+id+".")
		return
	}
	writeJSON(w, r, http.StatusOK, m.info())
}

// handleReindex serves POST /api/v1/repos/{id}/reindex. The pass is always
// full: an incremental one is what the watcher does on every batch.
func (s *Server) handleReindex(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, ok := s.repos.lookup(id)
	if !ok {
		failProblem(w, r, codeRepoNotRegistered, "No repository is registered as "+id+".")
		return
	}
	if !m.ready() {
		failProblem(w, r, codeIndexUnavailable, "Repository "+id+" cannot be indexed: "+m.err.Error())
		return
	}
	stats, err := m.reindex(r.Context(), s.now)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	s.publishIndexUpdated(m, indexCounts{Full: true}, requestIDOf(r))
	writeJSON(w, r, http.StatusOK, stats)
}

// handleProjects serves GET /api/v1/projects: the merged view of every mounted
// repository. Each entry is exactly what the shared core reports, so a project
// looks the same in browser-only mode and here.
func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	projects := make([]any, 0, len(s.repos.all()))
	for _, m := range s.repos.ready() {
		list, err := s.dispatch(r, m, "project.list", struct{}{})
		if err != nil {
			writeVaultError(w, r, err)
			return
		}
		projects = append(projects, flatten(list)...)
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"projects": projects, "total": len(projects)})
}

// handleProject serves GET /api/v1/projects/{key}.
func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	m, found := s.repos.forProject(key)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository exposes project "+key+".")
		return
	}
	list, ok := s.call(w, r, m, "project.list", struct{}{})
	if !ok {
		return
	}
	for _, entry := range flatten(list) {
		if stringField(entry, "Key") == key {
			writeJSON(w, r, http.StatusOK, entry)
			return
		}
	}
	failProblem(w, r, codeNotFound, "Project "+key+" is not indexed.")
}

// handleWorkspaces serves GET /api/v1/workspaces. The companion serves one
// workspace per process: the one `gintrack serve` was started for.
func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]any{
		"workspaces":      []map[string]any{s.workspaceInfo()},
		"activeWorkspace": s.opts.Workspace,
	})
}

// handleWorkspace serves GET /api/v1/workspaces/{name}.
func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name != s.opts.Workspace {
		failProblem(w, r, codeNotFound, "This companion serves workspace "+s.opts.Workspace+" only.")
		return
	}
	writeJSON(w, r, http.StatusOK, s.workspaceInfo())
}

// workspaceInfo renders the served workspace.
func (s *Server) workspaceInfo() map[string]any {
	ids := make([]string, 0, len(s.repos.all()))
	for _, m := range s.repos.all() {
		ids = append(ids, m.id)
	}
	return map[string]any{"name": s.opts.Workspace, "repos": ids, "active": true}
}

// flatten turns a slice the core returned into a slice of its elements, each
// still carrying its original type so that marshaling it produces the same
// bytes as the single-repository answer.
func flatten(value any) []any {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		if value == nil {
			return nil
		}
		return []any{value}
	}
	out := make([]any, 0, v.Len())
	for i := range v.Len() {
		out = append(out, v.Index(i).Interface())
	}
	return out
}

// stringField reads a named string field of a struct the core returned.
func stringField(value any, name string) string {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	field := v.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}
