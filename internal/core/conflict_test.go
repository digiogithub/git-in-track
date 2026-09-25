package core

import (
	"context"
	"errors"
	"testing"
)

// TestUpdateReportsTheFieldsInConflict pins what a refused conditional write
// tells the caller: the revision on disk now, and the fields the write would
// still have changed against it.
// Verifies: GIT-SP-0001.R2, GIT-SP-0001.R3, GIT-SP-0001.R4
func TestUpdateReportsTheFieldsInConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	high := PriorityHigh
	critical := PriorityCritical
	claimed := []string{"marta"}
	body := "## Description\n\nRewritten."
	blank := "   "
	renamed := "Login with SSO everywhere"

	tests := []struct {
		name  string
		first ItemPatch
		then  ItemPatch
		want  []ConflictField
	}{
		{
			name:  "a field both writers changed",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Priority: &critical},
			want:  []ConflictField{{Field: "priority", Current: "high", Proposed: "critical"}},
		},
		{
			name:  "a field only the loser changes",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Assignees: &claimed},
			want:  []ConflictField{{Field: "assignees", Current: "jose", Proposed: "marta"}},
		},
		{
			name:  "a change the winner already made is no conflict",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Priority: &high},
			want:  nil,
		},
		{
			name:  "the body is named but never quoted",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Body: &body},
			want:  []ConflictField{{Field: "body"}},
		},
		{
			// A patch the store would refuse anyway (GIT-US-0152) must never
			// come back with an empty list, which reads as "already applied".
			name:  "a refused patch names every field it carries",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Title: &blank, Priority: &critical},
			want: []ConflictField{
				{Field: "title", Current: "Login with SSO", Proposed: "   "},
				{Field: "priority", Current: "high", Proposed: "critical"},
			},
		},
		{
			name:  "a refused patch names a field already on disk too",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Priority: &high, Unset: []string{"no-such-field"}},
			want: []ConflictField{
				{Field: "priority", Current: "high", Proposed: "high"},
				{Field: "no-such-field"},
			},
		},
		{
			name:  "a partial overlap names only the fields still to change",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Priority: &high, Title: &renamed},
			want:  []ConflictField{{Field: "title", Current: "Login with SSO", Proposed: renamed}},
		},
		{
			name:  "a custom field still to change is named",
			first: ItemPatch{Priority: &high},
			then:  ItemPatch{Custom: map[string]any{"team": "core"}},
			want:  []ConflictField{{Field: "custom"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, _, _ := newTestStore(t)
			it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			stale := it.Rev
			winner, err := store.Update(ctx, it.ID, tt.first, stale)
			if err != nil {
				t.Fatalf("the first write failed: %v", err)
			}

			_, err = store.Update(ctx, it.ID, tt.then, stale)
			var conflict *StaleRevisionError
			if !errors.As(err, &conflict) {
				t.Fatalf("error = %v, want a stale revision", err)
			}
			if conflict.Current != winner.Rev {
				t.Errorf("current = %q, want %q", conflict.Current, winner.Rev)
			}
			if len(conflict.Fields) != len(tt.want) {
				t.Fatalf("fields = %+v, want %+v", conflict.Fields, tt.want)
			}
			for i, want := range tt.want {
				if conflict.Fields[i] != want {
					t.Errorf("field %d = %+v, want %+v", i, conflict.Fields[i], want)
				}
			}
		})
	}
}

// TestUpdateValidatesAStatusChange is the reason a caller never needs two
// writes to change fields and status at once: the patch itself goes through the
// workflow, so one conditional write does both or neither.
func TestUpdateValidatesAStatusChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	done := Status("done")
	todo := Status("todo")

	tests := []struct {
		name    string
		patch   ItemPatch
		wantErr bool
		want    Status
	}{
		{
			name:  "a declared transition is applied",
			patch: ItemPatch{Status: &todo},
			want:  todo,
		},
		{
			name:    "a transition the workflow does not declare is refused",
			patch:   ItemPatch{Status: &done},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, _, _ := newTestStore(t)
			it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			title := "Login with SSO everywhere"
			tt.patch.Title = &title

			updated, err := store.Update(ctx, it.ID, tt.patch, it.Rev)
			if tt.wantErr {
				var denied *TransitionError
				if !errors.As(err, &denied) {
					t.Fatalf("error = %v, want a refused transition", err)
				}
				current, getErr := store.Get(ctx, it.ID)
				if getErr != nil {
					t.Fatalf("Get: %v", getErr)
				}
				if current.Title != "Login with SSO" || current.Rev != it.Rev {
					t.Error("the refused write changed the file")
				}
				return
			}
			if err != nil {
				t.Fatalf("Update: %v", err)
			}
			if updated.Status != tt.want || updated.Title != title {
				t.Errorf("item = %+v, want both halves of the patch applied", updated)
			}
			if updated.Started.IsZero() == (tt.want == "in_progress") {
				t.Error("the transition was not stamped")
			}
		})
	}
}

// TestAddCommentHonorsTheItemRevision keeps a comment from being written by a
// writer that has not seen the item as it stands.
// Verifies: GIT-SP-0001.R2
func TestAddCommentHonorsTheItemRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, _, _ := newTestStore(t)
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	stale := it.Rev
	title := "Login with SSO everywhere"
	fresh, err := store.Update(ctx, it.ID, ItemPatch{Title: &title}, stale)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	t.Run("a stale item rev is refused", func(t *testing.T) {
		_, err := store.AddComment(ctx, it.ID, CommentDraft{
			Author: "claude", Body: "Picked this up.", ItemRev: stale,
		})
		var conflict *StaleRevisionError
		if !errors.As(err, &conflict) {
			t.Fatalf("error = %v, want a stale revision", err)
		}
		if conflict.Current != fresh.Rev {
			t.Errorf("current = %q, want %q", conflict.Current, fresh.Rev)
		}
	})

	t.Run("the current item rev is accepted", func(t *testing.T) {
		comment, err := store.AddComment(ctx, it.ID, CommentDraft{
			Author: "claude", Body: "Picked this up.", ItemRev: fresh.Rev,
		})
		if err != nil {
			t.Fatalf("AddComment: %v", err)
		}
		if comment.Rev == "" {
			t.Error("the comment carries no rev")
		}
	})

	t.Run("an empty item rev writes unconditionally", func(t *testing.T) {
		if _, err := store.AddComment(ctx, it.ID, CommentDraft{
			Author: "jose", Body: "Typed in the UI.",
		}); err != nil {
			t.Fatalf("AddComment: %v", err)
		}
	})
}
