package mapping

import (
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// link builds one (linkType, direction) entry.
func link(name, direction string, aggregation bool, ids ...string) youtrack.IssueLink {
	refs := make([]youtrack.IssueRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, youtrack.IssueRef{IDReadable: id})
	}
	return youtrack.IssueLink{
		Direction: direction,
		LinkType:  youtrack.LinkType{Name: name, Aggregation: aggregation},
		Issues:    refs,
	}
}

func TestMapLinks(t *testing.T) {
	cases := []struct {
		name         string
		links        []youtrack.IssueLink
		wantParent   string
		wantChildren []string
		wantLinks    []core.Link
		wantWarnings int
	}{
		{
			name:  "the empty pairs are filtered before anything else",
			links: []youtrack.IssueLink{link("Subtask", youtrack.DirectionOutward, true), link("Relates", youtrack.DirectionBoth, false), link("Depend", youtrack.DirectionInward, false)},
		},
		{
			name:         "subtask outward names the children",
			links:        []youtrack.IssueLink{link("Subtask", youtrack.DirectionOutward, true, "ACME-43", "ACME-42")},
			wantChildren: []string{"ACME-42", "ACME-43"},
		},
		{
			name:       "subtask inward names the parent",
			links:      []youtrack.IssueLink{link("Subtask", youtrack.DirectionInward, true, "ACME-30")},
			wantParent: "ACME-30",
		},
		{
			name:       "an aggregation type keeps forming the hierarchy under another name",
			links:      []youtrack.IssueLink{link("Subtarea", youtrack.DirectionInward, true, "ACME-30")},
			wantParent: "ACME-30",
		},
		{
			name:         "a second parent is reported, not taken",
			links:        []youtrack.IssueLink{link("Subtask", youtrack.DirectionInward, true, "ACME-30", "ACME-31")},
			wantParent:   "ACME-30",
			wantWarnings: 1,
		},
		{
			name:         "a hierarchy link with direction BOTH is reported",
			links:        []youtrack.IssueLink{link("Subtask", youtrack.DirectionBoth, true, "ACME-30")},
			wantWarnings: 1,
		},
		{
			name:      "relates is undirected",
			links:     []youtrack.IssueLink{link("Relates", youtrack.DirectionBoth, false, "ACME-47")},
			wantLinks: []core.Link{{Kind: core.LinkRelatesTo, Target: "ACME-47"}},
		},
		{
			name:      "depend outward blocks and inward is blocked by",
			links:     []youtrack.IssueLink{link("Depend", youtrack.DirectionOutward, false, "ACME-51"), link("Depend", youtrack.DirectionInward, false, "ACME-52")},
			wantLinks: []core.Link{{Kind: core.LinkBlockedBy, Target: "ACME-52"}, {Kind: core.LinkBlocks, Target: "ACME-51"}},
		},
		{
			name:      "duplicate outward duplicates and inward is duplicated by",
			links:     []youtrack.IssueLink{link("Duplicate", youtrack.DirectionOutward, false, "ACME-55"), link("Duplicate", youtrack.DirectionInward, false, "ACME-56")},
			wantLinks: []core.Link{{Kind: core.LinkDuplicatedBy, Target: "ACME-56"}, {Kind: core.LinkDuplicates, Target: "ACME-55"}},
		},
		{
			name:         "an unsupported link type is reported, never invented",
			links:        []youtrack.IssueLink{link("Mentions", youtrack.DirectionBoth, false, "ACME-58")},
			wantWarnings: 1,
		},
		{
			name:      "a duplicate entry is emitted once",
			links:     []youtrack.IssueLink{link("Relates", youtrack.DirectionBoth, false, "ACME-47"), link("Relates", youtrack.DirectionBoth, false, "ACME-47")},
			wantLinks: []core.Link{{Kind: core.LinkRelatesTo, Target: "ACME-47"}},
		},
		{
			name:  "a self-reference is dropped",
			links: []youtrack.IssueLink{link("Relates", youtrack.DirectionBoth, false, "ACME-42")},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := youtrack.Issue{IDReadable: "ACME-42", Links: tc.links}
			parent, children, links, warnings := MapLinks(issue)
			if parent != tc.wantParent {
				t.Errorf("got parent %q, want %q", parent, tc.wantParent)
			}
			if !equalJSON(t, children, tc.wantChildren) {
				t.Errorf("got children %v, want %v", children, tc.wantChildren)
			}
			if !equalJSON(t, links, tc.wantLinks) {
				t.Errorf("got links %v, want %v", links, tc.wantLinks)
			}
			if len(warnings) != tc.wantWarnings {
				t.Errorf("got warnings %v, want %d", warningLines(warnings), tc.wantWarnings)
			}
			for _, l := range links {
				if !l.Kind.Valid() {
					t.Errorf("emitted the invalid link kind %q", l.Kind)
				}
			}
		})
	}
}

// TestMapLinksFixtureShape runs the real multi-entry shape from the YouTrack
// API reference, section 9, where most entries carry an empty issues array.
func TestMapLinksFixtureShape(t *testing.T) {
	issue := loadIssue(t, "story.json")
	if len(issue.Links) <= len(youtrack.NonEmptyLinks(issue.Links)) {
		t.Fatal("the fixture must contain at least one empty link entry")
	}
	parent, children, links, warnings := MapLinks(issue)
	if parent != "ACME-30" {
		t.Errorf("got parent %q, want ACME-30", parent)
	}
	if !equalJSON(t, children, []string{"ACME-60"}) {
		t.Errorf("got children %v", children)
	}
	want := []core.Link{
		{Kind: core.LinkBlockedBy, Target: "ACME-52"},
		{Kind: core.LinkBlocks, Target: "ACME-51"},
		{Kind: core.LinkDuplicatedBy, Target: "ACME-55"},
		{Kind: core.LinkRelatesTo, Target: "ACME-47"},
	}
	if !equalJSON(t, links, want) {
		t.Errorf("got links %v, want %v", links, want)
	}
	if len(warnings) != 1 || warnings[0].Value != "Mentions" {
		t.Errorf("got warnings %v, want one about Mentions", warningLines(warnings))
	}
}
