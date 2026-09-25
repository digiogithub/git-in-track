package core

import (
	"strings"
	"testing"
)

func TestLocateRequirement(t *testing.T) {
	t.Parallel()
	const spec = ItemID("ACME-SP-0003")
	body := "## Purpose\n\nWhy tokens rotate at all.\n\n## Requirements\n\n" +
		"### ACME-SP-0003.R1 — Rotate refresh tokens\n\n" +
		"The system SHALL rotate a refresh token on every use.\n\n" +
		"#### Scenario: reuse\n\n- WHEN a used token is presented\n- THEN the session is revoked\n\n" +
		"### ACME-SP-0003.R2 — Expire idle sessions\n\n" +
		"The system SHALL expire a session idle for thirty days.\n"
	long := strings.Repeat("filler words that are not in the body at all ", 3)

	tests := []struct {
		name     string
		fragment string
		want     string // ref, or "" for no match
		clip     string // text expected inside the clipped range
	}{
		{name: "a chunk inside one block", fragment: "rotate a refresh token on every use", want: "ACME-SP-0003.R1", clip: "every use"},
		{name: "re-flowed white space still matches", fragment: "rotate  a\nrefresh\ttoken", want: "ACME-SP-0003.R1", clip: "refresh"},
		{name: "a chunk spanning two blocks goes to the larger overlap",
			fragment: "the session is revoked\n\n### ACME-SP-0003.R2 — Expire idle sessions\n\nThe system SHALL expire a session idle for thirty days.",
			want:     "ACME-SP-0003.R2", clip: "thirty days"},
		{name: "the introduction belongs to no block", fragment: "Why tokens rotate at all.", want: ""},
		{name: "a fragment not in the body belongs to no block", fragment: "nothing like this", want: ""},
		{name: "an empty fragment belongs to no block", fragment: "  \n", want: ""},
		{name: "a stale head still locates by its tail",
			fragment: long + "### ACME-SP-0003.R2 — Expire idle sessions\n\nThe system SHALL expire a session idle for thirty days.",
			want:     "ACME-SP-0003.R2", clip: "thirty days"},
		{name: "a stale tail still locates by its head",
			fragment: "The system SHALL rotate a refresh token on every use.\n\n#### Scenario: reuse\n\n- WHEN a used token is presented " + long,
			want:     "ACME-SP-0003.R1", clip: "rotate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := LocateRequirement(spec, body, tt.fragment)
			if tt.want == "" {
				if ok {
					t.Fatalf("LocateRequirement = %s, want no match", got.Block.Ref)
				}
				return
			}
			if !ok {
				t.Fatalf("LocateRequirement found nothing, want %s", tt.want)
			}
			if got.Block.Ref.String() != tt.want {
				t.Errorf("ref = %s, want %s", got.Block.Ref, tt.want)
			}
			if got.Start < got.Block.Start || got.End > got.Block.End || got.Start >= got.End {
				t.Errorf("clip [%d,%d) outside block [%d,%d)", got.Start, got.End, got.Block.Start, got.Block.End)
			}
			if clipped := body[got.Start:got.End]; !strings.Contains(clipped, tt.clip) {
				t.Errorf("clipped text %q does not contain %q", clipped, tt.clip)
			}
		})
	}
}
