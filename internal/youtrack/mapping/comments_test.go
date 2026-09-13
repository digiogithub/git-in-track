package mapping

import (
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/core"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

func TestCommentsToDrafts(t *testing.T) {
	comments := loadComments(t, "comments.json")
	drafts, warnings := CommentsToDrafts(comments, "ACME-42", testOptions("ACME-US-0042"))

	if len(drafts) != 2 {
		t.Fatalf("got %d drafts, want 2 (one empty and one deleted comment are skipped)", len(drafts))
	}
	if len(warnings) != 1 || warnings[0].Value != "4-89" {
		t.Fatalf("got warnings %v, want one about the empty comment", warningLines(warnings))
	}

	first := drafts[0]
	if first.Draft.Author != "ana" || first.Draft.AuthorName != "Ana Ruiz" || first.Draft.AuthorEmail != "ana@example.com" {
		t.Errorf("the original author was not preserved: %+v", first.Draft)
	}
	if first.Draft.Kind != core.CommentKindComment {
		t.Errorf("got kind %q, want comment", first.Draft.Kind)
	}
	wantCreated := time.UnixMilli(1767230000000).UTC()
	if !first.Draft.Created.Equal(wantCreated) {
		t.Errorf("got created %v, want %v", first.Draft.Created, wantCreated)
	}
	if !first.Updated.IsZero() {
		t.Errorf("a null updated must stay zero, got %v", first.Updated)
	}
	if first.Draft.Body != "Reproduced on 17.4. See ![trace](.pmngr/attachments/ACME-US-0042/trace.png) for the stack." {
		t.Errorf("the body was not normalised: %q", first.Draft.Body)
	}
	if first.External.System != System || first.External.ID != "4-88" {
		t.Errorf("got external %+v, want the comment id under youtrack", first.External)
	}
	if first.External.URL != "https://yt.example.com/youtrack/issue/ACME-42#focus=Comments-4-88" {
		t.Errorf("got url %q", first.External.URL)
	}

	second := drafts[1]
	if second.Draft.Author != "jose" {
		t.Errorf("got author %q, want jose", second.Draft.Author)
	}
	if second.Updated.IsZero() {
		t.Error("an edited comment keeps its updated timestamp")
	}
	if !drafts[0].Draft.Created.Before(drafts[1].Draft.Created.Time) {
		t.Error("drafts must come back in created order")
	}
}

func TestCommentsToDraftsEdgeCases(t *testing.T) {
	t.Run("no comments produce nothing", func(t *testing.T) {
		drafts, warnings := CommentsToDrafts(nil, "ACME-42", Options{})
		if drafts != nil || warnings != nil {
			t.Fatalf("got %v and %v", drafts, warnings)
		}
	})
	t.Run("an author without a login falls back to the full name", func(t *testing.T) {
		drafts, _ := CommentsToDrafts([]youtrack.Comment{{
			ID: "4-1", Text: "Noted.", Created: youtrack.Millis(1767230000000),
			Author: youtrack.User{FullName: "Ana Ruiz"},
		}}, "ACME-42", Options{})
		if len(drafts) != 1 || drafts[0].Draft.Author != "Ana Ruiz" {
			t.Fatalf("got %+v", drafts)
		}
	})
	t.Run("a missing created timestamp is reported", func(t *testing.T) {
		drafts, warnings := CommentsToDrafts([]youtrack.Comment{{ID: "4-2", Text: "Noted."}}, "ACME-42", Options{})
		if len(drafts) != 1 || !drafts[0].Draft.Created.IsZero() {
			t.Fatalf("got %+v", drafts)
		}
		if len(warnings) != 1 {
			t.Fatalf("got warnings %v, want one", warningLines(warnings))
		}
	})
	t.Run("no base url means no comment url", func(t *testing.T) {
		drafts, _ := CommentsToDrafts([]youtrack.Comment{{
			ID: "4-3", Text: "Noted.", Created: youtrack.Millis(1767230000000),
			Author: youtrack.User{Login: "ana"},
		}}, "ACME-42", Options{})
		if drafts[0].External.URL != "" || drafts[0].External.ID != "4-3" {
			t.Fatalf("got %+v", drafts[0].External)
		}
	})
}
