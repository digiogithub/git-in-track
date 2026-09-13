package vault

import (
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The vault half of GIT-US-0071: the exclusion of triage work is a filter over
// the index, not a second corpus, so the very write that accepts an item must
// be enough to put it on the board. No rebuild, no reload, no second pass.
//
// This runs through the workspace rather than a single vault because a board
// lives in the team repository and the items live in the project one, which is
// exactly the arrangement the guarantee has to hold in.

// inboxTeamYAML is a team that owns the inbox fixture project.
const inboxTeamYAML = `schema: 1
key: INBX-TEAM
name: Inbox Team
timezone: UTC
knowledge:
  path: knowledge
defaults:
  board: delivery
members:
  - handle: jose
    name: Jose Ruiz
    role: lead
    active: true
projects:
  - key: INBX
    name: Inbox Fixture
    repo: https://example.com/inbox.git
    default_branch: main
    docs_path: docs
`

// inboxTeamBoard is a board whose columns cover every category the fixture
// workflow declares — including, deliberately, a column that asks for the
// triage status by name. A board must not be able to opt back into the inbox.
const inboxTeamBoard = `---
id: delivery
type: board
kind: kanban
title: Delivery
projects: [INBX]
columns:
  - id: inbox
    name: Inbox
    statuses:
      "*": [triage]
  - id: todo
    name: To Do
    categories: [todo]
  - id: in_progress
    name: In Progress
    categories: [in_progress]
  - id: done
    name: Done
    categories: [done, cancelled]
created: 2026-09-01T09:00:00Z
updated: 2026-09-01T09:00:00Z
author: jose
---

## Notes

The fixture board of the inbox acceptance test.
`

// inboxWorkspace is a browser-shaped workspace: the team repository that owns
// the board, and the project repository that owns the items.
func inboxWorkspace(t *testing.T) *Workspace {
	t.Helper()
	w := NewWorkspace()
	w.SetClock(func() time.Time { return inboxClock })
	wsCall(t, w, "vault.load", map[string]any{
		"vaultId": "inbx-team", "role": RoleTeam,
		"files": []map[string]string{
			{"path": "team.yaml", "text": inboxTeamYAML},
			{"path": ".pmngr/boards/delivery.md", "text": inboxTeamBoard},
		},
	})
	wsCall(t, w, "vault.load", map[string]any{
		"vaultId": "inbx", "role": RoleProject,
		"files": []map[string]string{
			{"path": "docs/.pmngr/project.yaml", "text": inboxProjectYAML},
			{"path": "docs/.pmngr/epics/INBX-EP-0001-existing.md", "text": inboxParentEpic},
			{"path": "docs/.pmngr/stories/INBX-US-0001-existing.md", "text": inboxTargetStory},
		},
	})
	return w
}

// boardRefs returns the card refs of one column of the rendered board.
func boardRefs(t *testing.T, w *Workspace, column string) []string {
	t.Helper()
	view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "delivery"}))
	for _, c := range view.Columns {
		if c.ID != column {
			continue
		}
		out := make([]string, 0, len(c.Cards))
		for _, card := range c.Cards {
			out = append(out, card.Ref)
		}
		return out
	}
	t.Fatalf("the board has no column %q", column)
	return nil
}

// TestAcceptingASubmissionPutsItOnTheBoard exercises the real write path: an
// item arrives in the inbox, is invisible everywhere a planner looks, and the
// single write that accepts it makes it a card in the column its new status
// maps to — in the same index generation, with nothing reloaded in between.
func TestAcceptingASubmissionPutsItOnTheBoard(t *testing.T) {
	w := inboxWorkspace(t)

	submitted := decode[struct {
		Item core.Item `json:"item"`
	}](t, wsCall(t, w, "item.create", map[string]any{
		"project": "INBX", "type": "story", "title": "The checkout page hangs on Safari",
		"inbox": map[string]any{"source": "web"},
	})).Item
	if submitted.Status != "triage" {
		t.Fatalf("status = %q, want triage", submitted.Status)
	}
	ref := "INBX/" + string(submitted.ID)

	// While it is in the inbox it is on no board — not even in the column that
	// names the triage status outright, which a reader would expect to catch
	// it. The exclusion happens before any column is consulted.
	for _, column := range []string{"inbox", "todo", "in_progress", "done"} {
		if got := boardRefs(t, w, column); containsRef(got, ref) {
			t.Errorf("a triage item reached column %q: %v", column, got)
		}
	}
	// And it is absent from the default listing, while the inbox listing has it.
	if listed := listedIDs(t, w, map[string]any{"project": "INBX"}); containsRef(listed, string(submitted.ID)) {
		t.Errorf("a triage item is in the default listing: %v", listed)
	}
	page := decode[InboxPage](t, wsCall(t, w, "inbox.list", map[string]any{"project": "INBX"}))
	inbox := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		inbox = append(inbox, string(it.ID))
	}
	if !containsRef(inbox, string(submitted.ID)) {
		t.Errorf("the inbox listing does not hold the submission: %v", inbox)
	}

	accepted := decode[InboxTriageResult](t, wsCall(t, w, "inbox.triage", map[string]any{
		"id": submitted.ID, "rev": submitted.Rev, "action": "accept",
	}))
	if accepted.Item.Status != "backlog" {
		t.Fatalf("status = %q, want the workflow's initial status", accepted.Item.Status)
	}
	if accepted.Item.Inbox == nil || accepted.Item.Inbox.Status != core.InboxAccepted {
		t.Fatalf("inbox block = %+v, want the provenance to survive acceptance", accepted.Item.Inbox)
	}

	// The same index generation already shows it as ordinary work.
	if got := boardRefs(t, w, "todo"); !containsRef(got, ref) {
		t.Errorf("the To Do column holds %v, want the accepted item in it", got)
	}
	if got := boardRefs(t, w, "inbox"); containsRef(got, ref) {
		t.Errorf("the accepted item is still in the inbox column: %v", got)
	}
	if listed := listedIDs(t, w, map[string]any{"project": "INBX"}); !containsRef(listed, string(submitted.ID)) {
		t.Errorf("the accepted item is not in the default listing: %v", listed)
	}
}

// listedIDs runs "item.list" and returns the ids it answered.
func listedIDs(t *testing.T, w *Workspace, params map[string]any) []string {
	t.Helper()
	page := decode[struct {
		Items []core.Item `json:"items"`
	}](t, wsCall(t, w, "item.list", params))
	out := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		out = append(out, string(it.ID))
	}
	return out
}

// containsRef reports whether list holds want.
func containsRef(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
