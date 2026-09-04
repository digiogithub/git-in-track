package server

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// maxItemsPerPage is the largest page GET /items serves (docs/07 section 5.3).
const maxItemsPerPage = 500

// mountItems registers the item routes.
func (s *Server) mountItems(r chi.Router) {
	r.Get("/", s.handleItemList)
	r.Post("/", s.handleItemCreate)
	// Static segments win over {id} in chi, so the validation route is reachable
	// even though an id could spell "validate".
	r.Post("/validate", s.handleValidate)

	r.Get("/{id}", s.handleItemGet)
	r.Patch("/{id}", s.handleItemUpdate)
	r.Put("/{id}", s.notImplemented("A full replace (PUT) arrives with Phase 3; PATCH already replaces the body."))
	r.Delete("/{id}", s.handleItemDelete)
	r.Post("/{id}/move", s.handleItemMove)
	r.Get("/{id}/comments", s.handleCommentList)
	r.Post("/{id}/comments", s.handleCommentAdd)
	s.deferRoute(r, "/{id}/links", "Typed links are edited through PATCH /items/{id} until Phase 3.")
}

// itemFilter is the query half of the CoreApi ItemFilter. The JSON tags are the
// contract's, because this value is marshaled straight into vault.Dispatch.
type itemFilter struct {
	Project        string   `json:"project,omitempty"`
	Type           []string `json:"type,omitempty"`
	Status         []string `json:"status,omitempty"`
	Category       []string `json:"category,omitempty"`
	Priority       []string `json:"priority,omitempty"`
	Assignee       string   `json:"assignee,omitempty"`
	Label          []string `json:"label,omitempty"`
	Parent         string   `json:"parent,omitempty"`
	Milestone      string   `json:"milestone,omitempty"`
	UpdatedSince   string   `json:"updatedSince,omitempty"`
	Text           string   `json:"text,omitempty"`
	IncludeDeleted bool     `json:"includeDeleted,omitempty"`
	Sort           string   `json:"sort,omitempty"`
	Order          string   `json:"order,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Cursor         string   `json:"cursor,omitempty"`
	Fields         []string `json:"fields,omitempty"`
}

// parseItemFilter maps the documented query string onto the filter. Repeatable
// parameters are OR within a field and AND across fields (docs/07 section 5.3).
func parseItemFilter(r *http.Request) itemFilter {
	q := r.URL.Query()
	f := itemFilter{
		Project:      q.Get("project"),
		Type:         q["type"],
		Status:       q["status"],
		Category:     q["category"],
		Priority:     q["priority"],
		Assignee:     q.Get("assignee"),
		Label:        q["label"],
		Parent:       q.Get("parent"),
		Milestone:    q.Get("milestone"),
		UpdatedSince: q.Get("updatedSince"),
		Sort:         q.Get("sort"),
		Order:        q.Get("order"),
		Cursor:       q.Get("cursor"),
	}
	// `q` is the documented spelling of the full-text filter; `text` is what the
	// core contract calls it and is accepted too.
	f.Text = q.Get("q")
	if f.Text == "" {
		f.Text = q.Get("text")
	}
	if v := q.Get("includeDeleted"); v != "" {
		f.IncludeDeleted, _ = strconv.ParseBool(v)
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Limit = min(n, maxItemsPerPage)
		}
	}
	if v := q.Get("fields"); v != "" {
		f.Fields = splitList(v)
	}
	return f
}

// splitList splits a comma-separated query value, dropping empty entries.
func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// handleItemList serves GET /api/v1/items.
func (s *Server) handleItemList(w http.ResponseWriter, r *http.Request) {
	filter := parseItemFilter(r)
	m, ok := s.mountForFilter(w, r, filter.Project)
	if !ok {
		return
	}
	page, ok := s.call(w, r, m, "item.list", filter)
	if !ok {
		return
	}
	if total, found := totalOf(page); found {
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	writeJSON(w, r, http.StatusOK, page)
}

// mountForFilter resolves the repository a list-shaped request reads from: the
// one exposing the requested project, or the only mounted one. A request that
// names no project against several repositories is ambiguous and says so.
func (s *Server) mountForFilter(w http.ResponseWriter, r *http.Request, project string) (*mount, bool) {
	if project != "" {
		m, found := s.repos.forProject(project)
		if !found {
			failProblem(w, r, codeNotFound, "No mounted repository exposes project "+project+".")
			return nil, false
		}
		return m, true
	}
	ready := s.repos.ready()
	switch len(ready) {
	case 0:
		failProblem(w, r, codeRepoNotRegistered,
			"No repository is mounted. Start the server with --repo <path> or register one with `gintrack add`.")
		return nil, false
	case 1:
		return ready[0], true
	default:
		failProblem(w, r, codeInvalidRequest,
			"Several repositories are mounted; add ?project=<KEY> to say which one this request is about.")
		return nil, false
	}
}

// mountForItem resolves the repository owning an item id.
func (s *Server) mountForItem(w http.ResponseWriter, r *http.Request, id string) (*mount, bool) {
	m, found := s.repos.forItem(id)
	if !found {
		failProblem(w, r, codeNotFound, "No mounted repository owns item "+id+".")
		return nil, false
	}
	return m, true
}

// handleItemGet serves GET /api/v1/items/{id}.
func (s *Server) handleItemGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, ok := s.mountForItem(w, r, id)
	if !ok {
		return
	}
	item, ok := s.call(w, r, m, "item.get", map[string]any{"id": id})
	if !ok {
		return
	}
	writeEntity(w, r, http.StatusOK, item, revOf(item))
}

// handleItemCreate serves POST /api/v1/items.
func (s *Server) handleItemCreate(w http.ResponseWriter, r *http.Request) {
	var draft map[string]any
	if !decodeBody(w, r, &draft) {
		return
	}
	project, _ := draft["project"].(string)
	m, ok := s.mountForFilter(w, r, project)
	if !ok {
		return
	}
	if project == "" {
		// The vault needs a project when the repository holds several; with one
		// it picks the only one itself.
		if keys := m.projectKeys(); len(keys) == 1 {
			draft["project"] = keys[0]
		}
	}
	result, ok := s.call(w, r, m, "item.create", draft)
	if !ok {
		return
	}
	item := field(result, "item")
	s.publishWrite(r, m, result, itemIDOf(item), "created")
	if id := itemIDOf(item); id != "" {
		w.Header().Set("Location", apiPrefix+"/items/"+id)
	}
	writeEntity(w, r, http.StatusCreated, item, revOf(item))
}

// itemPatch is the body of PATCH /api/v1/items/{id}: the documented flat form,
// which this handler folds into the sparse patch the core contract declares.
type itemPatch struct {
	Set   map[string]any `json:"set,omitempty"`
	Unset []string       `json:"unset,omitempty"`
	Body  *string        `json:"body,omitempty"`
}

// parsePatch accepts both the flat REST body ({"status":"done"}) and the nested
// core form ({"set":{"status":"done"}}), so that the web client and an agent
// speaking the contract literally both work.
func parsePatch(raw map[string]any) itemPatch {
	patch := itemPatch{Set: map[string]any{}}
	if nested, ok := raw["set"].(map[string]any); ok {
		patch.Set = nested
	}
	for key, value := range raw {
		switch key {
		case "set":
			continue
		case "unset":
			if list, ok := value.([]any); ok {
				for _, entry := range list {
					if name, ok := entry.(string); ok {
						patch.Unset = append(patch.Unset, name)
					}
				}
			}
		case "body":
			if text, ok := value.(string); ok {
				body := text
				patch.Body = &body
			}
		default:
			if _, taken := patch.Set[key]; !taken {
				patch.Set[key] = value
			}
		}
	}
	return patch
}

// handleItemUpdate serves PATCH /api/v1/items/{id}.
func (s *Server) handleItemUpdate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var raw map[string]any
	if !decodeBody(w, r, &raw) {
		return
	}
	m, ok := s.mountForItem(w, r, id)
	if !ok {
		return
	}
	result, ok := s.call(w, r, m, "item.update", map[string]any{
		"id": id, "rev": rev, "patch": parsePatch(raw),
	})
	if !ok {
		return
	}
	item := field(result, "item")
	s.publishWrite(r, m, result, id, "updated")
	writeEntity(w, r, http.StatusOK, item, revOf(item))
}

// handleItemMove serves POST /api/v1/items/{id}/move.
func (s *Server) handleItemMove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Status == "" {
		failProblem(w, r, codeInvalidRequest, "A move needs a target status.")
		return
	}
	m, ok := s.mountForItem(w, r, id)
	if !ok {
		return
	}
	result, ok := s.call(w, r, m, "item.move", map[string]any{"id": id, "rev": rev, "status": body.Status})
	if !ok {
		return
	}
	item := field(result, "item")
	s.publishWrite(r, m, result, id, "moved")
	writeEntity(w, r, http.StatusOK, item, revOf(item))
}

// handleItemDelete serves DELETE /api/v1/items/{id}. It soft-deletes by
// default; ?hard=true removes the file.
func (s *Server) handleItemDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	hard, _ := strconv.ParseBool(r.URL.Query().Get("hard"))
	m, ok := s.mountForItem(w, r, id)
	if !ok {
		return
	}
	result, ok := s.call(w, r, m, "item.delete", map[string]any{"id": id, "rev": rev, "hard": hard})
	if !ok {
		return
	}
	s.publishWrite(r, m, result, id, "deleted")
	w.WriteHeader(http.StatusNoContent)
}

// handleCommentList serves GET /api/v1/items/{id}/comments.
func (s *Server) handleCommentList(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, ok := s.mountForItem(w, r, id)
	if !ok {
		return
	}
	comments, ok := s.call(w, r, m, "comment.list", map[string]any{"id": id})
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, comments)
}

// handleCommentAdd serves POST /api/v1/items/{id}/comments.
func (s *Server) handleCommentAdd(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Body      string `json:"body"`
		Author    string `json:"author"`
		InReplyTo string `json:"inReplyTo,omitempty"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		failProblem(w, r, codeInvalidRequest, "A comment needs a body.")
		return
	}
	m, ok := s.mountForItem(w, r, id)
	if !ok {
		return
	}
	result, ok := s.call(w, r, m, "comment.add", map[string]any{
		"id": id, "body": body.Body, "author": body.Author, "inReplyTo": body.InReplyTo,
	})
	if !ok {
		return
	}
	comment := field(result, "comment")
	s.publishWrite(r, m, result, id, "commented")
	writeEntity(w, r, http.StatusCreated, comment, revOfComment(comment))
}

// handleValidate serves POST /api/v1/validate and POST /api/v1/items/validate.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id,omitempty"`
		Text    string `json:"text,omitempty"`
		Path    string `json:"path,omitempty"`
		Project string `json:"project,omitempty"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	var (
		m  *mount
		ok bool
	)
	if body.ID != "" && body.Project == "" {
		m, ok = s.mountForItem(w, r, body.ID)
	} else {
		m, ok = s.mountForFilter(w, r, body.Project)
	}
	if !ok {
		return
	}
	diagnostics, ok := s.call(w, r, m, "item.validate", map[string]any{
		"id": body.ID, "text": body.Text, "path": body.Path,
	})
	if !ok {
		return
	}
	writeJSON(w, r, http.StatusOK, diagnostics)
}

// handleSearch serves GET /api/v1/search.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	project := q.Get("project")
	if project != "" {
		if _, found := s.repos.forProject(project); !found {
			failProblem(w, r, codeNotFound, "No mounted repository exposes project "+project+".")
			return
		}
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, maxItemsPerPage)
		}
	}
	// Search spans every mounted repository: each hit says which project — and
	// which repository — it came from, so a workspace holding a team repository
	// and several clones answers one query instead of one per repository
	// (GIT-US-0016).
	hits, err := s.repos.workspace().Search(r.Context(), q.Get("q"), limit, project)
	if err != nil {
		writeVaultError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, hits)
}

// ------------------------------------------------------- result inspection --

// field reads one member of the map a mutating core method returns. The value
// is handed back untouched, so the response carries the very bytes the browser
// build receives.
func field(result any, name string) any {
	m, ok := result.(map[string]any)
	if !ok {
		return result
	}
	value, ok := m[name]
	if !ok {
		return result
	}
	return value
}

// writesOf reports the files a mutating call wrote and removed.
func writesOf(result any) (vault.WriteSet, bool) {
	m, ok := result.(map[string]any)
	if !ok {
		return vault.WriteSet{}, false
	}
	writes, ok := m["writes"].(vault.WriteSet)
	return writes, ok
}

// revOf reads the revision of an item the core returned.
func revOf(value any) string {
	if it, ok := value.(*core.Item); ok {
		return string(it.Rev)
	}
	return ""
}

// revOfComment reads the revision of a comment the core returned.
func revOfComment(value any) string {
	if c, ok := value.(*core.Comment); ok {
		return string(c.Rev)
	}
	return ""
}

// itemIDOf reads the id of an item the core returned.
func itemIDOf(value any) string {
	if it, ok := value.(*core.Item); ok {
		return string(it.ID)
	}
	return ""
}

// totalOf reads the Total field of a page without decoding it, so that the
// X-Total-Count header can be set while the body stays exactly what the core
// produced.
func totalOf(page any) (int, bool) {
	v := reflect.ValueOf(page)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	field := v.FieldByName("Total")
	if !field.IsValid() || field.Kind() != reflect.Int {
		return 0, false
	}
	return int(field.Int()), true
}
