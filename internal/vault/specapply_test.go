package vault

import (
	"strings"
	"testing"
	"time"
)

// The Spec Delta of a story is applied when it moves to a done-category status
// (docs/03 R-DELTA-12 to R-DELTA-16, GIT-US-0110).

// deltaItem is the part of an item the Spec Delta tests read back.
type deltaItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Rev    string `json:"rev"`
	Path   string `json:"path"`
	Body   string `json:"body"`
	Links  []struct {
		Kind   string `json:"kind"`
		Target string `json:"target"`
	} `json:"links"`
}

// deltaMoveResult is the result of an item.move or item.update that may have
// applied a Spec Delta.
type deltaMoveResult struct {
	Item      deltaItem `json:"item"`
	Writes    WriteSet  `json:"writes"`
	SpecDelta *struct {
		Item  string `json:"item"`
		Added []struct {
			Ref        string `json:"ref"`
			Supersedes string `json:"supersedes"`
		} `json:"added"`
		Modified  []string `json:"modified"`
		Removed   []string `json:"removed"`
		Unchanged []string `json:"unchanged"`
		Specs     []string `json:"specs"`
	} `json:"specDelta"`
	SchemaUpgraded int `json:"schemaUpgraded"`
}

// deltaBody is a Spec Delta over the DEMO-SP-0001 fixture of specVault: one
// addition, one replacement of R1 and the removal of R2.
const deltaBody = "## Description\n\nTighten the address rules.\n\n## Spec Delta\n\n" +
	"### ADDED DEMO-SP-0001 — Reject overlong input\n\nThe checkout SHALL refuse an address longer than 200 characters.\n\n" +
	"#### Scenario: long address\n- **WHEN** an address has 201 characters\n- **THEN** it is refused\n\n" +
	"### MODIFIED DEMO-SP-0001.R1 — Trim pasted input\n\nThe checkout SHALL trim pasted addresses on both ends.\n\n" +
	"### REMOVED DEMO-SP-0001.R2 — Reject empty input\n\nReason: an empty address is caught by the form.\n\n" +
	"## Notes\n\nNone.\n"

// deltaStory creates a story in review carrying body and returns it.
func deltaStory(t *testing.T, v *Vault, body string) deltaItem {
	t.Helper()
	raw := call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "story", "title": "Tighten addresses", "status": "in_review", "body": body,
	})
	return decode[struct {
		Item deltaItem `json:"item"`
	}](t, raw).Item
}

func getDeltaItem(t *testing.T, v *Vault, id string) deltaItem {
	t.Helper()
	return decode[deltaItem](t, call(t, v, "item.get", map[string]any{"id": id}))
}

func moveItem(t *testing.T, v *Vault, id, status string) envelope {
	t.Helper()
	rev := getDeltaItem(t, v, id).Rev
	return rawCall(t, v, "item.move", map[string]any{"id": id, "status": status, "rev": rev})
}

func hasItemLink(it deltaItem, kind, target string) bool {
	for _, l := range it.Links {
		if l.Kind == kind && l.Target == target {
			return true
		}
	}
	return false
}

func TestSpecDeltaAppliedOnDone(t *testing.T) {
	v := specVault(t)
	r1 := getRequirement(t, v, "DEMO-SP-0001.R1").Requirement
	// A durable stamp on R1: MODIFIED must leave it alone, so it goes suspect.
	stamp := map[string]any{"rev": r1.BlockRev, "commit": strings.Repeat("a", 40), "at": "2026-09-01T10:00:00Z", "by": "jose"}
	call(t, v, "requirement.update", map[string]any{"ref": "DEMO-SP-0001.R1", "rev": r1.Rev, "patch": map[string]any{"verified": stamp}})
	r1 = getRequirement(t, v, "DEMO-SP-0001.R1").Requirement

	story := deltaStory(t, v, deltaBody)
	env := moveItem(t, v, story.ID, "done")
	if !env.OK {
		t.Fatalf("move to done: %s: %s", env.Error.Code, env.Error.Message)
	}
	got := decode[deltaMoveResult](t, env.Result)

	t.Run("result", func(t *testing.T) {
		d := got.SpecDelta
		if d == nil {
			t.Fatal("no specDelta in the result")
		}
		if len(d.Added) != 1 || d.Added[0].Ref != "DEMO-SP-0001.R3" {
			t.Errorf("added = %+v, want DEMO-SP-0001.R3", d.Added)
		}
		if strings.Join(d.Modified, ",") != "DEMO-SP-0001.R1" || strings.Join(d.Removed, ",") != "DEMO-SP-0001.R2" {
			t.Errorf("modified %v removed %v", d.Modified, d.Removed)
		}
		if strings.Join(d.Specs, ",") != "DEMO-SP-0001" {
			t.Errorf("specs = %v", d.Specs)
		}
		var paths []string
		for _, f := range got.Writes.Written {
			paths = append(paths, f.Path)
		}
		if len(paths) != 2 {
			t.Errorf("writes = %v, want the story and the spec", paths)
		}
	})

	t.Run("ADDED allocates the next number with the initial status", func(t *testing.T) {
		r3 := getRequirement(t, v, "DEMO-SP-0001.R3").Requirement
		if r3.Title != "Reject overlong input" || r3.Status != "backlog" || r3.StatusImplicit {
			t.Errorf("R3 = %+v", r3)
		}
		if !strings.HasPrefix(r3.Text, "The checkout SHALL refuse an address longer") || !strings.Contains(r3.Text, "#### Scenario: long address") {
			t.Errorf("R3 text = %q", r3.Text)
		}
	})

	t.Run("MODIFIED replaces the block and leaves the stamp", func(t *testing.T) {
		after := getRequirement(t, v, "DEMO-SP-0001.R1").Requirement
		if after.Title != "Trim pasted input" || after.Text != "The checkout SHALL trim pasted addresses on both ends." {
			t.Errorf("R1 = %q / %q", after.Title, after.Text)
		}
		if after.BlockRev == r1.BlockRev {
			t.Error("the block rev did not change")
		}
		spec := specText(t, got.Writes)
		if !strings.Contains(spec, "verified: {rev: \""+r1.BlockRev+"\"") {
			t.Errorf("the verified stamp was rewritten:\n%s", spec)
		}
	})

	t.Run("REMOVED cancels and keeps the block", func(t *testing.T) {
		r2 := getRequirement(t, v, "DEMO-SP-0001.R2").Requirement
		if r2.Status != "cancelled" || r2.Title != "Reject empty input" {
			t.Errorf("R2 = %+v", r2)
		}
	})

	t.Run("the story records what it created", func(t *testing.T) {
		s := getDeltaItem(t, v, story.ID)
		if s.Status != "done" {
			t.Errorf("status = %s", s.Status)
		}
		if !strings.Contains(s.Body, "### ADDED DEMO-SP-0001.R3 — Reject overlong input\n") {
			t.Errorf("ADDED heading not rewritten:\n%s", s.Body)
		}
		if !strings.Contains(s.Body, "### MODIFIED DEMO-SP-0001.R1 — Trim pasted input") {
			t.Errorf("MODIFIED heading changed:\n%s", s.Body)
		}
		for _, l := range [][2]string{
			{"implements", "DEMO-SP-0001.R3"}, {"modifies", "DEMO-SP-0001.R1"}, {"modifies", "DEMO-SP-0001.R2"},
		} {
			if !hasItemLink(s, l[0], l[1]) {
				t.Errorf("story lacks %s %s: %+v", l[0], l[1], s.Links)
			}
		}
	})
}

func TestSpecDeltaAppliedByItemUpdate(t *testing.T) {
	v := specVault(t)
	story := deltaStory(t, v, deltaBody)
	raw := call(t, v, "item.update", map[string]any{
		"id": story.ID, "rev": story.Rev, "patch": map[string]any{"set": map[string]any{"status": "done"}},
	})
	got := decode[deltaMoveResult](t, raw)
	if got.SpecDelta == nil || len(got.SpecDelta.Added) != 1 {
		t.Fatalf("specDelta = %+v", got.SpecDelta)
	}
	if !strings.Contains(got.Item.Body, "### ADDED DEMO-SP-0001.R3 — ") {
		t.Errorf("body = %s", got.Item.Body)
	}
}

func TestSpecDeltaMoveSupersedes(t *testing.T) {
	v := specVault(t)
	call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "spec", "title": "Address input",
		"body": "## Requirements\n\n### DEMO-SP-0002.R1 — Accept input\n\nThe form SHALL accept an address.\n",
	})
	story := deltaStory(t, v, "## Spec Delta\n\n### ADDED DEMO-SP-0002 — Trim input\n\nSupersedes: DEMO-SP-0001.R1\n\n"+
		"The form SHALL trim pasted addresses.\n")
	env := moveItem(t, v, story.ID, "done")
	if !env.OK {
		t.Fatalf("move: %s: %s", env.Error.Code, env.Error.Message)
	}
	got := decode[deltaMoveResult](t, env.Result)
	if d := got.SpecDelta; d == nil || len(d.Added) != 1 || d.Added[0].Ref != "DEMO-SP-0002.R2" || d.Added[0].Supersedes != "DEMO-SP-0001.R1" {
		t.Fatalf("specDelta = %+v", got.SpecDelta)
	}
	if strings.Join(got.SpecDelta.Specs, ",") != "DEMO-SP-0002,DEMO-SP-0001" {
		t.Errorf("specs = %v", got.SpecDelta.Specs)
	}
	moved := getRequirement(t, v, "DEMO-SP-0002.R2").Requirement
	if moved.Text != "The form SHALL trim pasted addresses." {
		t.Errorf("the Supersedes: line leaked into the block: %q", moved.Text)
	}
	var text2 string
	for _, f := range got.Writes.Written {
		if strings.Contains(f.Path, "DEMO-SP-0002") {
			text2 = f.Text
		}
	}
	if !strings.Contains(text2, "  R2:\n    status: backlog\n    links:\n      - { kind: supersedes, target: DEMO-SP-0001.R1 }\n") {
		t.Errorf("the new requirement lacks supersedes:\n%s", text2)
	}
	if old := getRequirement(t, v, "DEMO-SP-0001.R1").Requirement; old.Status != "cancelled" {
		t.Errorf("the superseded requirement is %s, want cancelled", old.Status)
	}
	s := getDeltaItem(t, v, story.ID)
	if !hasItemLink(s, "implements", "DEMO-SP-0002.R2") || !hasItemLink(s, "modifies", "DEMO-SP-0001.R1") {
		t.Errorf("story links = %+v", s.Links)
	}
	// The old side holds no superseded_by: links are stored on one side.
	for _, f := range got.Writes.Written {
		if strings.Contains(f.Path, "DEMO-SP-0001") && strings.Contains(f.Text, "superseded_by") {
			t.Errorf("superseded_by written on the old spec:\n%s", f.Text)
		}
	}
}

func TestSpecDeltaHonoursReservedNumbers(t *testing.T) {
	v := specVault(t)
	// Another story names R7 in a link: R7 is reserved, R8 is next.
	call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "story", "title": "Refers ahead",
		"links": []map[string]any{{"kind": "relates_to", "target": "DEMO-SP-0001.R7"}},
	})
	story := deltaStory(t, v, "## Spec Delta\n\n### ADDED DEMO-SP-0001 — One\n\nThe checkout SHALL do one thing.\n\n"+
		"### ADDED DEMO-SP-0001 — Two\n\nThe checkout SHALL do another thing.\n")
	env := moveItem(t, v, story.ID, "done")
	if !env.OK {
		t.Fatalf("move: %s: %s", env.Error.Code, env.Error.Message)
	}
	got := decode[deltaMoveResult](t, env.Result)
	var refs []string
	for _, a := range got.SpecDelta.Added {
		refs = append(refs, a.Ref)
	}
	if strings.Join(refs, ",") != "DEMO-SP-0001.R8,DEMO-SP-0001.R9" {
		t.Errorf("allocated %v, want R8 and R9", refs)
	}
	body := getDeltaItem(t, v, story.ID).Body
	if !strings.Contains(body, "### ADDED DEMO-SP-0001.R8 — One\n") || !strings.Contains(body, "### ADDED DEMO-SP-0001.R9 — Two\n") {
		t.Errorf("headings not rewritten:\n%s", body)
	}
}

func TestSpecDeltaIsIdempotent(t *testing.T) {
	v := specVault(t)
	story := deltaStory(t, v, deltaBody)
	if env := moveItem(t, v, story.ID, "done"); !env.OK {
		t.Fatalf("first move: %s: %s", env.Error.Code, env.Error.Message)
	}
	specRev := getDeltaItem(t, v, "DEMO-SP-0001").Rev

	// A plain edit of a done story applies nothing.
	s := getDeltaItem(t, v, story.ID)
	raw := call(t, v, "item.update", map[string]any{"id": story.ID, "rev": s.Rev, "patch": map[string]any{"set": map[string]any{"priority": "high"}}})
	if got := decode[deltaMoveResult](t, raw); got.SpecDelta != nil {
		t.Errorf("a plain edit applied %+v", got.SpecDelta)
	}

	// Reopening and finishing again finds every operation applied.
	for _, status := range []string{"in_progress", "in_review"} {
		if env := moveItem(t, v, story.ID, status); !env.OK {
			t.Fatalf("move to %s: %s: %s", status, env.Error.Code, env.Error.Message)
		}
	}
	env := moveItem(t, v, story.ID, "done")
	if !env.OK {
		t.Fatalf("second done: %s: %s", env.Error.Code, env.Error.Message)
	}
	got := decode[deltaMoveResult](t, env.Result)
	if d := got.SpecDelta; d == nil || len(d.Added)+len(d.Modified)+len(d.Removed)+len(d.Specs) != 0 || len(d.Unchanged) != 3 {
		t.Errorf("second application = %+v, want three unchanged refs", got.SpecDelta)
	}
	for _, f := range got.Writes.Written {
		if strings.Contains(f.Path, "/specs/") {
			t.Errorf("the spec was rewritten: %s", f.Path)
		}
	}
	if rev := getDeltaItem(t, v, "DEMO-SP-0001").Rev; rev != specRev {
		t.Errorf("spec rev moved %s -> %s", specRev, rev)
	}
	if env := rawCall(t, v, "requirement.get", map[string]any{"ref": "DEMO-SP-0001.R4"}); env.OK {
		t.Error("a second number was allocated")
	}
}

func TestSpecDeltaRefusalChangesNothing(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, v *Vault)
		body     string
		wantCode string
		wantMsg  string
	}{
		{
			name:     "ADDED to a missing spec",
			body:     deltaBody + "\n## Spec Delta\n\n### ADDED DEMO-SP-0009 — Ghost\n\nThe checkout SHALL haunt.\n",
			wantCode: "conflict", wantMsg: "DEMO-SP-0009: the spec does not exist",
		},
		{
			name:     "MODIFIED a missing block",
			body:     deltaBody + "\n## Spec Delta\n\n### MODIFIED DEMO-SP-0001.R9 — Ghost\n\nThe checkout SHALL haunt.\n",
			wantCode: "conflict", wantMsg: "DEMO-SP-0001.R9",
		},
		{
			name:     "REMOVED a missing block",
			body:     deltaBody + "\n## Spec Delta\n\n### REMOVED DEMO-SP-0001.R9 — Ghost\n\nReason: gone.\n",
			wantCode: "conflict", wantMsg: "DEMO-SP-0001.R9",
		},
		{
			name:     "Supersedes: a missing block",
			body:     "## Spec Delta\n\n### ADDED DEMO-SP-0001 — New\n\nSupersedes: DEMO-SP-0001.R9\n\nThe checkout SHALL do it.\n",
			wantCode: "conflict", wantMsg: "DEMO-SP-0001.R9",
		},
		{
			name: "MODIFIED a removed requirement",
			setup: func(t *testing.T, v *Vault) {
				call(t, v, "requirement.update", map[string]any{"ref": "DEMO-SP-0001.R2", "rev": "*", "patch": map[string]any{"status": "cancelled"}})
			},
			body:     "## Spec Delta\n\n### MODIFIED DEMO-SP-0001.R2 — Back\n\nThe checkout SHALL come back.\n",
			wantCode: "conflict", wantMsg: "was removed",
		},
		{
			name:     "the same requirement modified twice",
			body:     "## Spec Delta\n\n### MODIFIED DEMO-SP-0001.R1 — A\n\nThe checkout SHALL A.\n\n### MODIFIED DEMO-SP-0001.R1 — B\n\nThe checkout SHALL B.\n",
			wantCode: "conflict", wantMsg: "more than once",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := specVault(t)
			if tc.setup != nil {
				tc.setup(t, v)
			}
			story := deltaStory(t, v, tc.body)
			specBefore := getDeltaItem(t, v, "DEMO-SP-0001")
			storyBefore := getDeltaItem(t, v, story.ID)

			env := moveItem(t, v, story.ID, "done")
			if env.OK || env.Error.Code != tc.wantCode || !strings.Contains(env.Error.Message, tc.wantMsg) {
				t.Fatalf("ok %v code %q message %q, want %s containing %q", env.OK, env.Error.Code, env.Error.Message, tc.wantCode, tc.wantMsg)
			}
			if after := getDeltaItem(t, v, "DEMO-SP-0001"); after.Rev != specBefore.Rev {
				t.Errorf("the spec changed: %s -> %s", specBefore.Rev, after.Rev)
			}
			after := getDeltaItem(t, v, story.ID)
			if after.Rev != storyBefore.Rev || after.Status != "in_review" {
				t.Errorf("the story changed: %s %s -> %s %s", storyBefore.Status, storyBefore.Rev, after.Status, after.Rev)
			}
			if env := rawCall(t, v, "requirement.get", map[string]any{"ref": "DEMO-SP-0001.R3"}); env.OK {
				t.Error("an ADDED block was written by a refused transition")
			}
		})
	}
}

func TestSpecDeltaStaleStoryRev(t *testing.T) {
	v := specVault(t)
	story := deltaStory(t, v, deltaBody)
	call(t, v, "item.update", map[string]any{"id": story.ID, "rev": story.Rev, "patch": map[string]any{"set": map[string]any{"priority": "high"}}})
	specBefore := getDeltaItem(t, v, "DEMO-SP-0001").Rev
	env := rawCall(t, v, "item.move", map[string]any{"id": story.ID, "status": "done", "rev": story.Rev})
	if env.OK || env.Error.Code != "stale_revision" {
		t.Fatalf("ok %v code %q, want stale_revision", env.OK, env.Error.Code)
	}
	if after := getDeltaItem(t, v, "DEMO-SP-0001").Rev; after != specBefore {
		t.Error("the spec changed on a stale move")
	}
}

// deltaFixtureVault loads the fixture with a hand-written spec and story, and
// project.yaml edited by edit.
func deltaFixtureVault(t *testing.T, edit func(string) string, storyBody string) *Vault {
	t.Helper()
	v := NewInMemory()
	v.SetClock(func() time.Time { return time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC) })
	files := fixtureFiles(t)
	for _, f := range files {
		if strings.HasSuffix(f["path"], ".pmngr/project.yaml") {
			f["text"] = edit(f["text"])
		}
	}
	files = append(files,
		map[string]string{
			"path": "docs/.pmngr/specs/DEMO-SP-0001-checkout-addresses.md",
			"text": "---\nid: DEMO-SP-0001\ntype: spec\ntitle: Checkout addresses\nstatus: backlog\n" +
				"created: 2026-09-01T10:00:00Z\nupdated: 2026-09-01T10:00:00Z\nrequirements:\n  R1:\n    status: backlog\n  R2:\n    status: backlog\n---\n\n" + specBody,
		},
		map[string]string{
			"path": "docs/.pmngr/stories/DEMO-US-0003-tighten-addresses.md",
			"text": "---\nid: DEMO-US-0003\ntype: story\ntitle: Tighten addresses\nstatus: in_review\n" +
				"created: 2026-09-01T10:00:00Z\nupdated: 2026-09-01T10:00:00Z\n---\n\n" + storyBody,
		},
	)
	call(t, v, "vault.load", map[string]any{"files": files})
	return v
}

func TestSpecDeltaUpgradesSchema(t *testing.T) {
	// A schema-1 project whose spec was written by hand: the transition adds
	// the first implements link, so it raises project.yaml in the same write.
	v := deltaFixtureVault(t, func(s string) string { return s }, deltaBody)
	env := moveItem(t, v, "DEMO-US-0003", "done")
	if !env.OK {
		t.Fatalf("move: %s: %s", env.Error.Code, env.Error.Message)
	}
	got := decode[deltaMoveResult](t, env.Result)
	if got.SchemaUpgraded != 2 {
		t.Errorf("schemaUpgraded = %d, want 2", got.SchemaUpgraded)
	}
	var project string
	for _, f := range got.Writes.Written {
		if strings.HasSuffix(f.Path, "/project.yaml") {
			project = f.Text
		}
	}
	if !strings.Contains(project, "\nschema: 2\n") {
		t.Errorf("project.yaml not raised in the write set:\n%s", project)
	}
}

func TestSpecDeltaLintErrorRefuses(t *testing.T) {
	// specs.lint at error: an ADDED block without SHALL refuses the move.
	lintError := func(s string) string {
		return strings.Replace(s, "\nschema: 1\n", "\nschema: 2\nspecs:\n  lint: error\n", 1)
	}
	body := "## Spec Delta\n\n### ADDED DEMO-SP-0001 — Vague\n\nThe checkout trims addresses.\n"
	v := deltaFixtureVault(t, lintError, body)
	specBefore := getDeltaItem(t, v, "DEMO-SP-0001").Rev
	env := moveItem(t, v, "DEMO-US-0003", "done")
	if env.OK || env.Error.Code != "validation_failed" || !strings.Contains(env.Error.Message, "LINT-REQ") {
		t.Fatalf("ok %v code %q message %q, want a LINT-REQ validation failure", env.OK, env.Error.Code, env.Error.Message)
	}
	if after := getDeltaItem(t, v, "DEMO-SP-0001").Rev; after != specBefore {
		t.Error("the spec changed on a refused move")
	}
	if s := getDeltaItem(t, v, "DEMO-US-0003"); s.Status != "in_review" {
		t.Errorf("story status = %s", s.Status)
	}
}

func TestSpecDeltaReadOnlyProject(t *testing.T) {
	v := deltaFixtureVault(t, func(s string) string {
		return strings.Replace(s, "\nschema: 1\n", "\nschema: 3\n", 1)
	}, deltaBody)
	env := rawCall(t, v, "item.move", map[string]any{"id": "DEMO-US-0003", "status": "done", "rev": "*"})
	if env.OK || env.Error.Code != "read_only" {
		t.Fatalf("ok %v code %q, want read_only", env.OK, env.Error.Code)
	}
}
