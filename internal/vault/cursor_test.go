package vault

import (
	"testing"
	"time"
)

// The browser runs item.list and inbox.list through this dispatcher, and so
// does the companion's REST API: a cursor presented with a changed filter has
// to be refused here, not only by the MCP layer (GIT-US-0156).

// Verifies: GIT-SP-0004.R5
func TestItemListCursorIsBoundToTheFilter(t *testing.T) {
	v := inboxVault(t)
	base := map[string]any{"project": "INBX", "sort": "id", "limit": 1}
	first := decode[itemPage](t, call(t, v, "item.list", base))
	if first.NextCursor == "" {
		t.Fatal("the fixture does not produce a second page")
	}
	with := func(extra map[string]any) map[string]any {
		args := map[string]any{"cursor": first.NextCursor}
		for k, val := range base {
			args[k] = val
		}
		for k, val := range extra {
			args[k] = val
		}
		return args
	}

	refused := map[string]map[string]any{
		"type":         {"type": []string{"story"}},
		"status":       {"status": []string{"done"}},
		"category":     {"category": []string{"done"}},
		"label":        {"label": []string{"agent-ok"}},
		"text":         {"text": "existing"},
		"updatedSince": {"updatedSince": "7d"},
		"order":        {"order": "desc"},
	}
	for name, extra := range refused {
		t.Run("refuses a changed "+name, func(t *testing.T) {
			env := rawCall(t, v, "item.list", with(extra))
			if env.OK || env.Error.Code != "invalid_cursor" {
				t.Fatalf("envelope = %+v, want invalid_cursor", env)
			}
		})
	}

	t.Run("a relative updatedSince survives the clock", func(t *testing.T) {
		args := map[string]any{"project": "INBX", "sort": "id", "limit": 1, "updatedSince": "30d"}
		page := decode[itemPage](t, call(t, v, "item.list", args))
		if page.NextCursor == "" {
			t.Fatal("the fixture does not produce a second page")
		}
		v.SetClock(func() time.Time { return inboxClock.Add(time.Minute) })
		defer v.SetClock(func() time.Time { return inboxClock })
		args["cursor"] = page.NextCursor
		call(t, v, "item.list", args)
	})
}

// Verifies: GIT-SP-0004.R5
func TestInboxListCursorIsBoundToTheFilter(t *testing.T) {
	v := inboxVault(t)
	submit(t, v, "First submission", "web")
	submit(t, v, "Second submission", "web")
	base := map[string]any{"project": "INBX", "sort": "id", "limit": 1}
	first := decode[InboxPage](t, call(t, v, "inbox.list", base))
	if first.NextCursor == "" {
		t.Fatal("the fixture does not produce a second page")
	}

	t.Run("refuses a changed state filter", func(t *testing.T) {
		env := rawCall(t, v, "inbox.list", map[string]any{
			"project": "INBX", "sort": "id", "limit": 1, "status": "rejected", "cursor": first.NextCursor,
		})
		if env.OK || env.Error.Code != "invalid_cursor" {
			t.Fatalf("envelope = %+v, want invalid_cursor", env)
		}
	})

	t.Run("refuses an item.list cursor", func(t *testing.T) {
		items := decode[itemPage](t, call(t, v, "item.list", base))
		env := rawCall(t, v, "inbox.list", map[string]any{
			"project": "INBX", "sort": "id", "limit": 1, "cursor": items.NextCursor,
		})
		if env.OK || env.Error.Code != "invalid_cursor" {
			t.Fatalf("envelope = %+v, want invalid_cursor", env)
		}
	})

	t.Run("the snooze clock moving is the same walk", func(t *testing.T) {
		v.SetClock(func() time.Time { return inboxClock.Add(time.Hour) })
		defer v.SetClock(func() time.Time { return inboxClock })
		next := decode[InboxPage](t, call(t, v, "inbox.list", map[string]any{
			"project": "INBX", "sort": "id", "limit": 1, "cursor": first.NextCursor,
		}))
		if len(next.Items) == 0 || next.Items[0].ID == first.Items[0].ID {
			t.Errorf("the walk did not continue: %+v", next.Items)
		}
	})
}
