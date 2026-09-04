package server

import (
	"net/http"
	"testing"
)

// boardViewBody is the documented shape of GET /api/v1/boards/{slug}.
type boardViewBody struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Rev     string `json:"rev"`
	Columns []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		WIP      int    `json:"wip"`
		Exceeded bool   `json:"exceeded"`
		Cards    []struct {
			Ref      string `json:"ref"`
			Project  string `json:"project"`
			Item     string `json:"item"`
			Title    string `json:"title"`
			Status   string `json:"status"`
			Remote   bool   `json:"remote"`
			Declared bool   `json:"declared"`
			Reason   string `json:"reason"`
			Rev      string `json:"rev"`
		} `json:"cards"`
	} `json:"columns"`
	Unmapped []struct {
		Item   string `json:"item"`
		Reason string `json:"reason"`
	} `json:"unmapped"`
}

// boardMoveBody is the documented shape of POST /boards/{slug}/cards/move.
type boardMoveBody struct {
	Board boardViewBody `json:"board"`
	Item  *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Rev    string `json:"rev"`
	} `json:"item"`
	Move struct {
		Ref           string `json:"ref"`
		FromColumn    string `json:"fromColumn"`
		ToColumn      string `json:"toColumn"`
		Status        string `json:"status"`
		StatusChanged bool   `json:"statusChanged"`
		WIP           struct {
			Column   string `json:"column"`
			Used     int    `json:"used"`
			Limit    int    `json:"limit"`
			Exceeded bool   `json:"exceeded"`
		} `json:"wip"`
	} `json:"move"`
	Writes []struct {
		VaultID string `json:"vaultId"`
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
	} `json:"writes"`
}

// columnNamed returns a rendered column of a board body.
func columnNamed(t *testing.T, body boardViewBody, id string) (int, bool) {
	t.Helper()
	for i, c := range body.Columns {
		if c.ID == id {
			return i, true
		}
	}
	return 0, false
}

func TestBoardEndpoints(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		s := newTeamServer(t)
		var body struct {
			Boards []struct {
				ID      string `json:"id"`
				Kind    string `json:"kind"`
				Title   string `json:"title"`
				VaultID string `json:"vaultId"`
				Columns int    `json:"columns"`
				Rev     string `json:"rev"`
			} `json:"boards"`
		}
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/boards"}), http.StatusOK, &body)
		if len(body.Boards) != 2 {
			t.Fatalf("boards = %d, want 2", len(body.Boards))
		}
		if body.Boards[0].ID != "delivery" || body.Boards[0].Kind != "kanban" {
			t.Fatalf("board = %+v", body.Boards[0])
		}
		if body.Boards[0].VaultID != teamRepoID {
			t.Errorf("vaultId = %q, want %q", body.Boards[0].VaultID, teamRepoID)
		}
	})

	t.Run("get resolves cards from every mounted project", func(t *testing.T) {
		s := newTeamServer(t)
		var view boardViewBody
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/boards/delivery"})
		decode(t, rec, http.StatusOK, &view)
		if view.Rev == "" || rec.Header().Get("ETag") == "" {
			t.Fatalf("a board response carries its revision: rev=%q etag=%q",
				view.Rev, rec.Header().Get("ETag"))
		}
		i, ok := columnNamed(t, view, "todo")
		if !ok {
			t.Fatalf("no todo column in %+v", view.Columns)
		}
		cards := view.Columns[i].Cards
		if len(cards) != 2 {
			t.Fatalf("todo = %+v", cards)
		}
		if cards[0].Item != "DEMO-US-0002" || cards[0].Title == "" || cards[0].Remote {
			t.Errorf("the cloned card = %+v", cards[0])
		}
		if !cards[1].Remote || cards[1].Project != "WEB" || cards[1].Reason == "" {
			t.Errorf("the remote card = %+v", cards[1])
		}
	})

	t.Run("an unknown board is a 404", func(t *testing.T) {
		s := newTeamServer(t)
		rec := send(t, s, request{method: http.MethodGet, target: "/api/v1/boards/nope"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	t.Run("a move without If-Match is refused", func(t *testing.T) {
		s := newTeamServer(t)
		rec := send(t, s, request{
			method: http.MethodPost, target: "/api/v1/boards/delivery/cards/move",
			body: map[string]any{"ref": "DEMO/DEMO-US-0002", "toColumn": "in_progress"},
		})
		if rec.Code != http.StatusPreconditionRequired {
			t.Fatalf("status = %d, want 428: %s", rec.Code, rec.Body)
		}
	})

	t.Run("a move writes both repositories", func(t *testing.T) {
		s := newTeamServer(t)
		var view boardViewBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/boards/delivery"}),
			http.StatusOK, &view)

		var moved boardMoveBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/boards/delivery/cards/move",
			header: map[string]string{"If-Match": view.Rev},
			body: map[string]any{
				"ref": "DEMO/DEMO-US-0002", "toColumn": "in_progress", "position": 0, "force": true,
			},
		}), http.StatusOK, &moved)

		if moved.Item == nil || moved.Item.Status != "in_progress" {
			t.Fatalf("item = %+v", moved.Item)
		}
		if !moved.Move.StatusChanged || moved.Move.FromColumn != "todo" {
			t.Fatalf("move = %+v", moved.Move)
		}
		if len(moved.Writes) != 2 {
			t.Fatalf("writes = %+v", moved.Writes)
		}
		ids := map[string]bool{}
		for _, set := range moved.Writes {
			ids[set.VaultID] = true
		}
		if !ids[teamRepoID] || !ids[testRepoID] {
			t.Fatalf("a move must write both repositories: %v", ids)
		}
	})

	t.Run("a move over a WIP limit is a 409 the caller can confirm", func(t *testing.T) {
		s := newTeamServer(t)
		var view boardViewBody
		decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/boards/delivery"}),
			http.StatusOK, &view)

		rec := send(t, s, request{
			method: http.MethodPost, target: "/api/v1/boards/delivery/cards/move",
			header: map[string]string{"If-Match": view.Rev},
			body:   map[string]any{"ref": "DEMO/DEMO-US-0002", "toColumn": "in_review", "position": 0},
		})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body)
		}
		var prob struct {
			Code   string `json:"code"`
			Detail string `json:"detail"`
		}
		decode(t, rec, http.StatusConflict, &prob)
		if prob.Code != "wip_limit_exceeded" {
			t.Fatalf("code = %q: %s", prob.Code, prob.Detail)
		}

		var moved boardMoveBody
		decode(t, send(t, s, request{
			method: http.MethodPost, target: "/api/v1/boards/delivery/cards/move?force=true",
			header: map[string]string{"If-Match": view.Rev},
			body:   map[string]any{"ref": "DEMO/DEMO-US-0002", "toColumn": "in_review", "position": 0},
		}), http.StatusOK, &moved)
		if !moved.Move.WIP.Exceeded {
			t.Fatalf("wip = %+v", moved.Move.WIP)
		}
	})

	t.Run("a remote card cannot be moved", func(t *testing.T) {
		s := newTeamServer(t)
		rec := send(t, s, request{
			method: http.MethodPost, target: "/api/v1/boards/delivery/cards/move",
			header: map[string]string{"If-Match": "*"},
			body:   map[string]any{"ref": "WEB/WEB-US-0031", "toColumn": "in_progress"},
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body)
		}
	})

	t.Run("a stale board revision is a 412", func(t *testing.T) {
		s := newTeamServer(t)
		rec := send(t, s, request{
			method: http.MethodPost, target: "/api/v1/boards/delivery/cards/move",
			header: map[string]string{"If-Match": "sha256:0000000000000000"},
			body:   map[string]any{"ref": "DEMO/DEMO-US-0002", "toColumn": "in_progress"},
		})
		if rec.Code != http.StatusPreconditionFailed {
			t.Fatalf("status = %d, want 412: %s", rec.Code, rec.Body)
		}
	})
}
