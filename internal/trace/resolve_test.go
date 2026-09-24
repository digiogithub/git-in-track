package trace

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

// specIndex builds an index over one project holding one spec with blocks R1
// and R2, and a story.
func specIndex(t *testing.T) *core.Index {
	t.Helper()
	spec := "---\nid: ACME-SP-0001\ntype: spec\ntitle: Allocation\nstatus: todo\n" +
		"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n" +
		"requirements:\n  R1:\n    status: todo\n  R2:\n    status: todo\n---\n\n" +
		"## Requirements\n\n### ACME-SP-0001.R1 — One\n\nThe system SHALL do one.\n\n" +
		"### ACME-SP-0001.R2 — Two\n\nThe system SHALL do two.\n"
	story := "---\nid: ACME-US-0001\ntype: story\ntitle: A story\nstatus: todo\n" +
		"created: 2026-01-01T00:00:00Z\nupdated: 2026-01-01T00:00:00Z\n---\n"
	fsys := core.NewMemFSFromMap(map[string]string{
		"docs/.pmngr/project.yaml": "schema: 2\nkey: ACME\nname: Acme\nworkflow:\n  statuses:\n" +
			"    - {id: todo, category: todo}\n    - {id: done, category: done}\n",
		"docs/.pmngr/specs/ACME-SP-0001-allocation.md": spec,
		"docs/.pmngr/stories/ACME-US-0001-a-story.md":  story,
	})
	projects, err := core.DiscoverProjects(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	ix := core.NewIndex(fsys, projects)
	if _, err := ix.Build(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestDangling(t *testing.T) {
	t.Parallel()
	ix := specIndex(t)
	src := "package a\n\n" +
		"// Implements: ACME-SP-0001.R1, ACME/ACME-SP-0001.R2\n" + // both resolve
		"// Verifies: ACME-SP-0001.R3\n" + // missing block
		"// Implements: ACME-SP-0009.R1\n" + // missing spec
		"// Implements: ACME-SP-0001.R1\n" +
		"// Verifies: ACME-US-0001.R1\n" + // not a spec: malformed, never dangling
		"func F() {}\n"
	r := ScanFile("a.go", []byte(src))
	var got []string
	for _, f := range Dangling(r.Markers, IndexResolver(ix)) {
		got = append(got, f.Path+":"+strconv.Itoa(f.Line)+" "+string(f.Code)+" "+f.Message)
	}
	want := []string{
		"a.go:4 W-MARKER-DANGLING verifies marker names ACME-SP-0001.R3: spec ACME-SP-0001 has no requirement R3",
		"a.go:5 W-MARKER-DANGLING implements marker names ACME-SP-0009.R1: no spec ACME-SP-0009",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Dangling() = %q\nwant %q", got, want)
	}
	if len(r.Findings) != 1 || r.Findings[0].Line != 7 {
		t.Errorf("Findings = %+v, want one W-MARKER-SYNTAX on line 7", r.Findings)
	}
}

func TestResolverFuncAndCacheDangling(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, map[string]string{"x.py": "# Verifies: WEB/WEB-SP-0002.R1\n"})
	c, err := Scan(context.Background(), root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var seen core.ProjectKey
	r := ResolverFunc(func(p core.ProjectKey, _ core.RequirementRef) Resolution {
		seen = p
		return MissingBlock
	})
	d := c.Dangling(r)
	if len(d) != 1 || d[0].Path != "x.py" || seen != "WEB" {
		t.Errorf("Dangling() = %+v, qualifier seen %q", d, seen)
	}
	if d := c.Dangling(ResolverFunc(func(core.ProjectKey, core.RequirementRef) Resolution { return Resolved })); d != nil {
		t.Errorf("Dangling() with everything resolved = %+v", d)
	}
}
