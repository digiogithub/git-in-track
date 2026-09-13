package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The team fixture ships one active sprint. These tests add two more to the
// temporary copy the harness made — never to the checked-in fixture — so that
// there is a sprint to start, a sprint to transfer into, and a sprint whose
// whole scope lives in a project nobody cloned, which is what makes a refusal
// reachable.

// upcomingSprint is the sprint a start and a transfer aim at. Its dates are far
// enough away that its derived status does not depend on the day the test runs.
const upcomingSprint = `---
id: DEMO-TEAM-S-0002
type: sprint
title: Sprint 2 — Payment methods
board: demo-scrum
state: planned
start: 2099-01-05
end: 2099-01-18
goal: Save a payment method and reuse it.
created: 2026-09-01T09:00:00Z
updated: 2026-09-01T09:00:00Z
author: jose
---

## Goal

The sprint the rollover lands in.
`

// remoteOnlySprint holds a single reference into a project nobody here cloned,
// so every decision about its scope is refused.
const remoteOnlySprint = `---
id: DEMO-TEAM-S-0003
type: sprint
title: Website only
board: demo-scrum
state: planned
items:
  - WEB/WEB-US-0031
created: 2026-09-01T09:00:00Z
updated: 2026-09-01T09:00:00Z
author: jose
---

## Goal

A draft whose whole scope is remote.
`

// sprintHarness registers the project fixture and the team fixture side by
// side, which is the shape every sprint call needs: the sprint files live in
// the team repository and the items live in the project one.
func sprintHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	team := filepath.Join(filepath.Dir(h.Repo), "acme-team")
	copyTree(t, filepath.Join("..", "..", "testdata", "fixtures", "team-basic"), team)
	if err := os.MkdirAll(filepath.Join(team, ".git"), 0o755); err != nil {
		t.Fatalf("create .git: %v", err)
	}
	writeFixture(t, team, ".pmngr/sprints/DEMO-TEAM-S-0002.md", upcomingSprint)
	writeFixture(t, team, ".pmngr/sprints/DEMO-TEAM-S-0003.md", remoteOnlySprint)
	h.register()
	if _, stderr, code := h.run("add", team, "--team"); code != exitOK {
		t.Fatalf("add the team repository: exit %d\n%s", code, stderr)
	}
	return h
}

func TestSprintList(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantIDs []string
	}{
		{
			name:    "every sprint, in group order",
			args:    []string{"sprint", "list", "--json"},
			wantIDs: []string{"DEMO-TEAM-S-0002", "DEMO-TEAM-S-0003", "DEMO-TEAM-S-0001"},
		},
		{
			name:    "filtered by derived status",
			args:    []string{"sprint", "list", "--status", "completed", "--json"},
			wantIDs: []string{"DEMO-TEAM-S-0001"},
		},
		{
			name:    "two derived statuses",
			args:    []string{"sprint", "list", "--status", "upcoming", "--status", "draft", "--json"},
			wantIDs: []string{"DEMO-TEAM-S-0002", "DEMO-TEAM-S-0003"},
		},
		{
			name:    "filtered by board",
			args:    []string{"sprint", "list", "--board", "delivery", "--json"},
			wantIDs: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := sprintHarness(t)
			payload := decode[sprintListPayload](t, h.mustRun(tc.args...))
			var got []string
			for _, s := range payload.Sprints {
				got = append(got, s.ID)
			}
			if strings.Join(got, ",") != strings.Join(tc.wantIDs, ",") {
				t.Fatalf("sprints = %v, want %v", got, tc.wantIDs)
			}
			if payload.Total != len(tc.wantIDs) {
				t.Fatalf("total = %d, want %d", payload.Total, len(tc.wantIDs))
			}
		})
	}
}

func TestSprintListGroupsTheTable(t *testing.T) {
	h := sprintHarness(t)
	stdout := h.mustRun("sprint", "list")
	for _, want := range []string{"UPCOMING", "DRAFT", "COMPLETED", "DEMO-TEAM-S-0001"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout has no %q:\n%s", want, stdout)
		}
	}
}

func TestSprintListCarriesTheDerivedStatus(t *testing.T) {
	h := sprintHarness(t)
	payload := decode[sprintListPayload](t, h.mustRun("sprint", "list", "--json"))
	byID := map[string]sprintRowPayload{}
	for _, s := range payload.Sprints {
		byID[s.ID] = s
	}
	if got := byID["DEMO-TEAM-S-0001"].Status; got != "completed" {
		t.Fatalf("S-0001 status = %q, want completed", got)
	}
	if got := byID["DEMO-TEAM-S-0001"].State; got != "active" {
		t.Fatalf("S-0001 state = %q, want active", got)
	}
	if got := byID["DEMO-TEAM-S-0003"].Status; got != "draft" {
		t.Fatalf("S-0003 status = %q, want draft", got)
	}
	if payload.Counts["completed"] != 1 {
		t.Fatalf("counts[completed] = %d, want 1", payload.Counts["completed"])
	}
}

func TestSprintShow(t *testing.T) {
	h := sprintHarness(t)
	payload := decode[sprintShowPayload](t, h.mustRun("sprint", "show", "DEMO-TEAM-S-0001", "--json"))
	if payload.Sprint.ID != "DEMO-TEAM-S-0001" {
		t.Fatalf("sprint = %q", payload.Sprint.ID)
	}
	if len(payload.Cards) != 3 {
		t.Fatalf("cards = %d, want 3", len(payload.Cards))
	}
	var remote *sprintCardPayload
	for i, c := range payload.Cards {
		if c.Ref == "WEB/WEB-US-0031" {
			remote = &payload.Cards[i]
		}
	}
	if remote == nil {
		t.Fatalf("the remote reference is missing from the scope")
	}
	if remote.Source != "snapshot" {
		t.Fatalf("the remote card came from %q, want the committed snapshot", remote.Source)
	}
	if !payload.Cards[0].Committed {
		t.Fatalf("DEMO/DEMO-US-0001 should be part of the commitment")
	}
}

func TestSprintShowTable(t *testing.T) {
	h := sprintHarness(t)
	stdout := h.mustRun("sprint", "show", "DEMO-TEAM-S-0001")
	if !strings.Contains(stdout, "DEMO-TEAM-S-0001") || !strings.Contains(stdout, "DEMO/DEMO-US-0001") {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestSprintStart(t *testing.T) {
	t.Run("refuses a second active sprint on the same board", func(t *testing.T) {
		h := sprintHarness(t)
		_, stderr, code := h.run("sprint", "start", "DEMO-TEAM-S-0002")
		if code != exitConflict {
			t.Fatalf("exit %d, want %d\n%s", code, exitConflict, stderr)
		}
	})

	t.Run("starts it with --force", func(t *testing.T) {
		h := sprintHarness(t)
		payload := decode[sprintWritePayload](t, h.mustRun("sprint", "start", "DEMO-TEAM-S-0002", "--force", "--json"))
		if payload.Sprint.State != "active" {
			t.Fatalf("state = %q, want active", payload.Sprint.State)
		}
		if len(payload.Written) == 0 {
			t.Fatalf("starting a sprint wrote nothing")
		}
	})

	t.Run("reports an unknown sprint", func(t *testing.T) {
		h := sprintHarness(t)
		_, stderr, code := h.run("sprint", "start", "DEMO-TEAM-S-9999")
		if code != exitNotFound {
			t.Fatalf("exit %d, want %d\n%s", code, exitNotFound, stderr)
		}
	})
}

func TestSprintCloseDryRunWritesNothing(t *testing.T) {
	h := sprintHarness(t)
	before := h.readTeamFile(".pmngr/sprints/DEMO-TEAM-S-0001.md")

	payload := decode[sprintWritePayload](t, h.mustRun(
		"sprint", "close", "DEMO-TEAM-S-0001", "--transfer", "next",
		"--target", "DEMO-TEAM-S-0002", "--dry-run", "--json"))

	if !payload.DryRun {
		t.Fatalf("the answer does not say it was a dry run")
	}
	if len(payload.Written) != 0 {
		t.Fatalf("a dry run wrote %v", payload.Written)
	}
	if payload.Report == nil || len(payload.Report.Carried) != 3 {
		t.Fatalf("report = %+v", payload.Report)
	}
	if payload.Sprint.State == "closed" {
		t.Fatalf("a dry run closed the sprint")
	}
	if h.readTeamFile(".pmngr/sprints/DEMO-TEAM-S-0001.md") != before {
		t.Fatalf("a dry run rewrote the sprint file")
	}
}

func TestSprintCloseApplies(t *testing.T) {
	h := sprintHarness(t)
	payload := decode[sprintWritePayload](t, h.mustRun(
		"sprint", "close", "DEMO-TEAM-S-0001", "--transfer", "next",
		"--target", "DEMO-TEAM-S-0002", "--json"))

	if payload.DryRun {
		t.Fatalf("a real close reported itself as a dry run")
	}
	if payload.Sprint.State != "closed" {
		t.Fatalf("state = %q, want closed", payload.Sprint.State)
	}
	if len(payload.Written) == 0 {
		t.Fatalf("closing a sprint wrote nothing")
	}
	if !strings.Contains(h.readTeamFile(".pmngr/sprints/DEMO-TEAM-S-0002.md"), "DEMO/DEMO-US-0001") {
		t.Fatalf("the unfinished work did not reach the next sprint")
	}
	for _, c := range payload.Report.Carried {
		if c.Error != "" {
			t.Fatalf("carrying %s was refused: %s", c.Ref, c.Error)
		}
	}
}

func TestSprintCloseReportsPerItemRefusals(t *testing.T) {
	h := sprintHarness(t)
	stdout, stderr, code := h.run("sprint", "close", "DEMO-TEAM-S-0001", "--transfer", "backlog")
	if code != exitOK {
		t.Fatalf("exit %d: a partial refusal must not fail the command\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "refused WEB/WEB-US-0031") {
		t.Fatalf("the refusal is not on its own line:\n%s", stdout)
	}
	if !strings.Contains(stderr, "1 of 3 decisions were refused") {
		t.Fatalf("the summary of the refusals is missing:\n%s", stderr)
	}
}

func TestSprintCloseFailsWhenNothingCouldBeApplied(t *testing.T) {
	h := sprintHarness(t)
	_, stderr, code := h.run("sprint", "close", "DEMO-TEAM-S-0003", "--transfer", "backlog")
	if code != exitFailure {
		t.Fatalf("exit %d, want %d\n%s", code, exitFailure, stderr)
	}
}

func TestSprintTransfer(t *testing.T) {
	t.Run("a dry run writes nothing", func(t *testing.T) {
		h := sprintHarness(t)
		before := h.readTeamFile(".pmngr/sprints/DEMO-TEAM-S-0002.md")
		payload := decode[sprintWritePayload](t, h.mustRun(
			"sprint", "transfer", "DEMO-TEAM-S-0001", "--to", "DEMO-TEAM-S-0002", "--dry-run", "--json"))
		if !payload.DryRun || len(payload.Written) != 0 {
			t.Fatalf("a dry run wrote %v", payload.Written)
		}
		if h.readTeamFile(".pmngr/sprints/DEMO-TEAM-S-0002.md") != before {
			t.Fatalf("a dry run rewrote the target sprint")
		}
	})

	t.Run("without the flag it moves the work", func(t *testing.T) {
		h := sprintHarness(t)
		payload := decode[sprintWritePayload](t, h.mustRun(
			"sprint", "transfer", "DEMO-TEAM-S-0001", "--to", "DEMO-TEAM-S-0002", "--json"))
		if len(payload.Written) == 0 {
			t.Fatalf("the transfer wrote nothing")
		}
		target := h.readTeamFile(".pmngr/sprints/DEMO-TEAM-S-0002.md")
		for _, ref := range []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-T-0001", "WEB/WEB-US-0031"} {
			if !strings.Contains(target, ref) {
				t.Fatalf("%s did not reach the target sprint:\n%s", ref, target)
			}
		}
		// The source sprint keeps every reference it ever held.
		if !strings.Contains(h.readTeamFile(".pmngr/sprints/DEMO-TEAM-S-0001.md"), "DEMO/DEMO-US-0001") {
			t.Fatalf("the transfer emptied the source sprint")
		}
	})
}

func TestSprintBadInvocations(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"show with no id", []string{"sprint", "show"}, exitUsage},
		{"show with two ids", []string{"sprint", "show", "A", "B"}, exitUsage},
		{"an unknown derived status", []string{"sprint", "list", "--status", "nope"}, exitUsage},
		{"an unknown transfer mode", []string{"sprint", "close", "DEMO-TEAM-S-0001", "--transfer", "sideways"}, exitUsage},
		{"transfer with no target", []string{"sprint", "transfer", "DEMO-TEAM-S-0001"}, exitUsage},
		{"an unknown flag", []string{"sprint", "list", "--nope"}, exitUsage},
		{"an unknown sprint", []string{"sprint", "show", "DEMO-TEAM-S-9999"}, exitNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := sprintHarness(t)
			_, stderr, code := h.run(tc.args...)
			if code != tc.want {
				t.Fatalf("exit %d, want %d\n%s", code, tc.want, stderr)
			}
		})
	}
}

// readTeamFile reads a file from the temporary copy of the team fixture.
func (h *harness) readTeamFile(rel string) string {
	h.t.Helper()
	team := filepath.Join(filepath.Dir(h.Repo), "acme-team")
	data, err := os.ReadFile(filepath.Join(team, filepath.FromSlash(rel)))
	if err != nil {
		h.t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}
