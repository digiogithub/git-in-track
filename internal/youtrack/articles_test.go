package youtrack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestArticleDecodes(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "article.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	article, err := client.Article(context.Background(), "ACME-A-3")
	if err != nil {
		t.Fatalf("Article: %v", err)
	}
	req := rec.all()[0]
	if req.URL.Path != "/api/articles/ACME-A-3" {
		t.Fatalf("path = %q", req.URL.Path)
	}
	if got := req.URL.Query().Get("fields"); got != ArticleFields {
		t.Fatalf("fields = %q", got)
	}
	if article.ID != "42-7" || article.IDReadable != "ACME-A-3" {
		t.Fatalf("ids = %q / %q", article.ID, article.IDReadable)
	}
	// The body of an article is "content", not "description" as on an issue,
	// and it is returned verbatim: untrusted Markdown the caller must sanitize.
	if !strings.HasPrefix(article.Content, "## Prerequisites") {
		t.Fatalf("content = %q", article.Content)
	}
	if article.ParentArticle.IDReadable != "ACME-A-1" || !article.HasChildren {
		t.Fatalf("article = %+v", article)
	}
	if article.Project.ShortName != "ACME" || article.Ordinal != 2 {
		t.Fatalf("article = %+v", article)
	}
}

func TestCreateArticle(t *testing.T) {
	var payload map[string]any
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		writeJSON(t, w, "article.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	article, err := client.CreateArticle(context.Background(), ArticleInput{
		ProjectID: "0-1",
		Summary:   ptr("Deployment handbook"),
		Content:   ptr("## Prerequisites"),
	})
	if err != nil {
		t.Fatalf("CreateArticle: %v", err)
	}
	req := rec.all()[0]
	if req.Method != http.MethodPost || req.URL.Path != "/api/articles" {
		t.Fatalf("%s %s", req.Method, req.URL.Path)
	}
	project, ok := payload["project"].(map[string]any)
	if !ok || project["id"] != "0-1" {
		t.Fatalf("project = %v, want {\"id\":\"0-1\"}", payload["project"])
	}
	if payload["summary"] != "Deployment handbook" || payload["content"] != "## Prerequisites" {
		t.Fatalf("payload = %v", payload)
	}
	if article.IDReadable != "ACME-A-3" {
		t.Fatalf("article = %+v", article)
	}
}

func TestCreateArticleValidatesBeforeAnyRequest(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should have been made")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	tests := []struct {
		name string
		in   ArticleInput
	}{
		{"no project", ArticleInput{Summary: ptr("Title")}},
		{"no summary", ArticleInput{ProjectID: "0-1"}},
		{"empty summary", ArticleInput{ProjectID: "0-1", Summary: ptr("")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := client.CreateArticle(context.Background(), tc.in); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}
	if rec.count() != 0 {
		t.Fatalf("%d requests were made, want none", rec.count())
	}
}

// TestUpdateArticleSendsOnlyWhatWasSet pins the partial-merge contract:
// YouTrack has no PUT, an update is a POST carrying only the changed keys.
func TestUpdateArticleSendsOnlyWhatWasSet(t *testing.T) {
	tests := []struct {
		name     string
		in       ArticleInput
		wantKeys []string
		check    func(t *testing.T, payload map[string]any)
	}{
		{
			name:     "content only",
			in:       ArticleInput{Content: ptr("new body")},
			wantKeys: []string{"content"},
		},
		{
			name:     "summary and content",
			in:       ArticleInput{Summary: ptr("New title"), Content: ptr("new body")},
			wantKeys: []string{"summary", "content"},
		},
		{
			name:     "re-parent",
			in:       ArticleInput{ParentArticleID: ptr("42-1")},
			wantKeys: []string{"parentArticle"},
			check: func(t *testing.T, payload map[string]any) {
				parent, ok := payload["parentArticle"].(map[string]any)
				if !ok || parent["id"] != "42-1" {
					t.Fatalf("parentArticle = %v", payload["parentArticle"])
				}
			},
		},
		{
			name:     "detach from the parent",
			in:       ArticleInput{ParentArticleID: ptr("")},
			wantKeys: []string{"parentArticle"},
			check: func(t *testing.T, payload map[string]any) {
				if payload["parentArticle"] != nil {
					t.Fatalf("parentArticle = %v, want null", payload["parentArticle"])
				}
			},
		},
		{
			name: "the project is never sent on an update",
			// The project of an article is read-only after creation.
			in:       ArticleInput{ProjectID: "0-9", Content: ptr("new body")},
			wantKeys: []string{"content"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Errorf("body is not JSON: %v", err)
				}
				writeJSON(t, w, "article.json")
			})
			client := newTestClient(t, srv.URL, newFakeClock(), nil)

			if _, err := client.UpdateArticle(context.Background(), "ACME-A-3", tc.in); err != nil {
				t.Fatalf("UpdateArticle: %v", err)
			}
			req := rec.all()[0]
			if req.Method != http.MethodPost || req.URL.Path != "/api/articles/ACME-A-3" {
				t.Fatalf("%s %s", req.Method, req.URL.Path)
			}
			if len(payload) != len(tc.wantKeys) {
				t.Fatalf("payload = %v, want exactly the keys %v", payload, tc.wantKeys)
			}
			for _, key := range tc.wantKeys {
				if _, ok := payload[key]; !ok {
					t.Fatalf("payload = %v, missing %q", payload, key)
				}
			}
			if tc.check != nil {
				tc.check(t, payload)
			}
		})
	}
}

func TestUpdateArticleNeedsAtLeastOneField(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should have been made")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	if _, err := client.UpdateArticle(context.Background(), "ACME-A-3", ArticleInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput, got %v", err)
	}
	if rec.count() != 0 {
		t.Fatalf("%d requests were made, want none", rec.count())
	}
}

// TestChildArticlesWalksTheTreeDownwards covers the sub-resource an article
// itself cannot provide: an article carries its parent, never its children.
func TestChildArticlesWalksTheTreeDownwards(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/articles/ACME-A-3/childArticles":
			writeJSON(t, w, "child_articles.json")
		case "/api/articles/ACME-A-5/childArticles":
			_, _ = io.WriteString(w, "[]")
		case "/api/articles/ACME-A-4/childArticles":
			_, _ = io.WriteString(w, "[]")
		default:
			http.NotFound(w, r)
		}
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	// Walk the tree breadth first, the way a knowledge-base sync will.
	visited := []string{}
	queue := []string{"ACME-A-3"}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		children, err := client.ChildArticles(context.Background(), id)
		if err != nil {
			t.Fatalf("ChildArticles(%s): %v", id, err)
		}
		for _, child := range children {
			visited = append(visited, child.IDReadable)
			queue = append(queue, child.IDReadable)
		}
	}
	if strings.Join(visited, ",") != "ACME-A-4,ACME-A-5" {
		t.Fatalf("visited = %v", visited)
	}
	if rec.count() != 3 {
		t.Fatalf("%d requests, want 3", rec.count())
	}
	if got := rec.all()[0].URL.Query().Get("fields"); got != ChildArticleFields {
		t.Fatalf("fields = %q", got)
	}
}

func TestSearchArticlesIsOrdered(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "[]")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	if _, err := client.SearchArticles(context.Background(), "project: {ACME}", Page{}); err != nil {
		t.Fatalf("SearchArticles: %v", err)
	}
	query := rec.all()[0].URL.Query().Get("query")
	if !strings.Contains(strings.ToLower(query), "order by:") {
		t.Fatalf("query = %q: article paging needs an ordering just as much as issue paging", query)
	}
}
