package core

import (
	"strings"
	"testing"
)

// bomString is the UTF-8 byte order mark, which a Windows editor may prepend.
const bomString = "\xef\xbb\xbf"

func TestComputeRev(t *testing.T) {
	t.Parallel()

	const canonical = "---\nid: ACME-US-0042\n---\n\nBody.\n"

	tests := []struct {
		name string
		in   string
		same bool // same rev as the canonical form above
	}{
		{name: "canonical bytes", in: canonical, same: true},
		{name: "crlf line endings", in: strings.ReplaceAll(canonical, "\n", "\r\n"), same: true},
		{name: "utf-8 bom", in: bomString + canonical, same: true},
		{name: "missing trailing newline", in: strings.TrimSuffix(canonical, "\n"), same: true},
		{name: "extra trailing newlines", in: canonical + "\n\n\n", same: true},
		{name: "bom and crlf", in: bomString + strings.ReplaceAll(canonical, "\n", "\r\n"), same: true},
		{name: "body edited", in: "---\nid: ACME-US-0042\n---\n\nOther body.\n", same: false},
		{name: "front matter edited", in: "---\nid: ACME-US-0043\n---\n\nBody.\n", same: false},
		{name: "trailing space added", in: "---\nid: ACME-US-0042\n---\n\nBody. \n", same: false},
	}

	want := ComputeRev([]byte(canonical))
	if !want.Valid() {
		t.Fatalf("ComputeRev produced an invalid rev %q", want)
	}
	if !strings.HasPrefix(string(want), "sha256:") || len(string(want)) != len("sha256:")+16 {
		t.Fatalf("rev %q is not %q + 16 hex characters", want, "sha256:")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeRev([]byte(tt.in))
			if tt.same && got != want {
				t.Errorf("ComputeRev() = %q, want %q", got, want)
			}
			if !tt.same && got == want {
				t.Errorf("ComputeRev() = %q, want a different rev", got)
			}
		})
	}
}

func TestComputeRevIsStable(t *testing.T) {
	t.Parallel()

	// A hard-coded expectation guards the algorithm against accidental change:
	// every client that computes a different value would report bogus conflicts.
	const input = "---\nid: ACME-US-0042\ntype: story\n---\n"
	const want = "sha256:749bd863b65603a4"
	if got := ComputeRev([]byte(input)); string(got) != want {
		t.Errorf("ComputeRev() = %q, want %q (update this only with a documented reason)", got, want)
	}
}

func TestCanonicalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "adds a trailing newline", in: "a", want: "a\n"},
		{name: "collapses trailing newlines", in: "a\n\n\n", want: "a\n"},
		{name: "converts crlf", in: "a\r\nb\r\n", want: "a\nb\n"},
		{name: "strips the bom", in: bomString + "a\n", want: "a\n"},
		{name: "empty input", in: "", want: "\n"},
		{name: "keeps inner blank lines", in: "a\n\nb\n", want: "a\n\nb\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(Canonicalize([]byte(tt.in))); got != tt.want {
				t.Errorf("Canonicalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRevValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rev  Rev
		want bool
	}{
		{name: "computed", rev: ComputeRev([]byte("x")), want: true},
		{name: "empty", rev: "", want: false},
		{name: "no prefix", rev: "9f2b1c7d0a4e5b31", want: false},
		{name: "too short", rev: "sha256:9f2b1c7d", want: false},
		{name: "not hex", rev: "sha256:zzzzzzzzzzzzzzzz", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.rev.Valid(); got != tt.want {
				t.Errorf("Rev(%q).Valid() = %t, want %t", tt.rev, got, tt.want)
			}
		})
	}
}
