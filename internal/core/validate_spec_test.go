package core

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// specConfig is the validator fixture project raised to the spec schema, with
// a triage status so the inbox exclusion can be exercised.
func specConfig(t *testing.T) *ProjectConfig {
	t.Helper()
	cfg := testConfig(t)
	cfg.Schema = SpecSchema
	cfg.Workflow.Statuses = append(cfg.Workflow.Statuses, StatusDef{ID: "triage", Category: CategoryTriage})
	return cfg
}

// validSpec returns a spec that must validate without a single diagnostic.
func validSpec(t *testing.T) *Item {
	t.Helper()
	return &Item{
		ID:       "TEST-SP-0001",
		Type:     TypeSpec,
		Title:    "Item ID allocation",
		Status:   "in_progress",
		Priority: PriorityHigh,
		Author:   "jose",
		Labels:   []string{"core"},
		Created:  mustTimestamp(t, "2026-01-05T09:00:00Z"),
		Updated:  mustTimestamp(t, "2026-02-01T10:30:00Z"),
		Started:  mustTimestamp(t, "2026-01-20T08:00:00Z"),
		Requirements: Requirements{
			"R1": {Status: "done"},
			"R2": {
				Status: "in_progress",
				Trace:  &RequirementTrace{Code: []string{"internal/core/allocator.go#NextID"}, Tests: []string{"internal/core/allocator_test.go"}},
				Verified: &Verification{
					Rev: "sha256:4e1b9c0d7a3f2e61", Commit: strings.Repeat("9c", 20),
					At: mustTimestamp(t, "2026-02-01T10:00:00Z"), By: "claude",
				},
				Links: []Link{{Kind: "supersedes", Target: "TEST-SP-0002.R7"}, {Kind: "relates_to", Target: "TEST-US-0001"}},
			},
		},
		Body: "## Requirements\n\n### TEST-SP-0001.R1 — One\n\nThe system SHALL do one.\n\n" +
			"#### Scenario: one\n- **WHEN** asked\n- **THEN** it does one\n\n" +
			"### TEST-SP-0001.R2 — Two\n\nThe system SHALL do two.\n\n" +
			"#### Scenario: two\n- **WHEN** asked\n- **THEN** it does two\n",
		Path: "docs/.pmngr/specs/TEST-SP-0001-item-id-allocation.md",
	}
}

// Verifies: GIT-SP-0003.R6
func TestValidateSpecRules(t *testing.T) {
	t.Parallel()
	cfg := specConfig(t)

	tests := []struct {
		name   string
		mutate func(*Item)
		cfg    func(*ProjectConfig)
		want   []Code
	}{
		{name: "a valid spec reports nothing", mutate: func(*Item) {}},
		{
			name:   "a spec in a schema-1 project",
			mutate: func(*Item) {},
			cfg:    func(c *ProjectConfig) { c.Schema = 1 },
			want:   []Code{CodeSchemaFeature},
		},
		{
			name: "planning fields are not spec fields",
			mutate: func(it *Item) {
				e := 3.0
				it.Milestone, it.Sprint, it.Estimate = "TEST-M-0001", "s1", &e
			},
			want: []Code{CodeFieldType, CodeFieldType, CodeFieldType},
		},
		{
			name:   "a spec has no parent",
			mutate: func(it *Item) { it.Parent = "TEST-EP-0001" },
			want:   []Code{CodeRefParentType},
		},
		{
			name:   "a spec is not an inbox target",
			mutate: func(it *Item) { it.Status = "triage" },
			want:   []Code{CodeReqStatus, CodeWarnWorkflowTransition},
		},
		{
			name: "duplicate block",
			mutate: func(it *Item) {
				it.Body += "\n### TEST-SP-0001.R2 — Two again\n\nThe system SHALL do it twice.\n"
			},
			want: []Code{CodeReqDuplicate},
		},
		{
			name:   "malformed ref in a heading",
			mutate: func(it *Item) { it.Body += "\n### TEST-SP-0001.R03 — Padded\n" },
			want:   []Code{CodeIDGrammar},
		},
		{
			name:   "foreign ref in a heading",
			mutate: func(it *Item) { it.Body += "\n### TEST-SP-0009.R3 — Elsewhere\n" },
			want:   []Code{CodeReqForeign},
		},
		{
			name:   "block without an entry",
			mutate: func(it *Item) { delete(it.Requirements, "R1") },
			want:   []Code{CodeWarnReqNoEntry},
		},
		{
			name:   "entry without a status",
			mutate: func(it *Item) { it.Requirements["R1"].Status = "" },
			want:   []Code{CodeWarnReqNoEntry},
		},
		{
			name:   "entry without a block",
			mutate: func(it *Item) { it.Requirements["R9"] = &Requirement{Status: "cancelled"} },
			want:   []Code{CodeStatusUnknown, CodeWarnReqOrphanEntry},
		},
		{
			name:   "malformed key",
			mutate: func(it *Item) { it.Requirements["R02"] = &Requirement{Status: "todo"} },
			want:   []Code{CodeReqField},
		},
		{
			name:   "unknown requirement status",
			mutate: func(it *Item) { it.Requirements["R1"].Status = "shipped" },
			want:   []Code{CodeStatusUnknown},
		},
		{
			name:   "triage requirement status",
			mutate: func(it *Item) { it.Requirements["R1"].Status = "triage" },
			want:   []Code{CodeReqStatus},
		},
		{
			name: "bad trace refs",
			mutate: func(it *Item) {
				it.Requirements["R2"].Trace.Code = []string{"/abs/path.go", "../outside.go", "x.go#"}
			},
			want: []Code{CodeReqField, CodeReqField, CodeReqField},
		},
		{
			name: "bad stamp",
			mutate: func(it *Item) {
				it.Requirements["R2"].Verified = &Verification{Rev: "4e1b", Commit: "abc"}
			},
			want: []Code{CodeReqField, CodeReqField, CodeReqField, CodeReqField},
		},
		{
			name: "requirement link kinds",
			mutate: func(it *Item) {
				it.Requirements["R2"].Links = []Link{
					{Kind: "implements", Target: "TEST-SP-0002.R1"},
					{Kind: "supersedes", Target: "TEST-SP-0002"},
					{Kind: "relates_to", Target: "nonsense"},
				}
			},
			want: []Code{CodeIDGrammar, CodeLinkTargetType, CodeReqField},
		},
		{
			name: "requirements on a story",
			mutate: func(it *Item) {
				it.Type = TypeStory
				it.ID = "TEST-US-0009"
				it.Path = "docs/.pmngr/stories/TEST-US-0009-item-id-allocation.md"
			},
			want: []Code{CodeReqField},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := validSpec(t)
			item.Requirements = item.Requirements.Clone()
			tt.mutate(item)
			c := *cfg
			if tt.cfg != nil {
				tt.cfg(&c)
			}
			diags := ValidateItem(item, &c)
			got := sortedCodes(diags)
			want := append([]Code{}, tt.want...)
			sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
			if !reflect.DeepEqual(got, want) {
				t.Errorf("codes = %v, want %v\ndiagnostics:\n%s", got, want, render(diags))
			}
		})
	}
}

func TestSchemaFeatureOnLinks(t *testing.T) {
	t.Parallel()
	cfg := testConfig(t) // schema 1

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "a spec target", target: "TEST-SP-0001", want: true},
		{name: "a qualified spec target", target: "WEB/WEB-SP-0001", want: true},
		{name: "a story target", target: "TEST-T-0002", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := validStory(t)
			item.Links = []Link{{Kind: LinkRelatesTo, Target: tt.target}}
			_, got := SchemaFeatureDiagnostic(item, cfg)
			if got != tt.want {
				t.Errorf("E-SCHEMA-FEATURE = %v, want %v", got, tt.want)
			}
		})
	}
}

// specStore mounts a store over a fresh project at the given schema.
func specStore(t *testing.T, schema string) (*FileStore, *MemFS) {
	t.Helper()
	fsys := NewMemFS()
	yaml := "schema: " + schema + "\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n" +
		"    - {id: todo, category: todo}\n    - {id: done, category: done}\n"
	if err := fsys.MkdirAll("docs/.pmngr"); err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFile("docs/.pmngr/project.yaml", []byte(yaml)); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadProjectConfig([]byte(yaml))
	cfg.IDAllocation.WriteCounters = false
	store := NewStore(fsys, "docs", cfg)
	store.Clock = ClockFunc(func() time.Time { return time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC) })
	return store, fsys
}

func TestStoreUpgradesSchemaOnFirstSpecConstruct(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("creating the first spec raises schema in the same write", func(t *testing.T) {
		t.Parallel()
		store, fsys := specStore(t, "1")
		it, err := store.Create(ctx, ItemDraft{Type: TypeSpec, Title: "Item ID allocation"})
		if err != nil {
			t.Fatalf("Create(): %v", err)
		}
		if it.ID != "ACME-SP-0001" || !strings.HasPrefix(it.Path, "docs/.pmngr/specs/") {
			t.Errorf("created %s at %s", it.ID, it.Path)
		}
		data, _ := fsys.ReadFile("docs/.pmngr/project.yaml")
		if !strings.HasPrefix(string(data), "schema: 2\n") || store.Schema() != 2 {
			t.Errorf("project.yaml = %q, store schema %d", data, store.Schema())
		}
	})

	t.Run("a story without spec constructs leaves schema 1", func(t *testing.T) {
		t.Parallel()
		store, fsys := specStore(t, "1")
		if _, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Plain"}); err != nil {
			t.Fatalf("Create(): %v", err)
		}
		data, _ := fsys.ReadFile("docs/.pmngr/project.yaml")
		if !strings.HasPrefix(string(data), "schema: 1\n") || store.Schema() != 1 {
			t.Errorf("project.yaml = %q", data)
		}
	})

	t.Run("adding a link to a spec raises schema", func(t *testing.T) {
		t.Parallel()
		store, fsys := specStore(t, "1")
		story, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Plain"})
		if err != nil {
			t.Fatalf("Create(): %v", err)
		}
		if _, err := store.Update(ctx, story.ID, ItemPatch{AddLinks: []Link{{Kind: LinkRelatesTo, Target: "ACME-SP-0004"}}}, story.Rev); err != nil {
			t.Fatalf("Update(): %v", err)
		}
		data, _ := fsys.ReadFile("docs/.pmngr/project.yaml")
		if !strings.HasPrefix(string(data), "schema: 2\n") {
			t.Errorf("project.yaml = %q", data)
		}
	})

	t.Run("a refused write leaves project.yaml alone", func(t *testing.T) {
		t.Parallel()
		store, fsys := specStore(t, "1")
		e := 3.0
		if _, err := store.Create(ctx, ItemDraft{Type: TypeSpec, Title: "Bad", Estimate: &e}); err == nil {
			t.Fatal("Create() accepted a spec with an estimate")
		}
		data, _ := fsys.ReadFile("docs/.pmngr/project.yaml")
		if !strings.HasPrefix(string(data), "schema: 1\n") || store.Schema() != 1 {
			t.Errorf("project.yaml = %q", data)
		}
	})
}

func TestStoreWriteGate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, schema := range []string{"3", "0"} {
		t.Run("schema "+schema, func(t *testing.T) {
			t.Parallel()
			store, _ := specStore(t, schema)
			_, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Refused"})
			if !errors.Is(err, ErrReadOnly) || !errors.Is(err, ErrSchemaUnsupported) {
				t.Fatalf("Create() = %v, want a read-only refusal", err)
			}
			if _, err := store.WritePage(ctx, "", "guide.md", []byte("# Guide\n"), ""); !errors.Is(err, ErrReadOnly) {
				t.Errorf("WritePage() = %v, want a read-only refusal", err)
			}
		})
	}
}

func TestIndexReportsSpecFindings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store, fsys := specStore(t, "1")
	// A spec written by hand, with no requirements entry for its block, in a
	// project still at schema 1.
	spec := "---\nid: ACME-SP-0002\ntype: spec\ntitle: Hand made\nstatus: todo\n" +
		"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n\n" +
		"## Requirements\n\n### ACME-SP-0002.R1 — One\n\nThe system SHALL do one.\n"
	if err := fsys.MkdirAll("docs/.pmngr/specs"); err != nil {
		t.Fatal(err)
	}
	if err := fsys.WriteFile("docs/.pmngr/specs/ACME-SP-0002-hand-made.md", []byte(spec)); err != nil {
		t.Fatal(err)
	}
	projects, err := DiscoverProjects(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(fsys, projects)
	if _, err := ix.Build(ctx, true); err != nil {
		t.Fatalf("Build(): %v", err)
	}
	var codes []Code
	for _, d := range ix.Warnings() {
		codes = append(codes, d.Code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	// counters.spec is a hint like every other counter; the block has no
	// scenario, which the grammar lint reports at its default, warning.
	want := []Code{CodeSchemaFeature, LintReqScenario, CodeWarnCounterStale, CodeWarnReqNoEntry}
	if !reflect.DeepEqual(codes, want) {
		t.Errorf("index findings = %v, want %v", codes, want)
	}
	if n, err := ix.NextRequirementNumber("ACME-SP-0002"); err != nil || n != 2 {
		t.Errorf("NextRequirementNumber() = %d, %v", n, err)
	}
	// An existing hand-written construct is never upgraded implicitly: the
	// write stays refused until doctor --fix raises the schema.
	if _, err := store.Update(ctx, "ACME-SP-0002", ItemPatch{Title: strPtr("Renamed")}, ""); err == nil {
		t.Error("Update() of a hand-written spec in a schema-1 project was accepted")
	}
}

func strPtr(v string) *string { return &v }
