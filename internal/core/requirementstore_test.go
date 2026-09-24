package core

import "testing"

func TestInsertRequirementBlock(t *testing.T) {
	block := "### XY-SP-0001.R2 — New\n\nThe system SHALL do it.\n"
	tests := []struct {
		name, body, want string
	}{
		{
			name: "after the last block, before the next section",
			body: "## Requirements\n\n### XY-SP-0001.R1 — Old\n\nThe system SHALL.\n\n## Notes\n\nN.\n",
			want: "## Requirements\n\n### XY-SP-0001.R1 — Old\n\nThe system SHALL.\n\n" + block + "\n## Notes\n\nN.\n",
		},
		{
			name: "last block at the end of the body without a newline",
			body: "## Requirements\n\n### XY-SP-0001.R1 — Old\n\nThe system SHALL.",
			want: "## Requirements\n\n### XY-SP-0001.R1 — Old\n\nThe system SHALL.\n\n" + block,
		},
		{
			name: "empty requirements section",
			body: "## Purpose\n\nP.\n\n## Requirements\n\n## Notes\n",
			want: "## Purpose\n\nP.\n\n## Requirements\n\n" + block + "\n## Notes\n",
		},
		{
			name: "no requirements section",
			body: "## Purpose\n\nP.\n",
			want: "## Purpose\n\nP.\n\n## Requirements\n\n" + block,
		},
		{
			name: "empty body",
			body: "",
			want: "## Requirements\n\n" + block,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := insertRequirementBlock(tc.body, "XY-SP-0001", block); got != tc.want {
				t.Errorf("got\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestApplyRequirementPatchKeepsOtherBytes(t *testing.T) {
	body := "## Requirements\n\n### XY-SP-0001.R1 - Old title\nThe system SHALL keep this spacing.\n\n\n### XY-SP-0001.R2 — Other\n\nThe system SHALL.\n"
	cfg := &ProjectConfig{Workflow: Workflow{Initial: "todo"}}
	tests := []struct {
		name  string
		patch RequirementPatch
		want  string
	}{
		{
			name:  "status only leaves the body alone",
			patch: RequirementPatch{Status: refOf(Status("todo"))},
			want:  body,
		},
		{
			name:  "title only rewrites the heading line",
			patch: RequirementPatch{Title: refOf("New title")},
			want:  "## Requirements\n\n### XY-SP-0001.R1 — New title\nThe system SHALL keep this spacing.\n\n\n### XY-SP-0001.R2 — Other\n\nThe system SHALL.\n",
		},
		{
			name:  "text keeps the heading and the separation after the block",
			patch: RequirementPatch{Text: refOf("\n\nThe system SHALL change.\n\n")},
			want:  "## Requirements\n\n### XY-SP-0001.R1 - Old title\n\nThe system SHALL change.\n\n\n### XY-SP-0001.R2 — Other\n\nThe system SHALL.\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			it := &Item{ID: "XY-SP-0001", Type: TypeSpec, Body: body}
			r2, err := FindRequirement(it, 2, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := applyRequirementPatch(it, 1, tc.patch, cfg); err != nil {
				t.Fatal(err)
			}
			if it.Body != tc.want {
				t.Errorf("body\n%q\nwant\n%q", it.Body, tc.want)
			}
			if got := it.Requirements["R1"]; got == nil || got.Status != "todo" {
				t.Errorf("status not materialized: %+v", got)
			}
			after, err := FindRequirement(it, 2, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if after.BlockRev != r2.BlockRev {
				t.Errorf("R2 block rev moved")
			}
		})
	}
}

// refOf returns a pointer to a value, for the sparse patch fields.
func refOf[T any](v T) *T { return &v }
