package youtrack

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIssueDecodesTheWideSelector(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "issue.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	issue, err := client.Issue(context.Background(), "ACME-42")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got := rec.all()[0].URL.Path; got != "/api/issues/ACME-42" {
		t.Fatalf("path = %q", got)
	}
	if got := rec.all()[0].URL.Query().Get("fields"); got != IssueFields {
		t.Fatalf("fields selector = %q", got)
	}
	if issue.ID != "2-345" || issue.IDReadable != "ACME-42" {
		t.Fatalf("ids = %q / %q", issue.ID, issue.IDReadable)
	}
	if issue.Project.ShortName != "ACME" || issue.Reporter.Login != "jose" {
		t.Fatalf("issue = %+v", issue)
	}
	if !issue.Resolved.IsZero() {
		t.Fatalf("a null resolved must decode as zero, got %v", issue.Resolved)
	}
	if got := issue.Created.Time().Format("2006-01-02"); got != "2026-01-01" {
		t.Fatalf("created = %s", got)
	}
	if len(issue.Tags) != 1 || issue.Tags[0].Name != "regression" {
		t.Fatalf("tags = %+v", issue.Tags)
	}
	// The links of the wide selector carry the empty (linkType, direction)
	// pairs; the caller filters them with NonEmptyLinks.
	if len(issue.Links) != 3 {
		t.Fatalf("got %d raw links, want 3", len(issue.Links))
	}
	if got := len(NonEmptyLinks(issue.Links)); got != 2 {
		t.Fatalf("got %d non-empty links, want 2", got)
	}
}

func TestCustomFieldValueDecoding(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "issue.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)
	issue, err := client.Issue(context.Background(), "ACME-42")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	tests := []struct {
		name      string
		field     string
		fieldType string
		wantCount int
		check     func(t *testing.T, values []FieldValue)
	}{
		{
			name:      "single enum",
			field:     "Type",
			fieldType: "SingleEnumIssueCustomField",
			wantCount: 1,
			check: func(t *testing.T, values []FieldValue) {
				if values[0].Name != "Bug" || values[0].ID != "70-1" {
					t.Fatalf("value = %+v", values[0])
				}
			},
		},
		{
			name:      "single user",
			field:     "Assignee",
			fieldType: "SingleUserIssueCustomField",
			wantCount: 1,
			check: func(t *testing.T, values []FieldValue) {
				if values[0].Login != "ana" || values[0].FullName != "Ana Ruiz" {
					t.Fatalf("value = %+v", values[0])
				}
			},
		},
		{
			name:      "period",
			field:     "Estimation",
			fieldType: "PeriodIssueCustomField",
			wantCount: 1,
			check: func(t *testing.T, values []FieldValue) {
				if values[0].Presentation != "3d" {
					t.Fatalf("presentation = %q", values[0].Presentation)
				}
				if values[0].Minutes == nil || *values[0].Minutes != 1440 {
					t.Fatalf("minutes = %v", values[0].Minutes)
				}
			},
		},
		{
			name:      "multi version",
			field:     "Sprints",
			fieldType: "MultiVersionIssueCustomField",
			wantCount: 2,
			check: func(t *testing.T, values []FieldValue) {
				if values[0].Name != "Sprint 12" || values[1].Name != "Sprint 13" {
					t.Fatalf("values = %+v", values)
				}
			},
		},
		{
			name:      "date scalar",
			field:     "Due Date",
			fieldType: "DateIssueCustomField",
			wantCount: 1,
			check: func(t *testing.T, values []FieldValue) {
				if len(values[0].Scalar) == 0 {
					t.Fatal("a scalar value must be kept raw")
				}
				var millis Millis
				if err := json.Unmarshal(values[0].Scalar, &millis); err != nil {
					t.Fatalf("decoding the scalar: %v", err)
				}
				if got := millis.Time().Format("2006-01-02"); got != "2026-01-15" {
					t.Fatalf("due date = %s", got)
				}
			},
		},
		{
			name:      "null",
			field:     "Notes",
			fieldType: "SimpleIssueCustomField",
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			field, ok := issue.CustomField(tc.field)
			if !ok {
				t.Fatalf("no custom field named %q", tc.field)
			}
			if field.Type != tc.fieldType {
				t.Fatalf("$type = %q, want %q", field.Type, tc.fieldType)
			}
			values, err := field.Value.Values()
			if err != nil {
				t.Fatalf("Values: %v", err)
			}
			if len(values) != tc.wantCount {
				t.Fatalf("got %d values, want %d", len(values), tc.wantCount)
			}
			if tc.wantCount == 0 && !field.Value.IsNull() {
				t.Fatal("an absent value must report IsNull")
			}
			if tc.check != nil {
				tc.check(t, values)
			}
		})
	}
}

func TestIssueLinksFiltersEmptyPairs(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "issue_links.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	links, err := client.IssueLinks(context.Background(), "ACME-42")
	if err != nil {
		t.Fatalf("IssueLinks: %v", err)
	}
	if got := rec.all()[0].URL.Path; got != "/api/issues/ACME-42/links" {
		t.Fatalf("path = %q", got)
	}
	// The fixture has four (linkType, direction) pairs and only two carry
	// issues, which is the shape a real instance returns.
	if len(links) != 2 {
		t.Fatalf("got %d links, want the 2 non-empty ones", len(links))
	}
	children, parents := 0, 0
	for _, link := range links {
		if !link.LinkType.Aggregation {
			continue
		}
		switch link.Direction {
		case DirectionOutward:
			children += len(link.Issues)
		case DirectionInward:
			parents += len(link.Issues)
		}
	}
	if children != 1 || parents != 1 {
		t.Fatalf("children = %d, parents = %d, want 1 and 1", children, parents)
	}
}

func TestCommentsAlwaysSendTop(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, "comments.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	comments, err := client.Comments(context.Background(), "ACME-42", Page{})
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	query := rec.all()[0].URL.Query()
	if got := query.Get("$top"); got != strconv.Itoa(DefaultTop) {
		t.Fatalf("$top = %q, want %d: the endpoint truncates silently without it", got, DefaultTop)
	}
	if got := query.Get("fields"); got != CommentFields {
		t.Fatalf("fields = %q", got)
	}
	if len(comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(comments))
	}
	if comments[0].Author.Login != "ana" || comments[0].Text != "Reproduced on 17.4." {
		t.Fatalf("comment[0] = %+v", comments[0])
	}
	if !comments[0].Updated.IsZero() {
		t.Fatal("a null updated must decode as zero")
	}
	if comments[1].Updated.IsZero() {
		t.Fatal("comment[1] has an updated timestamp")
	}
}

func TestAddCommentPostsTheDocumentedBody(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		if payload["text"] != "Landed in **main**." {
			t.Errorf("body = %v", payload)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"4-90","text":"Landed in **main**.","created":1767250000000,`+
			`"author":{"login":"jose","fullName":"Jose F. Rives"},"$type":"IssueComment"}`)
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	comment, err := client.AddComment(context.Background(), "ACME-42", "Landed in **main**.")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	req := rec.all()[0]
	if req.Method != http.MethodPost || req.URL.Path != "/api/issues/ACME-42/comments" {
		t.Fatalf("%s %s", req.Method, req.URL.Path)
	}
	if got := req.URL.Query().Get("fields"); got != CommentFields {
		t.Fatalf("fields = %q", got)
	}
	if comment.ID != "4-90" || comment.Author.Login != "jose" {
		t.Fatalf("comment = %+v", comment)
	}
}

func TestAttachmentsAndSignedDownload(t *testing.T) {
	const payload = "binary-bytes"
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/yt/api/files/") {
			if r.URL.Query().Get("sign") == "" {
				t.Error("the signed query parameter was dropped")
			}
			if r.Header.Get("Authorization") == "" {
				t.Error("the bearer token must still be sent on a signed download")
			}
			_, _ = io.WriteString(w, payload)
			return
		}
		writeJSON(t, w, "attachments.json")
	})
	client := newTestClient(t, srv.URL+"/yt", newFakeClock(), nil)

	attachments, err := client.Attachments(context.Background(), "ACME-42")
	if err != nil {
		t.Fatalf("Attachments: %v", err)
	}
	if len(attachments) != 1 || attachments[0].Name != "screenshot.png" || attachments[0].Size != 84213 {
		t.Fatalf("attachments = %+v", attachments)
	}

	target, err := client.AttachmentURL(attachments[0])
	if err != nil {
		t.Fatalf("AttachmentURL: %v", err)
	}
	if !strings.HasPrefix(target, srv.URL+"/yt/api/files/8-12?sign=") {
		t.Fatalf("attachment URL = %q: the context path must survive", target)
	}

	body, err := client.DownloadAttachment(context.Background(), attachments[0])
	if err != nil {
		t.Fatalf("DownloadAttachment: %v", err)
	}
	defer func() { _ = body.Close() }()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("reading the attachment: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("body = %q", got)
	}
	if rec.count() != 2 {
		t.Fatalf("%d requests, want 2", rec.count())
	}

	if _, err := client.AttachmentURL(Attachment{ID: "8-13"}); err == nil {
		t.Fatal("an attachment without a url must fail")
	}

	// An absolute URL is used as it stands.
	absolute, err := client.AttachmentURL(Attachment{ID: "8-14", URL: "https://cdn.example.com/a.png"})
	if err != nil || absolute != "https://cdn.example.com/a.png" {
		t.Fatalf("absolute URL = %q, err = %v", absolute, err)
	}
}

// TestUpdateCommentEditsInPlace covers the comment edit endpoint, which is the
// half of a comment push that keeps a re-delivered job from duplicating a
// comment: the verb is POST, exactly like a create, and the comment id in the
// path is the only thing that distinguishes the two.
func TestUpdateCommentEditsInPlace(t *testing.T) {
	const text = "Fixed in **1.4.1**.\n\n---\n_jose · git-in-track ACME-US-0042_"
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the request body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		if payload["text"] != text {
			t.Errorf("body = %v, want the edited text", payload)
		}
		if len(payload) != 1 {
			t.Errorf("body = %v, want only the text field", payload)
		}
		writeJSON(t, w, "comment_updated.json")
	})
	client := newTestClient(t, srv.URL+"/yt", newFakeClock(), nil)

	comment, err := client.UpdateComment(context.Background(), "ACME-42", "4-89", text)
	if err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	if rec.count() != 1 {
		t.Fatalf("%d requests, want exactly one: an edit must never post a second comment", rec.count())
	}
	req := rec.all()[0]
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST: YouTrack edits a comment with the same verb it creates one with", req.Method)
	}
	if req.URL.Path != "/yt/api/issues/ACME-42/comments/4-89" {
		t.Errorf("path = %q, want the comment sub-resource under the instance context path", req.URL.Path)
	}
	if got := req.URL.Query().Get("fields"); got != CommentFields {
		t.Errorf("fields = %q, want %q", got, CommentFields)
	}
	if comment.ID != "4-89" || comment.Text != text {
		t.Fatalf("comment = %+v, want the edited comment as the server stored it", comment)
	}
	if comment.Updated.Time().IsZero() || !comment.Updated.Time().After(comment.Created.Time()) {
		t.Errorf("updated = %v, created = %v: an edit must carry a later update stamp",
			comment.Updated.Time(), comment.Created.Time())
	}
}

// TestUpdateCommentRejectsEmptyArguments asserts the local validation: every
// one of these would otherwise hit the instance and either 404 or, in the
// empty-text case, blank a comment somebody is reading.
func TestUpdateCommentRejectsEmptyArguments(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no request may be sent for a locally invalid call")
		w.WriteHeader(http.StatusInternalServerError)
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	cases := []struct {
		name      string
		issue     string
		comment   string
		text      string
		wantInErr string
	}{
		{name: "no issue", issue: "", comment: "4-89", text: "hello", wantInErr: "issue id is empty"},
		{name: "no comment", issue: "ACME-42", comment: "", text: "hello", wantInErr: "comment id is empty"},
		{name: "no text", issue: "ACME-42", comment: "4-89", text: "", wantInErr: "comment text is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.UpdateComment(context.Background(), tc.issue, tc.comment, tc.text)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Errorf("err = %v, want it to mention %q", err, tc.wantInErr)
			}
		})
	}
	if rec.count() != 0 {
		t.Fatalf("%d requests were sent, want none", rec.count())
	}
}

// TestUpdateCommentRedactsTheTokenOnFailure asserts the error path of an edit
// obeys the package rule: a failing body is reported, but never with the token
// in it, even when the instance echoes the Authorization header back.
func TestUpdateCommentRedactsTheTokenOnFailure(t *testing.T) {
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":"Forbidden","error_description":"`+
			r.Header.Get("Authorization")+` may not edit this comment"}`)
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	_, err := client.UpdateComment(context.Background(), "ACME-42", "4-89", "edited")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("the token leaked into %q", err.Error())
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("err = %v, want the redaction marker where the token was", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Path != "/api/issues/ACME-42/comments/4-89" {
		t.Errorf("err = %v, want an APIError naming the comment path", err)
	}
}

// TestUpdateCommentRetriesWithoutDuplicating asserts that a retryable status is
// retried on the edit path and that the retry is still an edit: the id in the
// path is what stops a retry from becoming a second comment.
func TestUpdateCommentRetriesWithoutDuplicating(t *testing.T) {
	var attempts atomic.Int32
	srv, rec := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(t, w, "comment_updated.json")
	})
	clock := newFakeClock()
	client := newTestClient(t, srv.URL, clock, nil)

	comment, err := client.UpdateComment(context.Background(), "ACME-42", "4-89", "edited")
	if err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	if comment.ID != "4-89" {
		t.Fatalf("comment = %+v", comment)
	}
	if rec.count() != 2 {
		t.Fatalf("%d requests, want two: one rejected attempt and one retry", rec.count())
	}
	for i, req := range rec.all() {
		if req.URL.Path != "/api/issues/ACME-42/comments/4-89" {
			t.Errorf("request %d went to %q: every attempt must address the same comment", i, req.URL.Path)
		}
	}
	if clock.totalSlept() == 0 {
		t.Error("the retry did not back off")
	}
}
