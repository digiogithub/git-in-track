package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// inboxProjectYAML is a project that declares an inbox: one status in the
// reserved triage category, exactly as the scaffolded default does (ADR-033).
const inboxProjectYAML = `schema: 1
key: INBX
name: Inbox Fixture
timezone: UTC
docs:
  path: docs
workflow:
  initial: backlog
  statuses:
    - { id: triage,      name: Triage,      category: triage }
    - { id: backlog,     name: Backlog,     category: todo }
    - { id: in_progress, name: In Progress, category: in_progress }
    - { id: done,        name: Done,        category: done, terminal: true }
    - { id: cancelled,   name: Cancelled,   category: cancelled, terminal: true }
priorities: [critical, high, medium, low]
`

// inboxTargetStory is the item a duplicate decision can point at.
const inboxTargetStory = `---
id: INBX-US-0001
type: story
title: Existing story
status: backlog
created: 2026-09-01T09:00:00Z
updated: 2026-09-01T09:00:00Z
---

## Description

The item a duplicate submission points at.
`

// newInboxServer mounts a project that has a triage status, so the inbox can be
// exercised end to end over HTTP.
func newInboxServer(t *testing.T) *Server {
	t.Helper()

	root := t.TempDir()
	backlog := filepath.Join(root, "docs", ".pmngr")
	if err := os.MkdirAll(filepath.Join(backlog, "stories"), 0o755); err != nil {
		t.Fatalf("scaffold the fixture: %v", err)
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(filepath.Join(backlog, "project.yaml"), inboxProjectYAML)
	write(filepath.Join(backlog, "stories", "INBX-US-0001-existing.md"), inboxTargetStory)

	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: "inbox", Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return s
}

// inboxPageBody is the documented shape of GET /api/v1/inbox.
type inboxPageBody struct {
	Items []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"items"`
	NextCursor string         `json:"nextCursor"`
	Total      int            `json:"total"`
	Counts     map[string]int `json:"counts"`
	Pending    int            `json:"pending"`
}

// inboxTriageBodyOut is the documented shape of a triage answer.
type inboxTriageBodyOut struct {
	Item struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Rev    string `json:"rev"`
	} `json:"item"`
	Action  string `json:"action"`
	Pending int    `json:"pending"`
}

// submitToInbox files one submission and returns its id and revision.
func submitToInbox(t *testing.T, s *Server, title string) (string, string) {
	t.Helper()

	var created struct {
		ID  string `json:"id"`
		Rev string `json:"rev"`
	}
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/items",
		body: map[string]any{"type": "story", "title": title, "inbox": map[string]any{"source": "web"}},
	}), http.StatusCreated, &created)
	if created.ID == "" || created.Rev == "" {
		t.Fatalf("the create answered %+v", created)
	}
	return created.ID, created.Rev
}

// Verifies: GIT-SP-0004.R5
func TestInboxListing(t *testing.T) {
	t.Parallel()

	s := newInboxServer(t)
	client := newHubClient()
	client.subscribe([]string{eventInboxChanged})
	s.hub.register(client)

	for _, title := range []string{"First report", "Second report", "Third report"} {
		submitToInbox(t, s, title)
	}

	t.Run("a submission announces itself on inbox.changed", func(t *testing.T) {
		select {
		case ev := <-client.events:
			data, ok := ev.Data.(inboxChangedData)
			if !ok || data.Action != "created" || data.Project != "INBX" {
				t.Fatalf("payload = %+v", ev.Data)
			}
			if data.PendingCount == 0 {
				t.Error("the event carries no pending count")
			}
		default:
			t.Fatal("filing an item into the inbox published nothing")
		}
	})

	t.Run("the queue is listed with whole-queue counts", func(t *testing.T) {
		var body inboxPageBody
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/inbox?project=INBX"})
		decode(t, rec, http.StatusOK, &body)
		if body.Total != 3 || len(body.Items) != 3 {
			t.Fatalf("inbox = %d items, total %d", len(body.Items), body.Total)
		}
		if body.Pending != 3 {
			t.Fatalf("pending = %d, want 3", body.Pending)
		}
		if body.Counts["pending"] != 3 {
			t.Fatalf("counts = %+v", body.Counts)
		}
		if rec.Header().Get("X-Total-Count") != "3" {
			t.Errorf("X-Total-Count = %q", rec.Header().Get("X-Total-Count"))
		}
	})

	t.Run("it pages", func(t *testing.T) {
		var first inboxPageBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/inbox?project=INBX&limit=2"}),
			http.StatusOK, &first)
		if len(first.Items) != 2 || first.NextCursor == "" {
			t.Fatalf("first page = %d items, cursor %q", len(first.Items), first.NextCursor)
		}
		var second inboxPageBody
		decode(t, send(t, s, request{
			method: http.MethodGet,
			target: "/api/v1/inbox?project=INBX&limit=2&cursor=" + first.NextCursor,
		}), http.StatusOK, &second)
		if len(second.Items) != 1 {
			t.Fatalf("second page = %d items, want 1", len(second.Items))
		}
	})

	t.Run("a cursor refuses a changed filter", func(t *testing.T) {
		var first inboxPageBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/inbox?project=INBX&limit=2"}),
			http.StatusOK, &first)
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodGet,
			target: "/api/v1/inbox?project=INBX&limit=2&status=rejected&cursor=" + first.NextCursor,
		}), http.StatusBadRequest, &doc)
		if doc.Code != "invalid_cursor" {
			t.Fatalf("code = %q, want invalid_cursor", doc.Code)
		}
	})

	t.Run("an unknown triage state is refused", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/inbox?project=INBX&status=maybe"}),
			http.StatusBadRequest, &doc)
		if doc.Code != codeInvalidRequest {
			t.Fatalf("code = %q", doc.Code)
		}
	})
}

func TestInboxTriage(t *testing.T) {
	t.Parallel()

	s := newInboxServer(t)
	id, rev := submitToInbox(t, s, "Something is broken")

	t.Run("a triage without If-Match is refused", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/items/" + id + "/triage",
			body: map[string]any{"action": "accept"},
		}), http.StatusPreconditionRequired, &doc)
		if doc.Code != codePreconditionRequired {
			t.Fatalf("code = %q", doc.Code)
		}
	})

	t.Run("a stale revision is refused with the current one", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/items/" + id + "/triage",
			body:   map[string]any{"action": "accept"},
			header: map[string]string{"If-Match": "sha256:0000000000000000"},
		}), http.StatusPreconditionFailed, &doc)
		if doc.Code != "stale_revision" || doc.CurrentRev == "" {
			t.Fatalf("problem = %+v", doc)
		}
	})

	t.Run("accepting moves the item out of the queue and publishes", func(t *testing.T) {
		client := newHubClient()
		client.subscribe([]string{eventInboxChanged, eventItemChanged})
		s.hub.register(client)

		var out inboxTriageBodyOut
		rec := send(t, s, request{
			method: http.MethodPost, target: "/api/v1/items/" + id + "/triage",
			body:   map[string]any{"action": "accept"},
			header: map[string]string{"If-Match": rev},
		})
		decode(t, rec, http.StatusOK, &out)
		if out.Action != "accept" || out.Item.Status == "triage" {
			t.Fatalf("triage = %+v", out)
		}
		if rec.Header().Get("ETag") == "" {
			t.Error("a triage answer carries the new revision as an ETag")
		}

		seen := map[string]bool{}
		for len(seen) < 2 {
			select {
			case ev := <-client.events:
				seen[ev.Type] = true
			default:
				t.Fatalf("topics published = %v, want item.changed and inbox.changed", seen)
			}
		}
	})
}

func TestInboxTriageAcceptsTheWildcardPrecondition(t *testing.T) {
	t.Parallel()

	s := newInboxServer(t)
	id, _ := submitToInbox(t, s, "Unconditional")

	// If-Match: * must reach the vault as an empty revision, never as the
	// literal star, which the core would read as a revision that cannot match.
	var out inboxTriageBodyOut
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/items/" + id + "/triage",
		body:   map[string]any{"action": "reject"},
		header: map[string]string{"If-Match": "*"},
	}), http.StatusOK, &out)
	if out.Action != "reject" {
		t.Fatalf("triage = %+v", out)
	}
}

func TestInboxIsAbsentFromAProjectWithNoTriageStatus(t *testing.T) {
	t.Parallel()

	// The DEMO fixture declares no status in the triage category. Listing its
	// inbox is not an error — the queue is simply empty — but filing something
	// into it is, and the refusal is a conflict the caller fixes in
	// project.yaml rather than by retrying (ADR-033).
	s, _ := newAPIServer(t)
	var empty inboxPageBody
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/inbox?project=DEMO"}),
		http.StatusOK, &empty)
	if empty.Total != 0 || empty.Pending != 0 {
		t.Fatalf("a project with no triage status has an empty queue: %+v", empty)
	}

	var doc problemBody
	decode(t, send(t, s, request{
		method: http.MethodPost, target: "/api/v1/items",
		body: map[string]any{"type": "story", "title": "Submitted", "inbox": map[string]any{"source": "web"}},
	}), http.StatusConflict, &doc)
	if doc.Code != "no_triage_status" {
		t.Fatalf("code = %q, want no_triage_status", doc.Code)
	}
}
