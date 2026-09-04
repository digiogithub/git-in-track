package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/vault"
)

// mountKB registers the knowledge-base routes. The same handlers serve the
// project-scoped form (/projects/{key}/kb/…), the team-scoped one
// (/teams/{key}/kb/…) and the flat one (/kb/…?project=KEY).
func (s *Server) mountKB(r chi.Router) {
	r.Get("/tree", s.handleKBTree)
	r.Get("/page", s.handleKBPage)
	r.Put("/page", s.handleKBWrite)
	r.Get("/asset", s.notImplemented("Serving knowledge-base assets arrives with Phase 3."))
}

// kbScope resolves the repository and the project a knowledge-base request is
// about. The key comes from the path for the scoped routes and from ?project=
// for the flat one.
func (s *Server) kbScope(w http.ResponseWriter, r *http.Request) (*mount, string, bool) {
	key := chi.URLParam(r, "key")
	if key == "" {
		key = r.URL.Query().Get("project")
	}
	if key == "" {
		m, ok := s.mountForFilter(w, r, "")
		return m, "", ok
	}
	if m, found := s.repos.forProject(key); found {
		return m, key, true
	}
	// A team space is addressed by its repository id, not by a project key.
	if m, found := s.repos.lookup(key); found {
		return m, "", true
	}
	failProblem(w, r, codeNotFound, "No mounted repository exposes "+key+".")
	return nil, "", false
}

// handleKBTree serves GET …/kb/tree.
func (s *Server) handleKBTree(w http.ResponseWriter, r *http.Request) {
	m, project, ok := s.kbScope(w, r)
	if !ok {
		return
	}
	tree, ok := s.call(w, r, m, "kb.tree", map[string]any{"project": project})
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, tree)
}

// handleKBPage serves GET …/kb/page?path=…
//
// `format` is accepted and ignored: this build always answers with the raw
// Markdown, which is both the documented default and what the web app renders
// itself. Server-side goldmark rendering arrives with Phase 3.
func (s *Server) handleKBPage(w http.ResponseWriter, r *http.Request) {
	m, project, ok := s.kbScope(w, r)
	if !ok {
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		failProblem(w, r, codeInvalidRequest, "A page request needs ?path=<page>.")
		return
	}
	page, err := s.readPage(r, m, project, rel)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeEntity(w, r, http.StatusOK, page, stringField(page, "Rev"))
}

// readPage reads one page, retrying under the project's documentation folder
// when the caller addressed it relative to that folder instead of to the vault
// root. Both spellings appear in the wild: the tree hands out vault-relative
// paths, a wikilink resolves to a docs-relative one.
func (s *Server) readPage(r *http.Request, m *mount, project, rel string) (any, error) {
	page, err := s.dispatch(r, m, "kb.page", map[string]any{"path": rel})
	if err == nil {
		return page, nil
	}
	alt := s.docsRelative(m, project, rel)
	if alt == rel {
		return nil, err
	}
	page, altErr := s.dispatch(r, m, "kb.page", map[string]any{"path": alt})
	if altErr != nil {
		// Report the failure of the path the caller actually asked for.
		return nil, err
	}
	return page, nil
}

// docsRelative prefixes a page path with the documentation folder of a project
// when it is not already there.
func (s *Server) docsRelative(m *mount, project, rel string) string {
	if !m.ready() {
		return rel
	}
	clean := path.Clean(rel)
	for _, ref := range m.vlt.Projects() {
		if project != "" && string(ref.Key) != project {
			continue
		}
		docs := ref.DocsPath
		if docs == "" || docs == "." {
			continue
		}
		if clean == docs || strings.HasPrefix(clean, docs+"/") {
			return clean
		}
		return docs + "/" + clean
	}
	return clean
}

// handleKBWrite serves PUT …/kb/page.
func (s *Server) handleKBWrite(w http.ResponseWriter, r *http.Request) {
	m, project, ok := s.kbScope(w, r)
	if !ok {
		return
	}
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Text    string `json:"text"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Path == "" {
		body.Path = r.URL.Query().Get("path")
	}
	if body.Path == "" {
		failProblem(w, r, codeInvalidRequest, "A page write needs a path.")
		return
	}
	content := body.Content
	if content == "" {
		content = body.Text
	}

	target := s.docsRelative(m, project, body.Path)
	rev, present, _ := ifMatch(r)
	if !present {
		// The precondition is strict for an existing page and cannot be met by a
		// page that does not exist yet.
		if _, err := s.dispatch(r, m, "kb.page", map[string]any{"path": target}); err == nil {
			failProblem(w, r, codePreconditionRequired,
				"Overwriting an existing page needs an If-Match header carrying the revision you read.")
			return
		}
	}

	result, ok := s.call(w, r, m, "kb.write", map[string]any{
		"path": target, "text": content, "rev": rev,
	})
	if !ok {
		return
	}
	page := field(result, "page")
	s.publishPageWrite(r, m, result)
	writeEntity(w, r, http.StatusOK, page, stringField(page, "Rev"))
}

// dispatch runs one core method and returns its raw result, leaving the caller
// to decide what to do with a failure.
func (s *Server) dispatch(r *http.Request, m *mount, method string, params any) (any, error) {
	if m == nil || !m.ready() {
		return nil, &vault.Error{Code: codeIndexUnavailable, Message: "the repository is not indexed"}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode %s parameters: %w", method, err)
	}
	result, err := m.vlt.Dispatch(r.Context(), method, raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return result, nil
}
