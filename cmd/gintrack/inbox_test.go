package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture repository declares no triage status, because a project without
// one simply has no inbox. These tests therefore add one to the temporary copy
// the harness made — never to the checked-in fixture — and file two
// submissions into it, which is the smallest queue that exercises every
// subcommand.

// triageStatusLine is the status the inbox needs, in the reserved category.
const triageStatusLine = "    - { id: triage,      name: Triage,      category: triage }\n"

// triageTransitions is the transition out of triage an acceptance walks.
const triageTransitions = "    triage:      [backlog, todo, cancelled]\n"

// pendingSubmission is a submission nobody has looked at yet.
const pendingSubmission = `---
id: DEMO-US-0003
type: story
title: Dark mode for the storefront
status: triage
created: 2026-09-10T08:00:00Z
updated: 2026-09-10T08:00:00Z
author: marta
inbox:
  status: pending
  source: web
  received: 2026-09-10T08:00:00Z
---

## Description

Somebody asked for a dark theme in the feedback form.
`

// snoozedSubmission is a submission whose date has not arrived yet.
const snoozedSubmission = `---
id: DEMO-US-0004
type: story
title: Rewrite the returns policy page
status: triage
created: 2026-09-11T09:00:00Z
updated: 2026-09-11T09:00:00Z
author: jose
inbox:
  status: snoozed
  source: mcp
  received: 2026-09-11T09:00:00Z
  snoozed_until: 2099-01-01
---

## Description

Worth doing, but not before the checkout work lands.
`

// inboxHarness returns a registered workspace whose project declares an inbox
// and holds two submissions.
func inboxHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	enableInbox(t, h)
	writeFixture(t, h.Repo, "docs/.pmngr/stories/DEMO-US-0003-dark-mode.md", pendingSubmission)
	writeFixture(t, h.Repo, "docs/.pmngr/stories/DEMO-US-0004-returns-policy.md", snoozedSubmission)
	h.register()
	return h
}

// enableInbox adds a triage status to the copied project configuration.
func enableInbox(t *testing.T, h *harness) {
	t.Helper()
	path := filepath.Join(h.Repo, filepath.FromSlash("docs/.pmngr/project.yaml"))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the project configuration: %v", err)
	}
	text := string(data)
	const statusAnchor = "    - { id: backlog,     name: Backlog,     category: todo }\n"
	const transitionAnchor = "  transitions:\n"
	if !strings.Contains(text, statusAnchor) || !strings.Contains(text, transitionAnchor) {
		t.Fatalf("the fixture project configuration no longer has the anchors this test patches")
	}
	text = strings.Replace(text, statusAnchor, triageStatusLine+statusAnchor, 1)
	text = strings.Replace(text, transitionAnchor, transitionAnchor+triageTransitions, 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write the project configuration: %v", err)
	}
}

// writeFixture writes one file into the temporary copy of the fixture.
func writeFixture(t *testing.T, repo, rel, text string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestInboxList(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantIDs []string
	}{
		{
			name:    "pending by default",
			args:    []string{"inbox", "list", "--json"},
			wantIDs: []string{"DEMO-US-0003"},
		},
		{
			name:    "snoozed only",
			args:    []string{"inbox", "list", "--status", "snoozed", "--json"},
			wantIDs: []string{"DEMO-US-0004"},
		},
		{
			name:    "all states",
			args:    []string{"inbox", "list", "--status", "all", "--json"},
			wantIDs: []string{"DEMO-US-0004", "DEMO-US-0003"},
		},
		{
			name:    "filtered by project",
			args:    []string{"inbox", "list", "--status", "all", "--project", "demo", "--json"},
			wantIDs: []string{"DEMO-US-0004", "DEMO-US-0003"},
		},
		{
			name:    "a project with no submission",
			args:    []string{"inbox", "list", "--status", "rejected", "--json"},
			wantIDs: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := inboxHarness(t)
			payload := decode[inboxListPayload](t, h.mustRun(tc.args...))
			var got []string
			for _, item := range payload.Items {
				got = append(got, item.ID)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantIDs, ",") {
				t.Fatalf("items = %v, want %v", got, tc.wantIDs)
			}
			if payload.Pending != 1 {
				t.Fatalf("pending = %d, want 1", payload.Pending)
			}
			if payload.Counts["snoozed"] != 1 {
				t.Fatalf("counts[snoozed] = %d, want 1", payload.Counts["snoozed"])
			}
		})
	}
}

func TestInboxListTable(t *testing.T) {
	h := inboxHarness(t)
	stdout := h.mustRun("inbox", "list", "--status", "all")
	rows := lines(stdout)
	if len(rows) < 3 {
		t.Fatalf("want a header and two rows, got:\n%s", stdout)
	}
	header := columns(rows[0])
	if header[0] != "ID" || header[3] != "TRIAGE" {
		t.Fatalf("header = %v", header)
	}
	// The queue is newest decision first, so the snoozed submission leads.
	first := columns(rows[1])
	if first[0] != "DEMO-US-0004" || first[3] != "snoozed" {
		t.Fatalf("first row = %v", first)
	}
	second := columns(rows[2])
	if second[0] != "DEMO-US-0003" || second[3] != "pending" {
		t.Fatalf("second row = %v", second)
	}
}

func TestInboxListJSONKeepsHumanLinesOffStdout(t *testing.T) {
	h := inboxHarness(t)
	stdout, stderr, code := h.run("inbox", "list", "--json")
	if code != exitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("stdout is not a JSON document:\n%s", stdout)
	}
}

func TestInboxTriage(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantTriage  string
		wantStatus  string
		wantPending int
		wantUntil   string
	}{
		{
			name:        "accept into the initial status",
			args:        []string{"inbox", "accept", "DEMO-US-0003", "--json"},
			wantTriage:  "accepted",
			wantStatus:  "backlog",
			wantPending: 0,
		},
		{
			name:        "accept into a named status under a parent",
			args:        []string{"inbox", "accept", "DEMO-US-0003", "--status", "todo", "--parent", "DEMO-EP-0001", "--json"},
			wantTriage:  "accepted",
			wantStatus:  "todo",
			wantPending: 0,
		},
		{
			name:        "reject",
			args:        []string{"inbox", "reject", "DEMO-US-0003", "--json"},
			wantTriage:  "rejected",
			wantStatus:  "cancelled",
			wantPending: 0,
		},
		{
			name:        "snooze",
			args:        []string{"inbox", "snooze", "DEMO-US-0003", "--until", "2099-06-01", "--json"},
			wantTriage:  "snoozed",
			wantStatus:  "triage",
			wantPending: 0,
			wantUntil:   "2099-06-01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := inboxHarness(t)
			payload := decode[inboxTriagePayload](t, h.mustRun(tc.args...))
			if payload.Item.Triage != tc.wantTriage {
				t.Fatalf("triage = %q, want %q", payload.Item.Triage, tc.wantTriage)
			}
			if payload.Item.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", payload.Item.Status, tc.wantStatus)
			}
			if payload.Item.SnoozedUntil != tc.wantUntil {
				t.Fatalf("snoozedUntil = %q, want %q", payload.Item.SnoozedUntil, tc.wantUntil)
			}
			if payload.Pending != tc.wantPending {
				t.Fatalf("pending = %d, want %d", payload.Pending, tc.wantPending)
			}
			if len(payload.Written) == 0 {
				t.Fatalf("the decision wrote no file")
			}
			// The decision has to be on disk, not only in the answer.
			if !strings.Contains(h.readFile(payload.Item.Path), tc.wantTriage) {
				t.Fatalf("the file does not record %q:\n%s", tc.wantTriage, h.readFile(payload.Item.Path))
			}
		})
	}
}

func TestInboxTriageHumanOutput(t *testing.T) {
	h := inboxHarness(t)
	stdout := h.mustRun("inbox", "accept", "DEMO-US-0003")
	if !strings.Contains(stdout, "accepted DEMO-US-0003") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestInboxBadInvocations(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"unknown triage state", []string{"inbox", "list", "--status", "nope"}, exitUsage},
		{"a negative limit", []string{"inbox", "list", "--limit", "-1"}, exitUsage},
		{"accept with no id", []string{"inbox", "accept"}, exitUsage},
		{"accept with two ids", []string{"inbox", "accept", "DEMO-US-0003", "DEMO-US-0004"}, exitUsage},
		{"snooze with no date", []string{"inbox", "snooze", "DEMO-US-0003"}, exitUsage},
		{"an unknown flag", []string{"inbox", "list", "--nope"}, exitUsage},
		{"not an item id", []string{"inbox", "reject", "nonsense"}, exitValidation},
		{"an id nothing holds", []string{"inbox", "reject", "DEMO-US-9999"}, exitNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := inboxHarness(t)
			_, stderr, code := h.run(tc.args...)
			if code != tc.want {
				t.Fatalf("exit %d, want %d\n%s", code, tc.want, stderr)
			}
		})
	}
}
