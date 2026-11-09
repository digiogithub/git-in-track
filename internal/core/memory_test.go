package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestMemoryIDGrammar(t *testing.T) {
	t.Parallel()
	key, code, n, err := ParseItemID("ACME-MEM-0042")
	if err != nil || key != "ACME" || code != CodeMemory || n != 42 {
		t.Fatalf("ParseItemID(ACME-MEM-0042) = %s %s %d %v", key, code, n, err)
	}
	if got, ok := TypeCodeFor(TypeMemory); !ok || got != CodeMemory {
		t.Errorf("TypeCodeFor(memory) = %q, %v", got, ok)
	}
	if got, ok := ItemTypeFor(CodeMemory); !ok || got != TypeMemory {
		t.Errorf("ItemTypeFor(MEM) = %q, %v", got, ok)
	}
	if dir, ok := ItemDirName(TypeMemory); !ok || dir != MemoryDirName {
		t.Errorf("ItemDirName(memory) = %q, %v", dir, ok)
	}
	for _, bad := range []string{"ACME-ME-0001", "ACME-MEMO-0001", "ACME-MEM-001"} {
		if _, _, _, err := ParseItemID(bad); err == nil {
			t.Errorf("ParseItemID(%q) accepted a malformed id", bad)
		}
	}
	// The milestone code is untouched by the longer MEM code.
	if _, code, _, err := ParseItemID("ACME-M-0001"); err != nil || code != CodeMilestone {
		t.Errorf("ParseItemID(ACME-M-0001) = %s, %v", code, err)
	}
}

// TestMemoryContentHash pins MemoryContentHash to the alchemai.memory.v2
// formula with a fixed vector, computed independently of this package and of
// the protocol SDK: sha256 of kind, scope, key, path and content joined by
// U+001F.
func TestMemoryContentHash(t *testing.T) {
	t.Parallel()
	const want = "6a4773b30cf45a558333300b493982b89a933eb98e2d7cb6f66602ad5e126709"
	got := MemoryContentHash("fact", "project/acme", "customer-x-invoicing", "", "Customer X is invoiced quarterly.")
	if got != want {
		t.Fatalf("MemoryContentHash = %s, want %s", got, want)
	}
	if MemoryContentHash("fact", "project/acme", "k", "", "a") == MemoryContentHash("fact", "project/acme", "k", "", "b") {
		t.Error("different content must not share a hash")
	}
}

func TestMemoryScope(t *testing.T) {
	t.Parallel()
	if got := MemoryScope(ProjectRef{Key: "ACME"}); got != "project/acme" {
		t.Errorf("project scope = %q", got)
	}
	if got := MemoryScope(ProjectRef{Key: "OPS", Team: true}); got != "org/ops" {
		t.Errorf("team scope = %q", got)
	}
}

func TestMemoryWorkflow(t *testing.T) {
	t.Parallel()
	backlog := &ProjectConfig{Key: "ACME", Workflow: Workflow{
		Initial:  "backlog",
		Statuses: []StatusDef{{ID: "backlog", Category: CategoryTodo}, {ID: "active", Category: CategoryInProgress}},
	}}
	mem := backlog.For(TypeMemory)
	if backlog.For(TypeStory) != backlog {
		t.Error("For must return the project itself for a backlog type")
	}
	cases := []struct {
		status Status
		want   StatusCategory
	}{
		{MemoryProposed, CategoryTriage},
		{MemoryActive, CategoryDone}, // the project's own "active" does not leak in
		{MemorySuperseded, CategoryDone},
		{MemoryForgotten, CategoryCancelled},
		{MemoryCancelled, CategoryCancelled},
		{"backlog", ""},
	}
	for _, c := range cases {
		if got := mem.CategoryOf(c.status); got != c.want {
			t.Errorf("memory CategoryOf(%s) = %q, want %q", c.status, got, c.want)
		}
	}
	transitions := []struct {
		from, to Status
		allowed  bool
	}{
		{MemoryProposed, MemoryActive, true},
		{MemoryProposed, MemoryCancelled, true},
		{MemoryCancelled, MemoryProposed, true}, // a rejected proposal can be reopened
		{MemoryActive, MemoryForgotten, true},
		{MemoryForgotten, MemoryActive, true},
		{MemorySuperseded, MemoryActive, false},
		{MemoryProposed, MemoryForgotten, false},
	}
	for _, c := range transitions {
		if got := TransitionAllowed(mem, c.from, c.to); got != c.allowed {
			t.Errorf("memory %s -> %s allowed = %v, want %v", c.from, c.to, got, c.allowed)
		}
	}
	var none *ProjectConfig
	if got := none.For(TypeMemory).CategoryOf(MemoryProposed); got != CategoryTriage {
		t.Errorf("a nil configuration still yields the memory lifecycle, got %q", got)
	}
}

// memoryItem builds a valid active memory; tests mutate a copy.
func memoryItem() *Item {
	ts := func(s string) Timestamp { v, _ := ParseTimestamp(s); return v }
	return &Item{
		ID: "ACME-MEM-0001", Type: TypeMemory, Title: "A fact", Status: MemoryActive,
		Path:    "docs/.pmngr/memory/ACME-MEM-0001-a-fact.md",
		Created: ts("2026-11-08T10:00:00Z"), Updated: ts("2026-11-08T10:00:00Z"),
		Memory: &ItemMemory{Key: "a-fact", Version: 1},
	}
}

func TestValidateMemory(t *testing.T) {
	t.Parallel()
	cfg := &ProjectConfig{Key: "ACME", Workflow: Workflow{Initial: "backlog", Statuses: []StatusDef{
		{ID: "backlog", Category: CategoryTodo}, {ID: "done", Category: CategoryDone},
	}}}
	num := func(f float64) *float64 { return &f }
	ts := func(s string) Timestamp { v, _ := ParseTimestamp(s); return v }
	cases := []struct {
		name   string
		mutate func(*Item)
		cfg    *ProjectConfig
		want   Code // "" means no finding
	}{
		{"valid", func(*Item) {}, cfg, ""},
		{"memory status not in the project workflow is fine", func(it *Item) { it.Status = MemoryProposed }, cfg, ""},
		{"backlog status on a memory", func(it *Item) { it.Status = "backlog" }, cfg, CodeStatusUnknown},
		{"no block", func(it *Item) { it.Memory = nil }, cfg, CodeMemoryBlock},
		{"block on a story", func(it *Item) {
			it.Type, it.ID, it.Status = TypeStory, "ACME-US-0001", "backlog"
			it.Path = "docs/.pmngr/stories/ACME-US-0001-a-fact.md"
		}, cfg, CodeMemoryBlock},
		{"bad key", func(it *Item) { it.Memory.Key = "Has Spaces" }, cfg, CodeMemoryKey},
		{"zero version", func(it *Item) { it.Memory.Version = 0 }, cfg, CodeMemoryVersion},
		{"bad hash", func(it *Item) { it.Memory.ContentHash = "ABC" }, cfg, CodeMemoryHash},
		{"local id with another key", func(it *Item) { it.Memory.LocalID = "project/acme/other" }, cfg, CodeMemoryLocalID},
		{"local id with a user scope", func(it *Item) { it.Memory.LocalID = "user/ana/a-fact" }, cfg, CodeMemoryLocalID},
		{"local id ok", func(it *Item) { it.Memory.LocalID = "project/acme/a-fact" }, cfg, ""},
		{"forgotten without valid_to", func(it *Item) { it.Status = MemoryForgotten }, cfg, CodeMemoryValidTo},
		{"valid_to before valid_from", func(it *Item) {
			it.Memory.ValidFrom, it.Memory.ValidTo = ts("2026-11-08T10:00:00Z"), ts("2026-11-01T10:00:00Z")
		}, cfg, CodeMemoryValidTo},
		{"estimate on a memory", func(it *Item) { it.Estimate = num(3) }, cfg, CodeMemoryPlanning},
		{"assignees on a memory", func(it *Item) { it.Assignees = []string{"ana"} }, cfg, CodeMemoryPlanning},
		{"parent on a memory", func(it *Item) { it.Parent = "ACME-EP-0001" }, cfg, CodeRefParentType},
		{"supersedes a memory", func(it *Item) {
			it.Links = []Link{{Kind: LinkSupersedes, Target: "ACME-MEM-0002"}}
		}, &ProjectConfig{Key: "ACME", Schema: SpecSchema}, ""},
		{"supersedes a story", func(it *Item) {
			it.Links = []Link{{Kind: LinkSupersedes, Target: "ACME-US-0002"}}
		}, cfg, CodeLinkTargetType},
		{"writes superseded_by", func(it *Item) {
			it.Links = []Link{{Kind: LinkSupersededBy, Target: "ACME-MEM-0002"}}
		}, cfg, CodeLinkTargetType},
		{"a story in a knowledge project", func(it *Item) {
			it.Type, it.ID, it.Status, it.Memory = TypeStory, "ACME-US-0001", "backlog", nil
			it.Path = "docs/.pmngr/stories/ACME-US-0001-a-fact.md"
		}, &ProjectConfig{Key: "ACME", Kind: ProjectKnowledge}, CodeKindNotBacklog},
		{"a memory in a knowledge project", func(*Item) {}, &ProjectConfig{Key: "ACME", Kind: ProjectKnowledge}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			it := memoryItem()
			c.mutate(it)
			var codes []Code
			for _, d := range ValidateItem(it, c.cfg) {
				if d.Severity == SeverityError {
					codes = append(codes, d.Code)
				}
			}
			if c.want == "" {
				if len(codes) > 0 {
					t.Fatalf("unexpected findings %v", codes)
				}
				return
			}
			for _, got := range codes {
				if got == c.want {
					return
				}
			}
			t.Fatalf("findings %v, want %s", codes, c.want)
		})
	}
}

func TestKnowledgeProjectConfig(t *testing.T) {
	t.Parallel()
	cfg, err := LoadProjectConfig([]byte("schema: 2\nkey: ACME\nname: Acme knowledge\nkind: knowledge\ndocs: {path: docs}\n"))
	if err != nil {
		t.Fatalf("a knowledge project without a workflow must parse: %v", err)
	}
	if !cfg.IsKnowledge() {
		t.Fatal("IsKnowledge() = false")
	}
	for _, d := range cfg.Validate() {
		if d.Severity == SeverityError {
			t.Errorf("unexpected error %s", d)
		}
	}
	if _, err := LoadProjectConfig([]byte("schema: 2\nkey: ACME\nname: X\nkind: archive\nworkflow: {statuses: [{id: a, category: done}]}\n")); err == nil ||
		!strings.Contains(err.Error(), string(CodeProjKind)) {
		t.Errorf("an unknown kind must be %s, got %v", CodeProjKind, err)
	}
	if _, err := LoadProjectConfig([]byte("schema: 2\nkey: ACME\nname: X\n")); err == nil {
		t.Error("a backlog project still needs a workflow")
	}
}

// TestReconcileMemoryVersionsGolden replays an offline merge: two writers each
// took version 5 before they saw each other's commits, and the merged folder
// holds three memories at version 5. The index flags the duplicates, and
// ReconcileMemoryVersions renumbers them past the top deterministically.
func TestReconcileMemoryVersionsGolden(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "memory-merge")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var memories []*Item
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		it, err := ParseItem("docs/.pmngr/memory/"+e.Name(), data)
		if err != nil {
			t.Fatalf("ParseItem(%s): %v", e.Name(), err)
		}
		memories = append(memories, it)
	}
	// Input order must not matter.
	sort.Slice(memories, func(i, j int) bool { return memories[i].ID > memories[j].ID })

	fixes := ReconcileMemoryVersions(memories)
	var b strings.Builder
	fmt.Fprintf(&b, "next version before: %d\n", NextMemoryVersion(memories))
	for _, f := range fixes {
		fmt.Fprintf(&b, "%s %d -> %d\n", f.ID, f.From, f.To)
	}
	compareGolden(t, "memory-reconcile.txt", []byte(b.String()))

	// After the fixes, no version is held twice.
	seen := map[int64]ItemID{}
	for _, it := range memories {
		v := it.Memory.Version
		for _, f := range fixes {
			if f.ID == it.ID {
				v = f.To
			}
		}
		if other, dup := seen[v]; dup {
			t.Errorf("%s and %s still share version %d", it.ID, other, v)
		}
		seen[v] = it.ID
	}
	if again := ReconcileMemoryVersions(nil); again != nil {
		t.Errorf("nothing to reconcile, got %v", again)
	}
}

const memoryProjectYAML = `schema: 2
key: ACME
name: Acme
docs: {path: docs}
workflow:
  initial: backlog
  statuses:
    - {id: triage, category: triage}
    - {id: backlog, category: todo}
    - {id: done, category: done}
`

func memoryFile(id, key, status string, version int, extra string) string {
	return fmt.Sprintf(`---
id: %s
type: memory
title: Fact %s
status: %s
created: 2026-11-08T10:00:00Z
updated: 2026-11-08T10:00:00Z
%smemory:
  key: %s
  version: %d
---

Fact %s.
`, id, key, status, extra, key, version, key)
}

func TestIndexKeepsMemoryOutOfBacklogQueries(t *testing.T) {
	t.Parallel()
	m := NewMemFSFromMap(map[string]string{
		"docs/.pmngr/project.yaml": memoryProjectYAML,
		"docs/.pmngr/stories/ACME-US-0001-work.md": "---\nid: ACME-US-0001\ntype: story\ntitle: Work\nstatus: backlog\n" +
			"created: 2026-11-08T10:00:00Z\nupdated: 2026-11-08T10:00:00Z\n---\n",
		"docs/.pmngr/memory/ACME-MEM-0001-a.md": memoryFile("ACME-MEM-0001", "a", "active", 1, ""),
		"docs/.pmngr/memory/ACME-MEM-0002-b.md": memoryFile("ACME-MEM-0002", "b", "proposed", 2,
			"links:\n  - {kind: supersedes, target: ACME-MEM-0001}\n"),
		"docs/.pmngr/memory/ACME-MEM-0003-a.md": memoryFile("ACME-MEM-0003", "a", "active", 2, ""),
	})
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(m, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	ids := func(f Filter) []string {
		t.Helper()
		page, err := ix.Items(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range page.Items {
			out = append(out, string(it.ID))
		}
		sort.Strings(out)
		return out
	}
	if got := ids(Filter{}); strings.Join(got, ",") != "ACME-US-0001" {
		t.Errorf("an untyped query = %v, want the story alone", got)
	}
	if got := ids(Filter{Types: []ItemType{TypeMemory}}); strings.Join(got, ",") != "ACME-MEM-0001,ACME-MEM-0003" {
		t.Errorf("a memory query = %v, want the two non-proposed memories", got)
	}
	if got := ids(Filter{Inbox: InboxOnly}); strings.Join(got, ",") != "ACME-MEM-0002" {
		t.Errorf("the inbox = %v, want the proposed memory", got)
	}

	codes := map[Code][]string{}
	for _, w := range ix.Warnings() {
		codes[w.Code] = append(codes[w.Code], w.Path)
	}
	for _, want := range []Code{CodeWarnMemoryKeyDup, CodeWarnMemoryVersionDup, CodeWarnMemorySupersede} {
		if len(codes[want]) == 0 {
			t.Errorf("no %s in %v", want, ix.Warnings())
		}
	}
}

func TestIndexOrganizationMemory(t *testing.T) {
	t.Parallel()
	team := func(key string) *MemFS {
		return NewMemFSFromMap(map[string]string{
			"team.yaml":                            "schema: 1\nkey: " + key + "\nname: Ops\nknowledge: {path: knowledge}\n",
			"knowledge/README.md":                  "# Ops\n",
			".pmngr/memory/OPS-MEM-0001-oncall.md": strings.ReplaceAll(memoryFile("OPS-MEM-0001", "oncall", "active", 1, ""), "ACME", "OPS"),
		})
	}
	build := func(t *testing.T, m *MemFS) *Index {
		t.Helper()
		ref, ok, err := DiscoverTeam(m, ".")
		if err != nil || !ok {
			t.Fatalf("DiscoverTeam: %v %v", ok, err)
		}
		ix := NewIndex(m, []ProjectRef{ref.KBScope()})
		if _, err := ix.Build(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		return ix
	}

	t.Run("a valid team key indexes the memory", func(t *testing.T) {
		t.Parallel()
		ix := build(t, team("OPS"))
		it, err := ix.Item("OPS-MEM-0001")
		if err != nil {
			t.Fatalf("Item: %v (warnings %v)", err, ix.Warnings())
		}
		if it.Memory == nil || it.Memory.Key != "oncall" {
			t.Errorf("memory = %+v", it.Memory)
		}
	})
	t.Run("a team key the project grammar refuses disables the folder", func(t *testing.T) {
		t.Parallel()
		ix := build(t, team("OPS-TEAM"))
		if _, err := ix.Item("OPS-MEM-0001"); err == nil {
			t.Error("the memory was indexed under an invalid team key")
		}
		found := false
		for _, w := range ix.Warnings() {
			found = found || w.Code == CodeTeamKeyMemory
		}
		if !found {
			t.Errorf("no %s in %v", CodeTeamKeyMemory, ix.Warnings())
		}
	})
}

// Verifies: GIT-SP-0003.R5
func TestMemorySupersedesTargets(t *testing.T) {
	t.Parallel()
	cfg := &ProjectConfig{Key: "TEST", Schema: SpecSchema}
	cases := []struct {
		kind   LinkKind
		target string
		want   bool // true: E-LINK-TARGET-TYPE
	}{
		{LinkSupersedes, "TEST-MEM-0002", false},
		{LinkSupersedes, "TEST-US-0002", true},
		{LinkSupersedes, "TEST-SP-0001", true},
		{LinkSupersededBy, "TEST-MEM-0002", true},
	}
	for _, c := range cases {
		it := memoryItem()
		it.ID, it.Path = "TEST-MEM-0001", "docs/.pmngr/memory/TEST-MEM-0001-a-fact.md"
		it.Links = []Link{{Kind: c.kind, Target: c.target}}
		got := false
		for _, d := range ValidateItem(it, cfg) {
			got = got || d.Code == CodeLinkTargetType
		}
		if got != c.want {
			t.Errorf("memory %s %s: E-LINK-TARGET-TYPE = %v, want %v", c.kind, c.target, got, c.want)
		}
	}
}
