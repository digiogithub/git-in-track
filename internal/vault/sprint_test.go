package vault

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// sprintOf reads one sprint of the workspace, failing the test if it is gone.
func sprintOf(t *testing.T, w *Workspace, id string) core.SprintView {
	t.Helper()
	return decode[core.SprintView](t, wsCall(t, w, "sprint.get", map[string]any{"id": id}))
}

func TestWorkspaceSprintRead(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		t.Run("list carries the metrics of every sprint", func(t *testing.T) {
			result := decode[SprintListResult](t, wsCall(t, w, "sprint.list", nil))
			if len(result.Sprints) != 1 {
				t.Fatalf("sprints = %+v", result.Sprints)
			}
			s := result.Sprints[0]
			if s.ID != "DEMO-TEAM-S-0001" || s.Board != "demo-scrum" || s.State != core.SprintActive {
				t.Fatalf("sprint = %+v", s)
			}
			if s.Metrics.Items != 3 || s.Metrics.Points != 13 {
				t.Fatalf("metrics = %+v", s.Metrics)
			}
		})

		t.Run("list filters by board and by state", func(t *testing.T) {
			empty := decode[SprintListResult](t, wsCall(t, w, "sprint.list",
				map[string]any{"board": "delivery"}))
			if len(empty.Sprints) != 0 {
				t.Fatalf("the kanban board runs no sprint: %+v", empty.Sprints)
			}
			planned := decode[SprintListResult](t, wsCall(t, w, "sprint.list",
				map[string]any{"state": "planned"}))
			if len(planned.Sprints) != 0 {
				t.Fatalf("no sprint is planned: %+v", planned.Sprints)
			}
			code, _ := wsFail(t, w, "sprint.list", map[string]any{"state": "running"})
			if code != "invalid_request" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("get resolves cloned and remote items alike", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			if len(view.Cards) != 3 {
				t.Fatalf("cards = %d", len(view.Cards))
			}
			byRef := map[string]core.BoardCard{}
			for _, card := range view.Cards {
				byRef[card.Ref] = card
			}
			live := byRef["DEMO/DEMO-US-0001"]
			if live.Remote || live.Title == "" || live.Source != core.CardSourceLive {
				t.Fatalf("cloned card = %+v", live)
			}
			remote := byRef["WEB/WEB-US-0031"]
			if !remote.Remote || remote.Source != core.CardSourceSnapshot || remote.Title == "" {
				t.Fatalf("remote card = %+v", remote)
			}
			if len(view.Backlog) != 1 || view.Backlog[0].Ref != "DEMO/DEMO-US-0002" {
				t.Fatalf("backlog = %+v", view.Backlog)
			}
		})

		t.Run("the scrum board is scoped to the sprint", func(t *testing.T) {
			view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "demo-scrum"}))
			if view.SprintInfo == nil || view.SprintInfo.ID != "DEMO-TEAM-S-0001" {
				t.Fatalf("sprint header = %+v", view.SprintInfo)
			}
			if view.SprintInfo.Goal == "" || view.SprintInfo.TotalDays != 14 {
				t.Fatalf("sprint header = %+v", view.SprintInfo)
			}
			for _, column := range view.Columns {
				for _, card := range column.Cards {
					if !card.InSprint && !card.Backlog {
						t.Fatalf("card %s belongs to neither the sprint nor the backlog", card.Ref)
					}
				}
			}
			for _, d := range view.Diagnostics {
				if d.Severity == core.SeverityError {
					t.Fatalf("unexpected error diagnostic: %s", d)
				}
			}
		})

		t.Run("an unknown sprint is not found", func(t *testing.T) {
			code, _ := wsFail(t, w, "sprint.get", map[string]any{"id": "DEMO-TEAM-S-0099"})
			if code != "not_found" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("a lone vault refuses the call", func(t *testing.T) {
			m, ok := w.TeamMount()
			if !ok {
				t.Fatal("the fixture workspace holds a team repository")
			}
			var env envelope
			if err := json.Unmarshal([]byte(m.Vault.Call("sprint.list", "null")), &env); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if env.OK || env.Error.Code != "invalid_request" {
				t.Fatalf("a sprint call needs the workspace: %+v", env)
			}
		})
	})
}

func TestWorkspaceSprintWrite(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		t.Run("planning moves items in and out of the scope", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.update", map[string]any{
				"id":  "DEMO-TEAM-S-0001",
				"rev": string(view.Sprint.Rev),
				"patch": map[string]any{
					"goal":        "Ship guest checkout",
					"addItems":    []string{"DEMO/DEMO-US-0002"},
					"removeItems": []string{"DEMO/DEMO-T-0001"},
				},
			}))
			if result.Sprint.Sprint.Goal != "Ship guest checkout" {
				t.Fatalf("goal = %q", result.Sprint.Sprint.Goal)
			}
			want := []string{"DEMO/DEMO-US-0001", "WEB/WEB-US-0031", "DEMO/DEMO-US-0002"}
			if len(result.Sprint.Sprint.Items) != 3 {
				t.Fatalf("items = %v, want %v", result.Sprint.Sprint.Items, want)
			}
			for i, ref := range want {
				if result.Sprint.Sprint.Items[i] != ref {
					t.Fatalf("items = %v, want %v", result.Sprint.Sprint.Items, want)
				}
			}
			// Sprint membership lives in the team repository: one file write,
			// and no project repository touched (docs/04 section 11).
			if len(result.Writes) != 1 || len(result.Writes[0].Written) != 1 {
				t.Fatalf("writes = %+v", result.Writes)
			}
			if result.Writes[0].VaultID != "demo-team" {
				t.Fatalf("the write went to %q", result.Writes[0].VaultID)
			}
		})

		t.Run("a remote reference joins the scope like any other", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.update", map[string]any{
				"id":    "DEMO-TEAM-S-0001",
				"rev":   string(view.Sprint.Rev),
				"patch": map[string]any{"addItems": []string{"WEB/WEB-T-0007"}},
			}))
			found := false
			for _, card := range result.Sprint.Cards {
				if card.Ref == "WEB/WEB-T-0007" {
					found = true
					if !card.Remote {
						t.Fatalf("card = %+v", card)
					}
				}
			}
			if !found {
				t.Fatalf("cards = %+v", result.Sprint.Cards)
			}
		})

		t.Run("a stale revision is refused", func(t *testing.T) {
			code, _ := wsFail(t, w, "sprint.update", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": "sha256:0000000000000000",
				"patch": map[string]any{"goal": "nope"},
			})
			if code != "stale_revision" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("a new sprint is allocated the next id", func(t *testing.T) {
			result := decode[SprintResult](t, wsCall(t, w, "sprint.create", map[string]any{
				"board": "demo-scrum", "title": "Sprint 2",
				"start": "2026-09-07", "end": "2026-09-20",
				"goal": "Payment methods", "items": []string{"DEMO/DEMO-US-0002"},
			}))
			if result.Sprint.Sprint.ID != "DEMO-TEAM-S-0002" {
				t.Fatalf("id = %q", result.Sprint.Sprint.ID)
			}
			if result.Sprint.Sprint.State != core.SprintPlanned {
				t.Fatalf("a new sprint starts planned: %q", result.Sprint.Sprint.State)
			}
			if len(result.Sprint.Sprint.Items) != 1 {
				t.Fatalf("items = %v", result.Sprint.Sprint.Items)
			}
		})

		t.Run("overlapping dates are refused with a clear message", func(t *testing.T) {
			code, message := wsFail(t, w, "sprint.create", map[string]any{
				"board": "demo-scrum", "start": "2026-09-01", "end": "2026-09-14",
			})
			if code != SprintOverlapCode {
				t.Fatalf("code = %q", code)
			}
			if !strings.Contains(message, "DEMO-TEAM-S-0001") || !strings.Contains(message, "demo-scrum") {
				t.Fatalf("message = %q", message)
			}
		})

		t.Run("an unknown board is refused", func(t *testing.T) {
			code, _ := wsFail(t, w, "sprint.create", map[string]any{
				"board": "nowhere", "start": "2027-01-04", "end": "2027-01-17",
			})
			if code != "not_found" {
				t.Fatalf("code = %q", code)
			}
		})
	})
}

func TestWorkspaceSprintStartAndClose(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		t.Run("a second active sprint is refused, then confirmed", func(t *testing.T) {
			wsCall(t, w, "sprint.create", map[string]any{
				"board": "demo-scrum", "title": "Sprint 2",
				"start": "2026-09-07", "end": "2026-09-20",
			})
			code, message := wsFail(t, w, "sprint.start", map[string]any{"id": "DEMO-TEAM-S-0002"})
			if code != SprintActiveCode {
				t.Fatalf("code = %q (%s)", code, message)
			}
			result := decode[SprintResult](t, wsCall(t, w, "sprint.start",
				map[string]any{"id": "DEMO-TEAM-S-0002", "force": true}))
			if result.Sprint.Sprint.State != core.SprintActive {
				t.Fatalf("state = %q", result.Sprint.Sprint.State)
			}
			if result.Board == nil || result.Board.Sprint != "DEMO-TEAM-S-0002" {
				t.Fatalf("starting a sprint points its board at it: %+v", result.Board)
			}
		})

		t.Run("starting a sprint snapshots the commitment", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0002")
			wsCall(t, w, "sprint.update", map[string]any{
				"id":    "DEMO-TEAM-S-0002",
				"rev":   string(view.Sprint.Rev),
				"patch": map[string]any{"addItems": []string{"DEMO/DEMO-US-0002"}},
			})
			after := sprintOf(t, w, "DEMO-TEAM-S-0002")
			if len(after.Sprint.Committed) != 0 {
				t.Fatalf("the sprint started empty, so its commitment is empty: %v", after.Sprint.Committed)
			}
			if after.Sprint.Metrics.Added != 1 {
				t.Fatalf("an item added after the start is not committed: %+v", after.Sprint.Metrics)
			}
		})

		t.Run("closing reports completed against incomplete work", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0001")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.close", map[string]any{
				"id": "DEMO-TEAM-S-0001", "rev": string(view.Sprint.Rev),
				"carry": []map[string]any{
					{"ref": "DEMO/DEMO-T-0001", "action": "next", "sprint": "DEMO-TEAM-S-0002"},
					{"ref": "DEMO/DEMO-US-0001", "action": "backlog"},
					{"ref": "WEB/WEB-US-0031", "action": "leave"},
				},
			}))
			if result.Sprint.Sprint.State != core.SprintClosed {
				t.Fatalf("state = %q", result.Sprint.Sprint.State)
			}
			report := result.Report
			if report == nil {
				t.Fatal("closing a sprint reports what was finished")
			}
			if len(report.Completed) != 0 || len(report.Incomplete) != 3 {
				t.Fatalf("report = %d done, %d open", len(report.Completed), len(report.Incomplete))
			}
			if len(report.Carried) != 3 {
				t.Fatalf("carried = %+v", report.Carried)
			}
			for _, carried := range report.Carried {
				if carried.Error != "" {
					t.Fatalf("carry %s failed: %s", carried.Ref, carried.Error)
				}
			}
			// The carried item is in the next sprint, and the returned one went
			// back to a todo status in its own repository.
			next := sprintOf(t, w, "DEMO-TEAM-S-0002")
			found := false
			for _, ref := range next.Sprint.Items {
				if ref == "DEMO/DEMO-T-0001" {
					found = true
				}
			}
			if !found {
				t.Fatalf("the carried item is missing from %v", next.Sprint.Items)
			}
			returned := decode[core.Item](t, wsCall(t, w, "item.get", map[string]any{"id": "DEMO-US-0001"}))
			if returned.Status != "backlog" {
				t.Fatalf("status = %q, want backlog", returned.Status)
			}
		})

		t.Run("a remote item cannot be sent back to a backlog nobody cloned", func(t *testing.T) {
			view := sprintOf(t, w, "DEMO-TEAM-S-0002")
			result := decode[SprintResult](t, wsCall(t, w, "sprint.close", map[string]any{
				"id": "DEMO-TEAM-S-0002", "rev": string(view.Sprint.Rev),
				"carry": []map[string]any{{"ref": "DEMO/DEMO-T-0001", "action": "backlog"},
					{"ref": "DEMO/DEMO-US-0404", "action": "leave"}},
			}))
			for _, carried := range result.Report.Carried {
				if carried.Ref == "DEMO/DEMO-US-0404" && carried.Error == "" {
					t.Fatal("a reference the sprint does not list must be reported")
				}
			}
		})
	})
}

func TestWorkspaceBoardUpdate(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		t.Run("a WIP limit and a title are patched in place", func(t *testing.T) {
			view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "demo-scrum"}))
			columns := make([]map[string]any, 0, len(view.Columns))
			for _, column := range view.Columns {
				entry := map[string]any{"id": column.ID, "name": column.Name}
				switch column.ID {
				case "sprint_backlog":
					entry["categories"] = []string{"todo"}
				case "done":
					entry["categories"] = []string{"done", "cancelled"}
				case "in_progress":
					entry["statuses"] = map[string][]string{"*": {"in_progress"}, "WEB": {"doing"}}
					entry["wip"] = 5
				default:
					entry["statuses"] = map[string][]string{"*": {"in_review"}, "WEB": {"review"}}
				}
				columns = append(columns, entry)
			}
			result := decode[BoardUpdateResult](t, wsCall(t, w, "board.update", map[string]any{
				"board": "demo-scrum", "rev": string(view.Rev),
				"patch": map[string]any{"title": "Sprint board", "columns": columns},
			}))
			if result.Board.Title != "Sprint board" {
				t.Fatalf("title = %q", result.Board.Title)
			}
			for _, column := range result.Board.Columns {
				if column.ID == "in_progress" && column.WIP != 5 {
					t.Fatalf("wip = %d, want 5", column.WIP)
				}
			}
			if len(result.Writes) != 1 || result.Writes[0].VaultID != "demo-team" {
				t.Fatalf("writes = %+v", result.Writes)
			}
		})

		t.Run("a board can be pointed at another sprint", func(t *testing.T) {
			wsCall(t, w, "sprint.create", map[string]any{
				"board": "demo-scrum", "start": "2026-09-07", "end": "2026-09-20",
			})
			result := decode[BoardUpdateResult](t, wsCall(t, w, "board.update", map[string]any{
				"board": "demo-scrum", "rev": "*",
				"patch": map[string]any{"sprint": "DEMO-TEAM-S-0002"},
			}))
			if result.Board.Sprint != "DEMO-TEAM-S-0002" {
				t.Fatalf("sprint = %q", result.Board.Sprint)
			}
			if result.Board.SprintInfo == nil || result.Board.SprintInfo.ID != "DEMO-TEAM-S-0002" {
				t.Fatalf("header = %+v", result.Board.SprintInfo)
			}
		})

		t.Run("a sprint the repository does not hold is refused", func(t *testing.T) {
			code, _ := wsFail(t, w, "board.update", map[string]any{
				"board": "demo-scrum", "rev": "*",
				"patch": map[string]any{"sprint": "DEMO-TEAM-S-0404"},
			})
			if code != "not_found" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("a kanban board is never scoped to a sprint", func(t *testing.T) {
			code, _ := wsFail(t, w, "board.update", map[string]any{
				"board": "delivery", "rev": "*",
				"patch": map[string]any{"sprint": "DEMO-TEAM-S-0001"},
			})
			if code != "validation_failed" {
				t.Fatalf("code = %q", code)
			}
		})

		t.Run("a stale revision is refused", func(t *testing.T) {
			code, _ := wsFail(t, w, "board.update", map[string]any{
				"board": "demo-scrum", "rev": "sha256:0000000000000000",
				"patch": map[string]any{"title": "nope"},
			})
			if code != "stale_revision" {
				t.Fatalf("code = %q", code)
			}
		})
	})
}

func TestWorkspaceMoveCardIntoSprint(t *testing.T) {
	writableModes(t, func(t *testing.T, w *Workspace) {
		t.Run("dragging a candidate out of the backlog commits it", func(t *testing.T) {
			view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "demo-scrum"}))
			result := decode[BoardMoveResult](t, wsCall(t, w, "board.move", map[string]any{
				"board": "demo-scrum", "ref": "DEMO/DEMO-US-0002",
				"toColumn": "in_progress", "position": 0, "rev": string(view.Rev),
			}))
			if !result.Move.SprintAdd || result.Move.Sprint != "DEMO-TEAM-S-0001" {
				t.Fatalf("move = %+v", result.Move)
			}
			if !result.Move.StatusChanged || result.Move.Status != "in_progress" {
				t.Fatalf("move = %+v", result.Move)
			}
			// Three repositories' worth of writes: the sprint and the board in
			// the team repository, the item in its own clone.
			if len(result.Writes) != 3 {
				t.Fatalf("writes = %+v", result.Writes)
			}
			after := sprintOf(t, w, "DEMO-TEAM-S-0001")
			if len(after.Sprint.Items) != 4 {
				t.Fatalf("items = %v", after.Sprint.Items)
			}
			card, ok := cardIn(result.Board, "DEMO/DEMO-US-0002")
			if !ok || !card.InSprint || card.Backlog {
				t.Fatalf("card = %+v", card)
			}
		})

		t.Run("moving a card already in the sprint changes no membership", func(t *testing.T) {
			view := decode[core.BoardView](t, wsCall(t, w, "board.get", map[string]any{"board": "demo-scrum"}))
			result := decode[BoardMoveResult](t, wsCall(t, w, "board.move", map[string]any{
				"board": "demo-scrum", "ref": "DEMO/DEMO-T-0001",
				"toColumn": "done", "position": 0, "rev": string(view.Rev), "force": true,
			}))
			if result.Move.SprintAdd {
				t.Fatalf("move = %+v", result.Move)
			}
			after := sprintOf(t, w, "DEMO-TEAM-S-0001")
			if after.Sprint.Metrics.Done != 1 {
				t.Fatalf("metrics = %+v", after.Sprint.Metrics)
			}
		})
	})
}

// cardIn returns a card of a rendered board by reference.
func cardIn(view core.BoardView, ref string) (core.BoardCard, bool) {
	for _, column := range view.Columns {
		for _, card := range column.Cards {
			if card.Ref == ref {
				return card, true
			}
		}
	}
	return core.BoardCard{}, false
}
