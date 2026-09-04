package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// The optimistic lock of the agent surface (GIT-US-0025).
//
// Every write tool takes the rev of the read it is based on, refuses to run
// without one, and refuses to run with one that is no longer current. These
// tests drive real client sessions, so what they assert is what an agent
// receives: the JSON error object, not an internal value.

// agentSession opens a second client session against the same server, which is
// how a test plays two agents against one workspace.
func (h *harness) agentSession(t *testing.T) *harness {
	t.Helper()

	other := *h
	other.session = connect(t, h.server)
	return &other
}

func TestWritesRequireARev(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		args  map[string]any
		field string
	}{
		{
			name:  "update_item without a rev",
			tool:  "update_item",
			args:  map[string]any{"id": "DEMO-US-0002", "rev": "", "priority": "low"},
			field: "rev",
		},
		{
			name:  "add_comment without a rev",
			tool:  "add_comment",
			args:  map[string]any{"id": "DEMO-US-0002", "rev": " ", "body": "Looking at this."},
			field: "rev",
		},
		{
			name: "move_on_board without a board rev",
			tool: "move_on_board",
			args: map[string]any{
				"board": "delivery", "ref": "DEMO/DEMO-US-0001", "toColumn": "todo",
				"rev": "", "itemRev": wildcardRev,
			},
			field: "rev",
		},
		{
			name: "move_on_board without an item rev",
			tool: "move_on_board",
			args: map[string]any{
				"board": "delivery", "ref": "DEMO/DEMO-US-0001", "toColumn": "todo",
				"rev": wildcardRev, "itemRev": "",
			},
			field: "itemRev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, true)
			got := callFails(t, h, tt.tool, tt.args)
			if got.Code != codePreconditionRequired {
				t.Errorf("code = %q, want %q (%s)", got.Code, codePreconditionRequired, got.Message)
			}
			if got.Field != tt.field {
				t.Errorf("field = %q, want %q", got.Field, tt.field)
			}
			if got.Retry == "" || got.Expected == nil {
				t.Errorf("error = %+v, want it to say what a correct value looks like", got)
			}
		})
	}
}

// TestWriteToolsAdvertiseTheirRev checks the half of the contract a client sees
// before it calls anything: rev is a required property of every write schema,
// so a well-behaved agent cannot omit it by accident.
func TestWriteToolsAdvertiseTheirRev(t *testing.T) {
	h := newHarness(t, true)
	listed, err := h.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	want := map[string][]string{
		"update_item":   {"rev"},
		"add_comment":   {"rev"},
		"move_on_board": {"rev", "itemRev"},
	}
	for _, tool := range listed.Tools {
		fields, guarded := want[tool.Name]
		if !guarded {
			continue
		}
		t.Run(tool.Name, func(t *testing.T) {
			for _, field := range fields {
				if !required(tool.InputSchema, field) {
					t.Errorf("%s does not declare %q as required", tool.Name, field)
				}
			}
		})
	}
}

// required reports whether a tool's input schema demands a property. The
// schema travels as JSON, so it is read as JSON.
func required(schema any, field string) bool {
	raw, err := json.Marshal(schema)
	if err != nil {
		return false
	}
	var decoded struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	return contains(decoded.Required, field)
}

func TestStaleRevisionTeachesTheRetry(t *testing.T) {
	h := newHarness(t, true)
	before := call[ItemResult](t, h, "get_item", map[string]any{"id": "DEMO-US-0002"})

	// Someone else writes first, which makes the rev the caller holds stale.
	after := call[WriteResult](t, h, "update_item", map[string]any{
		"id": "DEMO-US-0002", "rev": before.Item.Rev, "priority": "low",
	})

	tests := []struct {
		name       string
		tool       string
		args       map[string]any
		wantFields []string
	}{
		{
			name: "update_item names the fields still in conflict",
			tool: "update_item",
			args: map[string]any{
				"id": "DEMO-US-0002", "rev": before.Item.Rev,
				"priority": "critical", "title": "Reworked title",
			},
			wantFields: []string{"title", "priority"},
		},
		{
			name: "a status change is refused with the status named",
			tool: "update_item",
			args: map[string]any{
				"id": "DEMO-US-0002", "rev": before.Item.Rev, "status": "in_progress",
			},
			wantFields: []string{"status"},
		},
		{
			name: "add_comment is refused on an item the agent has not seen",
			tool: "add_comment",
			args: map[string]any{
				"id": "DEMO-US-0002", "rev": before.Item.Rev, "body": "Working on it.",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callFails(t, h, tt.tool, tt.args)
			if got.Code != "stale_revision" {
				t.Fatalf("code = %q, want stale_revision (%s)", got.Code, got.Message)
			}
			if got.CurrentRev != after.Item.Rev {
				t.Errorf("currentRev = %q, want the rev on disk %q", got.CurrentRev, after.Item.Rev)
			}
			if got.Path == "" {
				t.Error("the conflict does not name the file it is about")
			}
			if !strings.Contains(got.Retry, "get_item") {
				t.Errorf("retry = %q, want the recovery protocol", got.Retry)
			}
			var named []string
			for _, c := range got.Conflicts {
				named = append(named, c.Field)
			}
			for _, want := range tt.wantFields {
				if !contains(named, want) {
					t.Errorf("conflicts = %v, want %q among them", named, want)
				}
			}
		})
	}
}

// contains reports whether a list holds a value.
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// TestTwoAgentsCannotLoseAnUpdate is the scenario the story exists for: two
// agents read one story, both decide to claim it, and both write. Exactly one
// write lands; the loser is told so, is handed the current rev, and its retry
// preserves what the winner wrote instead of overwriting it.
func TestTwoAgentsCannotLoseAnUpdate(t *testing.T) {
	h := newHarness(t, true)
	first := h.agentSession(t)
	second := h.agentSession(t)

	const story = "DEMO-US-0002"

	// Both agents read the same story, so both hold the same rev.
	readByFirst := call[ItemResult](t, first, "get_item", map[string]any{"id": story})
	readBySecond := call[ItemResult](t, second, "get_item", map[string]any{"id": story})
	if readByFirst.Item.Rev != readBySecond.Item.Rev {
		t.Fatalf("the two reads disagree: %q and %q", readByFirst.Item.Rev, readBySecond.Item.Rev)
	}

	// The first agent claims the story and starts it.
	won := call[WriteResult](t, first, "update_item", map[string]any{
		"id": story, "rev": readByFirst.Item.Rev,
		"assignees": []string{"agent-one"}, "status": "in_progress",
	})
	if won.Item.Status != "in_progress" || len(won.Item.Assignees) != 1 {
		t.Fatalf("the first write did not land: %+v", won.Item)
	}

	// The second agent claims the same story from the same read. It must be
	// refused, not applied on top.
	lost := callFails(t, second, "update_item", map[string]any{
		"id": story, "rev": readBySecond.Item.Rev,
		"assignees": []string{"agent-two"}, "status": "in_progress",
	})
	if lost.Code != "stale_revision" {
		t.Fatalf("the second write was not refused: %+v", lost)
	}
	if lost.CurrentRev != won.Item.Rev {
		t.Errorf("currentRev = %q, want %q", lost.CurrentRev, won.Item.Rev)
	}

	// The claim is settled: the first agent's assignee is on disk, untouched.
	settled := call[ItemResult](t, second, "get_item", map[string]any{"id": story})
	if len(settled.Item.Assignees) != 1 || settled.Item.Assignees[0] != "agent-one" {
		t.Fatalf("assignees = %v, want the first agent's claim intact", settled.Item.Assignees)
	}

	// The loser retries the way the error taught it: one round trip, quoting
	// the rev it was handed, and writing only what it still wants — a comment
	// rather than a second claim.
	note := call[CommentResult](t, second, "add_comment", map[string]any{
		"id": story, "rev": lost.CurrentRev,
		"body": "agent-one is on this; picking up something else.",
	})
	if note.Comment.Rev == "" {
		t.Errorf("the retry produced no comment: %+v", note)
	}

	// And the write events the host saw are exactly the writes that happened.
	var ops []string
	for _, ev := range h.writes {
		ops = append(ops, ev.Tool+":"+ev.Op)
	}
	if len(ops) != 2 || ops[0] != "update_item:moved" || ops[1] != "add_comment:commented" {
		t.Errorf("write events = %v, want one move and one comment", ops)
	}
}

// TestOneWriteForFieldsAndStatus pins the shape of the fix for the two-write
// hazard: a patch that changes fields *and* status is a single conditional
// write, so a caller's rev can never be silently replaced by one the caller
// never saw.
func TestOneWriteForFieldsAndStatus(t *testing.T) {
	h := newHarness(t, true)
	before := call[ItemResult](t, h, "get_item", map[string]any{"id": "DEMO-US-0002"})
	got := call[WriteResult](t, h, "update_item", map[string]any{
		"id": "DEMO-US-0002", "rev": before.Item.Rev,
		"status": "in_progress", "labels": []string{"claimed"},
	})
	if got.Item.Status != "in_progress" || len(got.Item.Labels) != 1 {
		t.Fatalf("item = %+v, want both halves of the patch applied", got.Item)
	}
	if len(h.writes) != 1 {
		t.Fatalf("write events = %d, want exactly one write", len(h.writes))
	}
	if h.writes[0].Method != "item.update" {
		t.Errorf("method = %q, want the single conditional write", h.writes[0].Method)
	}
	if len(got.Changed) != 1 {
		t.Errorf("changed = %v, want one file", got.Changed)
	}
}
