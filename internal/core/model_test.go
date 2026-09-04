package core

import (
	"encoding/json"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestParseTimestamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "canonical", in: "2026-09-01T10:45:12Z", want: "2026-09-01T10:45:12Z"},
		{name: "offset is normalised to utc", in: "2026-09-01T12:45:12+02:00", want: "2026-09-01T10:45:12Z"},
		{name: "fractional seconds are truncated", in: "2026-09-01T10:45:12.512Z", want: "2026-09-01T10:45:12Z"},
		{name: "date only", in: "2026-09-01", want: "2026-09-01T00:00:00Z"},
		{name: "surrounding spaces", in: "  2026-09-01T10:45:12Z ", want: "2026-09-01T10:45:12Z"},
		{name: "not a timestamp", in: "01/09/2026", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTimestamp(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTimestamp(%q) = %q, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTimestamp(%q): %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("ParseTimestamp(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTimestampYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	type doc struct {
		Created Timestamp `yaml:"created"`
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "yaml timestamp scalar", in: "created: 2026-09-01T10:45:12Z\n", want: "2026-09-01T10:45:12Z"},
		{name: "quoted string", in: "created: \"2026-09-01T10:45:12Z\"\n", want: "2026-09-01T10:45:12Z"},
		{name: "single quoted string", in: "created: '2026-09-01T10:45:12Z'\n", want: "2026-09-01T10:45:12Z"},
		{name: "absent", in: "created: null\n", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var d doc
			if err := yaml.Unmarshal([]byte(tt.in), &d); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := d.Created.String(); got != tt.want {
				t.Fatalf("created = %q, want %q", got, tt.want)
			}
			if tt.want == "" {
				return
			}
			out, err := yaml.Marshal(d)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if want := "created: " + tt.want + "\n"; string(out) != want {
				t.Errorf("marshal = %q, want %q (timestamps are written unquoted)", out, want)
			}
		})
	}
}

func TestTimestampJSON(t *testing.T) {
	t.Parallel()

	ts := NewTimestamp(time.Date(2026, 9, 1, 10, 45, 12, 999, time.UTC))
	data, err := json.Marshal(map[string]Timestamp{"created": ts})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `{"created":"2026-09-01T10:45:12Z"}` {
		t.Errorf("json = %s", data)
	}
	var back map[string]Timestamp
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back["created"] != ts {
		t.Errorf("round trip = %v, want %v", back["created"], ts)
	}

	var zero map[string]Timestamp
	if err := json.Unmarshal([]byte(`{"created":null}`), &zero); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if !zero["created"].IsZero() {
		t.Error("null must decode to the zero timestamp")
	}
}

func TestParseDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "canonical", in: "2026-09-15", want: "2026-09-15"},
		{name: "timestamp keeps its date", in: "2026-09-15T23:10:00Z", want: "2026-09-15"},
		{name: "not a date", in: "15/09/2026", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseDate(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDate(%q) = %q, want an error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q): %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("ParseDate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDateYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	type doc struct {
		Due Date `yaml:"due"`
	}
	var d doc
	if err := yaml.Unmarshal([]byte("due: 2026-09-15\n"), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Due.String() != "2026-09-15" {
		t.Fatalf("due = %q", d.Due)
	}
	out, err := yaml.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != "due: 2026-09-15\n" {
		t.Errorf("marshal = %q, want unquoted date", out)
	}
}

func TestEnumerationsAreClosed(t *testing.T) {
	t.Parallel()

	if !TypeStory.Valid() || ItemType("userstory").Valid() {
		t.Error("ItemType.Valid is wrong")
	}
	if !PriorityCritical.Valid() || Priority("urgent").Valid() {
		t.Error("Priority.Valid is wrong")
	}
	if !CategoryDone.Valid() || StatusCategory("finished").Valid() {
		t.Error("StatusCategory.Valid is wrong")
	}
	if !LinkBlocks.Valid() || LinkKind("causes").Valid() {
		t.Error("LinkKind.Valid is wrong")
	}
	if !CommentKindSystem.Valid() || CommentKind("note").Valid() {
		t.Error("CommentKind.Valid is wrong")
	}
}

func TestLinkKindInverse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind LinkKind
		want LinkKind
	}{
		{kind: LinkBlocks, want: LinkBlockedBy},
		{kind: LinkBlockedBy, want: LinkBlocks},
		{kind: LinkRelatesTo, want: LinkRelatesTo},
		{kind: LinkDuplicates, want: LinkDuplicatedBy},
		{kind: LinkDuplicatedBy, want: LinkDuplicates},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			if got := tt.kind.Inverse(); got != tt.want {
				t.Errorf("%q.Inverse() = %q, want %q", tt.kind, got, tt.want)
			}
			if got := tt.kind.Inverse().Inverse(); got != tt.kind {
				t.Errorf("double inverse of %q = %q", tt.kind, got)
			}
		})
	}
}

func TestCommentRef(t *testing.T) {
	t.Parallel()

	c := &Comment{Item: "ACME-US-0042", Path: "comments/ACME-US-0042/20260901T104512Z-marta.md"}
	if got, want := c.Ref(), "ACME-US-0042#20260901T104512Z-marta"; got != want {
		t.Errorf("Ref() = %q, want %q", got, want)
	}
	if (&Comment{}).Ref() != "" {
		t.Error("a comment without a path has no ref")
	}
}
