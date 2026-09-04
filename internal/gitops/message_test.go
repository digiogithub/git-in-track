package gitops

import (
	"strings"
	"testing"
)

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
		wantSrc string
	}{
		{name: "an empty template compiles the shipped default", text: "  ", wantSrc: DefaultTemplate},
		{name: "the documented field form", text: `pmngr: update {{.ItemID}} "{{.Title}}"`, wantSrc: `pmngr: update {{.ItemID}} "{{.Title}}"`},
		{name: "the short lowercase form of the story", text: "{{action}} {{id}}: {{title}}", wantSrc: "{{action}} {{id}}: {{title}}"},
		{name: "an unbalanced action is refused", text: "{{action", wantErr: true},
		{name: "an unknown function is refused", text: "{{nosuchplaceholder}}", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := ParseTemplate(tc.text)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseTemplate(%q) = nil error, want a failure", tc.text)
				}
				if CodeOf(err) != CodeTemplateInvalid {
					t.Errorf("code = %q, want %q", CodeOf(err), CodeTemplateInvalid)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTemplate(%q): %v", tc.text, err)
			}
			if tpl.Text() != tc.wantSrc {
				t.Errorf("Text() = %q, want %q", tpl.Text(), tc.wantSrc)
			}
		})
	}
}

func TestTemplateRenderSubject(t *testing.T) {
	fields := Fields{
		ItemID:     "ACME-US-0042",
		Title:      "Login with SSO",
		Type:       "story",
		Status:     "in_progress",
		PrevStatus: "todo",
		ProjectKey: "ACME",
		Board:      "team-alpha",
		Action:     ActionMove,
		User:       "jose",
		Date:       "2026-09-04",
		Tool:       "gintrack 0.4.1 (companion)",
	}

	tests := []struct {
		name   string
		text   string
		fields Fields
		want   string
	}{
		{
			name:   "the shipped default",
			text:   "",
			fields: fields,
			want:   `pmngr: update ACME-US-0042 "Login with SSO"`,
		},
		{
			name:   "the four placeholders the story asks for",
			text:   "{{action}} {{id}}: {{title}}",
			fields: fields,
			want:   "move ACME-US-0042: Login with SSO",
		},
		{
			name:   "the type placeholder",
			text:   "{{action}} {{type}} {{id}}",
			fields: fields,
			want:   "move story ACME-US-0042",
		},
		{
			name:   "the conventional-commits variant of docs/07",
			text:   "docs({{.ProjectKey}}): update {{.ItemID}} — {{.Title}}",
			fields: fields,
			want:   "docs(ACME): update ACME-US-0042 — Login with SSO",
		},
		{
			name:   "status, board, user and date",
			text:   "{{.Action}} {{.ItemID}} to {{.Status}} ({{.Board}}) {{.User}} {{.Date}}",
			fields: fields,
			want:   "move ACME-US-0042 to in_progress (team-alpha) jose 2026-09-04",
		},
		{
			name:   "an absent action defaults to update",
			text:   "{{action}} {{id}}",
			fields: Fields{ItemID: "ACME-T-0001"},
			want:   "update ACME-T-0001",
		},
		{
			name:   "a newline in the title is folded into the subject",
			text:   "{{action}} {{id}}: {{title}}",
			fields: Fields{ItemID: "ACME-T-0002", Title: "one\ntwo", Action: ActionCreate},
			want:   "create ACME-T-0002: one two",
		},
		{
			name:   "a batch of several items uses the built-in bulk subject",
			text:   "",
			fields: Fields{Action: ActionUpdate, Count: 12},
			want:   "pmngr: update 12 items",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tpl, err := ParseTemplate(tc.text)
			if err != nil {
				t.Fatalf("ParseTemplate: %v", err)
			}
			msg, err := tpl.Render(tc.fields)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if msg.Subject != tc.want {
				t.Errorf("subject = %q, want %q", msg.Subject, tc.want)
			}
			if strings.Contains(msg.Subject, "\n") {
				t.Error("a subject must never contain a newline")
			}
		})
	}
}

func TestTemplateRenderBodyTrailers(t *testing.T) {
	tests := []struct {
		name   string
		fields Fields
		want   []string
		absent []string
	}{
		{
			name: "a status change reports the transition",
			fields: Fields{
				ItemID: "ACME-US-0042", Title: "Login with SSO", Type: "story",
				PrevStatus: "todo", Status: "in_progress",
				Tool: "gintrack 0.4.1 (companion)", Action: ActionMove,
			},
			want: []string{
				"Item: ACME-US-0042",
				"Type: story",
				"Status: todo -> in_progress",
				"Tool: gintrack 0.4.1 (companion)",
			},
		},
		{
			name: "an unchanged status is reported once",
			fields: Fields{
				ItemID: "ACME-US-0042", Type: "story",
				PrevStatus: "todo", Status: "todo", Tool: "gintrack dev",
			},
			want:   []string{"Status: todo"},
			absent: []string{"->"},
		},
		{
			name: "an agent write is attributed",
			fields: Fields{
				ItemID: "ACME-T-0311", Type: "task", Tool: "gintrack dev", Agent: "claude-code",
			},
			want: []string{"Agent: claude-code"},
		},
		{
			name:   "a bulk write reports the item count",
			fields: Fields{Action: ActionUpdate, Count: 12, Tool: "gintrack dev"},
			want:   []string{"Items: 12", "Tool: gintrack dev"},
			absent: []string{"Item: "},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := MustParseTemplate("").Render(tc.fields)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(msg.Body, want) {
					t.Errorf("body is missing %q:\n%s", want, msg.Body)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(msg.Body, absent) {
					t.Errorf("body should not contain %q:\n%s", absent, msg.Body)
				}
			}
		})
	}
}

func TestTemplateRenderTruncatesTheSubject(t *testing.T) {
	long := strings.Repeat("very long title ", 12)
	msg, err := MustParseTemplate("").Render(Fields{ItemID: "ACME-US-0042", Title: long})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := len([]rune(msg.Subject)); got != SubjectLimit {
		t.Errorf("subject length = %d, want %d: %q", got, SubjectLimit, msg.Subject)
	}
	if !strings.Contains(msg.Body, strings.TrimSpace(long)) {
		t.Errorf("the full title must survive in the body:\n%s", msg.Body)
	}
}

func TestMessageString(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want string
	}{
		{name: "subject only", msg: Message{Subject: "pmngr: update X"}, want: "pmngr: update X"},
		{
			name: "subject and body are separated by a blank line",
			msg:  Message{Subject: "s", Body: "Item: X"},
			want: "s\n\nItem: X",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
