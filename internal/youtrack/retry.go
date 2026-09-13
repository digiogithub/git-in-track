package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// retryableStatus reports whether a status code is worth another attempt.
// Every other 4xx, 400 included, is a caller mistake and fails immediately.
func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// send performs the request, retrying transport errors and retryable statuses
// until the budget is spent. The returned response, when the error is nil, has
// an unread body that the caller must close.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, payload []byte) (*http.Response, error) {
	return c.sendTarget(ctx, method, c.requestURL(path, query), path, payload, acceptJSON)
}

// sendTarget is send against an already-built absolute URL. path is carried
// separately so errors name the endpoint without its query string, which may
// hold a signed attachment parameter.
func (c *Client) sendTarget(ctx context.Context, method, target, path string, payload []byte, accept string) (*http.Response, error) {
	var lastErr error

	for attempt := 0; ; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return nil, err
		}
		req, err := c.newRequest(ctx, method, target, payload, accept)
		if err != nil {
			return nil, err
		}

		resp, err := c.httpClient.Do(req) //nolint:bodyclose // closed by the caller, drained before a retry.
		switch {
		case err != nil:
			lastErr = fmt.Errorf("youtrack: %s %s: %w", method, path, redactURLError(err, c.token))
		case retryableStatus(resp.StatusCode):
			if attempt >= c.maxRetries {
				// Hand the final failing response back so the caller can read
				// the body into a typed error.
				return resp, nil
			}
			delay := c.retryDelay(resp, attempt)
			// Drain before retrying so the connection is reused.
			drain(resp.Body)
			closeBody(resp.Body)
			if err := c.sleep(ctx, delay); err != nil {
				return nil, err
			}
			continue
		default:
			return resp, nil
		}

		if attempt >= c.maxRetries {
			return nil, lastErr
		}
		if err := c.sleep(ctx, c.backoff(attempt)); err != nil {
			return nil, err
		}
	}
}

// retryDelay honors Retry-After when the server sent one, and otherwise backs
// off exponentially.
func (c *Client) retryDelay(resp *http.Response, attempt int) time.Duration {
	if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), c.now()); ok {
		return d
	}
	return c.backoff(attempt)
}

// backoff is retryBase * 2^attempt plus up to retryBase of jitter.
func (c *Client) backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 16 {
		attempt = 16
	}
	step := c.retryBase << uint(attempt)
	jitter := time.Duration(c.jitter() * float64(c.retryBase))
	return step + jitter
}

// parseRetryAfter accepts both forms of the header: delta-seconds and an
// HTTP-date. A date already in the past clamps to zero.
func parseRetryAfter(header string, now time.Time) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(header, 64); err == nil {
		if seconds < 0 {
			return 0, true
		}
		return time.Duration(seconds * float64(time.Second)), true
	}
	if when, err := http.ParseTime(header); err == nil {
		d := when.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// redactURLError strips the token from a transport error, whose message
// contains the request URL.
func redactURLError(err error, token string) error {
	msg := redactToken(err.Error(), token)
	if msg == err.Error() {
		return err
	}
	return fmt.Errorf("%s", msg) //nolint:err113 // the original message carried the token.
}

// ctxErr wraps a context error so wrapcheck and errors.Is both stay happy.
func ctxErr(err error) error {
	return fmt.Errorf("youtrack: request cancelled: %w", err)
}
