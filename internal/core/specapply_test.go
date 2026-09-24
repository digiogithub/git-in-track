package core

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// deltaStoreFixture creates ACME-SP-0001 with R1 and R2 and a story in review
// whose Spec Delta modifies R1.
func deltaStoreFixture(t *testing.T) (*FileStore, *MemFS, *Item, *Item) {
	t.Helper()
	store, fsys, _ := newTestStore(t)
	ctx := context.Background()
	spec, err := store.Create(ctx, ItemDraft{
		Type: TypeSpec, Title: "Sign-in",
		Body: "## Requirements\n\n### ACME-SP-0001.R1 — Accept SSO\n\nThe system SHALL accept SSO.\n\n" +
			"### ACME-SP-0001.R2 — Accept passwords\n\nThe system SHALL accept passwords.\n",
	})
	if err != nil {
		t.Fatalf("create spec: %v", err)
	}
	story, err := store.Create(ctx, ItemDraft{
		Type: TypeStory, Title: "Tighten SSO", Status: "in_review",
		Body: "## Spec Delta\n\n### MODIFIED ACME-SP-0001.R1 — Accept SSO only\n\nThe system SHALL accept SSO only.\n",
	})
	if err != nil {
		t.Fatalf("create story: %v", err)
	}
	return store, fsys, spec, story
}

func readFile(t *testing.T, fsys FS, p string) string {
	t.Helper()
	data, err := fsys.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(data)
}

func TestDoneHookSeesTheAppliedDelta(t *testing.T) {
	t.Parallel()
	store, fsys, spec, story := deltaStoreFixture(t)
	var seen []RequirementRef
	store.DoneHook = func(tr *DoneTransition) error {
		seen = append(seen, tr.Refs...)
		// The hook stages a change of its own, written in the same transaction:
		// the seam the verification stamp (GIT-US-0116) uses.
		s, err := tr.Spec("ACME-SP-0001")
		if err != nil {
			return err
		}
		s.Requirements["R2"] = &Requirement{Status: "todo"}
		return nil
	}
	_, applied, err := store.MoveReport(context.Background(), story.ID, "done", story.Rev, MoveOptions{})
	if err != nil {
		t.Fatalf("MoveReport: %v", err)
	}
	if want := []RequirementRef{{Spec: "ACME-SP-0001", Number: 1}}; !reflect.DeepEqual(seen, want) {
		t.Errorf("hook refs = %v, want %v", seen, want)
	}
	if applied == nil || !reflect.DeepEqual(applied.Specs, []ItemID{"ACME-SP-0001"}) {
		t.Errorf("applied = %+v", applied)
	}
	text := readFile(t, fsys, spec.Path)
	if !strings.Contains(text, "  R2:\n    status: todo\n") || !strings.Contains(text, "The system SHALL accept SSO only.") {
		t.Errorf("spec after the transition:\n%s", text)
	}
}

func TestDoneHookErrorRefusesTheMove(t *testing.T) {
	t.Parallel()
	store, fsys, spec, story := deltaStoreFixture(t)
	specBefore, storyBefore := readFile(t, fsys, spec.Path), readFile(t, fsys, story.Path)
	store.DoneHook = func(*DoneTransition) error { return errors.New("refused by the hook") }
	if _, err := store.Move(context.Background(), story.ID, "done", story.Rev); err == nil {
		t.Fatal("the move succeeded")
	}
	if readFile(t, fsys, spec.Path) != specBefore || readFile(t, fsys, story.Path) != storyBefore {
		t.Error("a refused transition changed a file")
	}
}

func TestSpecDeltaRefusesASpecChangedOnDisk(t *testing.T) {
	t.Parallel()
	store, fsys, spec, story := deltaStoreFixture(t)
	storyBefore := readFile(t, fsys, story.Path)
	// Another process edits the spec after the store read it and before it
	// writes: the file-level rev check refuses the whole transition.
	edited := strings.Replace(readFile(t, fsys, spec.Path), "accept passwords.", "accept passkeys.", 1)
	store.DoneHook = func(*DoneTransition) error { return fsys.WriteFile(spec.Path, []byte(edited)) }
	_, err := store.Move(context.Background(), story.ID, "done", story.Rev)
	var stale *StaleRevisionError
	if !errors.As(err, &stale) || stale.Path != spec.Path {
		t.Fatalf("err = %v, want a stale revision of the spec", err)
	}
	if readFile(t, fsys, story.Path) != storyBefore {
		t.Error("the story changed")
	}
	if readFile(t, fsys, spec.Path) != edited {
		t.Error("the other process's edit was overwritten")
	}
}

func TestSpecDeltaRollsBackAFailedWrite(t *testing.T) {
	t.Parallel()
	store, fsys, spec, story := deltaStoreFixture(t)
	specBefore, storyBefore := readFile(t, fsys, spec.Path), readFile(t, fsys, story.Path)
	// The story is written first; the spec's rename fails: the story is
	// restored.
	broken := NewStore(&failingFS{FS: fsys, failRenameTo: spec.Path}, "docs", store.cfg)
	broken.Clock = store.Clock
	if _, err := broken.Move(context.Background(), story.ID, "done", story.Rev); err == nil {
		t.Fatal("the move succeeded over a failing rename")
	}
	if readFile(t, fsys, spec.Path) != specBefore || readFile(t, fsys, story.Path) != storyBefore {
		t.Error("a failed transition left a file changed")
	}
}

func TestSpecDeltaNeedsACancelledStatus(t *testing.T) {
	t.Parallel()
	store, _, _, _ := deltaStoreFixture(t)
	story, err := store.Create(context.Background(), ItemDraft{
		Type: TypeStory, Title: "Drop passwords", Status: "in_review",
		Body: "## Spec Delta\n\n### REMOVED ACME-SP-0001.R2 — Accept passwords\n\nReason: SSO only.\n",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	cfg := *store.cfg
	cfg.Workflow.Statuses = nil
	for _, st := range store.cfg.Workflow.Statuses {
		if st.Category != CategoryCancelled {
			cfg.Workflow.Statuses = append(cfg.Workflow.Statuses, st)
		}
	}
	store.cfg = &cfg
	_, err = store.Move(context.Background(), story.ID, "done", story.Rev)
	if !errors.Is(err, ErrSpecDeltaConflict) || !strings.Contains(err.Error(), "no cancelled-category status") {
		t.Errorf("err = %v", err)
	}
}
