package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

// commentResponse is the slice of a created comment these tests read.
type commentResponse struct {
	Author      string `json:"author"`
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	Path        string `json:"path"`
}

func TestCommentAuthorDefaultsToTheGitIdentity(t *testing.T) {
	s, root := newGitServer(t, config.Git{})

	t.Run("no author takes the repository's user.name and user.email", func(t *testing.T) {
		var c commentResponse
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/items/DEMO-US-0001/comments",
			body: map[string]any{"body": "Looks good."},
		}), http.StatusCreated, &c)
		if c.Author != "test-user" || c.AuthorName != "Test User" || c.AuthorEmail != "test@example.com" {
			t.Fatalf("comment = %+v", c)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Path)))
		if err != nil {
			t.Fatalf("read the comment: %v", err)
		}
		for _, want := range []string{"author: test-user", "author_name: Test User", "author_email: test@example.com"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("the file is missing %q:\n%s", want, data)
			}
		}
	})

	t.Run("the legacy placeholder handle is replaced too", func(t *testing.T) {
		var c commentResponse
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/items/DEMO-US-0001/comments",
			body: map[string]any{"body": "Again.", "author": "me"},
		}), http.StatusCreated, &c)
		if c.Author != "test-user" || c.AuthorName != "Test User" {
			t.Errorf("comment = %+v", c)
		}
	})

	t.Run("an explicit author is kept", func(t *testing.T) {
		var c commentResponse
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/items/DEMO-US-0001/comments",
			body: map[string]any{"body": "Mine.", "author": "marta"},
		}), http.StatusCreated, &c)
		if c.Author != "marta" || c.AuthorName != "" {
			t.Errorf("comment = %+v", c)
		}
	})
}

func TestCommentWithoutGitKeepsWhatWasSent(t *testing.T) {
	s, _ := newAPIServer(t)
	var c commentResponse
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/items/DEMO-US-0001/comments",
		body: map[string]any{"body": "No git here."},
	}), http.StatusCreated, &c)
	if c.Author != "unknown" {
		t.Errorf("author = %q, want the fallback handle", c.Author)
	}
}

func TestKBFeedbackEndpoint(t *testing.T) {
	s, root := newGitServer(t, config.Git{})

	var page struct {
		Body string `json:"body"`
		Rev  string `json:"rev"`
	}
	decode(t, send(t, s, request{
		method: http.MethodGet, target: "/api/v1/projects/DEMO/kb/page?path=docs/index.md",
	}), http.StatusOK, &page)

	t.Run("notes are appended under the git identity", func(t *testing.T) {
		var written struct {
			Body string `json:"body"`
			Rev  string `json:"rev"`
		}
		rec := send(t, s, request{
			method: http.MethodPost, target: "/api/v1/projects/DEMO/kb/feedback",
			body: map[string]any{
				"path":  "docs/index.md",
				"notes": []map[string]any{{"startLine": 1, "endLine": 1, "note": "Rename the shop?"}},
			},
			header: map[string]string{"If-Match": page.Rev},
		})
		decode(t, rec, http.StatusOK, &written)
		if rec.Header().Get("ETag") == "" || written.Rev == page.Rev {
			t.Errorf("etag = %q, rev %s -> %s", rec.Header().Get("ETag"), page.Rev, written.Rev)
		}
		data, err := os.ReadFile(filepath.Join(root, "docs", "index.md"))
		if err != nil {
			t.Fatalf("read the page: %v", err)
		}
		for _, want := range []string{
			`author="Test User" email="test@example.com"`,
			"### Test User feedback: fb-",
			"> # Demo Shop knowledge base",
			"Rename the shop?",
		} {
			if !strings.Contains(string(data), want) {
				t.Errorf("the page is missing %q:\n%s", want, data)
			}
		}
	})

	t.Run("a stale If-Match is refused", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/projects/DEMO/kb/feedback",
			body: map[string]any{
				"path":  "docs/index.md",
				"notes": []map[string]any{{"startLine": 1, "endLine": 1, "note": "late"}},
			},
			header: map[string]string{"If-Match": page.Rev},
		}), http.StatusPreconditionFailed, &doc)
	})

	t.Run("notes that cannot be anchored are a bad request", func(t *testing.T) {
		for name, body := range map[string]map[string]any{
			"no notes":     {"path": "docs/index.md"},
			"no path":      {"notes": []map[string]any{{"startLine": 1, "endLine": 1, "note": "n"}}},
			"out of range": {"path": "docs/index.md", "notes": []map[string]any{{"startLine": 1, "endLine": 999, "note": "n"}}},
		} {
			var doc problemBody
			decode(t, send(t, s, request{
				method: http.MethodPost, target: "/api/v1/projects/DEMO/kb/feedback", body: body,
			}), http.StatusBadRequest, &doc)
			if doc.Code != "invalid_request" {
				t.Errorf("%s: code = %q", name, doc.Code)
			}
		}
	})
}
