package youtrack

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
)

// uploadCapture is what an upload handler saw: the multipart part it received
// and the headers that decided how it was parsed.
type uploadCapture struct {
	contentType string
	fieldName   string
	fileName    string
	content     string
}

// captureUpload parses a multipart/form-data request the way YouTrack does and
// records the single file part it carries.
func captureUpload(t *testing.T, r *http.Request) uploadCapture {
	t.Helper()
	got := uploadCapture{contentType: r.Header.Get("Content-Type")}
	mediaType, params, err := mime.ParseMediaType(got.contentType)
	if err != nil {
		t.Fatalf("parsing the content type %q: %v", got.contentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("media type = %q, want multipart/form-data", mediaType)
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatalf("reading the first part: %v", err)
	}
	defer func() { _ = part.Close() }()
	body, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("reading the part body: %v", err)
	}
	got.fieldName = part.FormName()
	got.fileName = part.FileName()
	got.content = string(body)
	return got
}

// TestSyncAttachmentsUploadsSkipsAndReports is the table over the upload path:
// what is sent, what is skipped because it is already there, and what an
// upload failure looks like.
func TestSyncAttachmentsUploadsSkipsAndReports(t *testing.T) {
	files := fstest.MapFS{
		"assets/diagram.png":  &fstest.MapFile{Data: []byte("PNG-CONTENT")},
		"assets/notes.txt":    &fstest.MapFile{Data: []byte("hello")},
		"assets/existing.png": &fstest.MapFile{Data: []byte("0123456789")},
	}

	tests := []struct {
		name         string
		listBody     string
		refs         []FileRef
		status       int
		wantUploaded []string
		wantSkipped  []string
		wantErr      error
		// wantFail marks a row that must fail without a sentinel of its own.
		wantFail bool
	}{
		{
			name:         "every referenced file is uploaded",
			listBody:     "[]",
			refs:         []FileRef{{Name: "diagram.png", Path: "assets/diagram.png"}, {Name: "notes.txt", Path: "assets/notes.txt"}},
			wantUploaded: []string{"diagram.png", "notes.txt"},
		},
		{
			name: "a file already attached with the same name and size is skipped",
			listBody: `[{"id":"8-1","name":"existing.png","size":10,"$type":"ArticleAttachment"},
			            {"id":"8-2","name":"notes.txt","size":999,"$type":"ArticleAttachment"}]`,
			refs: []FileRef{
				{Name: "existing.png", Path: "assets/existing.png"},
				{Name: "notes.txt", Path: "assets/notes.txt"},
			},
			wantUploaded: []string{"notes.txt"},
			wantSkipped:  []string{"existing.png"},
		},
		{
			name:     "the same file referenced twice is uploaded once",
			listBody: "[]",
			refs: []FileRef{
				{Name: "diagram.png", Path: "assets/diagram.png"},
				{Name: "diagram.png", Path: "assets/diagram.png"},
			},
			wantUploaded: []string{"diagram.png"},
			wantSkipped:  []string{"diagram.png"},
		},
		{
			name:     "a failing upload surfaces the typed error",
			listBody: "[]",
			refs:     []FileRef{{Name: "diagram.png", Path: "assets/diagram.png"}},
			status:   http.StatusForbidden,
			wantErr:  ErrForbidden,
		},
		{
			name:     "a missing local file fails before anything is uploaded",
			listBody: "[]",
			refs:     []FileRef{{Name: "gone.png", Path: "assets/gone.png"}},
			wantFail: true,
		},
		{
			name:     "a path climbing out of the tree is refused locally",
			listBody: "[]",
			refs:     []FileRef{{Name: "escape.png", Path: "../outside.png"}},
			wantErr:  ErrInvalidInput,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var uploads []uploadCapture
			srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					_, _ = io.WriteString(w, tc.listBody)
				case http.MethodPost:
					uploads = append(uploads, captureUpload(t, r))
					if tc.status != 0 {
						w.WriteHeader(tc.status)
						// The failing body echoes the credential, which the
						// error must never carry.
						_, _ = io.WriteString(w, "rejected for Bearer "+testToken)
						return
					}
					writeJSON(t, w, "attachment_uploaded.json")
				default:
					http.NotFound(w, r)
				}
			})
			// Retries are off so a 4xx table row makes exactly one request.
			client := newTestClient(t, srv.URL, newFakeClock(), func(o *Options) { o.MaxRetries = -1 })

			result, err := client.SyncArticleAttachments(context.Background(), "ACME-A-3", files, tc.refs)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if strings.Contains(err.Error(), testToken) {
					t.Fatalf("the error carries the token: %v", err)
				}
				return
			case tc.wantFail:
				if err == nil {
					t.Fatalf("the call must fail")
				}
				if len(uploads) != 0 {
					t.Fatalf("%d uploads were made for a missing file", len(uploads))
				}
				return
			case err != nil:
				t.Fatalf("SyncArticleAttachments: %v", err)
			}

			var uploadedNames []string
			for _, up := range uploads {
				uploadedNames = append(uploadedNames, up.fileName)
			}
			if strings.Join(uploadedNames, ",") != strings.Join(tc.wantUploaded, ",") {
				t.Fatalf("uploaded %v, want %v", uploadedNames, tc.wantUploaded)
			}
			if len(result.Uploaded) != len(tc.wantUploaded) {
				t.Fatalf("result.Uploaded = %d entries, want %d", len(result.Uploaded), len(tc.wantUploaded))
			}
			if strings.Join(result.Skipped, ",") != strings.Join(tc.wantSkipped, ",") {
				t.Fatalf("skipped %v, want %v", result.Skipped, tc.wantSkipped)
			}
			// One listing plus one request per upload.
			if rec.count() != 1+len(tc.wantUploaded) {
				t.Fatalf("%d requests, want %d", rec.count(), 1+len(tc.wantUploaded))
			}
		})
	}
}

// TestUploadArticleAttachmentSendsMultipartWithoutJSONContentType pins the one
// header rule of this endpoint: the body is multipart/form-data with its own
// boundary, and the JSON content type every other call sets must be absent.
func TestUploadArticleAttachmentSendsMultipartWithoutJSONContentType(t *testing.T) {
	var got uploadCapture
	var path, fields, accept string
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fields = r.URL.Query().Get("fields")
		accept = r.Header.Get("Accept")
		got = captureUpload(t, r)
		writeJSON(t, w, "attachment_uploaded.json")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	attachment, err := client.UploadArticleAttachment(context.Background(), "ACME-A-3",
		"diagram.png", strings.NewReader("PNG-CONTENT"))
	if err != nil {
		t.Fatalf("UploadArticleAttachment: %v", err)
	}
	if path != "/api/articles/ACME-A-3/attachments" {
		t.Fatalf("path = %q", path)
	}
	if fields != AttachmentUploadFields {
		t.Fatalf("fields = %q", fields)
	}
	if accept != acceptJSON {
		t.Fatalf("accept = %q", accept)
	}
	if strings.Contains(got.contentType, "application/json") {
		t.Fatalf("content type = %q: the JSON content type must not be set on an upload", got.contentType)
	}
	if !strings.HasPrefix(got.contentType, "multipart/form-data; boundary=") {
		t.Fatalf("content type = %q", got.contentType)
	}
	if got.fieldName != "diagram.png" || got.fileName != "diagram.png" {
		t.Fatalf("part = %q / %q", got.fieldName, got.fileName)
	}
	if got.content != "PNG-CONTENT" {
		t.Fatalf("content = %q", got.content)
	}
	if attachment.ID != "8-42" || attachment.Name != "diagram.png" {
		t.Fatalf("attachment = %+v", attachment)
	}
}

// TestUploadIssueAttachmentUsesTheIssuePath covers the symmetric endpoint: the
// import records attachment paths, and pushing them back needs the same call
// against /api/issues.
func TestUploadIssueAttachmentUsesTheIssuePath(t *testing.T) {
	var path string
	srv, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(t, w, "attachments.json")
		default:
			path = r.URL.Path
			_ = captureUpload(t, r)
			writeJSON(t, w, "attachment_uploaded.json")
		}
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	files := fstest.MapFS{
		// Same name and same size as the listed attachment: skipped.
		"assets/screenshot.png": &fstest.MapFile{Data: make([]byte, 84213)},
		"assets/diagram.png":    &fstest.MapFile{Data: []byte("PNG-CONTENT")},
	}
	result, err := client.SyncIssueAttachments(context.Background(), "ACME-42", files, []FileRef{
		{Name: "screenshot.png", Path: "assets/screenshot.png"},
		{Name: "diagram.png", Path: "assets/diagram.png"},
	})
	if err != nil {
		t.Fatalf("SyncIssueAttachments: %v", err)
	}
	if path != "/api/issues/ACME-42/attachments" {
		t.Fatalf("path = %q", path)
	}
	if len(result.Uploaded) != 1 || result.Uploaded[0].Name != "diagram.png" {
		t.Fatalf("uploaded = %+v", result.Uploaded)
	}
	if strings.Join(result.Skipped, ",") != "screenshot.png" {
		t.Fatalf("skipped = %v", result.Skipped)
	}
}

// TestUploadRejectsEmptyInputLocally keeps the validation before the wire, so a
// mistake costs no request.
func TestUploadRejectsEmptyInputLocally(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should have been made")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"no article id", func() error {
			_, err := client.UploadArticleAttachment(ctx, "", "a.png", strings.NewReader("x"))
			return err
		}},
		{"no issue id", func() error {
			_, err := client.UploadIssueAttachment(ctx, "", "a.png", strings.NewReader("x"))
			return err
		}},
		{"no name", func() error {
			_, err := client.UploadArticleAttachment(ctx, "ACME-A-3", "  ", strings.NewReader("x"))
			return err
		}},
		{"no content", func() error {
			_, err := client.UploadArticleAttachment(ctx, "ACME-A-3", "a.png", nil)
			return err
		}},
		{"no file system", func() error {
			_, err := client.SyncArticleAttachments(ctx, "ACME-A-3", nil, []FileRef{{Name: "a.png", Path: "a.png"}})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}
	if rec.count() != 0 {
		t.Fatalf("%d requests were made, want none", rec.count())
	}
}

// TestSyncAttachmentsWithoutFilesMakesNoRequest keeps the common case free: an
// article whose body references nothing local must not even list.
func TestSyncAttachmentsWithoutFilesMakesNoRequest(t *testing.T) {
	srv, rec := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should have been made")
	})
	client := newTestClient(t, srv.URL, newFakeClock(), nil)

	result, err := client.SyncArticleAttachments(context.Background(), "ACME-A-3", nil, nil)
	if err != nil {
		t.Fatalf("SyncArticleAttachments: %v", err)
	}
	if len(result.Uploaded) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if rec.count() != 0 {
		t.Fatalf("%d requests were made, want none", rec.count())
	}
}
