package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// AttachmentURL resolves the URL of an attachment against the instance base
// URL. YouTrack returns a signed relative URL such as
// "/api/files/8-12?sign=…&updated=…", and an absolute URL is returned
// unchanged. The signature lives in the URL and the bearer token is still sent
// alongside it.
func (c *Client) AttachmentURL(attachment Attachment) (string, error) {
	raw := strings.TrimSpace(attachment.URL)
	if raw == "" {
		return "", fmt.Errorf("%w: attachment %q has no url", ErrInvalidInput, attachment.ID)
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw, nil
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return c.baseURL + raw, nil
}

// DownloadAttachment opens the attachment for reading. The caller owns the
// returned reader and must close it. Redirects are followed by the injected
// HTTP client, which must not be configured to refuse them.
func (c *Client) DownloadAttachment(ctx context.Context, attachment Attachment) (io.ReadCloser, error) {
	target, err := c.AttachmentURL(attachment)
	if err != nil {
		return nil, err
	}
	// The path reported in errors deliberately excludes the signed query.
	path := "/api/files/" + attachment.ID
	//nolint:bodyclose // the body is the return value; the caller owns and closes it.
	resp, err := c.sendTarget(ctx, http.MethodGet, target, path, nil, acceptAny, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer closeBody(resp.Body)
		return nil, c.apiError(resp, http.MethodGet, path)
	}
	return resp.Body, nil
}

// MaxAttachmentSize caps how many bytes this client buffers for one upload. A
// multipart body has to be held in memory because a retried attempt must be
// able to send it again, so the cap is a guard against an accidental huge file
// rather than a YouTrack limit.
const MaxAttachmentSize = 64 << 20

// AttachmentUploadFields is the shape requested for a freshly created
// attachment. It is narrower than AttachmentFields: the author and the
// creation timestamp are known to the caller already.
const AttachmentUploadFields = "id,name,size,mimeType,extension,url"

// FileRef names one local file to attach. Name is the file name YouTrack
// resolves an inline reference such as "![](diagram.png)" against, and Path is
// where the bytes are read from inside the fs.FS the caller supplies. It is
// deliberately the same shape as mapping.AttachmentRef, which the transform
// produces, without this package depending on that one.
type FileRef struct {
	// Name is the attachment name as YouTrack must store it.
	Name string
	// Path is the file path inside the fs.FS handed to the sync call.
	Path string
}

// UploadResult reports what an attachment sync did. Uploaded holds the
// attachments YouTrack created, in the order they were sent; Skipped holds the
// names that were already attached with the same size.
type UploadResult struct {
	Uploaded []Attachment
	Skipped  []string
}

// UploadArticleAttachment uploads one file to an article as multipart/form-data.
// The JSON content type is deliberately not set: YouTrack rejects the upload
// when it is.
func (c *Client) UploadArticleAttachment(ctx context.Context, articleID, name string, content io.Reader) (Attachment, error) {
	if articleID == "" {
		return Attachment{}, fmt.Errorf("%w: article id is empty", ErrInvalidInput)
	}
	return c.uploadAttachment(ctx, "/api/articles/"+url.PathEscape(articleID)+"/attachments", name, content)
}

// UploadIssueAttachment uploads one file to an issue, exactly as
// UploadArticleAttachment does for an article.
func (c *Client) UploadIssueAttachment(ctx context.Context, issueID, name string, content io.Reader) (Attachment, error) {
	if issueID == "" {
		return Attachment{}, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	return c.uploadAttachment(ctx, "/api/issues/"+url.PathEscape(issueID)+"/attachments", name, content)
}

// SyncArticleAttachments uploads the files an article's body references,
// skipping every one already attached under the same name with the same size.
// It lists the current attachments once, then uploads what is missing.
func (c *Client) SyncArticleAttachments(ctx context.Context, articleID string, fsys fs.FS, files []FileRef) (UploadResult, error) {
	if articleID == "" {
		return UploadResult{}, fmt.Errorf("%w: article id is empty", ErrInvalidInput)
	}
	return c.syncAttachments(ctx, fsys, files,
		func(ctx context.Context) ([]Attachment, error) { return c.ArticleAttachments(ctx, articleID) },
		func(ctx context.Context, name string, content io.Reader) (Attachment, error) {
			return c.UploadArticleAttachment(ctx, articleID, name, content)
		})
}

// SyncIssueAttachments is SyncArticleAttachments for an issue.
func (c *Client) SyncIssueAttachments(ctx context.Context, issueID string, fsys fs.FS, files []FileRef) (UploadResult, error) {
	if issueID == "" {
		return UploadResult{}, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	return c.syncAttachments(ctx, fsys, files,
		func(ctx context.Context) ([]Attachment, error) { return c.Attachments(ctx, issueID) },
		func(ctx context.Context, name string, content io.Reader) (Attachment, error) {
			return c.UploadIssueAttachment(ctx, issueID, name, content)
		})
}

// syncAttachments is the body shared by the article and the issue sync: list,
// skip what matches by name and size, upload the rest.
func (c *Client) syncAttachments(
	ctx context.Context,
	fsys fs.FS,
	files []FileRef,
	list func(context.Context) ([]Attachment, error),
	upload func(context.Context, string, io.Reader) (Attachment, error),
) (UploadResult, error) {
	var result UploadResult
	if len(files) == 0 {
		return result, nil
	}
	if fsys == nil {
		return result, fmt.Errorf("%w: no file system was supplied for the uploads", ErrInvalidInput)
	}

	existing, err := list(ctx)
	if err != nil {
		return result, err
	}
	sizes := make(map[string]int64, len(existing))
	for _, attachment := range existing {
		sizes[attachment.Name] = attachment.Size
	}

	for _, file := range files {
		name := strings.TrimSpace(file.Name)
		if name == "" {
			return result, fmt.Errorf("%w: an attachment of %q has no name", ErrInvalidInput, file.Path)
		}
		if !fs.ValidPath(file.Path) {
			return result, fmt.Errorf("%w: attachment path %q is not a valid path", ErrInvalidInput, file.Path)
		}
		info, err := fs.Stat(fsys, file.Path)
		if err != nil {
			return result, fmt.Errorf("youtrack: reading the attachment %q: %w", file.Path, err)
		}
		if size, ok := sizes[name]; ok && size == info.Size() {
			result.Skipped = append(result.Skipped, name)
			continue
		}
		if info.Size() > MaxAttachmentSize {
			return result, fmt.Errorf("%w: attachment %q is %d bytes, over the %d byte limit",
				ErrInvalidInput, name, info.Size(), int64(MaxAttachmentSize))
		}
		uploaded, err := uploadFromFS(ctx, fsys, file.Path, name, upload)
		if err != nil {
			return result, err
		}
		result.Uploaded = append(result.Uploaded, uploaded)
		// A second reference to the same name must not upload twice.
		sizes[name] = info.Size()
	}
	return result, nil
}

// uploadFromFS opens one file and hands it to upload, closing it in every case.
func uploadFromFS(
	ctx context.Context,
	fsys fs.FS,
	path, name string,
	upload func(context.Context, string, io.Reader) (Attachment, error),
) (Attachment, error) {
	handle, err := fsys.Open(path)
	if err != nil {
		return Attachment{}, fmt.Errorf("youtrack: opening the attachment %q: %w", path, err)
	}
	defer closeBody(handle)
	return upload(ctx, name, handle)
}

// uploadAttachment posts one multipart/form-data body to an attachments
// collection. The request carries the multipart content type and its boundary,
// never the JSON one, which is why it does not go through do.
func (c *Client) uploadAttachment(ctx context.Context, path, name string, content io.Reader) (Attachment, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Attachment{}, fmt.Errorf("%w: the attachment has no name", ErrInvalidInput)
	}
	if content == nil {
		return Attachment{}, fmt.Errorf("%w: attachment %q has no content", ErrInvalidInput, name)
	}
	payload, contentType, err := multipartBody(name, content)
	if err != nil {
		return Attachment{}, err
	}

	target := c.requestURL(path, fieldsQuery(AttachmentUploadFields, nil))
	//nolint:bodyclose // sendTarget returns an unread body; it is closed below.
	resp, err := c.sendTarget(ctx, http.MethodPost, target, path, payload, acceptJSON, contentType)
	if err != nil {
		return Attachment{}, err
	}
	defer closeBody(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Attachment{}, c.apiError(resp, http.MethodPost, path)
	}
	return decodeUploadedAttachment(resp.Body, name, path)
}

// decodeUploadedAttachment accepts both shapes the endpoint is documented to
// return: the created attachment as an object, or a one-element array of them.
// An empty body is not an error — the upload succeeded, only its echo is
// missing — so the name that was sent is returned instead.
func decodeUploadedAttachment(body io.Reader, name, path string) (Attachment, error) {
	raw, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return Attachment{}, fmt.Errorf("youtrack: reading the response of POST %s: %w", path, err)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return Attachment{Name: name}, nil
	}
	if trimmed[0] == '[' {
		var many []Attachment
		if err := json.Unmarshal(trimmed, &many); err != nil {
			return Attachment{}, fmt.Errorf("youtrack: decoding the response of POST %s: %w", path, err)
		}
		if len(many) == 0 {
			return Attachment{Name: name}, nil
		}
		return many[0], nil
	}
	var one Attachment
	if err := json.Unmarshal(trimmed, &one); err != nil {
		return Attachment{}, fmt.Errorf("youtrack: decoding the response of POST %s: %w", path, err)
	}
	return one, nil
}

// multipartBody buffers content into a multipart/form-data body and returns it
// with the content type that carries the generated boundary. Buffering is
// required: a retried attempt has to send the same bytes again.
func multipartBody(name string, content io.Reader) (body []byte, contentType string, err error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile(name, name)
	if err != nil {
		return nil, "", fmt.Errorf("youtrack: building the upload of %q: %w", name, err)
	}
	written, err := io.Copy(part, io.LimitReader(content, MaxAttachmentSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("youtrack: reading the content of %q: %w", name, err)
	}
	if written > MaxAttachmentSize {
		return nil, "", fmt.Errorf("%w: attachment %q is over the %d byte limit",
			ErrInvalidInput, name, int64(MaxAttachmentSize))
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("youtrack: building the upload of %q: %w", name, err)
	}
	return buf.Bytes(), writer.FormDataContentType(), nil
}
