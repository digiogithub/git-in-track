package youtrack

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	resp, err := c.sendTarget(ctx, http.MethodGet, target, path, nil, acceptAny)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer closeBody(resp.Body)
		return nil, c.apiError(resp, http.MethodGet, path)
	}
	return resp.Body, nil
}
