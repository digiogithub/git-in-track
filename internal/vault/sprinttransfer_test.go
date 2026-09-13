package vault

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

// transferClock is the day every test in this file reads as today: inside the
// range of the fixture sprint, so that its derived status is `current` and a
// sprint planned right after it is `upcoming`. Pinning it keeps the derived
// statuses — and therefore the completed-target refusal — out of the hands of
// the wall clock.
var transferClock = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

// transferModes runs a subtest against both operating modes with a pinned clock.
func transferModes(t *testing.T, run func(t *testing.T, w *Workspace)) {
	t.Helper()
	writableModes(t, func(t *testing.T, w *Workspace) {
		w.SetClock(func() time.Time { return transferClock })
		run(t, w)
	})
}

// planNextSprint adds the sprint a transfer moves into, right after the fixture
// one so that the two never overlap.
func planNextSprint(t *testing.T, w *Workspace) string {
	t.Helper()
	result := decode[SprintResult](t, wsCall(t, w, "sprint.create", map[string]any{
		"board": "demo-scrum", "start": "2026-09-07", "end": "2026-09-20",
		"title": "Sprint 2",
	}))
	return result.Sprint.Sprint.ID
}

// sprintRefs is the scope of one sprint, for an assertion on what moved.
func sprintRefs(t *testing.T, w *Workspace, id string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, ref := range sprintOf(t, w, id).Sprint.Items {
		out[ref] = true
	}
	return out
}

func TestSprintBulkTransferOnClose(t *testing.T) {
	tests := []struct {
		name  string
		mode  string
		carry []map[string]any
		check func(t *testing.T, w *Workspace, next string, result SprintResult)
	}{
		{
			name: "next moves every unfinished reference into the target",
			mode: TransferNext,
			check: func(t *testing.T, w *Workspace, next string, result SprintResult) {
				if len(result.Report.Carried) != 3 {
					t.Fatalf("carried = %+v", result.Report.Carried)
				}
				refs := sprintRefs(t, w, next)
				for _, want := range []string{"DEMO/DEMO-US-0001", "DEMO/DEMO-T-0001", "WEB/WEB-US-0031"} {
					if !refs[want] {
						t.Errorf("%s did not reach %s: %v", want, next, refs)
					}
				}
				// One sprint receiving three references is written once.
				for _, set := range result.Writes {
					if set.VaultID != "demo-team" {
						t.Errorf("a bulk carry into a sprint writes only the team repository: %+v", set)
					}
				}
			},
		},
		{
			name: "backlog returns each item to its own project's todo status",
			mode: TransferBacklog,
			check: func(t *testing.T, w *Workspace, _ string, result SprintResult) {
				byRef := map[string]core.SprintCarryResult{}
				for _, carried := range result.Report.Carried {
					byRef[carried.Ref] = carried
				}
				if got := byRef["DEMO/DEMO-US-0001"]; got.Status != "backlog" || got.Error != "" {
					t.Fatalf("carry = %+v", got)
				}
				// R-SPR-8: the uncloned project is reported on its own line and
				// the rest of the operation still went through.
				remote := byRef["WEB/WEB-US-0031"]
				if remote.Error == "" {
					t.Fatal("a project nobody cloned cannot take an item back")
				}
				returned := decode[core.Item](t, wsCall(t, w, "item.get",
					map[string]any{"id": "DEMO-US-0001"}))
				if returned.Status != "backlog" {
					t.Errorf("status = %q, want backlog", returned.Status)
				}
			},
		},
		{
			name: "none matches the behavior of a close with no transfer",
			mode: TransferNone,
			check: func(t *testing.T, w *Workspace, next string, result SprintResult) {
				if len(result.Report.Carried) != 0 {
					t.Fatalf("closing modifies no item unless the user chose it: %+v",
						result.Report.Carried)
				}
				if len(sprintRefs(t, w, next)) != 0 {
					t.Errorf("the next sprint gained items nobody asked for")
				}
			},
		},
		{
			name:  "an explicit decision overrides the bulk mode",
			mode:  TransferNext,
			carry: []map[string]any{{"ref": "DEMO/DEMO-US-0001", "action": "leave"}},
			check: func(t *testing.T, w *Workspace, next string, result SprintResult) {
				for _, carried := range result.Report.Carried {
					if carried.Ref == "DEMO/DEMO-US-0001" && carried.Action != core.CarryLeave {
						t.Fatalf("the explicit decision lost: %+v", carried)
					}
				}
				refs := sprintRefs(t, w, next)
				if refs["DEMO/DEMO-US-0001"] {
					t.Error("the item the caller chose to leave was carried anyway")
				}
				if !refs["DEMO/DEMO-T-0001"] {
					t.Error("the bulk mode did not apply to the rest")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transferModes(t, func(t *testing.T, w *Workspace) {
				next := planNextSprint(t, w)
				view := sprintOf(t, w, "DEMO-TEAM-S-0001")
				params := map[string]any{
					"id": "DEMO-TEAM-S-0001", "rev": string(view.Sprint.Rev),
					"transfer": map[string]any{"mode": tt.mode, "target": next},
				}
				if tt.carry != nil {
					params["carry"] = tt.carry
				}
				result := decode[SprintResult](t, wsCall(t, w, "sprint.close", params))
				if result.Sprint.Sprint.State != core.SprintClosed {
					t.Fatalf("state = %q, want closed", result.Sprint.Sprint.State)
				}
				if result.DryRun {
					t.Error("a real close is not a dry run")
				}
				tt.check(t, w, next, result)
			})
		})
	}

	t.Run("finished work is never moved", func(t *testing.T) {
		transferModes(t, func(t *testing.T, w *Workspace) {
			next := planNextSprint(t, w)
			done := decode[core.Item](t, wsCall(t, w, "item.get", map[string]any{"id": "DEMO-T-0001"}))
			wsCall(t, w, "item.update", map[string]any{
				"id": "DEMO-T-0001", "rev": string(done.Rev),
				"patch": map[string]any{"set": map[string]any{"status": "done"}},
			})
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.close", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": string(view.Sprint.Rev),
				"transfer": map[string]any{"mode": "next", "target": next},
			}))
			if len(result.Report.Completed) != 1 {
				t.Fatalf("completed = %+v", result.Report.Completed)
			}
			if sprintRefs(t, w, next)["DEMO/DEMO-T-0001"] {
				t.Error("a finished item was carried into the next sprint")
			}
		})
	})
}

func TestSprintTransfer(t *testing.T) {
	t.Run("it moves the incomplete references without closing either sprint", func(t *testing.T) {
		transferModes(t, func(t *testing.T, w *Workspace) {
			next := planNextSprint(t, w)
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.transfer", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": string(view.Sprint.Rev), "target": next,
			}))
			if result.Sprint.Sprint.State != core.SprintActive {
				t.Fatalf("state = %q, want the source sprint left open", result.Sprint.Sprint.State)
			}
			if !sprintRefs(t, w, next)["DEMO/DEMO-T-0001"] {
				t.Fatalf("nothing reached %s", next)
			}
			after := sprintOf(t, w, "DEMO-TEAM-S-0001")
			if len(after.Sprint.Committed) != 2 {
				t.Errorf("committed = %v, want the record of what was promised untouched",
					after.Sprint.Committed)
			}
		})
	})

	t.Run("a completed target is refused outright", func(t *testing.T) {
		transferModes(t, func(t *testing.T, w *Workspace) {
			next := planNextSprint(t, w)
			// Read the same sprints from a day after the target has ended.
			w.SetClock(func() time.Time { return time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) })
			code, _ := wsFail(t, w, "sprint.transfer", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": "*", "target": next,
			})
			if code != SprintTargetCompletedCode {
				t.Fatalf("code = %q, want %s", code, SprintTargetCompletedCode)
			}
		})
	})

	t.Run("an unknown mode is refused", func(t *testing.T) {
		transferModes(t, func(t *testing.T, w *Workspace) {
			planNextSprint(t, w)
			code, _ := wsFail(t, w, "sprint.transfer", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": "*", "mode": "somewhere",
			})
			if code != "invalid_request" {
				t.Fatalf("code = %q", code)
			}
		})
	})

	t.Run("a stale rev is refused", func(t *testing.T) {
		transferModes(t, func(t *testing.T, w *Workspace) {
			planNextSprint(t, w)
			code, _ := wsFail(t, w, "sprint.transfer", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": "sha256:0000000000000000",
			})
			if code != "stale_revision" {
				t.Fatalf("code = %q", code)
			}
		})
	})
}

func TestSprintTransferDryRun(t *testing.T) {
	for _, method := range []string{"sprint.close", "sprint.transfer"} {
		t.Run(method+" computes the report and writes nothing", func(t *testing.T) {
			transferModes(t, func(t *testing.T, w *Workspace) {
				next := planNextSprint(t, w)
				before := workspaceRevs(t, w)

				params := map[string]any{
					"id": "DEMO-TEAM-S-0001", "rev": "*", "dryRun": true,
				}
				if method == "sprint.close" {
					params["transfer"] = map[string]any{"mode": "next", "target": next}
				} else {
					params["target"] = next
				}
				result := decode[SprintResult](t, wsCall(t, w, method, params))

				if !result.DryRun {
					t.Error("a dry run must say so")
				}
				if len(result.Writes) != 0 {
					t.Fatalf("a dry run wrote %+v", result.Writes)
				}
				if result.Report == nil || len(result.Report.Incomplete) != 3 {
					t.Fatalf("report = %+v", result.Report)
				}
				if len(result.Report.Carried) != 3 {
					t.Fatalf("the preview lists every decision: %+v", result.Report.Carried)
				}
				for _, carried := range result.Report.Carried {
					if carried.Sprint != next {
						t.Errorf("the preview names the target: %+v", carried)
					}
				}
				if result.Sprint.Sprint.State != core.SprintActive {
					t.Errorf("state = %q, want the sprint left open", result.Sprint.Sprint.State)
				}
				for path, rev := range workspaceRevs(t, w) {
					if before[path] != rev {
						t.Errorf("%s changed during a dry run: %s -> %s", path, before[path], rev)
					}
				}
			})
		})
	}
}

// workspaceRevs is the content hash of every file a transfer could possibly
// write: the sprints of the team repository and the items of every project. A
// dry run has to leave all of them exactly as they were.
func workspaceRevs(t *testing.T, w *Workspace) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, sprint := range decode[SprintListResult](t, wsCall(t, w, "sprint.list", nil)).Sprints {
		out["sprint:"+sprint.ID] = string(sprint.Rev)
		out["scope:"+sprint.ID] = strings.Join(sprint.Items, ",")
	}
	page := decode[itemPage](t, wsCall(t, w, "item.list", map[string]any{"limit": 100}))
	for _, it := range page.Items {
		out["item:"+string(it.ID)] = string(it.Rev)
	}
	if len(out) < 2 {
		t.Fatalf("the fixture workspace holds sprints and items: %v", out)
	}
	return out
}
