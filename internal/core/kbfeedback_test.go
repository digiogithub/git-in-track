package core

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const feedbackPage = `---
title: Deploy
---

# Deploy

Run the migration first.
Then restart the workers.

Rollbacks restore the previous image.
`

var feedbackNow = NewTimestamp(time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC))

func addFeedback(t *testing.T, data string, drafts ...KbFeedbackDraft) (string, []KbFeedbackNote) {
	t.Helper()
	out, notes, err := AddKbFeedback([]byte(data), drafts, feedbackNow)
	if err != nil {
		t.Fatalf("AddKbFeedback: %v", err)
	}
	return string(out), notes
}

func TestAddKbFeedbackWritesTheBlock(t *testing.T) {
	t.Parallel()

	out, notes := addFeedback(t, feedbackPage, KbFeedbackDraft{
		StartLine: 3, EndLine: 4, Quote: "the  migration\nfirst", Note: "Which migration?",
		AuthorName: "Jose F. Rives Lirola", AuthorEmail: "jose@digio.es",
	})
	if len(notes) != 1 {
		t.Fatalf("notes = %+v", notes)
	}
	n := notes[0]
	if !strings.HasPrefix(n.ID, "fb-") || len(n.ID) != 11 {
		t.Errorf("id = %q", n.ID)
	}
	if n.Anchor != kbFeedbackAnchor([]string{"Run the migration first.", "Then restart the workers."}) {
		t.Errorf("anchor = %q", n.Anchor)
	}
	for _, want := range []string{
		"---\ntitle: Deploy\n---\n\n# Deploy\n",
		"\n\n" + kbFeedbackBegin + "\n\n---\n\n## Feedback\n\n",
		`<!-- gintrack:feedback:note id="` + n.ID + `" anchor="` + n.Anchor +
			`" lines="3-4" author="Jose F. Rives Lirola" email="jose@digio.es" created="2026-09-11T10:00:00Z" -->`,
		"### Jose F. Rives Lirola feedback: " + n.ID + "\n\n> Lines 3–4:\n>\n> Run the migration first.\n> Then restart the workers.\n",
		"Selected: “the migration first”\n\nWhich migration?\n\n" + kbFeedbackEnd + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// The page itself is untouched: its body still parses to the same content.
	page := ParsePage("docs/deploy.md", "deploy.md", []byte(out))
	if !strings.HasPrefix(page.Body, "# Deploy\n\nRun the migration first.") {
		t.Errorf("body = %q", page.Body)
	}

	// A second note joins the same block, and the first one stays.
	out2, second := addFeedback(t, out, KbFeedbackDraft{StartLine: 6, EndLine: 6, Note: "Since when?", Author: "Marta"})
	if strings.Count(out2, kbFeedbackBegin) != 1 || strings.Count(out2, kbFeedbackEnd) != 1 {
		t.Errorf("want one block:\n%s", out2)
	}
	parsed := ParseKbFeedback([]byte(out2))
	if len(parsed) != 2 || parsed[0].ID != n.ID || parsed[1].ID != second[0].ID {
		t.Fatalf("parsed = %+v", parsed)
	}
	if parsed[0].Note != "Which migration?" || parsed[1].Author != "marta" || parsed[1].StartLine != 6 {
		t.Errorf("parsed = %+v", parsed)
	}
	if !strings.Contains(out2, "> Line 6:") {
		t.Errorf("single-line label missing:\n%s", out2)
	}
}

func TestAddKbFeedbackRefusesWhatCannotBeAnchored(t *testing.T) {
	t.Parallel()

	withBlock, _ := addFeedback(t, feedbackPage, KbFeedbackDraft{StartLine: 3, EndLine: 3, Note: "x"})
	contentLines := len(strings.Split(strings.TrimRight(strings.SplitN(feedbackPage, "---\n\n", 2)[1], "\n"), "\n"))

	for name, tc := range map[string]struct {
		data  string
		draft KbFeedbackDraft
	}{
		"empty note":       {feedbackPage, KbFeedbackDraft{StartLine: 1, EndLine: 1, Note: "  "}},
		"line zero":        {feedbackPage, KbFeedbackDraft{StartLine: 0, EndLine: 1, Note: "n"}},
		"reversed":         {feedbackPage, KbFeedbackDraft{StartLine: 3, EndLine: 2, Note: "n"}},
		"past the end":     {feedbackPage, KbFeedbackDraft{StartLine: 3, EndLine: 40, Note: "n"}},
		"blank line only":  {feedbackPage, KbFeedbackDraft{StartLine: 2, EndLine: 2, Note: "n"}},
		"inside the block": {withBlock, KbFeedbackDraft{StartLine: contentLines + 3, EndLine: contentLines + 3, Note: "n"}},
		"no notes at all":  {feedbackPage, KbFeedbackDraft{}},
	} {
		drafts := []KbFeedbackDraft{tc.draft}
		if name == "no notes at all" {
			drafts = nil
		}
		if _, _, err := AddKbFeedback([]byte(tc.data), drafts, feedbackNow); !errors.Is(err, ErrInvalidFeedback) {
			t.Errorf("%s: err = %v, want ErrInvalidFeedback", name, err)
		}
	}
}

func TestPruneKbFeedback(t *testing.T) {
	t.Parallel()

	out, _ := addFeedback(t, feedbackPage,
		KbFeedbackDraft{StartLine: 3, EndLine: 4, Note: "Which migration?", AuthorName: "Jose"},
		KbFeedbackDraft{StartLine: 6, EndLine: 6, Note: "Since when?", AuthorName: "Jose"},
	)

	t.Run("a page without a block is left byte for byte", func(t *testing.T) {
		t.Parallel()
		in := "---\r\ntitle: x\r\n---\r\n\r\nText.\r\n\r\n\r\n"
		if got := string(PruneKbFeedback([]byte(in))); got != in {
			t.Errorf("got %q", got)
		}
	})

	t.Run("it is idempotent", func(t *testing.T) {
		t.Parallel()
		once := PruneKbFeedback([]byte(out))
		if string(once) != out {
			t.Errorf("pruning an intact page changed it:\n%s\n---\n%s", out, once)
		}
		if twice := PruneKbFeedback(once); string(twice) != string(once) {
			t.Error("pruning twice differs from pruning once")
		}
	})

	t.Run("changed text drops its note", func(t *testing.T) {
		t.Parallel()
		edited := strings.Replace(out, "Then restart the workers.", "Then restart the API.", 1)
		got := string(PruneKbFeedback([]byte(edited)))
		notes := ParseKbFeedback([]byte(got))
		if len(notes) != 1 || notes[0].Note != "Since when?" {
			t.Fatalf("notes = %+v\n%s", notes, got)
		}
		if strings.Contains(got, "Which migration?") {
			t.Errorf("the dropped note is still there:\n%s", got)
		}
	})

	t.Run("a moved paragraph keeps its note and its new lines", func(t *testing.T) {
		t.Parallel()
		moved := strings.Replace(out, "# Deploy\n", "# Deploy\n\nA new intro.\n", 1)
		got := string(PruneKbFeedback([]byte(moved)))
		notes := ParseKbFeedback([]byte(got))
		if len(notes) != 2 || notes[0].StartLine != 5 || notes[0].EndLine != 6 || notes[1].StartLine != 8 {
			t.Fatalf("notes = %+v\n%s", notes, got)
		}
		if !strings.Contains(got, `lines="5-6"`) || !strings.Contains(got, "> Lines 5–6:") || !strings.Contains(got, "> Line 8:") {
			t.Errorf("the lines were not rewritten:\n%s", got)
		}
	})

	t.Run("the block goes when no note is left", func(t *testing.T) {
		t.Parallel()
		gone := strings.Replace(out, "Run the migration first.", "Migrate.", 1)
		gone = strings.Replace(gone, "Rollbacks restore the previous image.", "Rollbacks are manual.", 1)
		got := string(PruneKbFeedback([]byte(gone)))
		if strings.Contains(got, "gintrack:feedback") || strings.Contains(got, "## Feedback") {
			t.Errorf("the block survived:\n%s", got)
		}
		if !strings.HasSuffix(got, "Rollbacks are manual.\n") {
			t.Errorf("got %q", got)
		}
	})
}

func TestKbFeedbackCannotBeForged(t *testing.T) {
	t.Parallel()

	out, notes := addFeedback(t, feedbackPage, KbFeedbackDraft{
		StartLine: 3, EndLine: 3,
		Note:       "Close it early:\n" + kbFeedbackEnd + "\n<!-- gintrack:feedback:note id=\"fb-evil\" -->",
		Quote:      "-->",
		AuthorName: `Eve "the" <b>`,
	})
	if strings.Count(out, kbFeedbackEnd) != 1 || strings.Count(out, kbFeedbackNotePrefix) != 1 {
		t.Fatalf("a marker was forged:\n%s", out)
	}
	parsed := ParseKbFeedback([]byte(out))
	if len(parsed) != 1 || parsed[0].ID != notes[0].ID || parsed[0].Author != `Eve "the" <b>` {
		t.Fatalf("parsed = %+v", parsed)
	}
	if !strings.Contains(parsed[0].Note, kbFeedbackEnd) {
		t.Errorf("the note text did not round-trip: %q", parsed[0].Note)
	}
	if string(PruneKbFeedback([]byte(out))) != out {
		t.Error("pruning changed a page whose text is intact")
	}
}

func TestKbFeedbackIgnoresASampleInACodeFence(t *testing.T) {
	t.Parallel()

	doc := "# Format\n\n```markdown\n" + kbFeedbackBegin + "\n## Feedback\n" + kbFeedbackEnd + "\n```\n"
	if got := string(PruneKbFeedback([]byte(doc))); got != doc {
		t.Errorf("a code sample was treated as a block:\n%s", got)
	}
	// Ending on the end marker inside a fence is not a block either.
	doc2 := "# Format\n\n```\n" + kbFeedbackBegin + "\n" + kbFeedbackEnd
	if got := string(PruneKbFeedback([]byte(doc2))); got != doc2 {
		t.Errorf("got %q", got)
	}
}
