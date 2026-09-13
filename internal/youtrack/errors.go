package youtrack

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Sentinel errors a caller can test with errors.Is. The settings UI turns each
// of them into a different message, which is why the distinction exists.
var (
	// ErrUnauthorized is returned for HTTP 401: the token is missing, wrong or
	// expired.
	ErrUnauthorized = errors.New("youtrack: unauthorized: the permanent token was rejected")
	// ErrForbidden is returned for HTTP 403: the token authenticates but the
	// account lacks the permission the endpoint needs.
	ErrForbidden = errors.New("youtrack: forbidden: the account lacks permission for this resource")
	// ErrNotFound is returned for HTTP 404. On an /api/... path this most often
	// means the base URL is missing its context path, for example /youtrack.
	ErrNotFound = errors.New("youtrack: not found: check that the base URL includes the instance context path")
	// ErrRateLimited is returned for HTTP 429 once the retry budget is spent.
	ErrRateLimited = errors.New("youtrack: rate limited: the instance is throttling this client")
	// ErrInvalidInput is returned when a call is rejected locally, before any
	// request is made.
	ErrInvalidInput = errors.New("youtrack: invalid input")
)

// maxErrorBody caps how much of a failing response body is kept in an APIError,
// so a stray HTML error page does not end up in a log line.
const maxErrorBody = 512

// APIError is the error returned for every non-2xx response. It unwraps to one
// of the sentinels above when the status has one, so both errors.Is and a
// status check work. Body is redacted and truncated; it never contains the
// token.
type APIError struct {
	Status int
	Method string
	// Path is the request path only. Query strings are excluded because they
	// may carry signed attachment parameters.
	Path string
	Body string
}

// Error implements error. It never renders the token.
func (e *APIError) Error() string {
	msg := fmt.Sprintf("youtrack: %s %s: %d %s", e.Method, e.Path, e.Status, http.StatusText(e.Status))
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Unwrap maps the status onto a sentinel so errors.Is(err, ErrNotFound) works.
func (e *APIError) Unwrap() error {
	switch e.Status {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusTooManyRequests:
		return ErrRateLimited
	default:
		return nil
	}
}

// redactToken replaces every occurrence of the token, and of anything that
// looks like a bearer credential, with a placeholder. It is applied to every
// string that could end up in an error or a log line.
func redactToken(s, token string) string {
	const placeholder = "[redacted]"
	if token != "" {
		s = strings.ReplaceAll(s, token, placeholder)
		// A permanent token is "perm:<user>.<workspace>.<secret>"; the secret
		// alone must not survive either.
		if i := strings.LastIndex(token, "."); i >= 0 && i+1 < len(token) {
			s = strings.ReplaceAll(s, token[i+1:], placeholder)
		}
	}
	return redactBearer(s)
}

// redactBearer removes any "Bearer <credential>" sequence a server might echo
// back in an error body.
func redactBearer(s string) string {
	const marker = "Bearer "
	var b strings.Builder
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		rest := s[i+len(marker):]
		end := strings.IndexAny(rest, " \t\r\n\"'")
		if end < 0 {
			end = len(rest)
		}
		b.WriteString(s[:i+len(marker)])
		b.WriteString("[redacted]")
		s = rest[end:]
	}
}

// truncate shortens s to at most n bytes, marking that it was cut.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…(truncated)"
}
