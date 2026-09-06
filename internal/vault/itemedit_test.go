package vault

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// itemBody reads one item and returns its body and its current revision.
func itemBody(t *testing.T, v *Vault, id string) (body, rev string) {
	t.Helper()
	it := decode[core.Item](t, call(t, v, "item.get", map[string]any{"id": id}))
	return it.Body, string(it.Rev)
}

func TestVaultItemTaskSet(t *testing.T) {
	t.Parallel()

	const id = "DEMO-US-0001"

	t.Run("ticking a criterion changes exactly that line", func(t *testing.T) {
		t.Parallel()
		v, _ := loadedVault(t)

		before, rev := itemBody(t, v, id)
		items := core.TaskListItems(before)
		if len(items) != 4 {
			t.Fatalf("the fixture story must hold 4 criteria, found %d", len(items))
		}
		target := items[2]
		if target.Checked {
			t.Fatalf("criterion 3 is expected to start unchecked: %+v", target)
		}

		call(t, v, "item.task.set", map[string]any{
			"id": id, "line": target.Line, "checked": true, "rev": rev,
		})

		after, _ := itemBody(t, v, id)
		beforeLines, afterLines := strings.Split(before, "\n"), strings.Split(after, "\n")
		if len(beforeLines) != len(afterLines) {
			t.Fatalf("the body gained or lost lines: %d -> %d", len(beforeLines), len(afterLines))
		}
		for i := range beforeLines {
			same := beforeLines[i] == afterLines[i]
			if i+1 == target.Line && same {
				t.Errorf("line %d was not toggled: %q", target.Line, afterLines[i])
			}
			if i+1 != target.Line && !same {
				t.Errorf("line %d changed as well:\n got %q\nwant %q", i+1, afterLines[i], beforeLines[i])
			}
		}
	})

	t.Run("the write is rev-guarded", func(t *testing.T) {
		t.Parallel()
		v, _ := loadedVault(t)

		before, rev := itemBody(t, v, id)
		line := core.TaskListItems(before)[0].Line
		call(t, v, "item.task.set", map[string]any{"id": id, "line": line, "checked": false, "rev": rev})

		// The second call quotes the revision the first one invalidated.
		env := rawCall(t, v, "item.task.set", map[string]any{
			"id": id, "line": line, "checked": true, "rev": rev,
		})
		if env.OK {
			t.Fatal("a toggle quoting a stale revision must be refused")
		}
		if env.Error.Code != core.StaleRevisionCode {
			t.Errorf("code = %q, want %q (%s)", env.Error.Code, core.StaleRevisionCode, env.Error.Message)
		}
	})

	t.Run("a line that is not a checkbox is refused", func(t *testing.T) {
		t.Parallel()
		v, _ := loadedVault(t)

		_, rev := itemBody(t, v, id)
		env := rawCall(t, v, "item.task.set", map[string]any{
			"id": id, "line": 1, "checked": true, "rev": rev,
		})
		if env.OK {
			t.Fatal("toggling a prose line must be refused")
		}
		if env.Error.Code != core.TaskListItemMismatchCode {
			t.Errorf("code = %q, want %q (%s)", env.Error.Code, core.TaskListItemMismatchCode, env.Error.Message)
		}
	})

	t.Run("a toggle without a rev is refused", func(t *testing.T) {
		t.Parallel()
		v, _ := loadedVault(t)

		before, _ := itemBody(t, v, id)
		line := core.TaskListItems(before)[0].Line
		env := rawCall(t, v, "item.task.set", map[string]any{"id": id, "line": line, "checked": true})
		if env.OK {
			t.Fatal("a toggle without a rev must be refused")
		}
		if env.Error.Code != "invalid_request" {
			t.Errorf("code = %q, want invalid_request", env.Error.Code)
		}
	})
}

func TestWorkspaceItemReferences(t *testing.T) {
	modes(t, func(t *testing.T, w *Workspace) {
		t.Run("a story reports its children, its links and its cards", func(t *testing.T) {
			result := decode[ItemReferencesResult](t,
				wsCall(t, w, "item.references", map[string]any{"id": "DEMO-US-0001"}))

			byField := map[string]core.ItemReference{}
			for _, ref := range result.References {
				byField[ref.Kind+":"+ref.Field] = ref
			}
			want := []string{
				"item:links.relates_to", "item:links.blocks", "item:parent",
				"board:order.in_progress", "sprint:items",
			}
			for _, want := range want {
				if _, ok := byField[want]; !ok {
					t.Errorf("no %s reference in %+v", want, result.References)
				}
			}
			for _, ref := range result.References {
				if ref.ID == "DEMO-US-0001" {
					t.Errorf("the item must not reference itself: %+v", ref)
				}
				if ref.Path == "" {
					t.Errorf("reference without a path: %+v", ref)
				}
			}
		})

		t.Run("an epic reports the stories parented to it", func(t *testing.T) {
			result := decode[ItemReferencesResult](t,
				wsCall(t, w, "item.references", map[string]any{"id": "DEMO-EP-0001"}))
			if len(result.Children) < 2 {
				t.Errorf("children = %+v, want the two fixture stories", result.Children)
			}
			for _, child := range result.Children {
				if child.Field != "parent" {
					t.Errorf("a child reference must sit in `parent`: %+v", child)
				}
			}
		})

		t.Run("an unknown project is a not_found", func(t *testing.T) {
			code, _ := wsFail(t, w, "item.references", map[string]any{"id": "NOPE-US-0001"})
			if code != "not_found" {
				t.Errorf("code = %q, want not_found", code)
			}
		})

		t.Run("an id that is not an id is an invalid_request", func(t *testing.T) {
			code, _ := wsFail(t, w, "item.references", map[string]any{"id": "not an id"})
			if code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", code)
			}
		})
	})
}
