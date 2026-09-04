package core

import (
	"strings"
	"testing"
)

// storyFile builds an item file with the given front matter lines and body.
func storyFile(front, body string) string {
	return "---\n" + front + "---\n\n" + body + "\n"
}

func TestMergeFilesFrontMatter(t *testing.T) {
	base := storyFile(`id: GIT-US-0042
type: story
title: Log in with SSO
status: todo
assignees: [ana]
labels: [auth, web]
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
`, "## Description\n\nA body nobody touched.")

	tests := []struct {
		name   string
		ours   string
		theirs string
		check  func(t *testing.T, res MergeResult)
	}{
		{
			name: "labels from both sides are kept and a deletion is honored",
			ours: storyFile(`id: GIT-US-0042
type: story
title: Log in with SSO
status: todo
assignees: [ana]
labels: [auth, web, urgent]
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-02-01T00:00:00Z
`, "## Description\n\nA body nobody touched."),
			theirs: storyFile(`id: GIT-US-0042
type: story
title: Log in with SSO
status: todo
assignees: [ana, bob]
labels: [auth, sso]
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-01-15T00:00:00Z
`, "## Description\n\nA body nobody touched."),
			check: func(t *testing.T, res MergeResult) {
				t.Helper()
				if !res.Structured {
					t.Fatalf("want a structured merge, got a text merge")
				}
				if !strings.Contains(res.Content, "labels: [auth, sso, urgent]") {
					t.Errorf("labels were not unioned:\n%s", res.Content)
				}
				if !strings.Contains(res.Content, "assignees: [ana, bob]") {
					t.Errorf("an assignee was lost:\n%s", res.Content)
				}
				if got := decisionFor(res, "labels"); got == nil || got.Kind != FieldSet {
					t.Errorf("the labels decision was not reported: %+v", res.Fields)
				}
			},
		},
		{
			name: "a scalar only one side changed is taken without review",
			ours: storyFile(`id: GIT-US-0042
type: story
title: Log in with SSO
status: in_progress
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-02-01T00:00:00Z
`, "## Description\n\nA body nobody touched."),
			theirs: base,
			check: func(t *testing.T, res MergeResult) {
				t.Helper()
				d := decisionFor(res, "status")
				if d == nil || d.Choice != SideOurs || d.Review {
					t.Fatalf("want status taken from ours with no review, got %+v", d)
				}
				if !strings.Contains(res.Content, "status: in_progress") {
					t.Errorf("status was not kept:\n%s", res.Content)
				}
			},
		},
		{
			name: "both sides changed a scalar so the newest updated wins and is reviewable",
			ours: storyFile(`id: GIT-US-0042
type: story
title: Log in with SSO
status: in_progress
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-02-01T00:00:00Z
`, "## Description\n\nA body nobody touched."),
			theirs: storyFile(`id: GIT-US-0042
type: story
title: Log in with SSO
status: done
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-03-01T00:00:00Z
`, "## Description\n\nA body nobody touched."),
			check: func(t *testing.T, res MergeResult) {
				t.Helper()
				d := decisionFor(res, "status")
				if d == nil || d.Choice != SideTheirs || !d.Review {
					t.Fatalf("want status from theirs marked for review, got %+v", d)
				}
				if res.Review == 0 {
					t.Errorf("the reviewable decision was not counted: %+v", res)
				}
				if !strings.Contains(res.Content, "updated: 2026-03-01T00:00:00Z") {
					t.Errorf("updated is not the newest of the two:\n%s", res.Content)
				}
			},
		},
		{
			name: "an immutable field that differs is an anomaly, and base wins",
			ours: storyFile(`id: GIT-US-0042
type: story
title: Log in with SSO
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-02-01T00:00:00Z
`, "## Description\n\nA body nobody touched."),
			theirs: storyFile(`id: GIT-US-0043
type: story
title: Log in with SSO
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-02-02T00:00:00Z
`, "## Description\n\nA body nobody touched."),
			check: func(t *testing.T, res MergeResult) {
				t.Helper()
				d := decisionFor(res, "id")
				if d == nil || d.Choice != SideBase || !d.Review {
					t.Fatalf("want the base id kept and flagged, got %+v", d)
				}
				if !strings.Contains(res.Content, "id: GIT-US-0042") {
					t.Errorf("the base id was not kept:\n%s", res.Content)
				}
				if len(res.Warnings) == 0 {
					t.Errorf("an immutable difference produced no warning")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := MergeFiles("docs/.pmngr/stories/GIT-US-0042-log-in-with-sso.md",
				MergeInput{Base: base, Ours: tc.ours, Theirs: tc.theirs}, nil)
			if err != nil {
				t.Fatalf("MergeFiles: %v", err)
			}
			tc.check(t, res)
		})
	}
}

func TestMergeFilesBody(t *testing.T) {
	front := `id: GIT-US-0042
type: story
title: Log in with SSO
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
`
	tests := []struct {
		name                 string
		base, ours, theirs   string
		wantConflicted       int
		wantContentContains  []string
		wantContentExcluding []string
	}{
		{
			name:                "a change on one side only is taken",
			base:                "## Description\n\nOne.\n\n## Notes\n\nNothing yet.",
			ours:                "## Description\n\nOne.\n\n## Notes\n\nNothing yet.",
			theirs:              "## Description\n\nTwo.\n\n## Notes\n\nNothing yet.",
			wantConflicted:      0,
			wantContentContains: []string{"Two."},
		},
		{
			name:                "disjoint edits in different sections both survive",
			base:                "## Description\n\nOne.\n\n## Notes\n\nNothing yet.",
			ours:                "## Description\n\nOne and a half.\n\n## Notes\n\nNothing yet.",
			theirs:              "## Description\n\nOne.\n\n## Notes\n\nSomething.",
			wantConflicted:      0,
			wantContentContains: []string{"One and a half.", "Something."},
		},
		{
			name:                "a checkbox-only difference resolves to checked",
			base:                "## Acceptance Criteria\n\n- [ ] One\n- [ ] Two",
			ours:                "## Acceptance Criteria\n\n- [x] One\n- [ ] Two",
			theirs:              "## Acceptance Criteria\n\n- [ ] One\n- [x] Two",
			wantConflicted:      0,
			wantContentContains: []string{"- [x] One", "- [x] Two"},
		},
		{
			name:                 "the same region changed on both sides is a conflict",
			base:                 "## Description\n\nOne.",
			ours:                 "## Description\n\nMine.",
			theirs:               "## Description\n\nTheirs.",
			wantConflicted:       1,
			wantContentContains:  []string{"Mine."},
			wantContentExcluding: []string{"<<<<<<<", ">>>>>>>"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := MergeFiles("docs/.pmngr/stories/GIT-US-0042-log-in-with-sso.md", MergeInput{
				Base:   storyFile(front, tc.base),
				Ours:   storyFile(front, tc.ours),
				Theirs: storyFile(front, tc.theirs),
			}, nil)
			if err != nil {
				t.Fatalf("MergeFiles: %v", err)
			}
			if res.Conflicted != tc.wantConflicted {
				t.Fatalf("conflicted hunks = %d, want %d (hunks: %+v)", res.Conflicted, tc.wantConflicted, res.Hunks)
			}
			for _, want := range tc.wantContentContains {
				if !strings.Contains(res.Content, want) {
					t.Errorf("content is missing %q:\n%s", want, res.Content)
				}
			}
			for _, unwanted := range tc.wantContentExcluding {
				if strings.Contains(res.Content, unwanted) {
					t.Errorf("content carries %q, which the resolver must never emit:\n%s", unwanted, res.Content)
				}
			}
		})
	}
}

func TestMergeFilesResolution(t *testing.T) {
	front := `id: GIT-US-0042
type: story
title: Log in with SSO
status: todo
author: ana
created: 2026-01-01T00:00:00Z
updated: 2026-01-01T00:00:00Z
`
	ourFront := strings.Replace(front, "status: todo", "status: in_progress", 1)
	theirFront := strings.Replace(front, "status: todo", "status: done", 1)
	in := MergeInput{
		Base:   storyFile(front, "## Description\n\nOne."),
		Ours:   storyFile(ourFront, "## Description\n\nMine."),
		Theirs: storyFile(theirFront, "## Description\n\nTheirs."),
	}
	path := "docs/.pmngr/stories/GIT-US-0042-log-in-with-sso.md"

	tests := []struct {
		name     string
		res      Resolution
		contains []string
		clean    bool
	}{
		{
			name:     "keep mine takes the whole side verbatim",
			res:      Resolution{Take: SideOurs},
			contains: []string{"status: in_progress", "Mine."},
			clean:    true,
		},
		{
			name:     "keep theirs takes the whole side verbatim",
			res:      Resolution{Take: SideTheirs},
			contains: []string{"status: done", "Theirs."},
			clean:    true,
		},
		{
			name: "a hunk taken from both keeps the two texts",
			res: Resolution{
				Hunks:  map[string]string{"0": SideBoth},
				Fields: map[string]string{"status": SideOurs},
			},
			contains: []string{"status: in_progress", "Mine.", "Theirs."},
			clean:    true,
		},
		{
			name:     "an edited hunk lands verbatim",
			res:      Resolution{Hunks: map[string]string{"0": SideEdited}, HunkText: map[string]string{"0": "Ours and theirs, rewritten."}},
			contains: []string{"Ours and theirs, rewritten."},
			clean:    true,
		},
		{
			name:     "a whole-file edit is written as given, canonicalised",
			res:      Resolution{Content: storyFile(front, "## Description\n\nHand written.")},
			contains: []string{"Hand written."},
			clean:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := MergeFiles(path, in, &tc.res)
			if err != nil {
				t.Fatalf("MergeFiles: %v", err)
			}
			if res.Clean != tc.clean {
				t.Errorf("clean = %v, want %v", res.Clean, tc.clean)
			}
			for _, want := range tc.contains {
				if !strings.Contains(res.Content, want) {
					t.Errorf("content is missing %q:\n%s", want, res.Content)
				}
			}
			if !strings.HasSuffix(res.Content, "\n") {
				t.Errorf("the merged file does not end with a newline")
			}
		})
	}
}

func TestMergeFilesBoardOrder(t *testing.T) {
	board := func(order string) string {
		return "---\ntype: board\nid: platform-kanban\ntitle: Platform\nkind: kanban\n" +
			"columns:\n  - id: todo\n    categories: [todo]\n  - id: doing\n    categories: [in_progress]\n" +
			"order:\n" + order + "---\n\nBoard body.\n"
	}
	base := board("  todo:\n    - GIT/GIT-US-0001\n    - GIT/GIT-US-0002\n")
	ours := board("  todo:\n    - GIT/GIT-US-0001\n    - GIT/GIT-US-0009\n    - GIT/GIT-US-0002\n")
	theirs := board("  todo:\n    - GIT/GIT-US-0001\n    - GIT/GIT-US-0002\n    - GIT/GIT-US-0007\n")

	res, err := MergeFiles("docs/.pmngr/boards/platform-kanban.md",
		MergeInput{Base: base, Ours: ours, Theirs: theirs}, nil)
	if err != nil {
		t.Fatalf("MergeFiles: %v", err)
	}
	for _, ref := range []string{"GIT-US-0001", "GIT-US-0002", "GIT-US-0007", "GIT-US-0009"} {
		if !strings.Contains(res.Content, ref) {
			t.Errorf("the order merge lost %s:\n%s", ref, res.Content)
		}
	}
	if res.Conflicted != 0 {
		t.Errorf("an order merge must not leave a conflict: %+v", res.Hunks)
	}
}

func TestMergeFilesPlainText(t *testing.T) {
	res, err := MergeFiles("docs/guide.md", MergeInput{
		Base:   "# Guide\n\nOne.\n",
		Ours:   "# Guide\n\nMine.\n",
		Theirs: "# Guide\n\nTheirs.\n",
	}, nil)
	if err != nil {
		t.Fatalf("MergeFiles: %v", err)
	}
	if res.Structured {
		t.Errorf("a page with no front matter must not be merged as an item")
	}
	if res.Conflicted != 1 {
		t.Fatalf("want one conflicted hunk, got %d", res.Conflicted)
	}
	if res.Hunks[0].Ours != "Mine." || res.Hunks[0].Theirs != "Theirs." {
		t.Errorf("the hunk does not carry both sides: %+v", res.Hunks[0])
	}
}

func TestDiff3Regions(t *testing.T) {
	tests := []struct {
		name               string
		base, ours, theirs []string
		want               []TextRegion
	}{
		{
			name: "no change at all is one stable region",
			base: []string{"a", "b"}, ours: []string{"a", "b"}, theirs: []string{"a", "b"},
			want: []TextRegion{{Stable: true, Lines: []string{"a", "b"}}},
		},
		{
			name: "an insertion on one side is unstable and carries all three",
			base: []string{"a", "c"}, ours: []string{"a", "b", "c"}, theirs: []string{"a", "c"},
			want: []TextRegion{
				{Stable: true, Lines: []string{"a"}},
				{Ours: []string{"b"}},
				{Stable: true, Lines: []string{"c"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Diff3(tc.base, tc.ours, tc.theirs)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d regions, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i].Stable != tc.want[i].Stable ||
					joinLines(got[i].Lines) != joinLines(tc.want[i].Lines) ||
					joinLines(got[i].Ours) != joinLines(tc.want[i].Ours) ||
					joinLines(got[i].Theirs) != joinLines(tc.want[i].Theirs) {
					t.Errorf("region %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// decisionFor finds one field decision in a result.
func decisionFor(res MergeResult, field string) *FieldDecision {
	for i := range res.Fields {
		if res.Fields[i].Field == field {
			return &res.Fields[i]
		}
	}
	return nil
}
