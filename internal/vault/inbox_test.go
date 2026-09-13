package vault

import (
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// inboxProjectYAML is a project that declares an inbox: one status in the
// reserved triage category, and a workflow that does not declare a transition
// out of it, exactly as the scaffolded default does.
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

// inboxParentEpic is the epic an accepted submission can be filed under.
const inboxParentEpic = `---
id: INBX-EP-0001
type: epic
title: Existing epic
status: backlog
created: 2026-09-01T09:00:00Z
updated: 2026-09-01T09:00:00Z
---

## Description

The epic an accepted submission is filed under.
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

// inboxClock is the instant every test in this file reads as "now".
var inboxClock = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

// inboxVault returns an in-memory vault holding the inbox fixture and a pinned
// clock, so that snooze expiry is decided by the test and not by the wall.
func inboxVault(t *testing.T) *Vault {
	t.Helper()
	v := NewInMemory()
	v.SetClock(func() time.Time { return inboxClock })
	call(t, v, "vault.load", map[string]any{"files": []map[string]string{
		{"path": "docs/.pmngr/project.yaml", "text": inboxProjectYAML},
		{"path": "docs/.pmngr/epics/INBX-EP-0001-existing.md", "text": inboxParentEpic},
		{"path": "docs/.pmngr/stories/INBX-US-0001-existing.md", "text": inboxTargetStory},
	}})
	return v
}

// submit files one item into the inbox and returns it.
func submit(t *testing.T, v *Vault, title, source string) core.Item {
	t.Helper()
	result := decode[struct {
		Item core.Item `json:"item"`
	}](t, call(t, v, "item.create", map[string]any{
		"type": "story", "title": title,
		"inbox": map[string]any{"source": source},
	}))
	return result.Item
}

// triage applies one decision and returns the result.
func triage(t *testing.T, v *Vault, params map[string]any) InboxTriageResult {
	t.Helper()
	return decode[InboxTriageResult](t, call(t, v, "inbox.triage", params))
}

func TestInboxCreate(t *testing.T) {
	t.Run("the inbox option forces the triage status and stamps the submission", func(t *testing.T) {
		v := inboxVault(t)
		it := submit(t, v, "The checkout page hangs on Safari", "web")

		if it.Status != "triage" {
			t.Errorf("status = %q, want triage", it.Status)
		}
		if it.Inbox == nil {
			t.Fatal("the item carries no inbox block")
		}
		if it.Inbox.Status != core.InboxPending {
			t.Errorf("inbox.status = %q, want pending", it.Inbox.Status)
		}
		if it.Inbox.Source != "web" {
			t.Errorf("inbox.source = %q, want web", it.Inbox.Source)
		}
		if !it.Inbox.Received.Equal(inboxClock) {
			t.Errorf("inbox.received = %s, want the vault clock", it.Inbox.Received)
		}
	})

	t.Run("without the option nothing about item.create changes", func(t *testing.T) {
		v := inboxVault(t)
		result := decode[struct {
			Item core.Item `json:"item"`
		}](t, call(t, v, "item.create", map[string]any{"type": "story", "title": "Ordinary story"}))
		if result.Item.Status != "backlog" {
			t.Errorf("status = %q, want the initial status", result.Item.Status)
		}
		if result.Item.Inbox != nil {
			t.Errorf("inbox block = %+v, want none", result.Item.Inbox)
		}
	})

	t.Run("a project with no triage status has no inbox and says so", func(t *testing.T) {
		v, _ := loadedVault(t)
		env := rawCall(t, v, "item.create", map[string]any{
			"type": "story", "title": "Submitted", "inbox": map[string]any{"source": "web"},
		})
		if env.OK {
			t.Fatal("the DEMO fixture declares no triage status")
		}
		if env.Error.Code != NoTriageStatusCode {
			t.Errorf("code = %q, want %s", env.Error.Code, NoTriageStatusCode)
		}
	})
}

func TestInboxList(t *testing.T) {
	v := inboxVault(t)
	pending := submit(t, v, "Pending submission", "web")
	snoozed := submit(t, v, "Snoozed submission", "mcp")

	triage(t, v, map[string]any{
		"id": snoozed.ID, "rev": snoozed.Rev, "action": "snooze", "snoozedUntil": "2026-09-20",
	})

	t.Run("the queue holds only triage items and counts every state", func(t *testing.T) {
		page := decode[InboxPage](t, call(t, v, "inbox.list", nil))
		if page.Total != 2 {
			t.Fatalf("total = %d, want the two submissions: %+v", page.Total, page.Items)
		}
		for _, it := range page.Items {
			if it.ID == "INBX-US-0001" {
				t.Fatalf("the ordinary story leaked into the inbox: %+v", page.Items)
			}
		}
		if page.Pending != 1 || page.Counts[core.InboxSnoozed] != 1 {
			t.Errorf("counts = %+v, pending = %d", page.Counts, page.Pending)
		}
	})

	t.Run("the state filter narrows the queue", func(t *testing.T) {
		page := decode[InboxPage](t, call(t, v, "inbox.list", map[string]any{"status": "pending"}))
		if page.Total != 1 || page.Items[0].ID != pending.ID {
			t.Fatalf("pending page = %+v", page.Items)
		}
	})

	t.Run("an unknown state is refused", func(t *testing.T) {
		env := rawCall(t, v, "inbox.list", map[string]any{"status": "later"})
		if env.OK || env.Error.Code != "invalid_request" {
			t.Fatalf("envelope = %+v", env)
		}
	})

	t.Run("a snooze expires at query time, with no scheduler", func(t *testing.T) {
		v.SetClock(func() time.Time { return time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC) })
		defer v.SetClock(func() time.Time { return inboxClock })

		page := decode[InboxPage](t, call(t, v, "inbox.list", map[string]any{"status": "pending"}))
		if page.Total != 2 {
			t.Fatalf("an expired snooze reads as pending again: %+v", page.Items)
		}
		if page.Counts[core.InboxSnoozed] != 0 {
			t.Errorf("snoozed count = %d, want 0 after expiry", page.Counts[core.InboxSnoozed])
		}
	})

	t.Run("the workspace routes the listing to the repository that holds it", func(t *testing.T) {
		w := NewWorkspace()
		if _, err := w.Attach("inbox", RoleProject, v); err != nil {
			t.Fatalf("attach: %v", err)
		}
		page := decode[InboxPage](t, wsCall(t, w, "inbox.list", map[string]any{"project": "INBX"}))
		if page.Total != 2 {
			t.Fatalf("total = %d over the workspace", page.Total)
		}
	})
}

func TestInboxTriageActions(t *testing.T) {
	tests := []struct {
		name   string
		params func(it core.Item) map[string]any
		check  func(t *testing.T, v *Vault, out InboxTriageResult)
	}{
		{
			name: "accept moves the item into the ordinary workflow",
			params: func(it core.Item) map[string]any {
				return map[string]any{"id": it.ID, "rev": it.Rev, "action": "accept"}
			},
			check: func(t *testing.T, _ *Vault, out InboxTriageResult) {
				if out.Item.Status != "backlog" {
					t.Errorf("status = %q, want the initial status", out.Item.Status)
				}
				if out.Item.Inbox == nil || out.Item.Inbox.Status != core.InboxAccepted {
					t.Fatalf("inbox block = %+v", out.Item.Inbox)
				}
				if out.Item.Inbox.Source != "web" {
					t.Errorf("source = %q, want the arrival facts to survive", out.Item.Inbox.Source)
				}
				if out.Pending != 0 {
					t.Errorf("pending = %d, want 0", out.Pending)
				}
			},
		},
		{
			name: "accept applies the chosen status and parent in the same write",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": it.Rev, "action": "accept",
					"status": "in_progress", "parent": "INBX-EP-0001",
				}
			},
			check: func(t *testing.T, _ *Vault, out InboxTriageResult) {
				if out.Item.Status != "in_progress" || out.Item.Parent != "INBX-EP-0001" {
					t.Fatalf("item = %+v", out.Item)
				}
				if out.Item.Started.IsZero() {
					t.Error("entering in_progress must stamp started")
				}
				if len(out.Writes.Written) != 1 {
					t.Errorf("writes = %d, want one file", len(out.Writes.Written))
				}
			},
		},
		{
			name: "reject cancels the item without deleting it",
			params: func(it core.Item) map[string]any {
				return map[string]any{"id": it.ID, "rev": it.Rev, "action": "reject"}
			},
			check: func(t *testing.T, _ *Vault, out InboxTriageResult) {
				if out.Item.Status != "cancelled" || out.Item.Deleted {
					t.Fatalf("item = %+v", out.Item)
				}
				if out.Item.Inbox.Status != core.InboxRejected {
					t.Errorf("inbox.status = %q, want rejected", out.Item.Inbox.Status)
				}
			},
		},
		{
			name: "snooze records the date and leaves the status alone",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": it.Rev, "action": "snooze", "snoozedUntil": "2026-10-01",
				}
			},
			check: func(t *testing.T, _ *Vault, out InboxTriageResult) {
				if out.Item.Status != "triage" {
					t.Errorf("status = %q, want the item to stay in triage", out.Item.Status)
				}
				if out.Item.Inbox.Status != core.InboxSnoozed ||
					out.Item.Inbox.SnoozedUntil.String() != "2026-10-01" {
					t.Fatalf("inbox block = %+v", out.Item.Inbox)
				}
			},
		},
		{
			name: "duplicate records the link and its inverse on the target",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": it.Rev, "action": "duplicate", "duplicateOf": "INBX-US-0001",
				}
			},
			check: func(t *testing.T, v *Vault, out InboxTriageResult) {
				if out.Item.Inbox.DuplicateOf != "INBX-US-0001" {
					t.Fatalf("inbox block = %+v", out.Item.Inbox)
				}
				if !hasLink(out.Item.Links, core.LinkDuplicates, "INBX-US-0001") {
					t.Errorf("links = %+v, want a duplicates link", out.Item.Links)
				}
				target := decode[core.Item](t, call(t, v, "item.get", map[string]any{"id": "INBX-US-0001"}))
				if !hasLink(target.Links, core.LinkDuplicatedBy, string(out.Item.ID)) {
					t.Errorf("target links = %+v, want the duplicated_by inverse", target.Links)
				}
				if len(out.Writes.Written) != 2 {
					t.Errorf("writes = %d, want both files in one WriteSet", len(out.Writes.Written))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := inboxVault(t)
			it := submit(t, v, "The checkout page hangs on Safari", "web")
			tt.check(t, v, triage(t, v, tt.params(it)))
		})
	}
}

func TestInboxTriageRefusals(t *testing.T) {
	tests := []struct {
		name   string
		params func(it core.Item) map[string]any
		want   string
	}{
		{
			name: "a stale rev is refused",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": "sha256:0000000000000000", "action": "accept",
				}
			},
			want: core.StaleRevisionCode,
		},
		{
			name: "an unknown action is refused",
			params: func(it core.Item) map[string]any {
				return map[string]any{"id": it.ID, "rev": it.Rev, "action": "defer"}
			},
			want: "invalid_request",
		},
		{
			name: "accepting into a triage status is refused",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": it.Rev, "action": "accept", "status": "triage",
				}
			},
			want: "invalid_request",
		},
		{
			name: "changing the type is refused: an id pins it for life",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": it.Rev, "action": "accept", "type": "task",
				}
			},
			want: "invalid_request",
		},
		{
			name: "snoozing with no date is refused",
			params: func(it core.Item) map[string]any {
				return map[string]any{"id": it.ID, "rev": it.Rev, "action": "snooze"}
			},
			want: "invalid_request",
		},
		{
			name: "a duplicate of an unknown item is refused",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": it.Rev, "action": "duplicate", "duplicateOf": "INBX-US-0404",
				}
			},
			want: "not_found",
		},
		{
			name: "an item cannot be a duplicate of itself",
			params: func(it core.Item) map[string]any {
				return map[string]any{
					"id": it.ID, "rev": it.Rev, "action": "duplicate", "duplicateOf": string(it.ID),
				}
			},
			want: "invalid_request",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := inboxVault(t)
			it := submit(t, v, "The checkout page hangs on Safari", "web")
			env := rawCall(t, v, "inbox.triage", tt.params(it))
			if env.OK {
				t.Fatal("the call was expected to fail")
			}
			if env.Error.Code != tt.want {
				t.Errorf("code = %q, want %q (%s)", env.Error.Code, tt.want, env.Error.Message)
			}
			after := decode[core.Item](t, call(t, v, "item.get", map[string]any{"id": it.ID}))
			if after.Rev != it.Rev {
				t.Errorf("a refused triage wrote the file: rev %s -> %s", it.Rev, after.Rev)
			}
		})
	}

	t.Run("an empty rev writes unconditionally, as every other vault write does", func(t *testing.T) {
		v := inboxVault(t)
		it := submit(t, v, "The checkout page hangs on Safari", "web")
		out := triage(t, v, map[string]any{"id": it.ID, "action": "accept"})
		if out.Item.Status != "backlog" {
			t.Errorf("status = %q", out.Item.Status)
		}
	})
}

// hasLink reports whether a link list carries one relation.
func hasLink(links []core.Link, kind core.LinkKind, target string) bool {
	for _, l := range links {
		if l.Kind == kind && l.Target == target {
			return true
		}
	}
	return false
}
