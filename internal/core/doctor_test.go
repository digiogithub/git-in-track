package core

import (
	"context"
	"strings"
	"testing"
)

// vaultSnapshot returns every file of a MemFS, so that a test can prove nothing
// changed.
func vaultSnapshot(t *testing.T, fsys *MemFS) map[string]string {
	t.Helper()

	out := make(map[string]string)
	for _, p := range fsys.Paths() {
		data, err := fsys.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		out[p] = string(data)
	}
	return out
}

func TestFindDuplicateIDs(t *testing.T) {
	t.Parallel()

	fsys := storeFixtureFS(t, "duplicates")
	dups, err := FindDuplicateIDs(fsys, "docs")
	if err != nil {
		t.Fatalf("FindDuplicateIDs: %v", err)
	}
	if len(dups) != 1 {
		t.Fatalf("got %d duplicates, want 1: %+v", len(dups), dups)
	}
	d := dups[0]
	if d.ID != "ACME-US-0043" || len(d.Files) != 2 {
		t.Fatalf("duplicate = %+v", d)
	}
	if !strings.HasSuffix(d.Files[0].Path, "ACME-US-0043-login-with-sso.md") {
		t.Errorf("the older file must come first, got %s", d.Files[0].Path)
	}
	if !strings.HasSuffix(d.Files[1].Path, "ACME-US-0043-reset-password.md") {
		t.Errorf("second file = %s", d.Files[1].Path)
	}
	diag := d.Diagnostic()
	if diag.Code != CodeIDDuplicate || diag.Severity != SeverityError ||
		!strings.Contains(diag.Message, "ACME-US-0043-reset-password.md") {
		t.Errorf("diagnostic = %+v", diag)
	}
}

func TestDuplicateTieBreak(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		items []scannedItem
		want  []string
	}{
		{
			name: "the earlier created keeps the id",
			items: []scannedItem{
				{Path: "stories/a.md", Created: mustTimestamp(t, "2026-09-01T09:41:33Z")},
				{Path: "stories/b.md", Created: mustTimestamp(t, "2026-09-01T09:12:00Z")},
			},
			want: []string{"stories/b.md", "stories/a.md"},
		},
		{
			name: "on a tie the smaller path wins",
			items: []scannedItem{
				{Path: "stories/b.md", Created: mustTimestamp(t, "2026-09-01T09:12:00Z")},
				{Path: "stories/a.md", Created: mustTimestamp(t, "2026-09-01T09:12:00Z")},
			},
			want: []string{"stories/a.md", "stories/b.md"},
		},
		{
			name: "a file without a created timestamp cannot claim seniority",
			items: []scannedItem{
				{Path: "stories/a.md"},
				{Path: "stories/b.md", Created: mustTimestamp(t, "2026-09-01T09:12:00Z")},
			},
			want: []string{"stories/b.md", "stories/a.md"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sortByTieBreak(tc.items)
			for i, want := range tc.want {
				if tc.items[i].Path != want {
					t.Errorf("position %d = %s, want %s", i, tc.items[i].Path, want)
				}
			}
		})
	}
}

func TestPlanRenumber(t *testing.T) {
	t.Parallel()

	fsys := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fsys)
	dups, err := FindDuplicateIDs(fsys, "docs")
	if err != nil {
		t.Fatalf("FindDuplicateIDs: %v", err)
	}
	alloc := NewAllocator(fsys, "docs", cfg)
	plan, err := PlanRenumber(context.Background(), dups, alloc)
	if err != nil {
		t.Fatalf("PlanRenumber: %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("plan = %+v, want one step", plan)
	}
	step := plan[0]
	switch {
	case step.OldID != "ACME-US-0043":
		t.Errorf("old id = %s", step.OldID)
	case step.NewID != "ACME-US-0044":
		t.Errorf("new id = %s, want the next free number", step.NewID)
	case step.Path != "docs/.pmngr/stories/ACME-US-0043-reset-password.md":
		t.Errorf("path = %s", step.Path)
	case step.NewPath != "docs/.pmngr/stories/ACME-US-0044-reset-password.md":
		t.Errorf("new path = %s, the slug must survive", step.NewPath)
	case step.Keeper != "docs/.pmngr/stories/ACME-US-0043-login-with-sso.md":
		t.Errorf("keeper = %s", step.Keeper)
	case step.Type != TypeStory:
		t.Errorf("type = %s", step.Type)
	}
	// Planning must not write anything.
	if _, err := fsys.Stat(step.NewPath); err == nil {
		t.Error("PlanRenumber created the new file")
	}
}

func TestApplyRenumber(t *testing.T) {
	t.Parallel()

	fsys := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fsys)
	ctx := context.Background()

	keeperBefore, err := fsys.ReadFile("docs/.pmngr/stories/ACME-US-0043-login-with-sso.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	dups, err := FindDuplicateIDs(fsys, "docs")
	if err != nil {
		t.Fatalf("FindDuplicateIDs: %v", err)
	}
	plan, err := PlanRenumber(ctx, dups, NewAllocator(fsys, "docs", cfg))
	if err != nil {
		t.Fatalf("PlanRenumber: %v", err)
	}
	res, err := ApplyRenumber(ctx, fsys, "docs", plan)
	if err != nil {
		t.Fatalf("ApplyRenumber: %v", err)
	}

	// The renumbered file moved and carries the new id.
	if _, err := fsys.Stat("docs/.pmngr/stories/ACME-US-0043-reset-password.md"); err == nil {
		t.Error("the old file survived the renumber")
	}
	moved, err := fsys.ReadFile("docs/.pmngr/stories/ACME-US-0044-reset-password.md")
	if err != nil {
		t.Fatalf("read the renumbered file: %v", err)
	}
	if !strings.Contains(string(moved), "id: ACME-US-0044") {
		t.Errorf("the id field was not rewritten:\n%s", moved)
	}
	if !strings.Contains(string(moved), "title: Reset password") {
		t.Errorf("the renumbered file lost its content:\n%s", moved)
	}

	// The keeper is untouched.
	keeperAfter, err := fsys.ReadFile("docs/.pmngr/stories/ACME-US-0043-login-with-sso.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(keeperBefore) != string(keeperAfter) {
		t.Errorf("the keeper was rewritten:\n%s", keeperAfter)
	}

	// Inbound references followed the item, in the front matter and in the body.
	task, err := fsys.ReadFile("docs/.pmngr/tasks/ACME-T-0107-add-oidc-discovery-client.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(task)
	for _, want := range []string{
		"parent: ACME-US-0044",
		"{ kind: blocks, target: ACME-US-0044 }",
		"[[ACME-US-0044]]",
		"[[ACME-US-0044|the SSO story]]",
		"[[architecture/sso-overview]]",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the task is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ACME-US-0043") {
		t.Errorf("the task still references the old id:\n%s", text)
	}

	// The redirect is recorded and the rest of project.yaml survived.
	project, err := fsys.ReadFile("docs/.pmngr/project.yaml")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(project), "ACME-US-0043: ACME-US-0044") {
		t.Errorf("no redirect was recorded:\n%s", project)
	}
	if !strings.Contains(string(project), "# hints only, NOT authoritative") {
		t.Errorf("project.yaml lost its comments:\n%s", project)
	}
	reloaded, err := LoadProjectConfig(project)
	if err != nil {
		t.Fatalf("reload project.yaml: %v", err)
	}
	if reloaded.IDAllocation.Redirects["ACME-US-0043"] != "ACME-US-0044" {
		t.Errorf("redirects = %v", reloaded.IDAllocation.Redirects)
	}

	// The result describes what happened.
	if len(res.Renamed) != 1 || res.Redirects["ACME-US-0043"] != "ACME-US-0044" {
		t.Errorf("result = %+v", res)
	}
	fields := map[string]bool{}
	for _, ref := range res.References {
		fields[ref.Field] = true
	}
	for _, want := range []string{"parent", "links", "body"} {
		if !fields[want] {
			t.Errorf("no reference update was reported for %s: %+v", want, res.References)
		}
	}
	if len(res.Warnings) == 0 {
		t.Error("the ambiguity of the comments folder was not reported")
	}

	// The vault is clean afterwards.
	after, err := FindDuplicateIDs(fsys, "docs")
	if err != nil {
		t.Fatalf("FindDuplicateIDs: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("duplicates remain: %+v", after)
	}
	for _, p := range fsys.Paths() {
		if strings.HasSuffix(p, ".tmp") {
			t.Errorf("a temporary file survived: %s", p)
		}
	}

	// A store built on the repaired vault resolves the old id through the
	// redirect table.
	store := NewStore(fsys, "docs", reloaded)
	got, err := store.Get(ctx, "ACME-US-0043")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "ACME-US-0043" {
		t.Errorf("the keeper is no longer reachable under its own id: %s", got.ID)
	}
}

func TestApplyRenumberRollsBackOnFailure(t *testing.T) {
	t.Parallel()

	fsys := storeFixtureFS(t, "duplicates")
	cfg := storeFixtureConfig(t, fsys)
	ctx := context.Background()
	before := vaultSnapshot(t, fsys)

	dups, err := FindDuplicateIDs(fsys, "docs")
	if err != nil {
		t.Fatalf("FindDuplicateIDs: %v", err)
	}
	plan, err := PlanRenumber(ctx, dups, NewAllocator(fsys, "docs", cfg))
	if err != nil {
		t.Fatalf("PlanRenumber: %v", err)
	}

	failing := &failingFS{FS: fsys, failWriteTo: "docs/.pmngr/tasks/ACME-T-0107-add-oidc-discovery-client.md"}
	if _, err := ApplyRenumber(ctx, failing, "docs", plan); err == nil {
		t.Fatal("ApplyRenumber succeeded over a failing write")
	}

	after := vaultSnapshot(t, fsys)
	if len(after) != len(before) {
		t.Fatalf("the vault has %d files, want the original %d: %v", len(after), len(before), fsys.Paths())
	}
	for p, want := range before {
		if after[p] != want {
			t.Errorf("%s changed despite the rollback:\n--- got ---\n%s\n--- want ---\n%s", p, after[p], want)
		}
	}
}

func TestApplyRenumberRejectsABrokenPlan(t *testing.T) {
	t.Parallel()

	fsys := storeFixtureFS(t, "duplicates")
	ctx := context.Background()
	plan := []Renumber{{
		OldID: "ACME-US-0043", NewID: "ACME-US-0043", Type: TypeStory,
		Path:    "docs/.pmngr/stories/ACME-US-0043-reset-password.md",
		NewPath: "docs/.pmngr/stories/ACME-US-0043-reset-password.md",
	}}
	if _, err := ApplyRenumber(ctx, fsys, "docs", plan); err == nil {
		t.Fatal("ApplyRenumber accepted an identity mapping")
	}
	if res, err := ApplyRenumber(ctx, fsys, "docs", nil); err != nil || len(res.Renamed) != 0 {
		t.Fatalf("an empty plan = %+v, %v", res, err)
	}
}

func TestRewriteWikilinks(t *testing.T) {
	t.Parallel()

	mapping := map[ItemID]ItemID{"ACME-US-0043": "ACME-US-0044"}
	cases := []struct {
		in   string
		want string
	}{
		{"see [[ACME-US-0043]]", "see [[ACME-US-0044]]"},
		{"see [[ACME-US-0043|the story]]", "see [[ACME-US-0044|the story]]"},
		{"see [[ACME-US-0043#20260901T104512Z-jose]]", "see [[ACME-US-0044#20260901T104512Z-jose]]"},
		{"see [[ACME-US-0043 ]]", "see [[ACME-US-0044]]"},
		{"untouched [[ACME-US-0143]] and [[architecture/x]]", "untouched [[ACME-US-0143]] and [[architecture/x]]"},
		{"plain ACME-US-0043 text", "plain ACME-US-0043 text"},
	}
	for _, tc := range cases {
		got, _ := rewriteWikilinks(tc.in, mapping)
		if got != tc.want {
			t.Errorf("rewriteWikilinks(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
