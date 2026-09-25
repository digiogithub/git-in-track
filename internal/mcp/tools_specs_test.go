package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// specFixture creates a spec with three requirements through the tools and
// returns the spec id. Every requirement starts at the workflow's initial
// status, backlog.
func specFixture(t *testing.T, h *harness) string {
	t.Helper()

	spec := call[WriteResult](t, h, "create_spec", map[string]any{
		"project": "DEMO", "title": "Checkout addresses",
		"body": "## Purpose\n\nValidate addresses at checkout.\n\n## Requirements\n",
	})
	for i, title := range []string{"Trim input", "Reject empty postcode", "Normalize country"} {
		got := call[RequirementWriteResult](t, h, "create_requirement", map[string]any{
			"spec": spec.Item.ID, "title": title,
			"text": "The checkout SHALL " + strings.ToLower(title) + ".",
		})
		if want := spec.Item.ID + ".R" + string(rune('1'+i)); got.Requirement.Ref != want {
			t.Fatalf("create_requirement allocated %s, want %s", got.Requirement.Ref, want)
		}
		if got.Requirement.Rev == "" || got.Requirement.BlockRev == "" || got.Requirement.Rev == got.Requirement.BlockRev {
			t.Fatalf("create_requirement revs = %q / %q, want two distinct hashes",
				got.Requirement.Rev, got.Requirement.BlockRev)
		}
		if got.Requirement.Text != "" {
			t.Errorf("a write result carries the block text back: %q", got.Requirement.Text)
		}
	}
	return spec.Item.ID
}

func TestCreateSpecReportsSchemaUpgrade(t *testing.T) {
	h := newHarness(t, true)

	first := call[WriteResult](t, h, "create_spec", map[string]any{"project": "DEMO", "title": "Checkout addresses"})
	if first.Item.ID != "DEMO-SP-0001" || first.Item.Type != "spec" {
		t.Fatalf("create_spec = %+v", first.Item)
	}
	if first.SchemaUpgraded != 2 {
		t.Errorf("schemaUpgraded = %d, want 2 on the first spec of the project", first.SchemaUpgraded)
	}
	if !containsSuffix(first.Changed, "/project.yaml") {
		t.Errorf("changed = %v, want project.yaml in the write", first.Changed)
	}
	second := call[WriteResult](t, h, "create_spec", map[string]any{"project": "DEMO", "title": "Payments"})
	if second.SchemaUpgraded != 0 {
		t.Errorf("second create_spec reported schemaUpgraded = %d", second.SchemaUpgraded)
	}

	listed := call[ItemPage](t, h, "list_items", map[string]any{"project": "DEMO", "type": []string{"spec"}})
	if listed.Total != 2 {
		t.Errorf("list_items type spec total = %d, want 2", listed.Total)
	}
	for _, it := range listed.Items {
		if it.Type != "spec" {
			t.Errorf("list_items type spec returned a %s", it.Type)
		}
	}
}

func TestGetItemOnRequirementRef(t *testing.T) {
	h := newHarness(t, true)
	spec := specFixture(t, h)

	t.Run("returns only the block and its entry", func(t *testing.T) {
		res := rawCall(t, h, "get_item", map[string]any{"id": spec + ".R2"})
		if res.IsError {
			t.Fatalf("get_item failed: %s", textOf(res))
		}
		raw, _ := json.Marshal(res.StructuredContent)
		body := string(raw)
		for _, other := range []string{"trim input", "normalize country", "Validate addresses"} {
			if strings.Contains(body, other) {
				t.Errorf("a requirement read leaks %q from the rest of the spec:\n%s", other, body)
			}
		}
		var got ItemResult
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Item != nil {
			t.Errorf("item = %+v, want none for a requirement ref", got.Item)
		}
		req := got.Requirement
		if req == nil || req.Ref != spec+".R2" || req.Title != "Reject empty postcode" || req.Status != "backlog" {
			t.Fatalf("requirement = %+v", req)
		}
		if req.Text != "The checkout SHALL reject empty postcode." {
			t.Errorf("text = %q", req.Text)
		}
		if req.Rev == "" || req.BlockRev == "" || got.SpecRev == "" {
			t.Errorf("rev %q, blockRev %q, specRev %q: want all three", req.Rev, req.BlockRev, got.SpecRev)
		}
	})

	t.Run("fields projects the requirement", func(t *testing.T) {
		got := call[ItemResult](t, h, "get_item", map[string]any{"id": spec + ".R1", "fields": []string{"status"}})
		req := got.Requirement
		if req.Ref == "" || req.Rev == "" || req.Status == "" {
			t.Fatalf("projection dropped ref, rev or status: %+v", req)
		}
		if req.Text != "" || req.Title != "" || req.BlockRev != "" {
			t.Errorf("projection kept fields it was not asked for: %+v", req)
		}
	})

	t.Run("a missing requirement is not_found", func(t *testing.T) {
		if got := callFails(t, h, "get_item", map[string]any{"id": spec + ".R9"}); got.Code != codeNotFound {
			t.Errorf("code = %q, want %q", got.Code, codeNotFound)
		}
	})

	t.Run("a malformed ref is invalid_request", func(t *testing.T) {
		if got := callFails(t, h, "get_item", map[string]any{"id": spec + ".R02"}); got.Code != codeInvalidRequest {
			t.Errorf("code = %q, want %q", got.Code, codeInvalidRequest)
		}
	})
}

func TestListRequirements(t *testing.T) {
	h := newHarness(t, true)
	spec := specFixture(t, h)

	t.Run("rows are compact by default", func(t *testing.T) {
		res := rawCall(t, h, "list_requirements", map[string]any{"spec": spec})
		raw, _ := json.Marshal(res.StructuredContent)
		var page RequirementPage
		if err := json.Unmarshal(raw, &page); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if page.Total != 3 || len(page.Requirements) != 3 || page.NextCursor != "" {
			t.Fatalf("page = %+v", page)
		}
		row := page.Requirements[0]
		if row.Ref != spec+".R1" || row.Spec != spec || row.Title != "Trim input" || row.Status != "backlog" || row.Rev == "" {
			t.Errorf("row = %+v", row)
		}
		for _, unwanted := range []string{"SHALL", "blockRev", "path", "anchor", "null", "\"\""} {
			if strings.Contains(string(raw), unwanted) {
				t.Errorf("a default row carries %s:\n%s", unwanted, raw)
			}
		}
	})

	t.Run("fields adds the text only when asked", func(t *testing.T) {
		page := call[RequirementPage](t, h, "list_requirements", map[string]any{
			"spec": spec, "fields": []string{"text", "blockRev"},
		})
		row := page.Requirements[0]
		if row.Text != "The checkout SHALL trim input." || row.BlockRev == "" {
			t.Errorf("row = %+v, want text and blockRev", row)
		}
		if row.Title != "" {
			t.Errorf("projection kept the title: %+v", row)
		}
	})

	t.Run("the cursor walks every row once", func(t *testing.T) {
		var refs []string
		cursor := ""
		for range 5 {
			page := call[RequirementPage](t, h, "list_requirements", map[string]any{
				"project": "DEMO", "limit": 2, "cursor": cursor,
			})
			for _, r := range page.Requirements {
				refs = append(refs, r.Ref)
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		if strings.Join(refs, ",") != spec+".R1,"+spec+".R2,"+spec+".R3" {
			t.Errorf("walk = %v", refs)
		}
	})

	t.Run("a cursor refuses a changed filter", func(t *testing.T) {
		page := call[RequirementPage](t, h, "list_requirements", map[string]any{"project": "DEMO", "limit": 1})
		got := callFails(t, h, "list_requirements", map[string]any{
			"project": "DEMO", "limit": 1, "status": []string{"todo"}, "cursor": page.NextCursor,
		})
		if got.Code != codeInvalidCursor {
			t.Errorf("code = %q, want %q", got.Code, codeInvalidCursor)
		}
	})

	t.Run("status filters", func(t *testing.T) {
		r2 := call[ItemResult](t, h, "get_item", map[string]any{"id": spec + ".R2"}).Requirement
		call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": r2.Ref, "rev": r2.Rev, "status": "todo",
		})
		page := call[RequirementPage](t, h, "list_requirements", map[string]any{"project": "DEMO", "status": []string{"todo"}})
		if page.Total != 1 || page.Requirements[0].Ref != r2.Ref {
			t.Errorf("status todo = %+v", page)
		}
	})
}

// TestRequirementRevProtocol is the per-block optimistic lock: a stale rev on
// one requirement is refused exactly like update_item's, while a write to a
// sibling requirement of the same spec does not make it stale.
func TestRequirementRevProtocol(t *testing.T) {
	read := func(t *testing.T, h *harness, ref string) Requirement {
		t.Helper()
		return *call[ItemResult](t, h, "get_item", map[string]any{"id": ref}).Requirement
	}

	t.Run("stale on the same block", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		before := read(t, h, spec+".R1")
		wrote := call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.Rev, "status": "todo",
		})
		if wrote.Requirement.Rev == before.Rev || wrote.Requirement.Status != "todo" {
			t.Fatalf("update_requirement = %+v", wrote.Requirement)
		}
		if wrote.Requirement.BlockRev != before.BlockRev {
			t.Errorf("a status change moved the block rev: %s -> %s", before.BlockRev, wrote.Requirement.BlockRev)
		}
		got := callFails(t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.Rev, "title": "Trim every field",
		})
		if got.Code != "stale_revision" || got.CurrentRev != wrote.Requirement.Rev || got.Retry == "" {
			t.Fatalf("stale write = %+v", got)
		}
		if len(got.Conflicts) != 1 || got.Conflicts[0].Field != "title" {
			t.Errorf("conflicts = %+v, want title", got.Conflicts)
		}
		// One deliberate retry quoting currentRev succeeds.
		call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": got.CurrentRev, "title": "Trim every field",
		})
	})

	t.Run("success on a sibling block", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		r1, r2 := read(t, h, spec+".R1"), read(t, h, spec+".R2")
		call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": r1.Ref, "rev": r1.Rev, "text": "The checkout SHALL trim every field.",
		})
		got := call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": r2.Ref, "rev": r2.Rev, "status": "todo",
		})
		if got.Requirement.Status != "todo" {
			t.Errorf("sibling write = %+v", got.Requirement)
		}
		if after := read(t, h, spec+".R1"); after.Text != "The checkout SHALL trim every field." {
			t.Errorf("the sibling write lost R1's change: %q", after.Text)
		}
	})

	t.Run("empty conflicts when the change is already there", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		before := read(t, h, spec+".R3")
		call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.Rev, "status": "todo",
		})
		got := callFails(t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.Rev, "status": "todo",
		})
		if got.Code != "stale_revision" || got.CurrentRev == "" {
			t.Fatalf("repeat write = %+v", got)
		}
		if len(got.Conflicts) != 0 {
			t.Errorf("conflicts = %+v, want none: the change is already on disk", got.Conflicts)
		}
	})

	t.Run("a partial overlap names only the field still in conflict", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		before := read(t, h, spec+".R3")
		call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.Rev, "title": "Trim every field", "status": "todo",
		})
		got := callFails(t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.Rev, "title": "Trim every field", "status": "in_progress",
		})
		if got.Code != "stale_revision" || got.Retry == "" {
			t.Fatalf("overlapping write = %+v", got)
		}
		if len(got.Conflicts) != 1 || got.Conflicts[0].Field != "status" {
			t.Errorf("conflicts = %+v, want status only", got.Conflicts)
		}
	})

	t.Run("the block rev is not a write token", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		before := read(t, h, spec+".R1")
		got := callFails(t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.BlockRev, "status": "todo",
		})
		if got.Code != "stale_revision" || got.CurrentRev != before.Rev {
			t.Errorf("write quoting blockRev = %+v", got)
		}
	})

	t.Run("a missing rev is refused", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		got := callFails(t, h, "update_requirement", map[string]any{"ref": spec + ".R1", "rev": "", "status": "todo"})
		if got.Code != codePreconditionRequired {
			t.Errorf("code = %q, want %q", got.Code, codePreconditionRequired)
		}
	})

	t.Run("the wildcard writes unconditionally", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		got := call[RequirementWriteResult](t, h, "update_requirement", map[string]any{
			"ref": spec + ".R1", "rev": "*", "status": "todo",
		})
		if got.Requirement.Status != "todo" {
			t.Errorf("wildcard write = %+v", got.Requirement)
		}
	})

	t.Run("the verification stamp cannot be cleared", func(t *testing.T) {
		h := newHarness(t, true)
		spec := specFixture(t, h)
		before := read(t, h, spec+".R1")
		got := callFails(t, h, "update_requirement", map[string]any{
			"ref": before.Ref, "rev": before.Rev, "unset": []string{"verified"},
		})
		if got.Code != codeInvalidRequest || got.Field != "unset" {
			t.Errorf("unset verified = %+v", got)
		}
	})
}

// containsSuffix reports whether any path ends with suffix.
func containsSuffix(paths []string, suffix string) bool {
	for _, p := range paths {
		if strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return false
}

// TestSpecToolsStateTheDataBoundary checks that every spec tool, write tools
// included, carries the data-not-instructions sentence: their results hold
// requirement titles written by people and by other agents.
func TestSpecToolsStateTheDataBoundary(t *testing.T) {
	h := newHarness(t, true)
	listed, err := h.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	want := map[string]bool{
		"list_requirements": true, "create_spec": true, "create_requirement": true, "update_requirement": true,
	}
	for _, tool := range listed.Tools {
		if !want[tool.Name] {
			continue
		}
		delete(want, tool.Name)
		if !strings.Contains(tool.Description, untrustedNote) {
			t.Errorf("%s does not carry the data boundary:\n%s", tool.Name, tool.Description)
		}
	}
	for name := range want {
		t.Errorf("%s is not advertised with writes enabled", name)
	}
}

func TestCreateRequirementSimilar(t *testing.T) {
	h := newHarness(t, true)
	spec := specFixture(t, h)

	t.Run("without Pando there is no similar key", func(t *testing.T) {
		got := call[RequirementWriteResult](t, h, "create_requirement", map[string]any{
			"spec": spec, "title": "Trim pasted input", "text": "The checkout SHALL trim pasted input.",
		})
		if got.Similar != nil {
			t.Errorf("similar = %+v", got.Similar)
		}
	})

	t.Run("near-duplicates come back compactly", func(t *testing.T) {
		backend := withSemantic(t, h, []core.SearchHit{
			{Kind: core.SearchKindRequirement, ID: core.ItemID(spec + ".R1"), Spec: core.ItemID(spec),
				Title: "Trim input", Status: "backlog", Project: "DEMO", Score: 0.9},
		})
		got := call[RequirementWriteResult](t, h, "create_requirement", map[string]any{
			"spec": spec, "title": "Strip pasted whitespace", "text": "The checkout SHALL strip pasted whitespace.",
		})
		if len(got.Similar) != 1 || got.Similar[0].Ref != spec+".R1" || got.Similar[0].Score != 0.9 ||
			got.Similar[0].Status != "backlog" {
			t.Errorf("similar = %+v", got.Similar)
		}
		if backend.asked.Kind != core.SearchKindRequirement || !strings.Contains(backend.asked.Q, "Strip pasted whitespace") {
			t.Errorf("asked = %+v", backend.asked)
		}
	})
}
