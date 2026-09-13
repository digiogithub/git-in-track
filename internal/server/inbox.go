package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/vault"
)

// The inbox surface, task GIT-T-0042 of story GIT-US-0056 (ADR-033,
// docs/07-cli-and-api.md section 5.3).
//
// Two routes, both thin wrappers over the vault: the triage queue of a project
// and the one decision that empties a row from it. Everything that decides what
// an inbox is — which status counts as triage, when a snooze has expired, what
// `accept` writes — lives in internal/core and internal/vault, so that the
// browser build answers exactly the same way with no companion at all.

// inboxListParams is the query half of "inbox.list". The JSON tags are the
// contract's, because this value is marshaled straight into vault.Dispatch.
//
// `status` is the *triage* state — pending, accepted, rejected, snoozed,
// duplicate — and never a workflow status: every item this endpoint can answer
// with is in a triage status by construction.
type inboxListParams struct {
	Project  string   `json:"project,omitempty"`
	Status   []string `json:"status,omitempty"`
	Type     []string `json:"type,omitempty"`
	Label    []string `json:"label,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Text     string   `json:"text,omitempty"`
	Sort     string   `json:"sort,omitempty"`
	Order    string   `json:"order,omitempty"`
	Limit    int      `json:"limit,omitempty"`
	Cursor   string   `json:"cursor,omitempty"`
	Fields   []string `json:"fields,omitempty"`
}

// inboxTriageBody is the body of POST /api/v1/items/{id}/triage. The revision
// comes from If-Match, never from the body, so that one write cannot claim two
// different preconditions.
type inboxTriageBody struct {
	Action       string `json:"action"`
	Status       string `json:"status,omitempty"`
	Type         string `json:"type,omitempty"`
	Parent       string `json:"parent,omitempty"`
	SnoozedUntil string `json:"snoozedUntil,omitempty"`
	DuplicateOf  string `json:"duplicateOf,omitempty"`
}

// inboxChangedData is the payload of an `inbox.changed` event: which project's
// queue moved, which item moved it, what was decided and how many submissions
// are still waiting. The count is what a sidebar badge renders without a second
// call (docs/07 section 5.6).
type inboxChangedData struct {
	Repo         string `json:"repo"`
	Project      string `json:"project"`
	ID           string `json:"id"`
	Action       string `json:"action"`
	PendingCount int    `json:"pendingCount"`
	Origin       string `json:"origin"`
	RequestID    string `json:"requestId,omitempty"`
}

// parseInboxFilter maps the documented query string onto the list parameters.
func parseInboxFilter(r *http.Request) inboxListParams {
	q := r.URL.Query()
	p := inboxListParams{
		Project:  q.Get("project"),
		Status:   q["status"],
		Type:     q["type"],
		Label:    q["label"],
		Assignee: q.Get("assignee"),
		Sort:     q.Get("sort"),
		Order:    q.Get("order"),
		Cursor:   q.Get("cursor"),
	}
	p.Text = q.Get("q")
	if p.Text == "" {
		p.Text = q.Get("text")
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			p.Limit = min(n, maxItemsPerPage)
		}
	}
	if v := q.Get("fields"); v != "" {
		p.Fields = splitList(v)
	}
	return p
}

// handleInboxList serves GET /api/v1/inbox.
//
// The answer carries `counts` and `pending` for the whole queue rather than for
// the page, and an expired snooze is already counted as pending: the vault
// resolves it against its own clock, so no caller can forget to.
func (s *Server) handleInboxList(w http.ResponseWriter, r *http.Request) {
	params := parseInboxFilter(r)
	m, ok := s.mountForFilter(w, r, params.Project)
	if !ok {
		return
	}
	page, ok := s.call(w, r, m, "inbox.list", params)
	if !ok {
		return
	}
	if total, found := totalOf(page); found {
		w.Header().Set("X-Total-Count", strconv.Itoa(total))
	}
	writeJSON(w, r, http.StatusOK, page)
}

// handleItemTriage serves POST /api/v1/items/{id}/triage: accept, reject,
// snooze or mark one submission as a duplicate.
func (s *Server) handleItemTriage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// An empty revision is what the vault reads as "unconditional", which is
	// what If-Match: * asks for. The literal star never reaches the core.
	rev, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var body inboxTriageBody
	if !decodeBody(w, r, &body) {
		return
	}
	m, ok := s.mountForItem(w, r, id)
	if !ok {
		return
	}
	params := vault.InboxTriageParams{
		ID: id, Rev: rev, Action: body.Action, Status: body.Status, Type: body.Type,
		Parent: body.Parent, SnoozedUntil: body.SnoozedUntil, DuplicateOf: body.DuplicateOf,
	}
	result, ok := s.call(w, r, m, "inbox.triage", params)
	if !ok {
		return
	}
	triaged, ok := result.(vault.InboxTriageResult)
	if !ok {
		writeJSON(w, r, http.StatusOK, result)
		return
	}
	s.publishTriage(r, m, triaged)
	writeEntity(w, r, http.StatusOK, triaged, string(triaged.Item.Rev))
}

// publishTriage announces one triage decision: the item event every backlog
// view listens to, the index refresh behind it, the commit commit-on-save owes,
// and the `inbox.changed` the queue badge reads.
func (s *Server) publishTriage(r *http.Request, m *mount, result vault.InboxTriageResult) {
	requestID := requestIDOf(r)
	id := string(result.Item.ID)
	s.hub.Publish(eventItemChanged, itemChangedData{
		Repo: m.id, ID: id, Op: "triaged", Rev: string(result.Item.Rev),
		Origin: "api", RequestID: requestID,
	})
	s.publishIndexUpdated(m, indexCounts{Updated: 1}, requestID)
	m.touch(s.now())
	// A duplicate decision writes two files — the entry and the item it points
	// at — so the commit stages the whole write set rather than one path.
	s.commitWriteSets(r.Context(),
		[]vault.RepoWriteSet{{VaultID: m.id, Written: result.Writes.Written, Removed: result.Writes.Removed}},
		itemFields(id, "updated", &result.Item))
	s.publishInboxChanged(r, m, id, string(result.Action), result.Pending)
}

// publishInboxChanged emits `inbox.changed`. It is published on every triage
// and on every create that files an item straight into the queue, so that a
// badge and an open triage list both react to the same stream.
func (s *Server) publishInboxChanged(r *http.Request, m *mount, id, action string, pending int) {
	project := ""
	if key, _, _, err := core.ParseItemID(id); err == nil {
		project = string(key)
	}
	s.hub.Publish(eventInboxChanged, inboxChangedData{
		Repo: m.id, Project: project, ID: id, Action: action,
		PendingCount: pending, Origin: "api", RequestID: requestIDOf(r),
	})
}

// inboxPendingCount asks the vault how many submissions are waiting, for the
// create path, which has no count of its own to report. A failure is not worth
// failing the create over: the event then carries zero and the next listing
// corrects it.
func (s *Server) inboxPendingCount(r *http.Request, m *mount, project string) int {
	if m == nil || !m.ready() {
		return 0
	}
	result, err := m.vlt.Dispatch(r.Context(), "inbox.list",
		mustJSON(inboxListParams{Project: project, Limit: 1, Fields: []string{"id"}}))
	if err != nil {
		return 0
	}
	page, ok := result.(vault.InboxPage)
	if !ok {
		return 0
	}
	return page.Pending
}

// inboxDraft reports whether a create body files the new item into the queue.
func inboxDraft(draft map[string]any) bool {
	_, ok := draft["inbox"]
	return ok
}
