package pando

import (
	"context"
	"errors"
	"fmt"
)

// Sentinel errors. Every exported call returns one of these (or an error that
// unwraps to one) so the HTTP layer above can map a failure onto a status code
// without matching on strings and without importing an MCP type.
var (
	// ErrNotConfigured is returned when the client has no endpoint for the
	// call: an empty MCP URL for a tool call, an empty REST URL for ReindexKB.
	// It is the "this feature is switched off" answer, not a failure.
	ErrNotConfigured = errors.New("pando: not configured")

	// ErrInvalidOptions is returned by New for options that cannot produce a
	// working client. It is never returned by a call.
	ErrInvalidOptions = errors.New("pando: invalid options")

	// ErrRemoteRefused is returned by New for a URL whose host is not a
	// loopback address while Options.AllowRemote is false. It unwraps to
	// ErrInvalidOptions.
	ErrRemoteRefused = fmt.Errorf("%w: refusing a non-loopback Pando URL", ErrInvalidOptions)

	// ErrUnreachable is returned when Pando could not be reached or answered
	// with something this client cannot use: connection refused, a dead
	// session that would not rebuild, a malformed result.
	ErrUnreachable = errors.New("pando: unreachable")

	// ErrUnreadable is returned when Pando answered with something this
	// client cannot decode: a malformed result, or a cached response
	// (cache.go) that could not be paged back. It unwraps to ErrUnreachable,
	// so IsUnavailable holds for it: a result that cannot be read is as
	// absent as one that never arrived.
	ErrUnreadable = fmt.Errorf("%w: the result could not be read", ErrUnreachable)

	// ErrUnauthorized is returned when Pando rejected the credential: a 401 or
	// 403 from the MCP transport or from the REST surface.
	ErrUnauthorized = errors.New("pando: unauthorized")

	// ErrTimeout is returned when a call did not finish inside its deadline.
	// It is deliberately distinct from ErrUnreachable: a slow Pando is not a
	// missing one. It unwraps to context.DeadlineExceeded.
	ErrTimeout = errors.New("pando: call timed out")

	// ErrToolFailed is returned when Pando ran the tool and the tool itself
	// reported an error. It is distinct from an empty result set, which is a
	// successful call returning no hits.
	ErrToolFailed = errors.New("pando: tool reported an error")

	// ErrReindexRunning is returned by ReindexKB for HTTP 409: another reindex
	// is already walking the corpus. The caller should retry later rather than
	// treat it as a failure.
	ErrReindexRunning = errors.New("pando: a knowledge base reindex is already running")
)

// toolError carries the message Pando's tool put in its text content. It
// unwraps to ErrToolFailed.
type toolError struct {
	Tool    string
	Message string
}

func (e *toolError) Error() string {
	if e.Message == "" {
		return "pando: tool " + e.Tool + " reported an error"
	}
	return "pando: tool " + e.Tool + ": " + e.Message
}

func (e *toolError) Unwrap() error { return ErrToolFailed }

// httpError carries a non-2xx status from the REST surface. It unwraps to the
// sentinel that matches the status.
type httpError struct {
	Status int
	Method string
	Path   string
	Body   string
}

func (e *httpError) Error() string {
	msg := "pando: " + e.Method + " " + e.Path + ": " + statusText(e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

func (e *httpError) Unwrap() error {
	switch e.Status {
	case 401, 403:
		return ErrUnauthorized
	case 409:
		return ErrReindexRunning
	case 503:
		return ErrNotConfigured
	default:
		return ErrUnreachable
	}
}

// IsUnavailable reports whether err means Pando could not answer at all: it is
// not configured, not reachable, refused the credential or ran out of time.
// Callers map it onto the `unavailable` code and degrade (an impact resolver
// falls back to what it can compute without the code graph). It is false for
// ErrToolFailed and ErrInvalidOptions, which are answers, not absences.
func IsUnavailable(err error) bool {
	return errors.Is(err, ErrNotConfigured) ||
		errors.Is(err, ErrUnreachable) ||
		errors.Is(err, ErrUnauthorized) ||
		errors.Is(err, ErrTimeout)
}

// Reason is a short, fixed sentence for an error IsUnavailable holds for, fit
// for a report that must be byte-for-byte reproducible: the impact tiers put
// it in their message. It never quotes the underlying error, whose text can
// carry a Pando cache id, an address or anything else that changes from one
// run to the next (GIT-US-0164). It returns "" for any other error, which the
// caller reports as itself.
func Reason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotConfigured):
		return "Pando is not configured"
	case errors.Is(err, ErrUnauthorized):
		return "Pando rejected the token"
	case errors.Is(err, ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "Pando did not answer in time"
	case errors.Is(err, ErrUnreadable):
		return "Pando answered with a result this client cannot read"
	case errors.Is(err, ErrUnreachable):
		return "Pando is unreachable"
	}
	return ""
}
