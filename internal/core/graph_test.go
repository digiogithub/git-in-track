package core

import (
	"context"
	"reflect"
	"testing"
)

// linkVault is a small in-memory project exercising every relation kind.
func linkVault(t testing.TB) *Index {
	t.Helper()
	files := map[string]string{
		"docs/.pmngr/project.yaml": "schema: 1\nkey: LNK\nname: Links\ndocs:\n  path: docs\n" +
			"workflow:\n  initial: todo\n  statuses:\n    - {id: todo, category: todo}\n" +
			"    - {id: done, category: done, terminal: true}\n" +
			"id_allocation:\n  counters:\n    epic: 1\n    story: 3\n",
		"docs/.pmngr/epics/LNK-EP-0001-platform.md": "---\nid: LNK-EP-0001\ntype: epic\ntitle: Platform\n" +
			"status: todo\ncreated: 2026-08-01T08:00:00Z\nupdated: 2026-08-01T08:00:00Z\n---\n\n" +
			"## Description\n\nDesign lives in [[architecture/platform]].\n",
		"docs/.pmngr/stories/LNK-US-0001-first.md": "---\nid: LNK-US-0001\ntype: story\ntitle: First\n" +
			"status: todo\nparent: LNK-EP-0001\ncreated: 2026-08-02T08:00:00Z\nupdated: 2026-08-02T08:00:00Z\n" +
			"links:\n  - {kind: blocks, target: LNK-US-0002}\n  - {kind: duplicates, target: LNK-US-0003}\n---\n\n" +
			"## Description\n\nSee [[LNK-US-0002]] and [[architecture/platform#Scope]].\n",
		"docs/.pmngr/stories/LNK-US-0002-second.md": "---\nid: LNK-US-0002\ntype: story\ntitle: Second\n" +
			"status: todo\nparent: LNK-EP-0001\ncreated: 2026-08-03T08:00:00Z\nupdated: 2026-08-03T08:00:00Z\n" +
			"links:\n  - {kind: relates_to, target: LNK-US-0003}\n  - {kind: blocked_by, target: LNK-US-0001}\n---\n\n" +
			"## Description\n\nNothing to see.\n",
		"docs/.pmngr/stories/LNK-US-0003-third.md": "---\nid: LNK-US-0003\ntype: story\ntitle: Third\n" +
			"status: todo\nparent: LNK-EP-0001\nmilestone: LNK-M-0009\ncreated: 2026-08-04T08:00:00Z\n" +
			"updated: 2026-08-04T08:00:00Z\n---\n\n## Description\n\nBroken: [[does-not-exist]].\n",
		"docs/architecture/platform.md": "---\ntitle: Platform design\ntags: [architecture, core]\n---\n\n" +
			"# Platform design\n\nImplements [[LNK-EP-0001]] and [[LNK-US-0001|the first story]].\n\n" +
			"## Scope\n\nSee <https://example.com/spec> and [the RFC](https://example.com/rfc).\n\n" +
			"```go\n// [[NotALink]] inside code\n```\n",
	}
	m := NewMemFSFromMap(files)
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func TestGraphHierarchy(t *testing.T) {
	g := linkVault(t).LinkGraph()

	if got := g.Children("LNK-EP-0001"); !reflect.DeepEqual(got, []ItemID{"LNK-US-0001", "LNK-US-0002", "LNK-US-0003"}) {
		t.Errorf("children = %v", got)
	}
	parent, ok := g.Parent("LNK-US-0002")
	if !ok || parent != "LNK-EP-0001" {
		t.Errorf("parent = %q, %v", parent, ok)
	}
	if _, ok := g.Parent("LNK-EP-0001"); ok {
		t.Error("the epic must have no parent")
	}
	if got := g.MilestoneItems("LNK-M-0009"); !reflect.DeepEqual(got, []ItemID{"LNK-US-0003"}) {
		t.Errorf("milestone items = %v", got)
	}
}

func TestGraphComputesInverseLinks(t *testing.T) {
	g := linkVault(t).LinkGraph()

	tests := []struct {
		name string
		got  []GraphLink
		want []GraphLink
	}{
		{
			name: "blocks is stored on one side and inverted on the other",
			got:  g.InverseLinks("LNK-US-0002"),
			want: []GraphLink{{Kind: LinkBlockedBy, From: "LNK-US-0002", To: "LNK-US-0001", Computed: true}},
		},
		{
			name: "relates_to is symmetric",
			got:  g.InverseLinks("LNK-US-0003"),
			want: []GraphLink{
				{Kind: LinkDuplicatedBy, From: "LNK-US-0003", To: "LNK-US-0001", Computed: true},
				{Kind: LinkRelatesTo, From: "LNK-US-0003", To: "LNK-US-0002", Computed: true},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Errorf("got %+v, want %+v", tc.got, tc.want)
			}
		})
	}

	t.Run("a relation declared on both sides appears once", func(t *testing.T) {
		all := g.AllLinks("LNK-US-0001")
		count := 0
		for _, l := range all {
			if l.Kind == LinkBlocks && l.To == "LNK-US-0002" {
				count++
				if l.Computed {
					t.Error("the declared edge must win over the computed one")
				}
			}
		}
		if count != 1 {
			t.Errorf("blocks edges = %d, want 1 in %+v", count, all)
		}
	})

	t.Run("blocking helpers", func(t *testing.T) {
		if got := g.Blocking("LNK-US-0001"); !reflect.DeepEqual(got, []ItemID{"LNK-US-0002"}) {
			t.Errorf("blocking = %v", got)
		}
		if got := g.BlockedBy("LNK-US-0002"); !reflect.DeepEqual(got, []ItemID{"LNK-US-0001"}) {
			t.Errorf("blocked by = %v", got)
		}
	})
}

func TestGraphWikilinksAndBacklinks(t *testing.T) {
	ix := linkVault(t)
	g := ix.LinkGraph()

	t.Run("page to item", func(t *testing.T) {
		refs := g.References(PageNode("docs/architecture/platform.md"))
		var targets []NodeID
		for _, r := range refs {
			if !r.Resolved {
				t.Errorf("unresolved reference %+v", r)
			}
			targets = append(targets, r.To)
		}
		want := []NodeID{ItemNode("LNK-EP-0001"), ItemNode("LNK-US-0001")}
		if !reflect.DeepEqual(targets, want) {
			t.Errorf("targets = %v, want %v", targets, want)
		}
		for _, r := range refs {
			if r.To == ItemNode("LNK-US-0001") && r.Text != "the first story" {
				t.Errorf("alias = %q", r.Text)
			}
		}
	})

	t.Run("item body to page keeps the anchor", func(t *testing.T) {
		refs := g.References(ItemNode("LNK-US-0001"))
		found := false
		for _, r := range refs {
			if r.To == PageNode("docs/architecture/platform.md") {
				found = true
				if r.Anchor != "Scope" {
					t.Errorf("anchor = %q", r.Anchor)
				}
			}
		}
		if !found {
			t.Errorf("references = %+v", refs)
		}
	})

	t.Run("backlinks", func(t *testing.T) {
		back := g.Backlinks(ItemNode("LNK-EP-0001"))
		if len(back) != 1 || back[0].From != PageNode("docs/architecture/platform.md") {
			t.Errorf("backlinks = %+v", back)
		}
		pageBack := g.Backlinks(PageNode("docs/architecture/platform.md"))
		if len(pageBack) != 2 {
			t.Errorf("page backlinks = %+v", pageBack)
		}
	})

	t.Run("links inside code fences are ignored", func(t *testing.T) {
		for _, n := range g.Nodes() {
			for _, r := range g.References(n) {
				if r.Target == "NotALink" {
					t.Errorf("a link inside a code fence was indexed: %+v", r)
				}
			}
		}
	})

	t.Run("a broken link is a warning", func(t *testing.T) {
		found := false
		for _, d := range ix.Warnings() {
			if d.Code == idxCodeLinkBroken && d.Path == "docs/.pmngr/stories/LNK-US-0003-third.md" {
				found = true
				if d.Severity != SeverityWarning {
					t.Errorf("severity = %q", d.Severity)
				}
			}
		}
		if !found {
			t.Errorf("no %s diagnostic in %v", idxCodeLinkBroken, ix.Warnings())
		}
	})

	t.Run("a dangling milestone is a warning", func(t *testing.T) {
		found := false
		for _, d := range ix.Warnings() {
			if d.Code == idxCodeRefDangling && d.Field == "milestone" {
				found = true
			}
		}
		if !found {
			t.Errorf("no %s diagnostic", idxCodeRefDangling)
		}
	})
}

func TestGraphAmbiguousWikilink(t *testing.T) {
	m := NewMemFSFromMap(map[string]string{
		"docs/.pmngr/project.yaml": "schema: 1\nkey: AMB\nworkflow:\n  statuses:\n    - {id: todo, category: todo}\n    - {id: done, category: done}\n",
		"docs/one/overview.md":     "# One\n",
		"docs/two/overview.md":     "# Two\n",
		"docs/index.md":            "# Index\n\nSee [[overview]].\n",
	})
	projects, err := DiscoverProjects(m, ".")
	if err != nil {
		t.Fatalf("DiscoverProjects: %v", err)
	}
	ix := NewIndex(m, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := false
	for _, d := range ix.Warnings() {
		if d.Code == idxCodeLinkAmbiguous {
			found = true
			if d.Path != "docs/index.md" {
				t.Errorf("path = %q", d.Path)
			}
		}
	}
	if !found {
		t.Errorf("no %s diagnostic in %v", idxCodeLinkAmbiguous, ix.Warnings())
	}
}

func TestGraphRebuiltOnIncrementalUpdate(t *testing.T) {
	ix, m := buildFixtureIndex(t)
	const story = "docs/.pmngr/stories/DEMO-US-0002-save-payment-methods.md"
	data, err := m.ReadFile(story)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	updated := string(data)
	updated = replaceOnce(t, updated,
		"  - { kind: relates_to, target: DEMO-US-0001 }",
		"  - { kind: blocked_by, target: DEMO-US-0001 }")
	if err := m.WriteFile(story, []byte(updated)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ix.ApplyFileEvents(context.Background(), []FileEvent{{Kind: FileModified, Path: story}}); err != nil {
		t.Fatalf("ApplyFileEvents: %v", err)
	}
	g := ix.LinkGraph()
	if got := g.Blocking("DEMO-US-0001"); !reflect.DeepEqual(got, []ItemID{"DEMO-US-0002"}) {
		t.Errorf("blocking = %v, want the recomputed inverse", got)
	}
	for _, l := range g.InverseLinks("DEMO-US-0001") {
		if l.Kind == LinkRelatesTo && l.To == "DEMO-US-0002" {
			t.Error("the old inverse edge survived the update")
		}
	}
}

func replaceOnce(t *testing.T, s, old, replacement string) string {
	t.Helper()
	i := indexOf(s, old)
	if i < 0 {
		t.Fatalf("%q not found", old)
	}
	return s[:i] + replacement + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
