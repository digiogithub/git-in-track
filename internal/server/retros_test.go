package server

import (
	"net/http"
	"strings"
	"testing"
)

// retroViewBody is the documented shape of GET /api/v1/retros/{id}.
type retroViewBody struct {
	Retro struct {
		ID           string   `json:"id"`
		Title        string   `json:"title"`
		Sprint       string   `json:"sprint"`
		Board        string   `json:"board"`
		Date         string   `json:"date"`
		State        string   `json:"state"`
		Participants []string `json:"participants"`
		VoteBudget   int      `json:"voteBudget"`
		CarriedFrom  string   `json:"carriedFrom"`
		Rev          string   `json:"rev"`
		Metrics      struct {
			Actions  int `json:"actions"`
			Promoted int `json:"promoted"`
			Done     int `json:"done"`
			Open     int `json:"open"`
			NoOwner  int `json:"noOwner"`
		} `json:"metrics"`
	} `json:"retro"`
	Notes []struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Text     string `json:"text"`
		Author   string `json:"author"`
	} `json:"notes"`
	Themes []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Votes int    `json:"votes"`
	} `json:"themes"`
	Actions []retroActionBody `json:"actions"`
	Carried []retroActionBody `json:"carried"`
	Body    string            `json:"body"`
}

// retroActionBody is one improvement action with its live task state.
type retroActionBody struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Owner  string `json:"owner"`
	Task   string `json:"task"`
	Status string `json:"status"`
	Retro  string `json:"retro"`
	Done   bool   `json:"done"`
	Open   bool   `json:"open"`
	Card   *struct {
		Ref    string `json:"ref"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Source string `json:"source"`
	} `json:"card"`
}

// retroResultBody is the documented shape of every retro write.
type retroResultBody struct {
	Retro retroViewBody `json:"retro"`
	Task  *struct {
		ID     string   `json:"id"`
		Type   string   `json:"type"`
		Title  string   `json:"title"`
		Labels []string `json:"labels"`
		Body   string   `json:"body"`
	} `json:"task"`
	Writes []struct {
		VaultID string `json:"vaultId"`
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
}

// readRetro reads one retro over the API, with its ETag.
func readRetro(t *testing.T, s *Server, id string) (retroViewBody, string) {
	t.Helper()
	var body retroViewBody
	rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/retros/" + id})
	decode(t, rec, http.StatusOK, &body)
	return body, body.Retro.Rev
}

func TestRetroEndpoints(t *testing.T) {
	t.Run("list carries the open actions of every retro", func(t *testing.T) {
		s := newTeamServer(t)
		var body struct {
			Retros []struct {
				ID      string `json:"id"`
				Sprint  string `json:"sprint"`
				Metrics struct {
					Open int `json:"open"`
				} `json:"metrics"`
			} `json:"retros"`
			Carried []retroActionBody `json:"carried"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/retros"}), http.StatusOK, &body)
		if len(body.Retros) != 1 || body.Retros[0].ID != "DEMO-TEAM-R-0001" {
			t.Fatalf("retros = %+v", body.Retros)
		}
		if body.Retros[0].Metrics.Open != 2 || len(body.Carried) != 2 {
			t.Fatalf("open = %+v, carried = %+v", body.Retros[0].Metrics, body.Carried)
		}
		var filtered struct {
			Retros []struct{ ID string } `json:"retros"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/retros?board=delivery"}),
			http.StatusOK, &filtered)
		if len(filtered.Retros) != 0 {
			t.Fatalf("the kanban board has no retro: %+v", filtered.Retros)
		}
	})

	t.Run("get carries the revision and the live state of every action", func(t *testing.T) {
		s := newTeamServer(t)
		var body retroViewBody
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/retros/DEMO-TEAM-R-0001"})
		decode(t, rec, http.StatusOK, &body)
		if rec.Header().Get("ETag") == "" || body.Retro.Rev == "" {
			t.Fatalf("a retro response carries its revision: etag=%q", rec.Header().Get("ETag"))
		}
		if len(body.Notes) != 4 || len(body.Themes) != 3 || len(body.Actions) != 3 {
			t.Fatalf("view = %d notes, %d themes, %d actions", len(body.Notes), len(body.Themes), len(body.Actions))
		}
		if body.Themes[0].ID != "t2" || body.Themes[0].Votes != 2 {
			t.Fatalf("top theme = %+v", body.Themes[0])
		}
		for _, action := range body.Actions {
			if action.ID != "a1" {
				continue
			}
			if action.Card == nil || action.Card.Source != "live" || !action.Open {
				t.Fatalf("a1 = %+v", action)
			}
		}
	})

	t.Run("an unknown retro is not found", func(t *testing.T) {
		s := newTeamServer(t)
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/retros/DEMO-TEAM-R-0099"}),
			http.StatusNotFound, nil)
	})

	t.Run("create allocates the id and links the sprint back", func(t *testing.T) {
		s := newTeamServer(t)
		var body retroResultBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/retros",
			body: map[string]any{"board": "demo-scrum", "title": "Incident review", "date": "2026-09-20"},
		}), http.StatusCreated, &body)
		if body.Retro.Retro.ID != "DEMO-TEAM-R-0002" {
			t.Fatalf("retro = %+v", body.Retro.Retro)
		}
		if body.Retro.Retro.CarriedFrom != "DEMO-TEAM-R-0001" || len(body.Retro.Carried) != 2 {
			t.Fatalf("carried = %q %+v", body.Retro.Retro.CarriedFrom, body.Retro.Carried)
		}
	})

	t.Run("update needs If-Match and applies one session's edits", func(t *testing.T) {
		s := newTeamServer(t)
		_, rev := readRetro(t, s, "DEMO-TEAM-R-0001")

		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/retros/DEMO-TEAM-R-0001",
			body: map[string]any{"title": "No If-Match"},
		}), http.StatusPreconditionRequired, nil)

		var body retroResultBody
		decode(t, send(t, s, request{
			method: http.MethodPatch, target: "/api/v1/retros/DEMO-TEAM-R-0001", header: map[string]string{"If-Match": rev},
			body: map[string]any{
				"addNotes": []map[string]any{
					{"category": "went_well", "text": "The sandbox held up", "author": "jose"},
				},
				"addActions": []map[string]any{
					{"title": "Alert on snapshot age", "owner": "jose", "due": "2026-09-30"},
				},
			},
		}), http.StatusOK, &body)
		if len(body.Retro.Notes) != 5 || len(body.Retro.Actions) != 4 {
			t.Fatalf("view = %d notes, %d actions", len(body.Retro.Notes), len(body.Retro.Actions))
		}
		if !strings.Contains(body.Retro.Body, "- [ ] a4 — Alert on snapshot age (jose, 2026-09-30)") {
			t.Fatalf("body = %q", body.Retro.Body)
		}
	})

	t.Run("an action is promoted into a task that links back", func(t *testing.T) {
		s := newTeamServer(t)
		_, rev := readRetro(t, s, "DEMO-TEAM-R-0001")

		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/retros/DEMO-TEAM-R-0001/actions/promote",
			header: map[string]string{"If-Match": rev}, body: map[string]any{"action": "a2"},
		}), http.StatusBadRequest, nil)

		var body retroResultBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/retros/DEMO-TEAM-R-0001/actions/promote",
			header: map[string]string{"If-Match": rev}, body: map[string]any{"action": "a2", "project": "DEMO"},
		}), http.StatusOK, &body)
		if body.Task == nil || body.Task.Type != "task" {
			t.Fatalf("task = %+v", body.Task)
		}
		if len(body.Task.Labels) != 1 || body.Task.Labels[0] != "retro" {
			t.Fatalf("labels = %v", body.Task.Labels)
		}
		if !strings.Contains(body.Task.Body, "Promoted from retro DEMO-TEAM-R-0001 (action a2).") {
			t.Fatalf("task body = %q", body.Task.Body)
		}
		var promoted retroActionBody
		for _, action := range body.Retro.Actions {
			if action.ID == "a2" {
				promoted = action
			}
		}
		if promoted.Task != "DEMO/"+body.Task.ID || promoted.Status != "promoted" {
			t.Fatalf("a2 = %+v", promoted)
		}
		// Two repositories were written: the project clone and the team repo.
		if len(body.Writes) != 2 {
			t.Fatalf("writes = %+v", body.Writes)
		}
	})
}
