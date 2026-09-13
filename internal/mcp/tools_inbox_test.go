package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/vault"
)

// inboxFixture is a project that declares a triage status, which is what gives
// a project an inbox. The shared project fixture deliberately declares none.
const inboxFixture = "testdata/project-inbox"

// newInboxHarness opens the team fixture alongside a project that has an inbox.
func newInboxHarness(t *testing.T, allowWrite bool) *harness {
	t.Helper()
	h := newHarnessWith(t, allowWrite, []mountSpec{
		{"demo-team", vault.RoleTeam, teamFixture},
		{"inbox", vault.RoleProject, inboxFixture},
	})
	h.space.SetClock(func() time.Time { return time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC) })
	return h
}

// submitInbox files one entry into the triage queue and returns it.
func submitInbox(t *testing.T, h *harness, title string) WriteResult {
	t.Helper()
	return call[WriteResult](t, h, "create_inbox_item", map[string]any{
		"project": "INBX", "title": title, "body": "Reported by a customer.",
	})
}

func TestInboxToolsAreWriteGated(t *testing.T) {
	h := newInboxHarness(t, false)
	advertised := map[string]bool{}
	for _, name := range h.server.Tools() {
		advertised[name] = true
	}
	for _, name := range []string{"create_inbox_item", "triage_inbox_item"} {
		if advertised[name] {
			t.Errorf("%s is advertised by a read-only server", name)
		}
	}
	if !advertised["list_inbox"] {
		t.Error("list_inbox reads and must be advertised everywhere")
	}
	if got := call[InboxPage](t, h, "list_inbox", map[string]any{"project": "INBX"}); got.Total != 0 {
		t.Errorf("the queue starts empty: %+v", got)
	}
}

func TestCreateInboxItem(t *testing.T) {
	h := newInboxHarness(t, true)

	t.Run("a submission lands in triage, pending", func(t *testing.T) {
		out := submitInbox(t, h, "Checkout hangs on Safari")
		if out.Item.ID == "" || out.Item.Rev == "" {
			t.Fatalf("every result carries id and rev: %+v", out.Item)
		}
		if out.Item.Status != "triage" {
			t.Errorf("status = %q, want triage", out.Item.Status)
		}
		if !containsPath(out.Changed, out.Item.ID) {
			t.Errorf("changed = %v, want the new item's file", out.Changed)
		}
		if len(h.writes) == 0 || h.writes[len(h.writes)-1].Tool != "create_inbox_item" {
			t.Errorf("the write was not announced to the host: %+v", h.writes)
		}
	})

	t.Run("a project with no inbox says so", func(t *testing.T) {
		plain := newHarness(t, true)
		got := callFails(t, plain, "create_inbox_item",
			map[string]any{"project": "DEMO", "title": "Anything"})
		if got.Code != vault.NoTriageStatusCode {
			t.Errorf("code = %q, want %s", got.Code, vault.NoTriageStatusCode)
		}
	})

	t.Run("a submission needs a title", func(t *testing.T) {
		got := callFails(t, h, "create_inbox_item",
			map[string]any{"project": "INBX", "title": "   "})
		if got.Code != codeInvalidRequest || got.Field != "title" {
			t.Errorf("error = %+v", got)
		}
	})
}

func TestListInbox(t *testing.T) {
	h := newInboxHarness(t, true)
	first := submitInbox(t, h, "Checkout hangs on Safari")
	second := submitInbox(t, h, "The invoice PDF is blank")

	call[TriageResult](t, h, "triage_inbox_item", map[string]any{
		"id": second.Item.ID, "rev": second.Item.Rev,
		"action": "snooze", "snoozedUntil": "2026-12-01",
	})

	page := call[InboxPage](t, h, "list_inbox", map[string]any{"project": "INBX"})
	if page.Total != 2 || page.Pending != 1 {
		t.Fatalf("page = %+v", page)
	}
	if page.Counts["snoozed"] != 1 {
		t.Errorf("counts = %+v", page.Counts)
	}
	for _, it := range page.Items {
		if it.ID == "" || it.Rev == "" {
			t.Errorf("every entry carries id and rev: %+v", it)
		}
		if it.Body != "" {
			t.Errorf("a listing never returns bodies: %+v", it)
		}
	}

	t.Run("the triage state narrows the queue", func(t *testing.T) {
		only := call[InboxPage](t, h, "list_inbox",
			map[string]any{"project": "INBX", "status": []string{"pending"}})
		if only.Total != 1 || only.Items[0].ID != first.Item.ID {
			t.Fatalf("pending page = %+v", only.Items)
		}
	})

	t.Run("the result is marked as untrusted repository content", func(t *testing.T) {
		res := rawCall(t, h, "list_inbox", map[string]any{"project": "INBX"})
		if res.Meta[untrustedMeta] != untrustedValue {
			t.Errorf("meta = %+v, want the untrusted marker", res.Meta)
		}
	})
}

func TestTriageInboxItem(t *testing.T) {
	tests := []struct {
		name  string
		args  func(out WriteResult) map[string]any
		check func(t *testing.T, h *harness, got TriageResult)
	}{
		{
			name: "accept",
			args: func(out WriteResult) map[string]any {
				return map[string]any{"id": out.Item.ID, "rev": out.Item.Rev, "action": "accept"}
			},
			check: func(t *testing.T, _ *harness, got TriageResult) {
				if got.Item.Status != "backlog" || got.Action != "accept" {
					t.Fatalf("result = %+v", got)
				}
				if got.Pending != 0 {
					t.Errorf("pending = %d, want 0", got.Pending)
				}
			},
		},
		{
			name: "reject",
			args: func(out WriteResult) map[string]any {
				return map[string]any{"id": out.Item.ID, "rev": out.Item.Rev, "action": "reject"}
			},
			check: func(t *testing.T, _ *harness, got TriageResult) {
				if got.Item.Status != "cancelled" {
					t.Fatalf("status = %q, want cancelled", got.Item.Status)
				}
			},
		},
		{
			name: "snooze",
			args: func(out WriteResult) map[string]any {
				return map[string]any{
					"id": out.Item.ID, "rev": out.Item.Rev,
					"action": "snooze", "snoozedUntil": "2026-12-01",
				}
			},
			check: func(t *testing.T, _ *harness, got TriageResult) {
				if got.Item.Status != "triage" {
					t.Errorf("a snooze leaves the status alone: %q", got.Item.Status)
				}
				if got.Pending != 0 {
					t.Errorf("pending = %d, want the snoozed entry out of the queue", got.Pending)
				}
			},
		},
		{
			name: "duplicate",
			args: func(out WriteResult) map[string]any {
				return map[string]any{
					"id": out.Item.ID, "rev": out.Item.Rev,
					"action": "duplicate", "duplicateOf": "INBX-US-0001",
				}
			},
			check: func(t *testing.T, h *harness, got TriageResult) {
				if !containsPath(got.Changed, got.Item.ID) ||
					!containsPath(got.Changed, "INBX-US-0001") {
					t.Fatalf("changed = %v, want both the entry and its target", got.Changed)
				}
				target := call[ItemResult](t, h, "get_item",
					map[string]any{"id": "INBX-US-0001", "fields": []string{"links"}})
				found := false
				for _, l := range target.Item.Links {
					if l.Kind == "duplicated_by" {
						found = true
					}
				}
				if !found {
					t.Errorf("the inverse link is missing: %+v", target.Item.Links)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newInboxHarness(t, true)
			out := submitInbox(t, h, "Checkout hangs on Safari")
			got := call[TriageResult](t, h, "triage_inbox_item", tt.args(out))
			if got.Item.ID != out.Item.ID {
				t.Fatalf("item = %+v", got.Item)
			}
			tt.check(t, h, got)
		})
	}

	t.Run("a write with no rev is refused before anything is written", func(t *testing.T) {
		h := newInboxHarness(t, true)
		out := submitInbox(t, h, "Checkout hangs on Safari")
		got := callFails(t, h, "triage_inbox_item",
			map[string]any{"id": out.Item.ID, "rev": "", "action": "accept"})
		if got.Code != codePreconditionRequired {
			t.Errorf("code = %q, want %s", got.Code, codePreconditionRequired)
		}
	})

	t.Run("a stale rev carries the current one and the retry protocol", func(t *testing.T) {
		h := newInboxHarness(t, true)
		out := submitInbox(t, h, "Checkout hangs on Safari")
		got := callFails(t, h, "triage_inbox_item", map[string]any{
			"id": out.Item.ID, "rev": "sha256:0000000000000000", "action": "accept",
		})
		if got.Code != "stale_revision" || got.CurrentRev == "" || got.Retry == "" {
			t.Fatalf("error = %+v", got)
		}
	})

	t.Run("the wildcard rev still waives the lock", func(t *testing.T) {
		h := newInboxHarness(t, true)
		out := submitInbox(t, h, "Checkout hangs on Safari")
		got := call[TriageResult](t, h, "triage_inbox_item",
			map[string]any{"id": out.Item.ID, "rev": "*", "action": "accept"})
		if got.Item.Status != "backlog" {
			t.Errorf("status = %q", got.Item.Status)
		}
	})
}

// containsPath reports whether a list of written paths names one item.
func containsPath(paths []string, id string) bool {
	for _, p := range paths {
		if strings.Contains(p, id) {
			return true
		}
	}
	return false
}
