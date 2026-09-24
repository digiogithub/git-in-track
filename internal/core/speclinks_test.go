package core

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestLinkKindValidAndInverse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind    LinkKind
		valid   bool
		spec    bool
		inverse LinkKind
	}{
		{kind: LinkBlocks, valid: true, inverse: LinkBlockedBy},
		{kind: LinkBlockedBy, valid: true, inverse: LinkBlocks},
		{kind: LinkRelatesTo, valid: true, inverse: LinkRelatesTo},
		{kind: LinkDuplicates, valid: true, inverse: LinkDuplicatedBy},
		{kind: LinkDuplicatedBy, valid: true, inverse: LinkDuplicates},
		{kind: "implements", valid: true, spec: true, inverse: "implemented_by"},
		{kind: "implemented_by", valid: true, spec: true, inverse: "implements"},
		{kind: "modifies", valid: true, spec: true, inverse: "modified_by"},
		{kind: "modified_by", valid: true, spec: true, inverse: "modifies"},
		{kind: "supersedes", valid: true, spec: true, inverse: "superseded_by"},
		{kind: "superseded_by", valid: true, spec: true, inverse: "supersedes"},
		{kind: "replaces", inverse: "replaces"},
		{kind: "Implements", inverse: "Implements"},
		{kind: "", inverse: ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			if got := tt.kind.Valid(); got != tt.valid {
				t.Errorf("Valid() = %v, want %v", got, tt.valid)
			}
			if got := tt.kind.Spec(); got != tt.spec {
				t.Errorf("Spec() = %v, want %v", got, tt.spec)
			}
			if got := tt.kind.Inverse(); got != tt.inverse {
				t.Errorf("Inverse() = %q, want %q", got, tt.inverse)
			}
			if tt.valid && tt.kind.Inverse().Inverse() != tt.kind {
				t.Errorf("the inverse of the inverse of %q is not itself", tt.kind)
			}
		})
	}
}

func TestValidateSpecLinkTargets(t *testing.T) {
	t.Parallel()
	cfg := specConfig(t) // key TEST, schema 2

	tests := []struct {
		name   string
		source ItemType
		kind   LinkKind
		target string
		want   []Code
	}{
		// implements / modifies and their inverses: spec or requirement only.
		{name: "story implements a requirement", source: TypeStory, kind: LinkImplements, target: "TEST-SP-0003.R2"},
		{name: "story implements a whole spec", source: TypeStory, kind: LinkImplements, target: "TEST-SP-0003"},
		{name: "task modifies a requirement", source: TypeTask, kind: LinkModifies, target: "TEST-SP-0003.R12"},
		{name: "qualified requirement in another project", source: TypeStory, kind: LinkImplements, target: "WEB/WEB-SP-0001.R4"},
		{name: "implemented_by a requirement", source: TypeStory, kind: LinkImplementedBy, target: "TEST-SP-0003.R2"},
		{name: "implements a story", source: TypeStory, kind: LinkImplements, target: "TEST-US-0002", want: []Code{CodeLinkTargetType}},
		{name: "modifies a task", source: TypeTask, kind: LinkModifies, target: "TEST-T-0002", want: []Code{CodeLinkTargetType}},
		{name: "modified_by an epic", source: TypeStory, kind: LinkModifiedBy, target: "TEST-EP-0002", want: []Code{CodeLinkTargetType}},
		{name: "implemented_by a story", source: TypeStory, kind: LinkImplementedBy, target: "TEST-US-0002", want: []Code{CodeLinkTargetType}},

		// supersedes / superseded_by: spec to spec at the item level.
		{name: "spec supersedes a spec", source: TypeSpec, kind: LinkSupersedes, target: "TEST-SP-0002"},
		{name: "spec superseded_by a qualified spec", source: TypeSpec, kind: LinkSupersededBy, target: "WEB/WEB-SP-0009"},
		{name: "spec supersedes a requirement", source: TypeSpec, kind: LinkSupersedes, target: "TEST-SP-0002.R1", want: []Code{CodeLinkTargetType}},
		{name: "spec supersedes a story", source: TypeSpec, kind: LinkSupersedes, target: "TEST-US-0002", want: []Code{CodeLinkTargetType}},
		{name: "story supersedes a story", source: TypeStory, kind: LinkSupersedes, target: "TEST-US-0002", want: []Code{CodeLinkTargetType}},
		{name: "story superseded_by a spec", source: TypeStory, kind: LinkSupersededBy, target: "TEST-SP-0002", want: []Code{CodeLinkTargetType}},

		// The existing kinds MAY target a spec or a requirement.
		{name: "relates_to a requirement", source: TypeStory, kind: LinkRelatesTo, target: "TEST-SP-0003.R2"},
		{name: "blocked_by a spec", source: TypeTask, kind: LinkBlockedBy, target: "TEST-SP-0003"},
		{name: "duplicates a requirement", source: TypeStory, kind: LinkDuplicates, target: "TEST-SP-0003.R1"},

		// The target grammar.
		{name: "padded requirement number", source: TypeStory, kind: LinkImplements, target: "TEST-SP-0003.R02", want: []Code{CodeIDGrammar}},
		{name: "lower-case r", source: TypeStory, kind: LinkImplements, target: "TEST-SP-0003.r2", want: []Code{CodeIDGrammar}},
		{name: "R0", source: TypeStory, kind: LinkImplements, target: "TEST-SP-0003.R0", want: []Code{CodeIDGrammar}},
		{name: "requirement of a story", source: TypeStory, kind: LinkRelatesTo, target: "TEST-US-0003.R2", want: []Code{CodeIDGrammar}},
		{name: "missing number", source: TypeStory, kind: LinkImplements, target: "TEST-SP-0003.R", want: []Code{CodeIDGrammar}},
		{name: "bad qualifier", source: TypeStory, kind: LinkImplements, target: "web/WEB-SP-0003.R2", want: []Code{CodeIDGrammar}},
		{name: "unqualified foreign requirement", source: TypeStory, kind: LinkImplements, target: "WEB-SP-0003.R2", want: []Code{CodeIDKey}},
		{name: "qualifier does not match", source: TypeStory, kind: LinkImplements, target: "WEB/TEST-SP-0003.R2", want: []Code{CodeIDKey}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var item *Item
			switch tt.source {
			case TypeSpec:
				item = validSpec(t)
				item.Requirements = item.Requirements.Clone()
			default:
				item = validStory(t)
				if tt.source == TypeTask {
					item.Type, item.ID, item.Parent, item.Custom = TypeTask, "TEST-T-0009", "TEST-US-0002", nil
					item.Path = "docs/.pmngr/tasks/TEST-T-0009-a-clean-story.md"
				}
			}
			item.Links = []Link{{Kind: tt.kind, Target: tt.target}}
			got := sortedCodes(ValidateItem(item, cfg))
			want := append([]Code{}, tt.want...)
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			if !reflect.DeepEqual(got, want) {
				t.Errorf("codes = %v, want %v\ndiagnostics:\n%s", got, want, render(ValidateItem(item, cfg)))
			}
		})
	}
}

func TestSchemaFeatureOnSpecLinkKinds(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t) // schema 1

	tests := []struct {
		name string
		link Link
		want bool
	}{
		{name: "implements a requirement", link: Link{Kind: LinkImplements, Target: "TEST-SP-0001.R2"}, want: true},
		{name: "modifies a spec", link: Link{Kind: LinkModifies, Target: "TEST-SP-0001"}, want: true},
		{name: "relates_to a requirement", link: Link{Kind: LinkRelatesTo, Target: "TEST-SP-0001.R2"}, want: true},
		{name: "relates_to a qualified requirement", link: Link{Kind: LinkRelatesTo, Target: "WEB/WEB-SP-0001.R2"}, want: true},
		// A spec kind is a spec construct whatever its target; the wrong target
		// is reported separately as E-LINK-TARGET-TYPE.
		{name: "supersedes a story", link: Link{Kind: LinkSupersedes, Target: "TEST-US-0002"}, want: true},
		{name: "blocks a task", link: Link{Kind: LinkBlocks, Target: "TEST-T-0002"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := validStory(t)
			item.Links = []Link{tt.link}
			if _, got := SchemaFeatureDiagnostic(item, cfg); got != tt.want {
				t.Errorf("E-SCHEMA-FEATURE = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStoreUpgradesSchemaOnFirstImplementsLink(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store, fsys := specStore(t, "1")
	story, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Plain"})
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}
	link := Link{Kind: LinkImplements, Target: "ACME-SP-0004.R2"}
	updated, err := store.Update(ctx, story.ID, ItemPatch{AddLinks: []Link{link}}, story.Rev)
	if err != nil {
		t.Fatalf("Update(): %v", err)
	}
	if len(updated.Links) != 1 || updated.Links[0] != link {
		t.Errorf("links = %+v", updated.Links)
	}
	data, _ := fsys.ReadFile("docs/.pmngr/project.yaml")
	if !strings.HasPrefix(string(data), "schema: 2\n") || store.Schema() != 2 {
		t.Errorf("project.yaml = %q, store schema %d", data, store.Schema())
	}

	// A wrong target type is refused, and refused before anything is written.
	if _, err := store.Update(ctx, story.ID, ItemPatch{AddLinks: []Link{{Kind: LinkImplements, Target: "ACME-US-0001"}}}, ""); err == nil {
		t.Error("Update() accepted implements with a story target")
	}
}

// requirementLinkVault is a project at schema 2 with two specs, the work that
// implements and modifies their requirements, and three kinds of broken ref.
func requirementLinkVault(t *testing.T) *Index {
	t.Helper()
	fsys := NewMemFS()
	files := map[string]string{
		"docs/.pmngr/project.yaml": "schema: 2\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n" +
			"    - {id: todo, category: todo}\n    - {id: done, category: done}\n",
		"docs/.pmngr/specs/ACME-SP-0001-allocation.md": "---\nid: ACME-SP-0001\ntype: spec\ntitle: Allocation\nstatus: todo\n" +
			"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" +
			"requirements:\n  R1:\n    status: done\n  R2:\n    status: todo\n    links:\n" +
			"      - { kind: supersedes, target: ACME-SP-0002.R1 }\n" +
			"  R3:\n    status: todo\n    links:\n      - { kind: supersedes, target: ACME-SP-0002.R7 }\n---\n\n" +
			"## Requirements\n\n### ACME-SP-0001.R1 — One\n\nThe system SHALL do one.\n\n" +
			"### ACME-SP-0001.R2 — Two\n\nThe system SHALL do two.\n\n" +
			"### ACME-SP-0001.R3 — Three\n\nThe system SHALL do three.\n",
		"docs/.pmngr/specs/ACME-SP-0002-legacy.md": "---\nid: ACME-SP-0002\ntype: spec\ntitle: Legacy\nstatus: done\n" +
			"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" +
			"requirements:\n  R1:\n    status: cancelled\n  R2:\n    status: done\n---\n\n" +
			"## Requirements\n\n### ACME-SP-0002.R1 — Old\n\nThe system SHALL do the old thing.\n",
		"docs/.pmngr/stories/ACME-US-0001-build-it.md": "---\nid: ACME-US-0001\ntype: story\ntitle: Build it\nstatus: todo\n" +
			"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" +
			"links:\n  - { kind: implements, target: ACME-SP-0001.R2 }\n  - { kind: implements, target: ACME-SP-0001.R1 }\n---\n",
		"docs/.pmngr/tasks/ACME-T-0001-change-it.md": "---\nid: ACME-T-0001\ntype: task\ntitle: Change it\nstatus: todo\n" +
			"parent: ACME-US-0001\ncreated: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" +
			"links:\n  - { kind: modifies, target: ACME-SP-0001.R2 }\n  - { kind: implements, target: ACME-SP-0001 }\n---\n",
		"docs/.pmngr/stories/ACME-US-0002-broken-refs.md": "---\nid: ACME-US-0002\ntype: story\ntitle: Broken refs\nstatus: todo\n" +
			"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" +
			"links:\n  - { kind: implements, target: ACME-SP-0001.R9 }\n  - { kind: implements, target: ACME-SP-0099.R1 }\n" +
			"  - { kind: implements, target: WEB/WEB-SP-0001.R1 }\n  - { kind: relates_to, target: ACME-SP-0002.R2 }\n---\n",
	}
	for p, data := range files {
		if err := fsys.MkdirAll(p[:strings.LastIndex(p, "/")]); err != nil {
			t.Fatal(err)
		}
		if err := fsys.WriteFile(p, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	projects, err := DiscoverProjects(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(fsys, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build(): %v", err)
	}
	return ix
}

func TestIndexResolvesRequirementLinks(t *testing.T) {
	t.Parallel()
	ix := requirementLinkVault(t)

	tests := []struct {
		name   string
		target string
		kind   LinkKind
		want   []ItemID
	}{
		{name: "which work implements R2", target: "ACME-SP-0001.R2", kind: LinkImplementedBy, want: []ItemID{"ACME-US-0001"}},
		{name: "which work modifies R2", target: "ACME-SP-0001.R2", kind: LinkModifiedBy, want: []ItemID{"ACME-T-0001"}},
		{name: "which work implements R1", target: "ACME-SP-0001.R1", kind: LinkImplementedBy, want: []ItemID{"ACME-US-0001"}},
		{name: "a qualified target", target: "ACME/ACME-SP-0001.R1", kind: LinkImplementedBy, want: []ItemID{"ACME-US-0001"}},
		{name: "whole-spec links stay on the spec", target: "ACME-SP-0001", kind: LinkImplementedBy, want: []ItemID{"ACME-T-0001"}},
		{name: "nothing implements R3", target: "ACME-SP-0001.R3", kind: LinkImplementedBy},
		{name: "what a story implements", target: "ACME-US-0001", kind: LinkImplements, want: []ItemID{"ACME-SP-0001.R1", "ACME-SP-0001.R2"}},
		{name: "a requirement's own supersedes", target: "ACME-SP-0001.R2", kind: LinkSupersedes, want: []ItemID{"ACME-SP-0002.R1"}},
		{name: "the superseded requirement sees its successor", target: "ACME-SP-0002.R1", kind: LinkSupersededBy, want: []ItemID{"ACME-SP-0001.R2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ix.Related(tt.target, tt.kind); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Related(%s, %s) = %v, want %v", tt.target, tt.kind, got, tt.want)
			}
		})
	}

	computed := ix.LinkGraph().InverseLinks("ACME-SP-0001.R2")
	if len(computed) != 2 {
		t.Fatalf("inverse links of R2 = %+v", computed)
	}
	for _, l := range computed {
		if !l.Computed || l.From != "ACME-SP-0001.R2" {
			t.Errorf("inverse link %+v is not a computed edge from the requirement", l)
		}
	}
}

func TestIndexReportsDanglingRequirementTargets(t *testing.T) {
	t.Parallel()
	ix := requirementLinkVault(t)

	type finding struct{ path, field, what string }
	var got []finding
	for _, d := range ix.Warnings() {
		if d.Code != CodeWarnRefDangling {
			continue
		}
		what := "?"
		for _, w := range []string{"unknown requirement", "unknown spec", "unknown item"} {
			if strings.Contains(d.Message, w) {
				what = w
			}
		}
		got = append(got, finding{d.Path, d.Field, what})
	}
	sort.Slice(got, func(i, j int) bool { return got[i].path+got[i].what < got[j].path+got[j].what })
	want := []finding{
		// ACME-SP-0002.R7 has neither a block nor an entry.
		{"docs/.pmngr/specs/ACME-SP-0001-allocation.md", "requirements.R3.links.supersedes", "unknown requirement"},
		// R9 has no block in an existing spec.
		{"docs/.pmngr/stories/ACME-US-0002-broken-refs.md", "links.implements", "unknown requirement"},
		// R2 has an entry but no block: its block was deleted by hand (R-REQ-6).
		{"docs/.pmngr/stories/ACME-US-0002-broken-refs.md", "links.relates_to", "unknown requirement"},
		// SP-0099 does not exist. WEB/WEB-SP-0001.R1 is remote, not dangling.
		{"docs/.pmngr/stories/ACME-US-0002-broken-refs.md", "links.implements", "unknown spec"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("W-REF-DANGLING findings =\n%v\nwant\n%v", got, want)
	}
	for _, d := range ix.Warnings() {
		if d.Code == CodeLinkTargetType || d.Code == CodeSchemaFeature {
			t.Errorf("unexpected finding %s: %s", d.Code, d.Message)
		}
	}
}
