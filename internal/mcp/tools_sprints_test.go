package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// sprintClock is the day the sprint tests read as today: inside the range of
// the fixture sprint, so that a sprint planned right after it derives
// `upcoming` rather than `completed` whatever the wall clock says.
var sprintClock = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

// newSprintHarness is the writable harness with a pinned clock and a second
// sprint to move work into.
func newSprintHarness(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t, true)
	h.space.SetClock(func() time.Time { return sprintClock })

	raw, err := h.space.Dispatch(context.Background(), "sprint.create", mustJSON(t, map[string]any{
		"board": "demo-scrum", "start": "2026-09-07", "end": "2026-09-20", "title": "Sprint 2",
	}))
	if err != nil {
		t.Fatalf("plan the next sprint: %v", err)
	}
	var created struct {
		Sprint struct {
			Sprint struct {
				ID string `json:"id"`
			} `json:"sprint"`
		} `json:"sprint"`
	}
	remarshal(t, raw, &created)
	return h, created.Sprint.Sprint.ID
}

// sprintRev reads the current rev of one sprint.
func sprintRev(t *testing.T, h *harness, id string) string {
	t.Helper()
	raw, err := h.space.Dispatch(context.Background(), "sprint.get", mustJSON(t, map[string]any{"id": id}))
	if err != nil {
		t.Fatalf("read sprint %s: %v", id, err)
	}
	var view core.SprintView
	remarshal(t, raw, &view)
	return string(view.Sprint.Rev)
}

// mustJSON encodes tool parameters for a direct dispatch.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return raw
}

// remarshal re-encodes a core answer into a local shape.
func remarshal(t *testing.T, from any, into any) {
	t.Helper()
	raw, err := json.Marshal(from)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestSprintToolsAreWriteGated(t *testing.T) {
	h := newHarness(t, false)
	advertised := map[string]bool{}
	for _, name := range h.server.Tools() {
		advertised[name] = true
	}
	for _, name := range []string{"close_sprint", "transfer_sprint_items"} {
		if advertised[name] {
			t.Errorf("%s is advertised by a read-only server", name)
		}
	}
}

func TestTransferSprintItems(t *testing.T) {
	t.Run("a dry run reports what would move and writes nothing", func(t *testing.T) {
		h, next := newSprintHarness(t)
		before := sprintRev(t, h, next)

		got := call[SprintReport](t, h, "transfer_sprint_items", map[string]any{
			"id": "DEMO-TEAM-S-0001", "rev": sprintRev(t, h, "DEMO-TEAM-S-0001"),
			"target": next, "dryRun": true,
		})
		if !got.DryRun {
			t.Fatal("the report must say it is a preview")
		}
		if got.Incomplete != 3 || got.Moved != 3 || len(got.Carried) != 3 {
			t.Fatalf("report = %+v", got)
		}
		for _, carried := range got.Carried {
			if carried.Sprint != next {
				t.Errorf("the preview names the target: %+v", carried)
			}
		}
		if len(got.Changed) != 0 {
			t.Errorf("a dry run wrote %v", got.Changed)
		}
		if sprintRev(t, h, next) != before {
			t.Error("a dry run changed the target sprint")
		}
		for _, ev := range h.writes {
			if ev.Tool == "transfer_sprint_items" {
				t.Error("a dry run announced a write")
			}
		}
	})

	t.Run("a real transfer moves the work and reports the files", func(t *testing.T) {
		h, next := newSprintHarness(t)
		got := call[SprintReport](t, h, "transfer_sprint_items", map[string]any{
			"id": "DEMO-TEAM-S-0001", "rev": sprintRev(t, h, "DEMO-TEAM-S-0001"),
			"target": next,
		})
		if got.DryRun || got.Moved != 3 {
			t.Fatalf("report = %+v", got)
		}
		if len(got.Changed) == 0 {
			t.Fatal("a real transfer writes files")
		}
		if len(h.writes) == 0 || h.writes[len(h.writes)-1].Tool != "transfer_sprint_items" {
			t.Errorf("the write was not announced: %+v", h.writes)
		}
	})

	t.Run("rev is required", func(t *testing.T) {
		h, next := newSprintHarness(t)
		got := callFails(t, h, "transfer_sprint_items", map[string]any{
			"id": "DEMO-TEAM-S-0001", "rev": "", "target": next,
		})
		if got.Code != codePreconditionRequired {
			t.Errorf("code = %q, want %s", got.Code, codePreconditionRequired)
		}
	})
}

func TestCloseSprintTool(t *testing.T) {
	t.Run("a dry run previews the close without closing", func(t *testing.T) {
		h, next := newSprintHarness(t)
		got := call[SprintReport](t, h, "close_sprint", map[string]any{
			"id": "DEMO-TEAM-S-0001", "rev": "*",
			"mode": "next", "target": next, "dryRun": true,
		})
		if !got.DryRun || got.State != string(core.SprintActive) {
			t.Fatalf("report = %+v", got)
		}
		if got.Incomplete != 3 || got.Completed != 0 {
			t.Fatalf("counts = %+v", got)
		}
	})

	t.Run("the close carries the unfinished work over", func(t *testing.T) {
		h, next := newSprintHarness(t)
		got := call[SprintReport](t, h, "close_sprint", map[string]any{
			"id": "DEMO-TEAM-S-0001", "rev": sprintRev(t, h, "DEMO-TEAM-S-0001"),
			"mode": "next", "target": next,
		})
		if got.State != string(core.SprintClosed) {
			t.Fatalf("state = %q, want closed", got.State)
		}
		if got.Moved != 3 || got.Failed != 0 {
			t.Fatalf("report = %+v", got)
		}
		if got.Rev == "" {
			t.Error("the report carries the sprint rev the next write must quote")
		}
	})

	t.Run("an explicit decision wins over the bulk mode", func(t *testing.T) {
		h, next := newSprintHarness(t)
		got := call[SprintReport](t, h, "close_sprint", map[string]any{
			"id": "DEMO-TEAM-S-0001", "rev": "*",
			"mode": "next", "target": next, "dryRun": true,
			"carry": []map[string]any{{"ref": "DEMO/DEMO-US-0001", "action": "leave"}},
		})
		for _, carried := range got.Carried {
			if carried.Ref == "DEMO/DEMO-US-0001" && carried.Action != string(core.CarryLeave) {
				t.Fatalf("the explicit decision lost: %+v", carried)
			}
		}
		if got.Moved != 2 {
			t.Errorf("moved = %d, want the other two", got.Moved)
		}
	})

	t.Run("an unknown sprint is not found", func(t *testing.T) {
		h, _ := newSprintHarness(t)
		got := callFails(t, h, "close_sprint", map[string]any{"id": "DEMO-TEAM-S-0404", "rev": "*"})
		if got.Code != codeNotFound {
			t.Errorf("code = %q, want %s", got.Code, codeNotFound)
		}
	})
}
