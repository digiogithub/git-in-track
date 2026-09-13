package syncengine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Errors the engine itself returns.
var (
	// ErrClosed is returned by Enqueue and the mutating calls once Close has
	// been called.
	ErrClosed = errors.New("syncengine: the engine is closed")
	// ErrNotStarted is returned when a job is enqueued before Start.
	ErrNotStarted = errors.New("syncengine: the engine has not been started")
	// ErrDuplicateKind is returned when a kind is registered twice.
	ErrDuplicateKind = errors.New("syncengine: a handler is already registered for this kind")
	// ErrNoHandler is returned when a job is enqueued for a kind nobody
	// registered, and is the terminal failure of a replayed job whose kind has
	// disappeared.
	ErrNoHandler = errors.New("syncengine: no handler is registered for this kind")
	// ErrUnknownJob is returned when an id does not name a job the engine
	// remembers.
	ErrUnknownJob = errors.New("syncengine: unknown job")
	// ErrBadTransition is returned when something attempts a state change the
	// state machine does not allow. Reaching it is a bug in the engine.
	ErrBadTransition = errors.New("syncengine: illegal state transition")
	// ErrNotRetryable is returned by RetryDeadLetter for a job that is not in
	// the dead-letter list.
	ErrNotRetryable = errors.New("syncengine: the job is not in the dead-letter list")
	// ErrInvalidOption is returned by New for a setting that cannot be honored.
	ErrInvalidOption = errors.New("syncengine: invalid option")
)

// ErrTerminal marks a handler error as final: wrap an error with it, or return
// it directly, and the engine fails the job on the spot instead of spending
// attempts on something that cannot improve. A rejected token and a malformed
// payload are both terminal.
var ErrTerminal = errors.New("terminal")

// ErrRetry marks a handler error as worth another attempt even though nothing
// else about it says so.
var ErrRetry = errors.New("retryable")

// ErrorClass is how the engine reads a handler error.
type ErrorClass string

// The error classes. A job's next move follows directly from its class.
const (
	// ClassRetryable means another attempt might succeed: a 429, a 5xx, a
	// transport failure.
	ClassRetryable ErrorClass = "retryable"
	// ClassTerminal means no number of attempts will help: 401, 403, 404, a
	// validation error, an unregistered kind.
	ClassTerminal ErrorClass = "terminal"
	// ClassCancelled means the work was abandoned rather than failed, because
	// the job or the engine was cancelled.
	ClassCancelled ErrorClass = "cancelled"
)

// Classification is what the engine learned from one handler error.
type Classification struct {
	// Class decides whether the job retries, fails or is abandoned.
	Class ErrorClass
	// RetryAfter is the delay the error asked for. When non-zero it overrides
	// the computed backoff, always.
	RetryAfter time.Duration
	// Status is the HTTP status the error carried, zero when it carried none.
	// It is kept for the error record, not used for anything else.
	Status int
}

// The interfaces the default classifier understands. They are declared here, on
// purpose: the engine is generic infrastructure and must not import the
// YouTrack client — or any other client — to know what a 429 is. A handler
// either returns an error implementing one of these, or wraps the client's
// error with [Terminal] or [RetryAfter].
type (
	// StatusCoder is an error that carries an HTTP status.
	StatusCoder interface{ StatusCode() int }
	// RetryableError says outright whether another attempt is worthwhile.
	RetryableError interface{ Retryable() bool }
	// TerminalError says outright that no further attempt is worthwhile.
	TerminalError interface{ Terminal() bool }
	// RetryAfterProvider carries a delay the remote asked for, already parsed.
	RetryAfterProvider interface{ RetryAfter() time.Duration }
	// RetryAfterHeaderProvider carries the raw Retry-After header, in either
	// the delta-seconds or the HTTP-date form.
	RetryAfterHeaderProvider interface{ RetryAfterHeader() string }
)

// Classify reads a handler error. The order is deliberate: an explicit answer
// from the error wins over a status, a status wins over a guess, and an error
// that says nothing at all is treated as retryable, because a transport failure
// is the commonest error that says nothing and it is exactly the one worth
// repeating.
func Classify(err error, now time.Time) Classification {
	out := Classification{Class: ClassRetryable}
	if err == nil {
		return out
	}

	if errors.Is(err, context.Canceled) {
		return Classification{Class: ClassCancelled}
	}

	var header RetryAfterHeaderProvider
	if errors.As(err, &header) {
		if d, ok := parseRetryAfter(header.RetryAfterHeader(), now); ok {
			out.RetryAfter = d
		}
	}
	var after RetryAfterProvider
	if errors.As(err, &after) {
		if d := after.RetryAfter(); d > 0 {
			out.RetryAfter = d
		}
	}

	var coder StatusCoder
	if errors.As(err, &coder) {
		out.Status = coder.StatusCode()
		out.Class = ClassifyStatus(out.Status)
	}

	// Explicit answers from the error override anything inferred above.
	var terminal TerminalError
	if errors.As(err, &terminal) && terminal.Terminal() {
		out.Class = ClassTerminal
	}
	var retryable RetryableError
	if errors.As(err, &retryable) {
		if retryable.Retryable() {
			out.Class = ClassRetryable
		} else {
			out.Class = ClassTerminal
		}
	}

	// The sentinels win over everything: they are how a handler states its
	// intent when the underlying error cannot.
	switch {
	case errors.Is(err, ErrTerminal), errors.Is(err, ErrNoHandler):
		out.Class = ClassTerminal
	case errors.Is(err, ErrRetry):
		out.Class = ClassRetryable
	case errors.Is(err, context.DeadlineExceeded):
		out.Class = ClassRetryable
	}

	// A transport error is worth repeating: it says nothing about the request,
	// only about the wire, and the default class already reflects that.
	var netErr net.Error
	if out.Status == 0 && out.Class == ClassTerminal && errors.As(err, &netErr) && netErr.Timeout() {
		out.Class = ClassRetryable
	}
	return out
}

// ClassifyStatus maps an HTTP status onto a class. 429, 408 and every 5xx are
// worth another attempt; every other 4xx is not — retrying a rejected token
// five times only locks the account out faster.
func ClassifyStatus(status int) ErrorClass {
	switch {
	case status == 0:
		return ClassRetryable
	case status == 408 || status == 425 || status == 429:
		return ClassRetryable
	case status >= 500:
		return ClassRetryable
	case status >= 400:
		return ClassTerminal
	default:
		return ClassRetryable
	}
}

// Terminal wraps err so the engine fails the job without spending further
// attempts on it. It is the wrapper a handler reaches for when the client it
// called reports a rejected credential or a request that can never be valid.
func Terminal(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrTerminal, err)
}

// RetryAfter wraps err so the engine retries the job after exactly d, whatever
// the backoff ladder would have computed. It is the wrapper a handler reaches
// for when the remote sent a Retry-After header.
func RetryAfter(err error, d time.Duration) error {
	if err == nil {
		return nil
	}
	return &retryAfterError{err: err, after: d}
}

// retryAfterError carries a delay the remote asked for.
type retryAfterError struct {
	err   error
	after time.Duration
}

// Error renders the wrapped error with the delay it asked for.
func (e *retryAfterError) Error() string {
	return fmt.Sprintf("%s (retry after %s)", e.err.Error(), e.after)
}

// Unwrap exposes the wrapped error to errors.Is and errors.As.
func (e *retryAfterError) Unwrap() error { return e.err }

// RetryAfter reports the delay the remote asked for.
func (e *retryAfterError) RetryAfter() time.Duration { return e.after }

// Retryable reports that a Retry-After always means "try again later".
func (e *retryAfterError) Retryable() bool { return true }

// parseRetryAfter accepts both documented forms of the header: delta-seconds
// and an HTTP date. It returns false for anything it cannot read, so a
// nonsensical header falls back to the computed backoff rather than to zero.
func parseRetryAfter(header string, now time.Time) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	for _, layout := range httpDateLayouts {
		t, err := time.Parse(layout, header)
		if err != nil {
			continue
		}
		d := t.Sub(now)
		if d < 0 {
			// The moment has passed: retry now, not never.
			return 0, true
		}
		return d, true
	}
	return 0, false
}

// httpDateLayouts are the three date formats RFC 9110 requires a client to
// accept. They are spelled out here rather than pulled from net/http so the
// engine keeps no HTTP dependency at all.
var httpDateLayouts = []string{
	time.RFC1123,
	time.RFC850,
	time.ANSIC,
	"Mon, 02 Jan 2006 15:04:05 GMT",
}

// sensitiveKeys are the JSON field names whose value is replaced wholesale
// before anything is written to the journal or to an error record.
var sensitiveKeys = []string{
	"token", "accesstoken", "refreshtoken", "permtoken", "apikey", "api_key",
	"password", "passwd", "secret", "credential", "credentials",
	"authorization", "auth", "bearer", "cookie", "session", "privatekey",
}

// redacted is what every removed credential is replaced with.
const redacted = "[redacted]"

// redactString removes the credentials the engine can recognize from a free
// text string: the userinfo of a URL, a bearer token, and a `key=value` or
// `"key": "value"` pair whose key is sensitive. It is applied to every error
// text the engine stores, because an error string is the likeliest place for a
// token to leak into the journal.
func redactString(s string) string {
	if s == "" {
		return s
	}
	s = redactURLCredentials(s)
	s = redactBearer(s)
	s = redactPairs(s)
	return s
}

// redactURLCredentials rewrites every URL carrying a user:password.
func redactURLCredentials(s string) string {
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "://")
		if i < 0 {
			b.WriteString(rest)
			return b.String()
		}
		start := i
		for start > 0 && isURLSchemeByte(rest[start-1]) {
			start--
		}
		end := strings.IndexAny(rest[i:], " \t\r\n\"'")
		if end < 0 {
			end = len(rest)
		} else {
			end += i
		}
		raw := rest[start:end]
		b.WriteString(rest[:start])
		if u, err := url.Parse(raw); err == nil && u.User != nil {
			u.User = url.User(redacted)
			b.WriteString(u.String())
		} else {
			b.WriteString(raw)
		}
		rest = rest[end:]
	}
}

// isURLSchemeByte reports whether c can be part of a URL scheme.
func isURLSchemeByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '+' || c == '-' || c == '.'
}

// redactBearer removes an Authorization value a remote echoed back.
func redactBearer(s string) string {
	const marker = "Bearer "
	out := s
	for i := 0; ; {
		j := strings.Index(out[i:], marker)
		if j < 0 {
			return out
		}
		j += i
		rest := out[j+len(marker):]
		end := strings.IndexAny(rest, " \t\r\n\"',)")
		if end < 0 {
			end = len(rest)
		}
		out = out[:j+len(marker)] + redacted + rest[end:]
		i = j + len(marker) + len(redacted)
	}
}

// redactPairs replaces the value of every sensitive key written as `key=value`,
// `key: value` or `"key":"value"`. It is one forward pass, so a string with
// many credentials in it costs no more than a string with one.
func redactPairs(s string) string {
	var b strings.Builder
	lower := strings.ToLower(s)
	for i := 0; i < len(s); {
		key := matchKey(lower, i)
		if key == "" {
			b.WriteByte(s[i])
			i++
			continue
		}
		start, end, ok := valueSpan(s, i+len(key))
		if !ok {
			b.WriteString(s[i : i+len(key)])
			i += len(key)
			continue
		}
		b.WriteString(s[i:start])
		b.WriteString(redacted)
		i = end
	}
	return b.String()
}

// matchKey returns the longest sensitive key starting at i on a word boundary.
func matchKey(lower string, i int) string {
	if i > 0 && isWordByte(lower[i-1]) {
		return ""
	}
	best := ""
	for _, k := range sensitiveKeys {
		if len(k) > len(best) && strings.HasPrefix(lower[i:], k) {
			best = k
		}
	}
	return best
}

// isWordByte reports whether c continues an identifier.
func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// valueSpan finds the bounds of the value that follows a key at position i,
// skipping the separator and any quoting.
func valueSpan(s string, i int) (start, end int, ok bool) {
	for i < len(s) && (s[i] == '"' || s[i] == ' ') {
		i++
	}
	if i >= len(s) || (s[i] != '=' && s[i] != ':') {
		return 0, 0, false
	}
	i++
	for i < len(s) && (s[i] == ' ' || s[i] == '"') {
		i++
	}
	start = i
	for i < len(s) && !strings.ContainsRune(" \t\r\n\"',}&", rune(s[i])) {
		i++
	}
	return start, i, start < i
}
