package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAddCommentNamingAndFrontMatter(t *testing.T) {
	t.Parallel()

	store, fsys, clock := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := store.AddComment(ctx, it.ID, CommentDraft{
		Author: "Marta Alonso",
		Body:   "Entra ID rejects a trailing slash in the redirect URI.",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	wantPath := "docs/.pmngr/comments/ACME-US-0001/20260903T120000Z-marta-alonso.md"
	if first.Path != wantPath {
		t.Errorf("path = %s, want %s", first.Path, wantPath)
	}
	if first.Item != it.ID || first.Author != "marta-alonso" || first.Kind != CommentKindComment {
		t.Errorf("comment = %+v", first)
	}
	if first.Created.String() != "2026-09-03T12:00:00Z" {
		t.Errorf("created = %s, want the injected clock", first.Created)
	}
	if first.Ref() != "ACME-US-0001#20260903T120000Z-marta-alonso" {
		t.Errorf("ref = %s", first.Ref())
	}

	data, err := fsys.ReadFile(first.Path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{"type: comment", "item: ACME-US-0001", "author: marta-alonso", "created: 2026-09-03T12:00:00Z"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the file is missing %q:\n%s", want, data)
		}
	}
	if ComputeRev(data) != first.Rev {
		t.Errorf("rev = %s, on-disk rev = %s", first.Rev, ComputeRev(data))
	}

	// The same author in the same second gets a numbered file name (R-CMT-1).
	second, err := store.AddComment(ctx, it.ID, CommentDraft{
		Author: "marta-alonso", Body: "Second thought.", InReplyTo: first.Ref(),
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if second.Path != "docs/.pmngr/comments/ACME-US-0001/20260903T120000Z-marta-alonso-2.md" {
		t.Errorf("path = %s, want the -2 suffix", second.Path)
	}
	if second.InReplyTo != first.Ref() {
		t.Errorf("in_reply_to = %q", second.InReplyTo)
	}

	// A later comment by another author, and the item file itself must not have
	// changed: a comment never touches the parent's rev (R-REV-6).
	clock.advance(90 * time.Second)
	if _, err := store.AddComment(ctx, it.ID, CommentDraft{Author: "jose", Body: "Agreed."}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	item, err := store.Get(ctx, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if item.Rev != it.Rev {
		t.Errorf("adding a comment changed the item rev: %s -> %s", it.Rev, item.Rev)
	}
}

func TestAddCommentValidatesItsInput(t *testing.T) {
	t.Parallel()

	store, _, _ := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.AddComment(ctx, "ACME-US-4242", CommentDraft{Author: "jose", Body: "x"}); !errors.Is(err, ErrItemNotFound) {
		t.Errorf("AddComment on an unknown item = %v, want ErrItemNotFound", err)
	}
	if _, err := store.AddComment(ctx, it.ID, CommentDraft{Author: "jose", Body: "  \n"}); err == nil {
		t.Error("AddComment accepted an empty body")
	}
	if _, err := store.AddComment(ctx, it.ID, CommentDraft{Author: "jose", Body: "x", Kind: "shout"}); err == nil {
		t.Error("AddComment accepted an unknown kind")
	}
}

func TestListCommentsIsChronological(t *testing.T) {
	t.Parallel()

	store, fsys, clock := newTestStore(t)
	ctx := context.Background()
	it, err := store.Create(ctx, ItemDraft{Type: TypeStory, Title: "Login with SSO", Author: "jose"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	clock.advance(2 * time.Hour)
	if _, err := store.AddComment(ctx, it.ID, CommentDraft{Author: "jose", Body: "Second."}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	// A comment that arrived through git with an earlier created must sort first
	// even though its file name sorts last (R-CMT-3).
	early := "---\ntype: comment\nitem: ACME-US-0001\nauthor: marta\ncreated: 2026-09-03T11:00:00Z\n---\n\nFirst.\n"
	if err := fsys.WriteFile("docs/.pmngr/comments/ACME-US-0001/zz-out-of-order.md", []byte(early)); err != nil {
		t.Fatalf("seed: %v", err)
	}
	clock.advance(time.Hour)
	if _, err := store.AddComment(ctx, it.ID, CommentDraft{Author: "jose", Body: "Third."}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	comments, err := store.ListComments(ctx, it.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 3 {
		t.Fatalf("got %d comments, want 3", len(comments))
	}
	want := []string{"First.", "Second.", "Third."}
	for i, c := range comments {
		if c.Body != want[i] {
			t.Errorf("comment %d = %q, want %q", i, c.Body, want[i])
		}
		if c.Item != it.ID {
			t.Errorf("comment %d belongs to %s", i, c.Item)
		}
	}

	empty, err := store.ListComments(ctx, "ACME-US-0002")
	if err != nil {
		t.Fatalf("ListComments on an item without comments: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %d comments, want none", len(empty))
	}
}

func TestSanitizeHandle(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"jose":            "jose",
		"Marta Alonso":    "marta-alonso",
		"BOT-CI":          "bot-ci",
		"José Ruiz":       "jose-ruiz",
		"  spaced  out  ": "spaced-out",
		"a..b":            "a-b",
		"":                "unknown",
		"🚀":               "unknown",
	}
	for in, want := range cases {
		if got := SanitizeHandle(in); got != want {
			t.Errorf("SanitizeHandle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommentRefHelper(t *testing.T) {
	t.Parallel()

	got := CommentRef("ACME-US-0042", "docs/.pmngr/comments/ACME-US-0042/20260901T104512Z-jose.md")
	if got != "ACME-US-0042#20260901T104512Z-jose" {
		t.Errorf("CommentRef = %q", got)
	}
}
