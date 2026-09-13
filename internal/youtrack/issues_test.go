package youtrack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
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
