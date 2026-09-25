package vault

import (
	"encoding/json"
	"strings"
	"testing"
)

// requirementEnvelope is the failure half of a requirement write, with the
// fields a stale revision carries.
type requirementEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		CurrentRev string `json:"currentRev"`
		Conflicts  []struct {
			Field    string `json:"field"`
			Current  string `json:"current"`
			Proposed string `json:"proposed"`
		} `json:"conflicts"`
	} `json:"error"`
}

// requirementRead is the shape of one requirement on the wire.
type requirementRead struct {
	Ref            string `json:"ref"`
	Spec           string `json:"spec"`
	Project        string `json:"project"`
	Title          string `json:"title"`
	Status         string `json:"status"`
	StatusImplicit bool   `json:"statusImplicit"`
	Text           string `json:"text"`
	Rev            string `json:"rev"`
	BlockRev       string `json:"blockRev"`
	Trace          *struct {
		Code []string `json:"code"`
	} `json:"trace"`
}

// requirementResultWire is the result of requirement.get and of a write.
type requirementResultWire struct {
	Requirement    requirementRead `json:"requirement"`
	SpecRev        string          `json:"specRev"`
	Writes         WriteSet        `json:"writes"`
	SchemaUpgraded int             `json:"schemaUpgraded"`
}

const specBody = "## Purpose\n\nAddresses.\n\n## Requirements\n\n" +
	"### DEMO-SP-0001.R1 — Trim input\n\nThe checkout SHALL trim pasted addresses.\n\n" +
	"#### Scenario: trailing spaces\n- **WHEN** an address ends in spaces\n- **THEN** they are removed\n\n" +
	"### DEMO-SP-0001.R2 — Reject empty input\n\nThe checkout SHALL refuse an empty address.\n\n" +
	"## Notes\n\nNothing yet.\n"

// specVault loads the fixture and creates DEMO-SP-0001 with two requirements.
func specVault(t *testing.T) *Vault {
	t.Helper()
	v, _ := loadedVault(t)
	call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "spec", "title": "Checkout addresses", "body": specBody,
	})
	return v
}

func getRequirement(t *testing.T, v *Vault, ref string) requirementResultWire {
	t.Helper()
	return decode[requirementResultWire](t, call(t, v, "requirement.get", map[string]any{"ref": ref}))
}

func rawRequirementCall(t *testing.T, v *Vault, method string, params any) requirementEnvelope {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var env requirementEnvelope
	if err := json.Unmarshal([]byte(v.Call(method, string(data))), &env); err != nil {
		t.Fatalf("%s returned invalid JSON: %v", method, err)
	}
	return env
}

func specText(t *testing.T, writes WriteSet) string {
	t.Helper()
	for _, f := range writes.Written {
		if strings.Contains(f.Path, "/specs/") {
			return f.Text
		}
	}
	t.Fatalf("no spec in the write set: %+v", writes.Written)
	return ""
}

// Verifies: GIT-SP-0001.R8
func TestRequirementGetReturnsBothRevs(t *testing.T) {
	v := specVault(t)
	got := getRequirement(t, v, "DEMO-SP-0001.R1")
	r := got.Requirement
	if r.Ref != "DEMO-SP-0001.R1" || r.Spec != "DEMO-SP-0001" || r.Project != "DEMO" || r.Title != "Trim input" {
		t.Errorf("requirement = %+v", r)
	}
	if r.Status != "backlog" || !r.StatusImplicit {
		t.Errorf("status = %q implicit %v, want the initial status, implicit", r.Status, r.StatusImplicit)
	}
	if !strings.HasPrefix(r.Text, "The checkout SHALL trim") || !strings.Contains(r.Text, "#### Scenario: trailing spaces") {
		t.Errorf("text = %q", r.Text)
	}
	if r.Rev == "" || r.BlockRev == "" || r.Rev == r.BlockRev {
		t.Errorf("rev %q and blockRev %q must both be set and differ", r.Rev, r.BlockRev)
	}
	if got.SpecRev == "" || got.SpecRev == r.Rev {
		t.Errorf("specRev = %q", got.SpecRev)
	}

	// A qualified ref reads the same requirement; a missing block is not_found.
	if q := getRequirement(t, v, "DEMO/DEMO-SP-0001.R1"); q.Requirement.Rev != r.Rev {
		t.Errorf("qualified ref read rev %q, want %q", q.Requirement.Rev, r.Rev)
	}
	if env := rawCall(t, v, "requirement.get", map[string]any{"ref": "DEMO-SP-0001.R9"}); env.OK || env.Error.Code != "not_found" {
		t.Errorf("missing requirement: ok %v code %q", env.OK, env.Error.Code)
	}
	if env := rawCall(t, v, "requirement.get", map[string]any{"ref": "DEMO-SP-0001.R01"}); env.OK || env.Error.Code != "invalid_request" {
		t.Errorf("padded ref: ok %v code %q", env.OK, env.Error.Code)
	}
}

// Verifies: GIT-SP-0001.R8, GIT-SP-0001.R10
func TestRequirementUpdatePatchesOneBlock(t *testing.T) {
	v := specVault(t)
	r1 := getRequirement(t, v, "DEMO-SP-0001.R1").Requirement
	r2 := getRequirement(t, v, "DEMO-SP-0001.R2").Requirement

	raw := call(t, v, "requirement.update", map[string]any{
		"ref": "DEMO-SP-0001.R1", "rev": r1.Rev,
		"patch": map[string]any{
			"title":  "Trim pasted input",
			"text":   "The checkout SHALL trim pasted addresses on both ends.\n",
			"status": "todo",
			"trace":  map[string]any{"code": []string{"internal/checkout/address.go#Trim"}},
		},
	})
	got := decode[requirementResultWire](t, raw)
	if got.Requirement.Title != "Trim pasted input" || got.Requirement.Status != "todo" || got.Requirement.StatusImplicit {
		t.Errorf("written requirement = %+v", got.Requirement)
	}
	if got.Requirement.Rev == r1.Rev || got.Requirement.BlockRev == r1.BlockRev {
		t.Errorf("revs did not move: %+v", got.Requirement)
	}
	text := specText(t, got.Writes)
	for _, want := range []string{
		"### DEMO-SP-0001.R1 — Trim pasted input\n\nThe checkout SHALL trim pasted addresses on both ends.\n\n### DEMO-SP-0001.R2",
		"requirements:\n  R1:\n    status: todo\n    trace:\n      code: [internal/checkout/address.go#Trim]\n",
		"\n## Notes\n\nNothing yet.\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("spec file lacks %q:\n%s", want, text)
		}
	}
	// R2 was not touched: its rev still holds.
	if again := getRequirement(t, v, "DEMO-SP-0001.R2").Requirement; again.Rev != r2.Rev || again.BlockRev != r2.BlockRev {
		t.Errorf("R2 revs moved: %s/%s -> %s/%s", r2.Rev, r2.BlockRev, again.Rev, again.BlockRev)
	}

	// A status-only change moves the requirement rev, never the block rev.
	before := got.Requirement
	raw = call(t, v, "requirement.update", map[string]any{
		"ref": "DEMO-SP-0001.R1", "rev": before.Rev, "patch": map[string]any{"status": "in_progress"},
	})
	after := decode[requirementResultWire](t, raw).Requirement
	if after.BlockRev != before.BlockRev || after.Rev == before.Rev {
		t.Errorf("status change: blockRev %s -> %s, rev %s -> %s", before.BlockRev, after.BlockRev, before.Rev, after.Rev)
	}
}

// Verifies: GIT-SP-0001.R2, GIT-SP-0001.R3, GIT-SP-0001.R4, GIT-SP-0001.R10
func TestRequirementUpdateConflictMatrix(t *testing.T) {
	tests := []struct {
		name string
		// first is the write another writer made after the base read.
		first map[string]any
		// second is the write under test, quoting the base rev of `ref`.
		ref    string
		second map[string]any
		// wantCode is the failure code, "" for a success.
		wantCode      string
		wantConflicts []string
	}{
		{
			name:          "stale rev on the same block",
			first:         map[string]any{"ref": "DEMO-SP-0001.R1", "patch": map[string]any{"status": "todo", "title": "Trim everything"}},
			ref:           "DEMO-SP-0001.R1",
			second:        map[string]any{"status": "cancelled", "title": "Trim nothing", "text": "The checkout SHALL NOT trim."},
			wantCode:      "stale_revision",
			wantConflicts: []string{"text", "title", "status"},
		},
		{
			name:   "concurrent edit to another block of the same spec",
			first:  map[string]any{"ref": "DEMO-SP-0001.R2", "patch": map[string]any{"status": "todo", "text": "The checkout SHALL refuse a blank address."}},
			ref:    "DEMO-SP-0001.R1",
			second: map[string]any{"status": "todo", "title": "Trim pasted input"},
		},
		{
			name:          "stale rev whose change already happened",
			first:         map[string]any{"ref": "DEMO-SP-0001.R1", "patch": map[string]any{"status": "todo"}},
			ref:           "DEMO-SP-0001.R1",
			second:        map[string]any{"status": "todo"},
			wantCode:      "stale_revision",
			wantConflicts: nil,
		},
		{
			name:          "stale rev whose every field already happened",
			first:         map[string]any{"ref": "DEMO-SP-0001.R1", "patch": map[string]any{"title": "Trim everything", "status": "todo", "trace": map[string]any{"code": []string{"internal/checkout/address.go"}}}},
			ref:           "DEMO-SP-0001.R1",
			second:        map[string]any{"title": "Trim everything", "status": "todo", "trace": map[string]any{"code": []string{"internal/checkout/address.go"}}},
			wantCode:      "stale_revision",
			wantConflicts: nil,
		},
		{
			name:          "stale rev with a partial overlap names only what still differs",
			first:         map[string]any{"ref": "DEMO-SP-0001.R1", "patch": map[string]any{"title": "Trim everything", "status": "todo"}},
			ref:           "DEMO-SP-0001.R1",
			second:        map[string]any{"title": "Trim everything", "status": "in_progress"},
			wantCode:      "stale_revision",
			wantConflicts: []string{"status"},
		},
		{
			// A patch the store would refuse cannot be judged field by field,
			// and an empty list would tell the caller its change is already
			// there. It names the fields it carries instead.
			name:          "stale rev with a patch the store would refuse is never already applied",
			first:         map[string]any{"ref": "DEMO-SP-0001.R1", "patch": map[string]any{"status": "todo"}},
			ref:           "DEMO-SP-0001.R1",
			second:        map[string]any{"title": "  ", "text": "The checkout SHALL trim.\n\n## Oops\n"},
			wantCode:      "stale_revision",
			wantConflicts: []string{"text", "title"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := specVault(t)
			base := getRequirement(t, v, tc.ref).Requirement

			firstRef := tc.first["ref"].(string)
			first := map[string]any{"ref": firstRef, "patch": tc.first["patch"], "rev": getRequirement(t, v, firstRef).Requirement.Rev}
			call(t, v, "requirement.update", first)

			env := rawRequirementCall(t, v, "requirement.update", map[string]any{"ref": tc.ref, "rev": base.Rev, "patch": tc.second})
			if tc.wantCode == "" {
				if !env.OK {
					t.Fatalf("write failed: %s: %s", env.Error.Code, env.Error.Message)
				}
				got := decode[requirementResultWire](t, env.Result)
				if got.Requirement.Title != "Trim pasted input" {
					t.Errorf("title = %q", got.Requirement.Title)
				}
				// The other writer's change survived the second write.
				if r2 := getRequirement(t, v, "DEMO-SP-0001.R2").Requirement; r2.Text != "The checkout SHALL refuse a blank address." || r2.Status != "todo" {
					t.Errorf("R2 lost the first write: %+v", r2)
				}
				return
			}
			if env.OK || env.Error.Code != tc.wantCode {
				t.Fatalf("ok %v code %q, want %s", env.OK, env.Error.Code, tc.wantCode)
			}
			current := getRequirement(t, v, tc.ref).Requirement
			if env.Error.CurrentRev != current.Rev {
				t.Errorf("currentRev = %q, want the requirement rev %q", env.Error.CurrentRev, current.Rev)
			}
			var fields []string
			for _, c := range env.Error.Conflicts {
				fields = append(fields, c.Field)
				if c.Field == "text" && (c.Current != "" || c.Proposed != "") {
					t.Errorf("text is quoted back: %+v", c)
				}
			}
			if strings.Join(fields, ",") != strings.Join(tc.wantConflicts, ",") {
				t.Errorf("conflicts = %v, want %v", fields, tc.wantConflicts)
			}
		})
	}
}

// Verifies: GIT-SP-0001.R9
func TestRequirementUpdateNeedsTheRequirementRev(t *testing.T) {
	v := specVault(t)
	r1 := getRequirement(t, v, "DEMO-SP-0001.R1")
	patch := map[string]any{"status": "todo"}

	if env := rawCall(t, v, "requirement.update", map[string]any{"ref": "DEMO-SP-0001.R1", "patch": patch}); env.OK || env.Error.Code != "precondition_required" {
		t.Errorf("no rev: ok %v code %q", env.OK, env.Error.Code)
	}
	for name, rev := range map[string]string{"blockRev": r1.Requirement.BlockRev, "specRev": r1.SpecRev} {
		if env := rawCall(t, v, "requirement.update", map[string]any{"ref": "DEMO-SP-0001.R1", "patch": patch, "rev": rev}); env.OK || env.Error.Code != "stale_revision" {
			t.Errorf("%s as the write token: ok %v code %q", name, env.OK, env.Error.Code)
		}
	}
}

func TestRequirementUpdateRefusals(t *testing.T) {
	tests := []struct {
		name  string
		patch map[string]any
		want  string
	}{
		{"heading in the text", map[string]any{"text": "The checkout SHALL trim.\n\n## Oops\n"}, "validation_failed"},
		{"open fence", map[string]any{"text": "```go\nfunc x() {}\n"}, "validation_failed"},
		{"empty title", map[string]any{"title": "  "}, "validation_failed"},
		{"unknown status", map[string]any{"status": "nope"}, "validation_failed"},
		{"transition outside the workflow", map[string]any{"status": "done"}, "workflow_transition_denied"},
		{"bad trace path", map[string]any{"trace": map[string]any{"code": []string{"../etc/passwd"}}}, "validation_failed"},
		{"unset status", map[string]any{"unset": []string{"status"}}, "validation_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := specVault(t)
			r1 := getRequirement(t, v, "DEMO-SP-0001.R1").Requirement
			env := rawCall(t, v, "requirement.update", map[string]any{"ref": "DEMO-SP-0001.R1", "rev": r1.Rev, "patch": tc.patch})
			if env.OK || env.Error.Code != tc.want {
				t.Errorf("ok %v code %q (%s), want %s", env.OK, env.Error.Code, env.Error.Message, tc.want)
			}
			if again := getRequirement(t, v, "DEMO-SP-0001.R1").Requirement; again.Rev != r1.Rev {
				t.Errorf("a refused write changed the requirement")
			}
		})
	}
}

// Verifies: GIT-SP-0002.R7
func TestRequirementCreateAllocatesMaxPlusOne(t *testing.T) {
	v := specVault(t)

	create := func(title string) requirementResultWire {
		t.Helper()
		return decode[requirementResultWire](t, call(t, v, "requirement.create", map[string]any{
			"spec": "DEMO-SP-0001", "title": title, "text": "The checkout SHALL " + strings.ToLower(title) + ".",
		}))
	}
	got := create("Keep the country")
	if got.Requirement.Ref != "DEMO-SP-0001.R3" || got.Requirement.Status != "backlog" || got.Requirement.StatusImplicit {
		t.Errorf("first create = %+v", got.Requirement)
	}
	text := specText(t, got.Writes)
	if !strings.Contains(text, "### DEMO-SP-0001.R3 — Keep the country\n\nThe checkout SHALL keep the country.\n\n## Notes") {
		t.Errorf("block not appended at the end of the requirements:\n%s", text)
	}
	if !strings.Contains(text, "  R3:\n    status: backlog\n") {
		t.Errorf("status not materialized:\n%s", text)
	}

	// An inbound link reserves R7; the next number is R8, never a reused one.
	call(t, v, "item.create", map[string]any{
		"project": "DEMO", "type": "task", "title": "Implement R7",
		"links": []map[string]any{{"kind": "implements", "target": "DEMO-SP-0001.R7"}},
	})
	if next := create("Keep the postcode"); next.Requirement.Ref != "DEMO-SP-0001.R8" {
		t.Errorf("second create = %s, want R8", next.Requirement.Ref)
	}
}

// Verifies: GIT-SP-0002.R7
func TestRequirementCreateSkipsOrphanEntryNumbers(t *testing.T) {
	v := NewInMemory()
	files := fixtureFiles(t)
	files = append(files, map[string]string{
		"path": "docs/.pmngr/specs/DEMO-SP-0001-orphans.md",
		"text": "---\nid: DEMO-SP-0001\ntype: spec\ntitle: Orphans\nstatus: backlog\ncreated: 2026-09-01T00:00:00Z\nupdated: 2026-09-01T00:00:00Z\n" +
			"requirements:\n  R1:\n    status: todo\n  R5:\n    status: cancelled\n---\n\n## Requirements\n\n### DEMO-SP-0001.R1 — Keep\n\nThe system SHALL keep.\n",
	})
	for _, f := range files {
		if strings.HasSuffix(f["path"], ".pmngr/project.yaml") {
			f["text"] = strings.Replace(f["text"], "\nschema: 1\n", "\nschema: 2\n", 1)
		}
	}
	call(t, v, "vault.load", map[string]any{"files": files})
	got := decode[requirementResultWire](t, call(t, v, "requirement.create", map[string]any{"spec": "DEMO-SP-0001", "title": "Next"}))
	if got.Requirement.Ref != "DEMO-SP-0001.R6" {
		t.Errorf("created %s, want R6 past the orphan R5 entry", got.Requirement.Ref)
	}
	text := specText(t, got.Writes)
	if !strings.Contains(text, "  R1:\n    status: todo\n  R5:\n    status: cancelled\n  R6:\n    status: backlog\n") {
		t.Errorf("requirements: not in numeric order:\n%s", text)
	}
	if !strings.HasSuffix(text, "The system SHALL keep.\n\n### DEMO-SP-0001.R6 — Next\n") {
		t.Errorf("block not appended:\n%s", text)
	}
}

func TestRequirementCreateAddsTheSection(t *testing.T) {
	v, _ := loadedVault(t)
	call(t, v, "item.create", map[string]any{"project": "DEMO", "type": "spec", "title": "Empty", "body": "## Purpose\n\nNothing yet.\n"})
	got := decode[requirementResultWire](t, call(t, v, "requirement.create", map[string]any{"spec": "DEMO-SP-0001", "title": "First", "status": "todo"}))
	if got.Requirement.Ref != "DEMO-SP-0001.R1" || got.Requirement.Status != "todo" {
		t.Errorf("created %+v", got.Requirement)
	}
	if text := specText(t, got.Writes); !strings.Contains(text, "## Purpose\n\nNothing yet.\n\n## Requirements\n\n### DEMO-SP-0001.R1 — First\n") {
		t.Errorf("section not added:\n%s", text)
	}
	if env := rawCall(t, v, "requirement.create", map[string]any{"spec": "DEMO-US-0001", "title": "Not a spec"}); env.OK || env.Error.Code != "not_found" {
		t.Errorf("create on a story: ok %v code %q", env.OK, env.Error.Code)
	}
}

func TestRequirementWriteUpgradesSchemaAndHonoursTheGate(t *testing.T) {
	// A spec written by hand into a schema-1 project is E-SCHEMA-FEATURE until
	// the project is upgraded; creating a spec through the vault upgrades it.
	v, _ := loadedVault(t)
	raw := call(t, v, "item.create", map[string]any{"project": "DEMO", "type": "spec", "title": "Spec"})
	if got := decode[specWriteResult](t, raw); got.SchemaUpgraded != 2 {
		t.Fatalf("schemaUpgraded = %d", got.SchemaUpgraded)
	}
	got := decode[requirementResultWire](t, call(t, v, "requirement.create", map[string]any{"spec": "DEMO-SP-0001", "title": "First"}))
	if got.SchemaUpgraded != 0 {
		t.Errorf("a requirement write on an upgraded project reported schemaUpgraded = %d", got.SchemaUpgraded)
	}

	// The write gate refuses requirement writes on a newer schema.
	gated := NewInMemory()
	files := fixtureFiles(t)
	files = append(files, map[string]string{
		"path": "docs/.pmngr/specs/DEMO-SP-0001-s.md",
		"text": "---\nid: DEMO-SP-0001\ntype: spec\ntitle: S\nstatus: backlog\ncreated: 2026-09-01T00:00:00Z\nupdated: 2026-09-01T00:00:00Z\n---\n\n## Requirements\n\n### DEMO-SP-0001.R1 — Keep\n\nThe system SHALL keep.\n",
	})
	for _, f := range files {
		if strings.HasSuffix(f["path"], ".pmngr/project.yaml") {
			f["text"] = strings.Replace(f["text"], "\nschema: 1\n", "\nschema: 3\n", 1)
		}
	}
	call(t, gated, "vault.load", map[string]any{"files": files})
	r1 := getRequirement(t, gated, "DEMO-SP-0001.R1").Requirement
	if env := rawCall(t, gated, "requirement.update", map[string]any{"ref": "DEMO-SP-0001.R1", "rev": r1.Rev, "patch": map[string]any{"status": "todo"}}); env.OK || env.Error.Code != "read_only" {
		t.Errorf("update on schema 3: ok %v code %q", env.OK, env.Error.Code)
	}
	if env := rawCall(t, gated, "requirement.create", map[string]any{"spec": "DEMO-SP-0001", "title": "No"}); env.OK || env.Error.Code != "read_only" {
		t.Errorf("create on schema 3: ok %v code %q", env.OK, env.Error.Code)
	}
}

func TestRequirementListAndSearchRows(t *testing.T) {
	v := specVault(t)
	r1 := getRequirement(t, v, "DEMO-SP-0001.R1").Requirement
	call(t, v, "requirement.update", map[string]any{"ref": "DEMO-SP-0001.R1", "rev": r1.Rev, "patch": map[string]any{"status": "todo"}})

	type listResult struct {
		Requirements []requirementRead `json:"requirements"`
		Total        int               `json:"total"`
	}
	all := decode[listResult](t, call(t, v, "requirement.list", map[string]any{"project": "DEMO"}))
	if all.Total != 2 || len(all.Requirements) != 2 {
		t.Fatalf("list = %+v", all)
	}
	row := all.Requirements[0]
	if row.Ref != "DEMO-SP-0001.R1" || row.Spec != "DEMO-SP-0001" || row.Title != "Trim input" || row.Status != "todo" || row.Rev == "" || row.BlockRev == "" {
		t.Errorf("row = %+v", row)
	}
	if row.Text != "" {
		t.Errorf("a list row carries text by default")
	}

	todo := decode[listResult](t, call(t, v, "requirement.list", map[string]any{"spec": "DEMO-SP-0001", "status": []string{"backlog"}}))
	if todo.Total != 1 || todo.Requirements[0].Ref != "DEMO-SP-0001.R2" {
		t.Errorf("status filter = %+v", todo)
	}
	found := decode[listResult](t, call(t, v, "requirement.list", map[string]any{"q": "empty address", "text": true}))
	if found.Total != 1 || found.Requirements[0].Ref != "DEMO-SP-0001.R2" || found.Requirements[0].Text == "" {
		t.Errorf("text filter = %+v", found)
	}

	// Search adds requirement rows only when asked to.
	plain := decode[[]SearchHit](t, call(t, v, "search", map[string]any{"q": "refuse"}))
	for _, h := range plain {
		if h.Kind == "requirement" {
			t.Errorf("requirement hit without requirements: true: %+v", h)
		}
	}
	hits := decode[[]SearchHit](t, call(t, v, "search", map[string]any{"q": "refuse", "requirements": true}))
	var req *SearchHit
	for i := range hits {
		if hits[i].Kind == "requirement" {
			req = &hits[i]
		}
	}
	if req == nil || req.ID != "DEMO-SP-0001.R2" || req.Spec != "DEMO-SP-0001" || req.Status != "backlog" || req.Title != "Reject empty input" {
		t.Errorf("requirement hit = %+v in %+v", req, hits)
	}
}

func TestWorkspaceRoutesRequirementCalls(t *testing.T) {
	w := NewWorkspace()
	call := func(method string, params any) requirementEnvelope {
		t.Helper()
		data, _ := json.Marshal(params)
		var env requirementEnvelope
		if err := json.Unmarshal([]byte(w.Call(method, string(data))), &env); err != nil {
			t.Fatal(err)
		}
		if !env.OK {
			t.Fatalf("%s: %s: %s", method, env.Error.Code, env.Error.Message)
		}
		return env
	}
	call("vault.load", map[string]any{"vaultId": "demo", "files": fixtureFiles(t)})
	call("item.create", map[string]any{"project": "DEMO", "type": "spec", "title": "S", "body": specBody})
	env := call("requirement.get", map[string]any{"ref": "DEMO-SP-0001.R2"})
	got := decode[requirementResultWire](t, env.Result)
	call("requirement.create", map[string]any{"spec": "DEMO-SP-0001", "title": "Third"})
	call("requirement.update", map[string]any{"ref": "DEMO-SP-0001.R2", "rev": got.Requirement.Rev, "patch": map[string]any{"status": "todo"}})
	hits := decode[[]SearchHit](t, call("search", map[string]any{"q": "third", "requirements": true}).Result)
	if len(hits) == 0 || hits[0].Kind != "requirement" || hits[0].ID != "DEMO-SP-0001.R3" || hits[0].VaultID != "demo" {
		t.Errorf("workspace search = %+v", hits)
	}
}
