package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemBodyAndRev reads one item and returns its body and revision.
func itemBodyAndRev(t *testing.T, s *Server, id string) (body, rev string) {
	t.Helper()

	var item struct {
		Body string `json:"body"`
		Rev  string `json:"rev"`
	}
	decode(t, send(t, s, request{method: http.MethodGet, target: "/api/v1/items/" + id}), http.StatusOK, &item)
	return item.Body, item.Rev
}

func TestItemTaskSet(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)
	body, rev := itemBodyAndRev(t, s, "DEMO-US-0001")
	boxes := core.TaskListItems(body)
	if len(boxes) == 0 {
		t.Fatal("the fixture story has no acceptance criteria")
	}
	target := boxes[len(boxes)-1]

	t.Run("without If-Match", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/items/DEMO-US-0001/tasks",
			body:   map[string]any{"line": target.Line, "checked": true},
		}), http.StatusPreconditionRequired, &doc)
		if doc.Code != "precondition_required" {
			t.Errorf("code = %q", doc.Code)
		}
	})

	t.Run("without a line", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/items/DEMO-US-0001/tasks",
			body:   map[string]any{"checked": true},
			header: map[string]string{"If-Match": rev},
		}), http.StatusBadRequest, &doc)
		if doc.Code != "invalid_request" {
			t.Errorf("code = %q", doc.Code)
		}
	})

	t.Run("ticking a criterion", func(t *testing.T) {
		var item struct {
			Body string `json:"body"`
			Rev  string `json:"rev"`
		}
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/items/DEMO-US-0001/tasks",
			body:   map[string]any{"line": target.Line, "checked": true},
			header: map[string]string{"If-Match": rev},
		}), http.StatusOK, &item)

		before, after := strings.Split(body, "\n"), strings.Split(item.Body, "\n")
		if len(before) != len(after) {
			t.Fatalf("the body gained or lost lines: %d -> %d", len(before), len(after))
		}
		for i := range before {
			if (before[i] != after[i]) != (i+1 == target.Line) {
				t.Errorf("line %d: got %q, want %q", i+1, after[i], before[i])
			}
		}
		if item.Rev == rev {
			t.Error("a toggle must produce a new revision")
		}
	})

	t.Run("a stale revision is refused", func(t *testing.T) {
		var doc problemBody
		decode(t, send(t, s, request{
			method: http.MethodPost,
			target: "/api/v1/items/DEMO-US-0001/tasks",
			body:   map[string]any{"line": target.Line, "checked": false},
			header: map[string]string{"If-Match": rev},
		}), http.StatusPreconditionFailed, &doc)
		if doc.Code != "stale_revision" {
			t.Errorf("code = %q, want stale_revision", doc.Code)
		}
	})
}

func TestItemReferences(t *testing.T) {
	t.Parallel()

	s, _ := newAPIServer(t)

	var answer struct {
		ID         string               `json:"id"`
		References []core.ItemReference `json:"references"`
		Children   []core.ItemReference `json:"children"`
	}
	decode(t, send(t, s, request{
		method: http.MethodGet,
		target: "/api/v1/items/DEMO-US-0001/references",
	}), http.StatusOK, &answer)

	if answer.ID != "DEMO-US-0001" {
		t.Errorf("id = %q", answer.ID)
	}
	if len(answer.References) == 0 {
		t.Fatal("the fixture story is referenced by its task and its sibling story")
	}
	if len(answer.Children) != 1 || answer.Children[0].ID != "DEMO-T-0001" {
		t.Errorf("children = %+v, want the one fixture task", answer.Children)
	}

	t.Run("an item nothing points at reports nothing", func(t *testing.T) {
		var empty struct {
			References []core.ItemReference `json:"references"`
		}
		decode(t, send(t, s, request{
			method: http.MethodGet,
			target: "/api/v1/items/DEMO-T-0001/references",
		}), http.StatusOK, &empty)
		for _, ref := range empty.References {
			if ref.Kind == "item" && ref.Field == "parent" {
				t.Errorf("a leaf task has no children: %+v", ref)
			}
		}
	})
}
