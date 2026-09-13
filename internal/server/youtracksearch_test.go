package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/config"
)

// GET /api/v1/youtrack/issues, story GIT-US-0054.
//
// The upstream is an httptest server rather than a fake client: this endpoint's
// job is the composition of a query and the projection of an answer, and both
// are only really asserted by looking at the request that went out and the
// bytes that came back.

// issueSearchStub is a YouTrack instance that records the searches it was asked
// for and answers with whatever the test set.
type issueSearchStub struct {
	server *httptest.Server

	mu sync.Mutex
	// queries and windows record the `query` parameter and the $top/$skip of
	// every /api/issues request.
	queries []string
	windows [][2]int
	// issues is the answer of a search, paged by the stub itself.
	issues []map[string]any
	// bundleValues is the answer of the version bundle listing, and bundleID
	// the bundle the custom-field settings point at.
	bundleValues []map[string]any
	bundleID     string
	// fieldName is the custom field the settings declare as carrying the
	// bundle.
	fieldName string
	// status, when set, is the status every endpoint answers with.
	status int
}

// newIssueSearchStub starts a stub instance and stops it with the test.
func newIssueSearchStub(t *testing.T) *issueSearchStub {
	t.Helper()

	stub := &issueSearchStub{bundleID: "b-1", fieldName: "Fix versions"}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		if stub.status != 0 {
			w.WriteHeader(stub.status)
			_, _ = w.Write([]byte(`{"error":"denied"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/issues"):
			stub.queries = append(stub.queries, r.URL.Query().Get("query"))
			top, _ := strconv.Atoi(r.URL.Query().Get("$top"))
			skip, _ := strconv.Atoi(r.URL.Query().Get("$skip"))
			stub.windows = append(stub.windows, [2]int{top, skip})
			page := []map[string]any{}
			for i := skip; i < len(stub.issues) && len(page) < top; i++ {
				page = append(page, stub.issues[i])
			}
			_ = json.NewEncoder(w).Encode(page)
		case strings.Contains(r.URL.Path, "/customFieldSettings/bundles/version/"):
			_ = json.NewEncoder(w).Encode(stub.bundleValues)
		case strings.Contains(r.URL.Path, "/customFieldSettings"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id":    "cfs-1",
				"field": map[string]any{"id": "d-1", "name": stub.fieldName},
				"bundle": map[string]any{
					"id": stub.bundleID, "$type": "VersionBundle",
				},
			}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "0-1", "shortName": "ACME"})
		}
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// issue builds one issue row of the stub's answer.
func issue(id, summary, issueType, state, assignee string) map[string]any {
	return map[string]any{
		"id": "2-" + id, "idReadable": id, "summary": summary,
		"updated": 1757764800000,
		"customFields": []map[string]any{
			{"name": "Type", "value": map[string]any{"name": issueType}},
			{"name": "State", "value": map[string]any{"name": state}},
			{"name": "Assignee", "value": map[string]any{"login": assignee, "fullName": assignee}},
		},
	}
}

// newSearchServer mounts the fixture linked to the stub instance, with a token
// stored, which is what every search below needs.
func newSearchServer(t *testing.T, stub *issueSearchStub) (*Server, string) {
	t.Helper()

	link := &config.YouTrackLink{URL: stub.server.URL, Project: "ACME"}
	s, root, _ := newYouTrackServer(t, link, ytToken, false)
	return s, root
}

// searchPage is the documented shape of GET /api/v1/youtrack/issues.
type searchPage struct {
	ProjectKey string `json:"projectKey"`
	Project    string `json:"project"`
	Query      string `json:"query"`
	Preset     string `json:"preset"`
	Items      []struct {
		ID          string `json:"id"`
		IDReadable  string `json:"idReadable"`
		Summary     string `json:"summary"`
		Type        string `json:"type"`
		State       string `json:"state"`
		Assignee    string `json:"assignee"`
		Updated     string `json:"updated"`
		URL         string `json:"url"`
		Released    bool   `json:"released"`
		Archived    bool   `json:"archived"`
		ReleaseDate string `json:"releaseDate"`
		Linked      *struct {
			ItemID string `json:"itemId"`
			Type   string `json:"type"`
			Status string `json:"status"`
			Title  string `json:"title"`
		} `json:"linked"`
	} `json:"items"`
	NextCursor string `json:"nextCursor"`
	Limit      int    `json:"limit"`
}

// TestIssueSearchProjectsAndOrders covers the happy path: the composed query
// carries the project clause and the ordering, and the answer is the documented
// projection.
func TestIssueSearchProjectsAndOrders(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	stub.issues = []map[string]any{
		issue("ACME-1", "Guest checkout", "User Story", "In Progress", "marta"),
		issue("ACME-2", "Saved cards", "Task", "Open", "jose"),
	}
	s, _ := newSearchServer(t, stub)

	var page searchPage
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/issues?q=checkout"}),
		http.StatusOK, &page)

	if page.Query != "project: {ACME} checkout order by: created asc" {
		t.Errorf("query = %q", page.Query)
	}
	if len(stub.queries) != 1 || stub.queries[0] != page.Query {
		t.Errorf("the instance was asked %v, want the echoed query", stub.queries)
	}
	if page.ProjectKey != ytProjectKey || page.Project != "ACME" {
		t.Errorf("page names %q/%q", page.ProjectKey, page.Project)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	first := page.Items[0]
	if first.IDReadable != "ACME-1" || first.Summary != "Guest checkout" {
		t.Errorf("first row = %+v", first)
	}
	if first.Type != "User Story" || first.State != "In Progress" || first.Assignee != "marta" {
		t.Errorf("the custom fields were not projected: %+v", first)
	}
	if first.URL != stub.server.URL+"/issue/ACME-1" {
		t.Errorf("url = %q", first.URL)
	}
	if first.Updated == "" {
		t.Error("updated was not rendered")
	}
	if first.Linked != nil {
		t.Errorf("an unimported issue reported a link: %+v", first.Linked)
	}
}

// TestIssueSearchPresets covers the four query presets and the refusal of an
// unknown one.
func TestIssueSearchPresets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		preset string
		want   string
		status int
	}{
		{name: "epics", preset: "epics", want: "project: {ACME} Type: Epic order by: created asc"},
		{name: "stories", preset: "stories", want: "project: {ACME} Type: {User Story} order by: created asc"},
		{name: "tasks", preset: "tasks", want: "project: {ACME} Type: Task order by: created asc"},
		{name: "unresolved", preset: "unresolved", want: "project: {ACME} #Unresolved order by: created asc"},
		{name: "an unknown preset is refused", preset: "nonsense", status: http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stub := newIssueSearchStub(t)
			s, _ := newSearchServer(t, stub)
			rec := send(t, s, request{
				method: http.MethodGet,
				target: "/api/v1/youtrack/issues?preset=" + url.QueryEscape(tc.preset),
			})
			if tc.status != 0 {
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), "preset") {
					t.Errorf("the problem does not name the field: %s", rec.Body.String())
				}
				return
			}
			var page searchPage
			decode(t, rec, http.StatusOK, &page)
			if page.Query != tc.want {
				t.Errorf("query = %q, want %q", page.Query, tc.want)
			}
			if page.Preset != tc.preset {
				t.Errorf("preset = %q, want %q", page.Preset, tc.preset)
			}
		})
	}
}

// TestIssueSearchQueryAndPresetCombine proves a typed query and a preset are
// both applied, and the ordering still lands last.
func TestIssueSearchQueryAndPresetCombine(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	s, _ := newSearchServer(t, stub)

	var page searchPage
	decode(t, send(t, s, request{
		method: http.MethodGet,
		target: "/api/v1/youtrack/issues?q=" + url.QueryEscape("Assignee: marta") + "&preset=tasks",
	}), http.StatusOK, &page)

	want := "project: {ACME} Assignee: marta Type: Task order by: created asc"
	if page.Query != want {
		t.Errorf("query = %q, want %q", page.Query, want)
	}
}

// TestIssueSearchPagesWithAnOpaqueCursor covers the paging: a full page hands
// back a cursor, the next request resumes at the right $skip, and a short page
// ends the walk.
func TestIssueSearchPagesWithAnOpaqueCursor(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	for i := 1; i <= 5; i++ {
		stub.issues = append(stub.issues, issue("ACME-"+strconv.Itoa(i), "Issue", "Task", "Open", "jose"))
	}
	s, _ := newSearchServer(t, stub)

	var first searchPage
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/issues?limit=2"}),
		http.StatusOK, &first)
	if len(first.Items) != 2 || first.NextCursor == "" {
		t.Fatalf("first page = %d items, cursor %q", len(first.Items), first.NextCursor)
	}
	if strings.Contains(first.NextCursor, "2") && first.NextCursor == "2" {
		t.Error("the cursor is not opaque")
	}

	var second searchPage
	decode(t, send(t, s, request{
		method: http.MethodGet,
		target: "/api/v1/youtrack/issues?limit=2&cursor=" + url.QueryEscape(first.NextCursor),
	}), http.StatusOK, &second)
	if len(second.Items) != 2 {
		t.Fatalf("second page = %d items", len(second.Items))
	}
	if second.Items[0].IDReadable == first.Items[0].IDReadable {
		t.Error("the second page repeated the first")
	}
	if len(stub.windows) != 2 || stub.windows[1] != [2]int{2, 2} {
		t.Errorf("windows = %v, want the second at $top=2 $skip=2", stub.windows)
	}

	var third searchPage
	decode(t, send(t, s, request{
		method: http.MethodGet,
		target: "/api/v1/youtrack/issues?limit=2&cursor=" + url.QueryEscape(second.NextCursor),
	}), http.StatusOK, &third)
	if third.NextCursor != "" {
		t.Errorf("a short page handed back a cursor: %q", third.NextCursor)
	}

	rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/issues?cursor=not-a-cursor!"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed cursor = %d, want 400", rec.Code)
	}
}

// TestIssueSearchResolvesLinkedItems is the "already imported" badge: an issue
// a local item claims through its `external` reference is reported with the
// item that claims it, resolved from the index and not from a second call.
func TestIssueSearchResolvesLinkedItems(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	stub.issues = []map[string]any{
		issue("DEMO-42", "Guest checkout", "User Story", "In Progress", "marta"),
		issue("DEMO-43", "Saved cards", "Task", "Open", "jose"),
	}
	link := &config.YouTrackLink{URL: stub.server.URL, Project: "ACME"}
	root := copyTree(t, fixtureRoot)
	linkStoryToIssue(t, root, "DEMO-42")
	s := newLinkedYouTrackServer(t, root, link)

	var page searchPage
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/issues"}),
		http.StatusOK, &page)

	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(page.Items))
	}
	if page.Items[0].Linked == nil || page.Items[0].Linked.ItemID != "DEMO-US-0001" {
		t.Errorf("the imported issue was not linked: %+v", page.Items[0].Linked)
	}
	if page.Items[0].Linked.Title == "" || page.Items[0].Linked.Status == "" {
		t.Errorf("the link carries no item detail: %+v", page.Items[0].Linked)
	}
	if page.Items[1].Linked != nil {
		t.Errorf("an unimported issue reported a link: %+v", page.Items[1].Linked)
	}
}

// newLinkedYouTrackServer mounts a prepared tree with a YouTrack block and a
// token, which newYouTrackServer cannot do because it copies the fixture
// itself.
func newLinkedYouTrackServer(t *testing.T, root string, link *config.YouTrackLink) *Server {
	t.Helper()

	if _, err := config.SaveYouTrackLink(ytProjectYAML(root), *link); err != nil {
		t.Fatalf("write the link: %v", err)
	}
	cfg := config.Default()
	cfg.SetYouTrackToken(ytProjectKey, ytToken)
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		YouTrack:  cfg.YouTrackTokens(),
		Now:       func() time.Time { return jobClock },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

// TestIssueSearchVersionsPreset covers the preset that is not a query: the
// project's version bundle is resolved through the custom-field settings and
// listed in the same envelope, archived values excluded unless asked for.
func TestIssueSearchVersionsPreset(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	stub.bundleValues = []map[string]any{
		{"id": "v-2", "name": "2.0", "released": false, "archived": false, "releaseDate": 1767225600000},
		{"id": "v-1", "name": "1.0", "released": true, "archived": false},
		{"id": "v-0", "name": "0.9", "released": true, "archived": true},
	}
	s, _ := newSearchServer(t, stub)

	var page searchPage
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/issues?preset=versions"}),
		http.StatusOK, &page)

	if len(page.Items) != 2 {
		t.Fatalf("items = %d, want the two unarchived versions: %+v", len(page.Items), page.Items)
	}
	if page.Items[0].Summary != "1.0" || page.Items[1].Summary != "2.0" {
		t.Errorf("versions are not ordered: %+v", page.Items)
	}
	for _, row := range page.Items {
		if row.Type != "version" {
			t.Errorf("row %q has type %q, want version", row.Summary, row.Type)
		}
	}
	if page.Items[1].ReleaseDate == "" {
		t.Error("a release date was not rendered")
	}

	var all searchPage
	decode(t, send(t, s, request{
		method: http.MethodGet, target: "/api/v1/youtrack/issues?preset=versions&archived=true",
	}), http.StatusOK, &all)
	if len(all.Items) != 3 {
		t.Errorf("archived=true returned %d versions, want 3", len(all.Items))
	}
}

// TestIssueSearchNeedsAConnection is the gating: a project with no YouTrack
// block, or with no token, is refused with the documented problem code and
// nothing reaches an instance.
func TestIssueSearchNeedsAConnection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		link  *config.YouTrackLink
		token string
	}{
		{name: "no block at all"},
		{name: "a block with no token", link: &config.YouTrackLink{URL: "https://yt.example.com", Project: "ACME"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s, _, _ := newYouTrackServer(t, tc.link, tc.token, false)
			rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/issues"})
			if rec.Code == http.StatusOK {
				t.Fatalf("an unconfigured project answered a search: %s", rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), codeYouTrackNotConfigured) {
				t.Errorf("problem = %s, want %s", rec.Body.String(), codeYouTrackNotConfigured)
			}
		})
	}
}

// TestIssueSearchNeverEchoesTheToken is the promise of ADR-032 applied to this
// endpoint: an upstream failure is reported without the credential, whatever
// the instance echoed back.
func TestIssueSearchNeverEchoesTheToken(t *testing.T) {
	t.Parallel()

	stub := newIssueSearchStub(t)
	stub.status = http.StatusInternalServerError
	s, _ := newSearchServer(t, stub)

	rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/youtrack/issues"})
	if rec.Code == http.StatusOK {
		t.Fatal("an upstream failure was reported as success")
	}
	if strings.Contains(rec.Body.String(), ytToken) {
		t.Fatalf("the token reached the response: %s", rec.Body.String())
	}
}

// TestIssueSearchRequiresTheBearerToken keeps the route inside the
// authenticated group with the rest of the subtree.
func TestIssueSearchRequiresTheBearerToken(t *testing.T) {
	t.Parallel()

	s, _, _ := newYouTrackServer(t, nil, "", false)
	resp := do(t, s, http.MethodGet, "/api/v1/youtrack/issues", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /issues without a token = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestIssuesCursorRoundTrips pins the cursor encoding on its own.
func TestIssuesCursorRoundTrips(t *testing.T) {
	t.Parallel()

	for _, skip := range []int{0, 1, 50, 1000} {
		encoded := encodeIssuesCursor(skip)
		rec := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		got, ok := issuesCursor(rec, r, encoded)
		if !ok || got != skip {
			t.Errorf("cursor for %d round-tripped to %d (ok=%v)", skip, got, ok)
		}
	}
}

// TestIssuesLimitIsClamped pins the page bounds.
func TestIssuesLimitIsClamped(t *testing.T) {
	t.Parallel()

	tests := map[string]int{"": defaultIssuesPerPage, "10": 10, "100000": maxIssuesPerPage}
	for raw, want := range tests {
		rec := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
		got, ok := issuesLimit(rec, r, raw)
		if !ok || got != want {
			t.Errorf("issuesLimit(%q) = %d (ok=%v), want %d", raw, got, ok, want)
		}
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	if _, ok := issuesLimit(rec, r, "-3"); ok {
		t.Error("a negative limit was accepted")
	}
}
