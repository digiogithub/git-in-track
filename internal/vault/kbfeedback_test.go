package vault

import (
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/core"
)

func TestVaultKbFeedback(t *testing.T) {
	v, _ := loadedVault(t)

	page := decode[kbPageResult](t, call(t, v, "kb.page", map[string]any{"path": "docs/index.md"}))
	lines := strings.Split(page.Body, "\n")
	if lines[2] != "This folder is the documentation root of the fixture project used by the" {
		t.Fatalf("body line 3 = %q: the line numbers are not those of the body", lines[2])
	}

	result := decode[struct {
		Page   kbPageResult          `json:"page"`
		Writes WriteSet              `json:"writes"`
		Notes  []core.KbFeedbackNote `json:"notes"`
	}](t, call(t, v, "kb.feedback.add", map[string]any{
		"path":        "docs/index.md",
		"rev":         page.Rev,
		"authorName":  "Jose F. Rives Lirola",
		"authorEmail": "jose@digio.es",
		"notes": []map[string]any{
			{"startLine": 3, "endLine": 5, "quote": "documentation root", "note": "Say which fixture."},
		},
	}))
	if len(result.Notes) != 1 || result.Notes[0].StartLine != 3 || result.Notes[0].EndLine != 5 {
		t.Fatalf("notes = %+v", result.Notes)
	}
	if len(result.Writes.Written) != 1 || result.Writes.Written[0].Path != "docs/index.md" {
		t.Errorf("writes = %+v", result.Writes.Written)
	}
	for _, want := range []string{
		"### Jose F. Rives Lirola feedback: " + result.Notes[0].ID,
		`email="jose@digio.es" created="2026-09-03T12:00:00Z"`,
		"Say which fixture.",
	} {
		if !strings.Contains(result.Page.Body, want) {
			t.Errorf("the page body is missing %q:\n%s", want, result.Page.Body)
		}
	}
	if result.Page.Rev == page.Rev {
		t.Error("the revision did not change")
	}

	t.Run("a stale revision is refused", func(t *testing.T) {
		env := rawCall(t, v, "kb.feedback.add", map[string]any{
			"path": "docs/index.md", "rev": page.Rev,
			"notes": []map[string]any{{"startLine": 1, "endLine": 1, "note": "late"}},
		})
		if env.OK || env.Error.Code != core.StaleRevisionCode {
			t.Errorf("got ok=%v code=%q", env.OK, env.Error.Code)
		}
	})

	t.Run("lines outside the content are an invalid request", func(t *testing.T) {
		env := rawCall(t, v, "kb.feedback.add", map[string]any{
			"path":  "docs/index.md",
			"notes": []map[string]any{{"startLine": 1, "endLine": 400, "note": "n"}},
		})
		if env.OK || env.Error.Code != "invalid_request" {
			t.Errorf("got ok=%v code=%q", env.OK, env.Error.Code)
		}
	})

	t.Run("rewriting the text through kb.write drops the note", func(t *testing.T) {
		current := decode[kbPageResult](t, call(t, v, "kb.page", map[string]any{"path": "docs/index.md"}))
		text := strings.Replace(current.Body, "documentation root", "docs root", 1)
		written := decode[struct {
			Page kbPageResult `json:"page"`
		}](t, call(t, v, "kb.write", map[string]any{
			"path": "docs/index.md", "text": text, "rev": current.Rev,
		}))
		if strings.Contains(written.Page.Body, "gintrack:feedback") {
			t.Errorf("the stale feedback survived the write:\n%s", written.Page.Body)
		}
	})
}
