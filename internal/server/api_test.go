package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixtureRoot is the vault every API test runs against, copied first so that no
// test ever writes into testdata/.
const fixtureRoot = "../../testdata/fixtures/project-basic"

// testRepoID is the id the fixture repository is mounted under.
const testRepoID = "demo"

// copyTree copies a directory tree into a fresh temporary directory.
func copyTree(t *testing.T, src string) string {
	t.Helper()

	dst := t.TempDir()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy the fixture: %v", err)
	}
	return dst
}

// newAPIServer mounts a copy of the fixture and returns the server and the
// directory it serves.
func newAPIServer(t *testing.T) (*Server, string) {
	t.Helper()

	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s, root
}

// request is one API call: method, target, optional JSON body and headers.
type request struct {
	method string
	target string
	body   any
	header map[string]string
}

// send runs a request through the router and returns the recorder, which
// carries the status, the headers and the body without a response body to
// close: nothing here ever crosses a socket.
func send(t *testing.T, s *Server, req request) *httptest.ResponseRecorder {
	t.Helper()

	var body io.Reader
	if req.body != nil {
		raw, err := json.Marshal(req.body)
		if err != nil {
			t.Fatalf("encode the request body: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	r := httptest.NewRequest(req.method, req.target, body)
	r.Header.Set("Authorization", "Bearer test-token")
	if req.body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range req.header {
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	return rec
}

// decode reads a JSON response body into a value, failing on the wrong status.
func decode(t *testing.T, rec *httptest.ResponseRecorder, want int, dst any) {
	t.Helper()

	data := rec.Body.Bytes()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d: %s", rec.Code, want, data)
	}
	if dst == nil || len(bytes.TrimSpace(data)) == 0 {
		return
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

// itemPageBody is the documented shape of GET /items.
type itemPageBody struct {
	Items []struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Rev    string `json:"rev"`
		Path   string `json:"path"`
		Body   string `json:"body"`
	} `json:"items"`
	NextCursor string `json:"nextCursor"`
	Total      int    `json:"total"`
}

// problemBody is the RFC 7807 document the API answers failures with.
type problemBody struct {
	Type       string `json:"type"`
	Title      string `json:"title"`
	Status     int    `json:"status"`
	Detail     string `json:"detail"`
	Code       string `json:"code"`
	Path       string `json:"path"`
	CurrentRev string `json:"currentRev"`
}

// getItem reads one item and returns its revision.
func getItem(t *testing.T, s *Server, id string) (string, string) {
	t.Helper()

	var item struct {
		Rev    string `json:"rev"`
		Status string `json:"status"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items/" + id}), http.StatusOK, &item)
	return item.Rev, item.Status
}

func TestItemListFiltersAndPaginates(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)

	t.Run("all items", func(t *testing.T) {
		resp := send(t, s, request{method: http.MethodGet, target: "/api/v1/items"})
		var page itemPageBody
		decode(t, resp, http.StatusOK, &page)
		if page.Total != 5 {
			t.Errorf("total = %d, want the 5 fixture items", page.Total)
		}
		if got := resp.Header().Get("X-Total-Count"); got != "5" {
			t.Errorf("X-Total-Count = %q, want 5", got)
		}
		for _, item := range page.Items {
			if item.Body != "" {
				t.Errorf("%s carries a body in a list answer", item.ID)
			}
		}
	})

	t.Run("filter by type", func(t *testing.T) {
		var page itemPageBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items?type=story"}), http.StatusOK, &page)
		if len(page.Items) != 2 {
			t.Fatalf("stories = %d, want 2", len(page.Items))
		}
		for _, item := range page.Items {
			if item.Type != "story" {
				t.Errorf("%s is a %s", item.ID, item.Type)
			}
		}
	})

	t.Run("filter by two statuses", func(t *testing.T) {
		var page itemPageBody
		decode(t, send(t, s, request{
			method: http.MethodGet,
			target: "/api/v1/items?status=in_progress&status=in_review",
		}), http.StatusOK, &page)
		if len(page.Items) == 0 {
			t.Fatal("no item matched two statuses")
		}
		for _, item := range page.Items {
			if item.Status != "in_progress" && item.Status != "in_review" {
				t.Errorf("%s has status %q", item.ID, item.Status)
			}
		}
	})

	t.Run("cursor pagination", func(t *testing.T) {
		var first itemPageBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items?limit=2&sort=id"}), http.StatusOK, &first)
		if len(first.Items) != 2 || first.NextCursor == "" {
			t.Fatalf("first page = %d items, cursor %q", len(first.Items), first.NextCursor)
		}
		var second itemPageBody
		decode(t, send(t, s, request{
			method: http.MethodGet,
			target: "/api/v1/items?limit=2&sort=id&cursor=" + first.NextCursor,
		}), http.StatusOK, &second)
		if len(second.Items) != 2 {
			t.Fatalf("second page = %d items, want 2", len(second.Items))
		}
		if first.Items[0].ID == second.Items[0].ID {
			t.Errorf("the second page repeats %s", second.Items[0].ID)
		}
	})

	t.Run("full text", func(t *testing.T) {
		var page itemPageBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items?q=checkout"}), http.StatusOK, &page)
		if len(page.Items) == 0 {
			t.Error("no item matched the text filter")
		}
	})
}

func TestItemGetCarriesRevAndETag(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	resp := send(t, s, request{method: http.MethodGet, target: "/api/v1/items/DEMO-US-0001"})
	var item struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  string `json:"body"`
		Rev   string `json:"rev"`
	}
	decode(t, resp, http.StatusOK, &item)
	if item.ID != "DEMO-US-0001" || item.Title != "Guest checkout" {
		t.Errorf("item = %+v", item)
	}
	if item.Body == "" {
		t.Error("a single item must carry its body")
	}
	if etag := resp.Header().Get("ETag"); etag != `"`+item.Rev+`"` {
		t.Errorf("ETag = %q, rev = %q", etag, item.Rev)
	}
}

func TestItemGetUnknownIsANotFoundProblem(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	resp := send(t, s, request{method: http.MethodGet, target: "/api/v1/items/DEMO-US-9999"})
	var doc problemBody
	decode(t, resp, http.StatusNotFound, &doc)
	if doc.Code != "not_found" {
		t.Errorf("code = %q, want not_found", doc.Code)
	}
	if ct := resp.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("content type = %q", ct)
	}
}

func TestItemCreateWritesTheFile(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)
	resp := send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/items",
		body: map[string]any{
			"project": "DEMO",
			"type":    "task",
			"title":   "Wire the companion API",
			"parent":  "DEMO-US-0001",
			"body":    "## Description\nServe the REST surface.\n",
		},
	})
	var item struct {
		ID   string `json:"id"`
		Path string `json:"path"`
		Rev  string `json:"rev"`
	}
	decode(t, resp, http.StatusCreated, &item)
	if item.ID == "" {
		t.Fatal("the created item has no id")
	}
	if loc := resp.Header().Get("Location"); loc != "/api/v1/items/"+item.ID {
		t.Errorf("Location = %q", loc)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(item.Path)))
	if err != nil {
		t.Fatalf("the item file is not on disk: %v", err)
	}
	if !strings.Contains(string(data), "Wire the companion API") {
		t.Errorf("the file does not hold the title:\n%s", data)
	}
}

func TestItemUpdateHonoursIfMatch(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	rev, _ := getItem(t, s, "DEMO-T-0001")

	t.Run("without If-Match", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPatch,
			target: "/api/v1/items/DEMO-T-0001",
			body:   map[string]any{"priority": "low"},
		}), http.StatusPreconditionRequired, &doc)
		if doc.Code != "precondition_required" {
			t.Errorf("code = %q", doc.Code)
		}
	})

	t.Run("with a stale revision", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPatch,
			target: "/api/v1/items/DEMO-T-0001",
			body:   map[string]any{"priority": "low"},
			header: map[string]string{"If-Match": "sha256:0000000000000000"},
		}), http.StatusPreconditionFailed, &doc)
		if doc.Code != "stale_revision" {
			t.Errorf("code = %q, want stale_revision", doc.Code)
		}
		if doc.CurrentRev != rev {
			t.Errorf("currentRev = %q, want %q", doc.CurrentRev, rev)
		}
		if doc.Path == "" {
			t.Error("the problem does not say which file is stale")
		}
	})

	t.Run("with the current revision", func(t *testing.T) {
		resp := send(t, s, request{
			method: http.MethodPatch,
			target: "/api/v1/items/DEMO-T-0001",
			body:   map[string]any{"priority": "low", "labels": []string{"backend", "api"}},
			header: map[string]string{"If-Match": rev},
		})
		var item struct {
			ID       string   `json:"id"`
			Priority string   `json:"priority"`
			Labels   []string `json:"labels"`
			Rev      string   `json:"rev"`
		}
		decode(t, resp, http.StatusOK, &item)
		if item.Priority != "low" {
			t.Errorf("priority = %q", item.Priority)
		}
		if len(item.Labels) != 2 {
			t.Errorf("labels = %v", item.Labels)
		}
		if item.Rev == rev {
			t.Error("the revision did not change after a write")
		}
	})
}

func TestItemMoveAndDelete(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)

	rev, status := getItem(t, s, "DEMO-US-0001")
	if status != "in_progress" {
		t.Fatalf("fixture status = %q", status)
	}
	var moved struct {
		Status string `json:"status"`
		Rev    string `json:"rev"`
	}
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/items/DEMO-US-0001/move",
		body:   map[string]any{"status": "in_review"},
		header: map[string]string{"If-Match": rev},
	}), http.StatusOK, &moved)
	if moved.Status != "in_review" {
		t.Errorf("status = %q, want in_review", moved.Status)
	}

	t.Run("a forbidden transition is a problem", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/items/DEMO-US-0001/move",
			body:   map[string]any{"status": "backlog"},
			header: map[string]string{"If-Match": moved.Rev},
		}), http.StatusUnprocessableEntity, &doc)
		if doc.Code != "workflow_transition_denied" {
			t.Errorf("code = %q", doc.Code)
		}
	})

	t.Run("delete", func(t *testing.T) {
		rev, _ := getItem(t, s, "DEMO-T-0001")
		resp := send(t, s, request{
			method: http.MethodDelete,
			target: "/api/v1/items/DEMO-T-0001",
			header: map[string]string{"If-Match": rev},
		})
		decode(t, resp, http.StatusNoContent, nil)

		var page itemPageBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items"}), http.StatusOK, &page)
		for _, item := range page.Items {
			if item.ID == "DEMO-T-0001" {
				t.Error("the deleted item is still listed")
			}
		}
	})
}

func TestComments(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)

	var existing []struct {
		Item   string `json:"item"`
		Author string `json:"author"`
		Body   string `json:"body"`
	}
	decode(t, send(t, s, request{
		method: http.MethodGet, target: "/api/v1/items/DEMO-US-0001/comments",
	}), http.StatusOK, &existing)
	if len(existing) != 1 {
		t.Fatalf("fixture comments = %d, want 1", len(existing))
	}

	var added struct {
		Item   string `json:"item"`
		Author string `json:"author"`
		Path   string `json:"path"`
	}
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/items/DEMO-US-0001/comments",
		body:   map[string]any{"body": "Blocked on the sandbox.", "author": "claude"},
	}), http.StatusCreated, &added)
	if added.Author != "claude" {
		t.Errorf("comment = %+v", added)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(added.Path))); err != nil {
		t.Fatalf("the comment file is not on disk: %v", err)
	}

	decode(t, send(t, s, request{
		method: http.MethodGet, target: "/api/v1/items/DEMO-US-0001/comments",
	}), http.StatusOK, &existing)
	if len(existing) != 2 {
		t.Errorf("comments after the write = %d, want 2", len(existing))
	}
}

func TestKnowledgeBase(t *testing.T) {
	t.Parallel()

	s, root := newAPIServer(t)

	t.Run("tree", func(t *testing.T) {
		var tree []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		}
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/projects/DEMO/kb/tree",
		}), http.StatusOK, &tree)
		if len(tree) == 0 {
			t.Fatal("the knowledge base tree is empty")
		}
		var found bool
		for _, node := range tree {
			if node.Path == "docs/index.md" {
				found = true
			}
		}
		if !found {
			t.Errorf("tree = %+v, want docs/index.md", tree)
		}
	})

	var page struct {
		Path string `json:"path"`
		Body string `json:"body"`
		Rev  string `json:"rev"`
	}
	t.Run("page", func(t *testing.T) {
		resp := send(t, s, request{
			method: http.MethodGet,
			target: "/api/v1/projects/DEMO/kb/page?path=docs/index.md&format=raw",
		})
		decode(t, resp, http.StatusOK, &page)
		if !strings.Contains(page.Body, "Demo Shop knowledge base") {
			t.Errorf("body = %q", page.Body)
		}
		if page.Rev == "" || resp.Header().Get("ETag") == "" {
			t.Error("a page must carry a revision and an ETag")
		}
	})

	t.Run("write without If-Match is refused", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPut,
			target: "/api/v1/projects/DEMO/kb/page",
			body:   map[string]any{"path": "docs/index.md", "content": "# Nope\n"},
		}), http.StatusPreconditionRequired, &doc)
		if doc.Code != "precondition_required" {
			t.Errorf("code = %q", doc.Code)
		}
	})

	t.Run("write", func(t *testing.T) {
		var written struct {
			Path string `json:"path"`
			Body string `json:"body"`
			Rev  string `json:"rev"`
		}
		decode(t, send(t, s, request{
			method: http.MethodPut,
			target: "/api/v1/projects/DEMO/kb/page",
			body:   map[string]any{"path": "docs/index.md", "content": "# Demo Shop knowledge base\n\nRewritten.\n"},
			header: map[string]string{"If-Match": page.Rev},
		}), http.StatusOK, &written)
		if written.Rev == page.Rev {
			t.Error("the revision did not change")
		}
		data, err := os.ReadFile(filepath.Join(root, "docs", "index.md"))
		if err != nil {
			t.Fatalf("read the page from disk: %v", err)
		}
		if !strings.Contains(string(data), "Rewritten.") {
			t.Errorf("the page on disk was not updated:\n%s", data)
		}
	})

	t.Run("a new page needs no precondition", func(t *testing.T) {
		decode(t, send(t, s, request{
			method: http.MethodPut,
			target: "/api/v1/projects/DEMO/kb/page",
			body:   map[string]any{"path": "docs/notes/new.md", "content": "# New\n"},
		}), http.StatusOK, nil)
		if _, err := os.Stat(filepath.Join(root, "docs", "notes", "new.md")); err != nil {
			t.Fatalf("the new page is not on disk: %v", err)
		}
	})

	t.Run("the flat route works too", func(t *testing.T) {
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/kb/page?path=docs/architecture/overview.md",
		}), http.StatusOK, nil)
	})
}

func TestSearchAndValidate(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)

	var hits []struct {
		Kind  string  `json:"kind"`
		ID    string  `json:"id"`
		Path  string  `json:"path"`
		Score float64 `json:"score"`
	}
	decode(t, send(t, s, request{
		method: http.MethodGet, target: "/api/v1/search?q=checkout&scope=items,kb&limit=10",
	}), http.StatusOK, &hits)
	if len(hits) == 0 {
		t.Fatal("the search found nothing")
	}

	var diagnostics []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
	}
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/validate",
		body:   map[string]any{"id": "DEMO-US-0001"},
	}), http.StatusOK, &diagnostics)
	if len(diagnostics) != 0 {
		t.Errorf("the fixture story is invalid: %+v", diagnostics)
	}

	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/items/validate",
		body:   map[string]any{"project": "DEMO", "path": "broken.md", "text": "no front matter here"},
	}), http.StatusOK, &diagnostics)
	if len(diagnostics) == 0 {
		t.Error("a file without front matter must produce a diagnostic")
	}
}

func TestReposProjectsAndReindex(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)

	var repos struct {
		Repos []struct {
			Key      string   `json:"key"`
			Path     string   `json:"path"`
			Role     string   `json:"role"`
			Projects []string `json:"projects"`
		} `json:"repos"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/repos"}), http.StatusOK, &repos)
	if len(repos.Repos) != 1 || repos.Repos[0].Key != testRepoID {
		t.Fatalf("repos = %+v", repos)
	}
	if len(repos.Repos[0].Projects) != 1 || repos.Repos[0].Projects[0] != "DEMO" {
		t.Errorf("projects = %v", repos.Repos[0].Projects)
	}

	var projects struct {
		Projects []struct {
			Key      string `json:"key"`
			Name     string `json:"name"`
			DocsPath string `json:"docsPath"`
		} `json:"projects"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/projects"}), http.StatusOK, &projects)
	if len(projects.Projects) != 1 || projects.Projects[0].Key != "DEMO" {
		t.Fatalf("projects = %+v", projects)
	}
	if projects.Projects[0].Name != "Demo Shop" {
		t.Errorf("name = %q", projects.Projects[0].Name)
	}

	var stats struct {
		Items    int `json:"items"`
		Pages    int `json:"pages"`
		Projects int `json:"projects"`
	}
	decode(t, send(t, s, request{
		method: http.MethodPost,
		target: "/api/v1/repos/" + testRepoID + "/reindex",
		body:   map[string]any{"full": true},
	}), http.StatusOK, &stats)
	if stats.Items != 5 || stats.Projects != 1 {
		t.Errorf("stats = %+v", stats)
	}

	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/repos/nope"}), http.StatusNotFound, nil)
}

func TestDeferredRoutesAnswerNotImplemented(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	// Boards are served since GIT-US-0017 and sprints since GIT-US-0018; both
	// are covered by boards_test.go and sprints_test.go.
	for _, target := range []string{
		"/api/v1/retros",
		"/api/v1/sync/status", "/api/v1/git/status?repo=demo",
	} {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodGet, target: target}), http.StatusNotImplemented, &doc)
		if doc.Code != "not_implemented" {
			t.Errorf("%s: code = %q", target, doc.Code)
		}
	}
}

func TestAPIRequiresTheToken(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var doc problemBody
	decode(t, rec, http.StatusUnauthorized, &doc)
	if doc.Code != "unauthorized" {
		t.Errorf("code = %q", doc.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("a 401 must carry WWW-Authenticate")
	}
}

func TestCORSPreflight(t *testing.T) {
	t.Parallel()

	root := copyTree(t, fixtureRoot)
	s, err := New(Options{
		Token: "test-token", Dev: true, Port: 7317,
		Repos: []Repo{{ID: testRepoID, Path: root}},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}

	t.Run("an allowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/items", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("Access-Control-Request-Method", "PATCH")
		req.Header.Set("Access-Control-Request-Headers", "If-Match")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("allow-origin = %q", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "If-Match") {
			t.Errorf("allow-headers = %q", got)
		}
		if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
			t.Errorf("vary = %q", got)
		}
	})

	t.Run("a foreign origin gets nothing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/items", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", "GET")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("allow-origin = %q, want none", got)
		}
	})
}

func TestNoRepositoryMountedIsAProblem(t *testing.T) {
	t.Parallel()

	s, err := New(Options{Token: "test-token"})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	var doc problemBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items"}), http.StatusNotFound, &doc)
	if doc.Code != "repo_not_registered" {
		t.Errorf("code = %q", doc.Code)
	}
}
