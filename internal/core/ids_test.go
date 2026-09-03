package core

import "testing"

func TestParseItemID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		key     ProjectKey
		code    TypeCode
		number  int
		wantErr bool
	}{
		{name: "epic", in: "ACME-EP-0001", key: "ACME", code: CodeEpic, number: 1},
		{name: "story", in: "ACME-US-0042", key: "ACME", code: CodeStory, number: 42},
		{name: "task", in: "ACME-T-0107", key: "ACME", code: CodeTask, number: 107},
		{name: "milestone", in: "ACME-M-0003", key: "ACME", code: CodeMilestone, number: 3},
		{name: "five digits", in: "ACME-T-10234", key: "ACME", code: CodeTask, number: 10234},
		{name: "digits in key", in: "ACME2-US-0007", key: "ACME2", code: CodeStory, number: 7},
		{name: "shortest key", in: "AB-US-0001", key: "AB", code: CodeStory, number: 1},
		{name: "longest key", in: "ABCDEFGHIJ-US-0001", key: "ABCDEFGHIJ", code: CodeStory, number: 1},
		{name: "lowercase key", in: "acme-US-0042", wantErr: true},
		{name: "lowercase type code", in: "ACME-us-0042", wantErr: true},
		{name: "unknown type code", in: "ACME-XX-0042", wantErr: true},
		{name: "too few digits", in: "ACME-US-42", wantErr: true},
		{name: "key too long", in: "ABCDEFGHIJK-US-0001", wantErr: true},
		{name: "key too short", in: "A-US-0001", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "trailing text", in: "ACME-US-0042-login", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			key, code, number, err := ParseItemID(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseItemID(%q) = (%q, %q, %d), want error", tt.in, key, code, number)
				}
				if ItemID(tt.in).Valid() {
					t.Errorf("ItemID(%q).Valid() = true, want false", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseItemID(%q): %v", tt.in, err)
			}
			if key != tt.key || code != tt.code || number != tt.number {
				t.Errorf("ParseItemID(%q) = (%q, %q, %d), want (%q, %q, %d)",
					tt.in, key, code, number, tt.key, tt.code, tt.number)
			}
			if got := FormatItemID(key, code, number); len(tt.in) == len(string(got)) && string(got) != tt.in {
				t.Errorf("FormatItemID round trip = %q, want %q", got, tt.in)
			}
		})
	}
}

func TestFormatItemID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		key    ProjectKey
		code   TypeCode
		number int
		want   ItemID
	}{
		{name: "pads to four digits", key: "ACME", code: CodeStory, number: 42, want: "ACME-US-0042"},
		{name: "keeps five digits", key: "ACME", code: CodeTask, number: 10234, want: "ACME-T-10234"},
		{name: "first id", key: "GIT", code: CodeEpic, number: 1, want: "GIT-EP-0001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatItemID(tt.key, tt.code, tt.number); got != tt.want {
				t.Errorf("FormatItemID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTypeCodeRoundTrip(t *testing.T) {
	t.Parallel()

	for _, typ := range []ItemType{TypeEpic, TypeStory, TypeTask, TypeMilestone} {
		code, ok := TypeCodeFor(typ)
		if !ok {
			t.Fatalf("TypeCodeFor(%q) = _, false", typ)
		}
		back, ok := ItemTypeFor(code)
		if !ok || back != typ {
			t.Errorf("ItemTypeFor(%q) = (%q, %t), want (%q, true)", code, back, ok, typ)
		}
	}
	if _, ok := TypeCodeFor(TypeComment); ok {
		t.Error("TypeCodeFor(comment) = _, true; comments have no id of their own")
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "plain title", title: "Login with SSO", want: "login-with-sso"},
		{name: "accents are folded", title: "Añadir métricas de latencia (p95)", want: "anadir-metricas-de-latencia-p95"},
		{name: "punctuation collapses", title: "Fix: 500 on /api/v2/users?filter=…", want: "fix-500-on-api-v2-users-filter"},
		{name: "emoji only", title: "🚀", want: "item"},
		{name: "empty", title: "", want: "item"},
		{name: "only punctuation", title: "--- ???", want: "item"},
		{name: "leading and trailing separators", title: "  Hello, world!  ", want: "hello-world"},
		{name: "german sharp s", title: "Straße", want: "strasse"},
		{name: "decomposed accent", title: "Café latte", want: "cafe-latte"},
		{name: "digits kept", title: "Release 1.0.0", want: "release-1-0-0"},
		{
			name:  "truncated on a dash boundary",
			title: "A very long title that will certainly exceed the sixty byte budget for slugs",
			want:  "a-very-long-title-that-will-certainly-exceed-the-sixty-byte",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Slugify(tt.title)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.title, got, tt.want)
			}
			if len(got) > slugMaxBytes {
				t.Errorf("Slugify(%q) is %d bytes, want at most %d", tt.title, len(got), slugMaxBytes)
			}
			if again := Slugify(got); again != got {
				t.Errorf("Slugify is not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestFileName(t *testing.T) {
	t.Parallel()

	got := FileName("ACME-US-0042", "Login with SSO")
	want := "ACME-US-0042-login-with-sso.md"
	if got != want {
		t.Errorf("FileName() = %q, want %q", got, want)
	}
}

func TestIDFromFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want ItemID
	}{
		{name: "plain name", in: "ACME-US-0042-login-with-sso.md", want: "ACME-US-0042"},
		{name: "with folders", in: "docs/.pmngr/tasks/ACME-T-0107-add-client.md", want: "ACME-T-0107"},
		{name: "no slug", in: "ACME-M-0003.md", want: "ACME-M-0003"},
		{name: "not an id", in: "notes.md", want: ""},
		{name: "lowercase", in: "acme-us-0042-login.md", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IDFromFileName(tt.in); got != tt.want {
				t.Errorf("IDFromFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidProjectKey(t *testing.T) {
	t.Parallel()

	valid := []ProjectKey{"GIT", "AB", "ACME2", "ABCDEFGHIJ"}
	invalid := []ProjectKey{"", "A", "acme", "1ACME", "ACME-X", "ABCDEFGHIJK"}
	for _, k := range valid {
		if !ValidProjectKey(k) {
			t.Errorf("ValidProjectKey(%q) = false, want true", k)
		}
	}
	for _, k := range invalid {
		if ValidProjectKey(k) {
			t.Errorf("ValidProjectKey(%q) = true, want false", k)
		}
	}
}
