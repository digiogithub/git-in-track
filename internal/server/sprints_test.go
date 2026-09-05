package server

import (
	"net/http"
	"testing"
)

// sprintViewBody is the documented shape of GET /api/v1/sprints/{id}.
type sprintViewBody struct {
	Sprint struct {
		ID            string   `json:"id"`
		Title         string   `json:"title"`
		Board         string   `json:"board"`
		State         string   `json:"state"`
		Start         string   `json:"start"`
		End           string   `json:"end"`
		Goal          string   `json:"goal"`
		Items         []string `json:"items"`
		Committed     []string `json:"committed"`
		TotalDays     int      `json:"totalDays"`
		RemainingDays int      `json:"remainingDays"`
		Rev           string   `json:"rev"`
		Metrics       struct {
			Items           int     `json:"items"`
			Done            int     `json:"done"`
			Points          float64 `json:"points"`
			CommittedPoints float64 `json:"committedPoints"`
			DonePoints      float64 `json:"donePoints"`
			Added           int     `json:"added"`
		} `json:"metrics"`
	} `json:"sprint"`
	Cards []struct {
		Ref      string `json:"ref"`
		Title    string `json:"title"`
		Remote   bool   `json:"remote"`
		Source   string `json:"source"`
		InSprint bool   `json:"inSprint"`
	} `json:"cards"`
	Backlog []struct {
		Ref     string `json:"ref"`
		Backlog bool   `json:"backlog"`
	} `json:"backlog"`
}

// sprintResultBody is the documented shape of every sprint write.
type sprintResultBody struct {
	Sprint sprintViewBody `json:"sprint"`
	Board  *struct {
		ID     string `json:"id"`
		Sprint string `json:"sprint"`
	} `json:"board"`
	Report *struct {
		Completed  []struct{ Ref string } `json:"completed"`
		Incomplete []struct{ Ref string } `json:"incomplete"`
		Carried    []struct {
			Ref    string `json:"ref"`
			Action string `json:"action"`
			Sprint string `json:"sprint"`
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"carried"`
	} `json:"report"`
	Writes []struct {
		VaultID string `json:"vaultId"`
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
}

// sprintOf reads one sprint over the API, with its ETag.
func readSprint(t *testing.T, s *Server, id string) (sprintViewBody, string) {
	t.Helper()
	var body sprintViewBody
	rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/sprints/" + id})
	decode(t, rec, http.StatusOK, &body)
	return body, body.Sprint.Rev
}

func TestSprintEndpoints(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		s := newTeamServer(t)
		var body struct {
			Sprints []struct {
				ID    string `json:"id"`
				Board string `json:"board"`
				State string `json:"state"`
			} `json:"sprints"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sprints"}), http.StatusOK, &body)
		if len(body.Sprints) != 1 || body.Sprints[0].ID != "DEMO-TEAM-S-0001" {
			t.Fatalf("sprints = %+v", body.Sprints)
		}
		var filtered struct {
			Sprints []struct{ ID string } `json:"sprints"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/sprints?board=delivery"}),
			http.StatusOK, &filtered)
		if len(filtered.Sprints) != 0 {
			t.Fatalf("the kanban board runs no sprint: %+v", filtered.Sprints)
		}
	})

	t.Run("get resolves cloned and remote items and carries the revision", func(t *testing.T) {
		s := newTeamServer(t)
		var body sprintViewBody
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/sprints/DEMO-TEAM-S-0001"})
		decode(t, rec, http.StatusOK, &body)
		if rec.Header().Get("ETag") == "" || body.Sprint.Rev == "" {
			t.Fatalf("a sprint response carries its revision: etag=%q", rec.Header().Get("ETag"))
		}
		if body.Sprint.Goal == "" || body.Sprint.TotalDays != 14 {
			t.Fatalf("sprint = %+v", body.Sprint)
		}
		if len(body.Cards) != 3 {
			t.Fatalf("cards = %+v", body.Cards)
		}
		var remote, live int
		for _, card := range body.Cards {
			if card.Remote && card.Source == "snapshot" && card.Title != "" {
				remote++
			}
			if !card.Remote && card.Title != "" {
				live++
			}
		}
		if remote != 1 || live != 2 {
			t.Fatalf("cards = %+v", body.Cards)
		}
		if len(body.Backlog) != 1 || body.Backlog[0].Ref != "DEMO/DEMO-US-0002" {
			t.Fatalf("backlog = %+v", body.Backlog)
		}
	})

	t.Run("the scrum board is scoped to the active sprint", func(t *testing.T) {
		s := newTeamServer(t)
		var view struct {
			Kind       string `json:"kind"`
			Sprint     string `json:"sprint"`
			SprintInfo *struct {
				ID            string `json:"id"`
				Goal          string `json:"goal"`
				RemainingDays int    `json:"remainingDays"`
			} `json:"sprintInfo"`
			Columns []struct {
				ID    string `json:"id"`
				Cards []struct {
					Ref      string `json:"ref"`
					InSprint bool   `json:"inSprint"`
					Backlog  bool   `json:"backlog"`
				} `json:"cards"`
			} `json:"columns"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/boards/demo-scrum"}),
			http.StatusOK, &view)
		if view.Kind != "scrum" || view.SprintInfo == nil {
			t.Fatalf("board = %+v", view)
		}
		if view.SprintInfo.ID != "DEMO-TEAM-S-0001" || view.SprintInfo.Goal == "" {
			t.Fatalf("sprint header = %+v", view.SprintInfo)
		}
		for _, column := range view.Columns {
			for _, card := range column.Cards {
				if !card.InSprint && !card.Backlog {
					t.Fatalf("card %s is neither in the sprint nor a candidate", card.Ref)
				}
			}
		}
	})

	t.Run("create allocates the next id", func(t *testing.T) {
		s := newTeamServer(t)
		var body sprintResultBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints",
			body: map[string]any{
				"board": "demo-scrum", "title": "Sprint 2",
				"start": "2026-09-07", "end": "2026-09-20", "goal": "Payment methods",
			},
		}), http.StatusCreated, &body)
		if body.Sprint.Sprint.ID != "DEMO-TEAM-S-0002" || body.Sprint.Sprint.State != "planned" {
			t.Fatalf("sprint = %+v", body.Sprint.Sprint)
		}
		if len(body.Writes) != 1 || body.Writes[0].VaultID != teamRepoID {
			t.Fatalf("writes = %+v", body.Writes)
		}
	})

	t.Run("overlapping sprints are refused with a clear message", func(t *testing.T) {
		s := newTeamServer(t)
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints",
			body: map[string]any{"board": "demo-scrum", "start": "2026-09-01", "end": "2026-09-14"},
		}), http.StatusConflict, &doc)
		if doc.Code != "sprint_overlap" {
			t.Fatalf("problem = %+v", doc)
		}
	})

	t.Run("a create without dates is refused", func(t *testing.T) {
		s := newTeamServer(t)
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints",
			body: map[string]any{"board": "demo-scrum"},
		}), http.StatusBadRequest, &doc)
		if doc.Code != "invalid_request" {
			t.Fatalf("problem = %+v", doc)
		}
	})

	t.Run("patch moves items in and out of the scope", func(t *testing.T) {
		s := newTeamServer(t)
		_, rev := readSprint(t, s, "DEMO-TEAM-S-0001")
		var body sprintResultBody
		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/sprints/DEMO-TEAM-S-0001",
			header: map[string]string{"If-Match": `"` + rev + `"`},
			body: map[string]any{
				"goal": "Ship guest checkout", "addItems": []string{"DEMO/DEMO-US-0002"},
				"removeItems": []string{"WEB/WEB-US-0031"},
			},
		}), http.StatusOK, &body)
		if body.Sprint.Sprint.Goal != "Ship guest checkout" {
			t.Fatalf("goal = %q", body.Sprint.Sprint.Goal)
		}
		if len(body.Sprint.Sprint.Items) != 3 {
			t.Fatalf("items = %v", body.Sprint.Sprint.Items)
		}
		// One file write, in the team repository: sprint membership never
		// touches an item file (docs/04 R-SPR-2).
		if len(body.Writes) != 1 || len(body.Writes[0].Written) != 1 {
			t.Fatalf("writes = %+v", body.Writes)
		}
		if got := body.Writes[0].Written[0].Path; got != ".pmngr/sprints/DEMO-TEAM-S-0001.md" {
			t.Fatalf("path = %q", got)
		}
	})

	t.Run("a patch without If-Match is refused", func(t *testing.T) {
		s := newTeamServer(t)
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/sprints/DEMO-TEAM-S-0001",
			body: map[string]any{"goal": "nope"},
		}), http.StatusPreconditionRequired, &doc)
		if doc.Code != "precondition_required" {
			t.Fatalf("problem = %+v", doc)
		}
	})

	t.Run("start and close run the sprint lifecycle", func(t *testing.T) {
		s := newTeamServer(t)
		var created sprintResultBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints",
			body: map[string]any{"board": "demo-scrum", "start": "2026-09-07", "end": "2026-09-20"},
		}), http.StatusCreated, &created)
		next := created.Sprint.Sprint.ID

		// A board already running a sprint refuses a second one, once.
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints/" + next + "/start",
			header: map[string]string{"If-Match": "*"},
		}), http.StatusConflict, &doc)
		if doc.Code != "sprint_already_active" {
			t.Fatalf("problem = %+v", doc)
		}

		// Closing the running sprint decides what happens to each unfinished
		// item: one is carried over, one goes back to the backlog, one stays.
		_, rev := readSprint(t, s, "DEMO-TEAM-S-0001")
		var closed sprintResultBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints/DEMO-TEAM-S-0001/close",
			header: map[string]string{"If-Match": `"` + rev + `"`},
			body: map[string]any{"carry": []map[string]any{
				{"ref": "DEMO/DEMO-T-0001", "action": "next", "sprint": next},
				{"ref": "DEMO/DEMO-US-0001", "action": "backlog"},
				{"ref": "WEB/WEB-US-0031", "action": "leave"},
			}},
		}), http.StatusOK, &closed)
		if closed.Sprint.Sprint.State != "closed" || closed.Report == nil {
			t.Fatalf("close = %+v", closed.Sprint.Sprint)
		}
		if len(closed.Report.Incomplete) != 3 || len(closed.Report.Carried) != 3 {
			t.Fatalf("report = %+v", closed.Report)
		}
		for _, carried := range closed.Report.Carried {
			if carried.Error != "" {
				t.Fatalf("carry %s: %s", carried.Ref, carried.Error)
			}
		}
		// The write reached the project repository as well as the team one.
		repos := map[string]bool{}
		for _, set := range closed.Writes {
			repos[set.VaultID] = true
		}
		if !repos[teamRepoID] || !repos[testRepoID] {
			t.Fatalf("writes = %+v", closed.Writes)
		}

		// With the running sprint closed, the next one starts and its board
		// follows it.
		var started sprintResultBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints/" + next + "/start",
			header: map[string]string{"If-Match": "*"},
		}), http.StatusOK, &started)
		if started.Sprint.Sprint.State != "active" {
			t.Fatalf("state = %q", started.Sprint.Sprint.State)
		}
		if started.Board == nil || started.Board.Sprint != next {
			t.Fatalf("board = %+v", started.Board)
		}
	})

	// The burndown route has its own test: TestSprintMetricsEndpoint.
}

func TestBoardPatch(t *testing.T) {
	t.Run("a scrum board is pointed at another sprint", func(t *testing.T) {
		s := newTeamServer(t)
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/sprints",
			body: map[string]any{"board": "demo-scrum", "start": "2026-09-07", "end": "2026-09-20"},
		}), http.StatusCreated, nil)

		var view boardViewBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/boards/demo-scrum"}),
			http.StatusOK, &view)
		var body struct {
			Board struct {
				Sprint     string `json:"sprint"`
				Title      string `json:"title"`
				SprintInfo *struct {
					ID string `json:"id"`
				} `json:"sprintInfo"`
			} `json:"board"`
			Writes []struct {
				VaultID string `json:"vaultId"`
			} `json:"writes"`
		}
		rec := send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/boards/demo-scrum",
			header: map[string]string{"If-Match": `"` + view.Rev + `"`},
			body:   map[string]any{"sprint": "DEMO-TEAM-S-0002", "title": "Sprint board"},
		})
		decode(t, rec, http.StatusOK, &body)
		if body.Board.Sprint != "DEMO-TEAM-S-0002" || body.Board.Title != "Sprint board" {
			t.Fatalf("board = %+v", body.Board)
		}
		if body.Board.SprintInfo == nil || body.Board.SprintInfo.ID != "DEMO-TEAM-S-0002" {
			t.Fatalf("sprint header = %+v", body.Board.SprintInfo)
		}
		if rec.Header().Get("ETag") == "" {
			t.Fatal("a board patch answers with the new revision")
		}
		if len(body.Writes) != 1 || body.Writes[0].VaultID != teamRepoID {
			t.Fatalf("writes = %+v", body.Writes)
		}
	})

	t.Run("a kanban board is never scoped to a sprint", func(t *testing.T) {
		s := newTeamServer(t)
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/boards/delivery",
			header: map[string]string{"If-Match": "*"},
			body:   map[string]any{"sprint": "DEMO-TEAM-S-0001"},
		}), http.StatusUnprocessableEntity, &doc)
		if doc.Code != "validation_failed" {
			t.Fatalf("problem = %+v", doc)
		}
	})

	t.Run("a patch without If-Match is refused", func(t *testing.T) {
		s := newTeamServer(t)
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/boards/demo-scrum",
			body: map[string]any{"title": "nope"},
		}), http.StatusPreconditionRequired, &doc)
		if doc.Code != "precondition_required" {
			t.Fatalf("problem = %+v", doc)
		}
	})
}

// TestSprintMetricsEndpoint covers GET /api/v1/sprints/{id}/burndown, the route
// that answered `not_implemented` until GIT-US-0028. The fixture repositories
// are copied out of the source tree and are not git working trees, so the
// companion has no history to read and the answer must be the stated
// approximation rather than an invented series.
func TestSprintMetricsEndpoint(t *testing.T) {
	t.Run("a sprint answers with both charts and their provenance", func(t *testing.T) {
		s := newTeamServer(t)
		var body struct {
			Sprint struct {
				ID string `json:"id"`
			} `json:"sprint"`
			Burndown struct {
				CommittedPoints float64 `json:"committedPoints"`
				Points          []struct {
					Date      string  `json:"date"`
					Day       int     `json:"day"`
					Ideal     float64 `json:"ideal"`
					Observed  bool    `json:"observed"`
					Remaining float64 `json:"remaining"`
					Unknown   int     `json:"unknown"`
				} `json:"points"`
			} `json:"burndown"`
			Flow struct {
				Bands []string `json:"bands"`
				Days  []struct {
					Date   string         `json:"date"`
					Counts map[string]int `json:"counts"`
					Total  int            `json:"total"`
				} `json:"days"`
			} `json:"flow"`
			Stats struct {
				Throughput int `json:"throughput"`
			} `json:"stats"`
			Provenance struct {
				Source      string `json:"source"`
				Approximate bool   `json:"approximate"`
				Items       int    `json:"items"`
				Note        string `json:"note"`
			} `json:"provenance"`
			Items []struct {
				Ref string `json:"ref"`
			} `json:"items"`
		}
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/sprints/DEMO-TEAM-S-0001/burndown",
		}), http.StatusOK, &body)

		if body.Sprint.ID != "DEMO-TEAM-S-0001" {
			t.Fatalf("sprint = %q", body.Sprint.ID)
		}
		if len(body.Burndown.Points) != 14 || len(body.Flow.Days) != 14 {
			t.Fatalf("points = %d, days = %d, want 14 of each",
				len(body.Burndown.Points), len(body.Flow.Days))
		}
		if body.Burndown.Points[0].Date != "2026-08-24" || body.Burndown.Points[0].Day != 1 {
			t.Errorf("first day = %+v", body.Burndown.Points[0])
		}
		if body.Burndown.Points[13].Ideal != 0 {
			t.Errorf("the ideal line must reach zero on the last day: %+v", body.Burndown.Points[13])
		}
		if len(body.Flow.Bands) != 5 || body.Flow.Bands[0] != "done" {
			t.Errorf("bands = %v, want five with done at the bottom", body.Flow.Bands)
		}
		if len(body.Items) != 3 {
			t.Errorf("items = %d, want the whole scope for the data table", len(body.Items))
		}
		if body.Provenance.Source == "" || body.Provenance.Note == "" {
			t.Errorf("every metric must state where it came from: %+v", body.Provenance)
		}
		if !body.Provenance.Approximate {
			t.Error("a fixture with no git history must not claim a reconstruction")
		}
		if body.Provenance.Items != 3 {
			t.Errorf("provenance items = %d, want 3", body.Provenance.Items)
		}
	})

	t.Run("an unknown sprint is not found", func(t *testing.T) {
		s := newTeamServer(t)
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodGet, target: "/api/v1/sprints/DEMO-TEAM-S-0099/burndown",
		}), http.StatusNotFound, &doc)
		if doc.Code != "not_found" {
			t.Errorf("code = %q", doc.Code)
		}
	})
}
