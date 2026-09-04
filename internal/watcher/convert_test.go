package watcher

import (
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
)

func ev(repo, p string, op Op) Event {
	return Event{Repo: repo, Path: p, Op: op, Time: time.Unix(0, 0).UTC()}
}

func TestToFileEventsMapsEveryOp(t *testing.T) {
	tests := []struct {
		name  string
		batch []Event
		want  []core.FileEvent
	}{
		{
			name:  "create",
			batch: []Event{ev("p", "a.md", Create)},
			want:  []core.FileEvent{{Kind: core.FileCreated, Path: "a.md"}},
		},
		{
			name:  "write",
			batch: []Event{ev("p", "a.md", Write)},
			want:  []core.FileEvent{{Kind: core.FileModified, Path: "a.md"}},
		},
		{
			name:  "chmod is treated as a modification",
			batch: []Event{ev("p", "a.md", Chmod)},
			want:  []core.FileEvent{{Kind: core.FileModified, Path: "a.md"}},
		},
		{
			name:  "remove",
			batch: []Event{ev("p", "a.md", Remove)},
			want:  []core.FileEvent{{Kind: core.FileRemoved, Path: "a.md"}},
		},
		{
			name:  "a lone rename is a removal",
			batch: []Event{ev("p", "a.md", Rename)},
			want:  []core.FileEvent{{Kind: core.FileRemoved, Path: "a.md"}},
		},
		{
			name:  "a rename paired with a create keeps the old path",
			batch: []Event{ev("p", "old.md", Rename), ev("p", "new.md", Create)},
			want:  []core.FileEvent{{Kind: core.FileRenamed, Path: "new.md", OldPath: "old.md"}},
		},
		{
			name: "an ambiguous rename degrades to remove plus create",
			batch: []Event{
				ev("p", "old.md", Rename),
				ev("p", "new.md", Create),
				ev("p", "other.md", Create),
			},
			want: []core.FileEvent{
				{Kind: core.FileRemoved, Path: "old.md"},
				{Kind: core.FileCreated, Path: "new.md"},
				{Kind: core.FileCreated, Path: "other.md"},
			},
		},
		{
			name: "renames are paired within a repository, not across repositories",
			batch: []Event{
				ev("a", "old.md", Rename),
				ev("b", "new.md", Create),
			},
			want: []core.FileEvent{
				{Kind: core.FileRemoved, Path: "old.md"},
				{Kind: core.FileCreated, Path: "new.md"},
			},
		},
		{
			name:  "an empty batch maps to nothing",
			batch: nil,
			want:  nil,
		},
		{
			name: "a mixed batch keeps its order",
			batch: []Event{
				ev("p", "a.md", Write),
				ev("p", "b.md", Remove),
				ev("p", "c.md", Chmod),
			},
			want: []core.FileEvent{
				{Kind: core.FileModified, Path: "a.md"},
				{Kind: core.FileRemoved, Path: "b.md"},
				{Kind: core.FileModified, Path: "c.md"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ToFileEvents(tc.batch)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d events %+v, want %d %+v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("event %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestToFileEventsCoversEveryOp fails when a new Op is added without a mapping.
func TestToFileEventsCoversEveryOp(t *testing.T) {
	for _, op := range []Op{Create, Write, Remove, Rename, Chmod} {
		got := ToFileEvents([]Event{ev("p", "a.md", op)})
		if len(got) != 1 {
			t.Errorf("op %q produced %d core events, want 1", op, len(got))
			continue
		}
		switch got[0].Kind {
		case core.FileCreated, core.FileModified, core.FileRemoved, core.FileRenamed:
		default:
			t.Errorf("op %q produced an unknown kind %q", op, got[0].Kind)
		}
	}
}

func TestGroupByRepo(t *testing.T) {
	batch := []Event{
		ev("a", "one.md", Create),
		ev("b", "two.md", Write),
		ev("a", "three.md", Remove),
	}
	groups := GroupByRepo(batch)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	if len(groups["a"]) != 2 || groups["a"][0].Path != "one.md" || groups["a"][1].Path != "three.md" {
		t.Errorf("group a = %+v", groups["a"])
	}
	if len(groups["b"]) != 1 || groups["b"][0].Path != "two.md" {
		t.Errorf("group b = %+v", groups["b"])
	}
	if GroupByRepo(nil) != nil {
		t.Error("GroupByRepo(nil) should be nil")
	}
}
