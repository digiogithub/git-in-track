package youtrack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Defaults applied by New when Options leaves a field at its zero value.
const (
	// DefaultRate is the shared outbound limit, in requests per second.
	DefaultRate = 5.0
	// DefaultMaxRetries is how many times a retryable response is retried,
	// on top of the first attempt.
	DefaultMaxRetries = 4
	// DefaultRetryBase is the first backoff step; it doubles per attempt.
	DefaultRetryBase = 500 * time.Millisecond
	// DefaultTimeout is the per-request timeout of the fallback HTTP client.
	DefaultTimeout = 30 * time.Second
	// DefaultTop is the page size used when a Page leaves Top unset.
	DefaultTop = 100
	// MaxTop is the largest page size this package will send. Wider pages are
	// accepted by YouTrack but time out with a wide fields selector.
	MaxTop = 200
)

// Options configures a Client. The zero value of every field is usable: it
// yields a client with the default rate limit, retry policy and HTTP client.
type Options struct {
	// BaseURL is the instance URL, optionally with a context path, for example
	// https://yt.example.com/youtrack. Trailing slashes are trimmed.
	BaseURL string
	// Token is the permanent token, including its "perm:" prefix.
	Token string
	// HTTPClient is the transport seam. Tests inject the client of an
	// httptest.Server here. Defaults to a client with DefaultTimeout.
	HTTPClient *http.Client
	// Rate is the shared outbound limit in requests per second. Zero selects
	// DefaultRate; a negative value disables throttling entirely.
	Rate float64
	// MaxRetries bounds the retries after the first attempt. Zero selects
	// DefaultMaxRetries; a negative value disables retrying.
	MaxRetries int
	// RetryBase is the first backoff step. Zero selects DefaultRetryBase.
	RetryBase time.Duration
	// Now is the clock seam. Defaults to time.Now.
	Now func() time.Time
	// Sleep is the delay seam, so tests never sleep for real. It must return
	// early with the context error when ctx is cancelled. Defaults to a
	// context-aware time.Timer sleep.
	Sleep func(ctx context.Context, d time.Duration) error
	// Jitter returns a value in [0,1) used to spread retries. Defaults to
	// math/rand/v2 Float64.
	Jitter func() float64
}

// String renders the options without the token.
func (o Options) String() string {
	return fmt.Sprintf("youtrack.Options{BaseURL: %q, Token: %q, Rate: %g, MaxRetries: %d}",
		o.BaseURL, tokenMask(o.Token), o.Rate, o.MaxRetries)
}

// tokenMask renders the presence of a token without any of its bytes.
func tokenMask(token string) string {
	if token == "" {
		return "<unset>"
	}
	return "[redacted]"
}

// Client talks to one YouTrack instance. It is safe for concurrent use: the
// rate limiter it owns is shared by every caller.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	limiter    *limiter
	maxRetries int
	retryBase  time.Duration
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration) error
	jitter     func() float64
}

// New validates the options and builds a Client.
func New(opts Options) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: base URL is empty", ErrInvalidInput)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("%w: base URL is not a URL: %s", ErrInvalidInput, redactToken(err.Error(), opts.Token))
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: base URL scheme must be http or https, got %q", ErrInvalidInput, parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: base URL has no host", ErrInvalidInput)
	}
	if strings.TrimSpace(opts.Token) == "" {
		return nil, fmt.Errorf("%w: token is empty", ErrInvalidInput)
	}

	c := &Client{
		baseURL:    base,
		token:      strings.TrimSpace(opts.Token),
		httpClient: opts.HTTPClient,
		maxRetries: opts.MaxRetries,
		retryBase:  opts.RetryBase,
		now:        opts.Now,
		sleep:      opts.Sleep,
		jitter:     opts.Jitter,
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	if c.maxRetries == 0 {
		c.maxRetries = DefaultMaxRetries
	}
	if c.maxRetries < 0 {
		c.maxRetries = 0
	}
	if c.retryBase <= 0 {
		c.retryBase = DefaultRetryBase
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}
	if c.jitter == nil {
		c.jitter = rand.Float64
	}

	rateValue := opts.Rate
	if rateValue == 0 {
		rateValue = DefaultRate
	}
	c.limiter = newLimiter(rateValue, c.now, c.sleep)
	return c, nil
}

// BaseURL returns the normalized base URL, context path included.
func (c *Client) BaseURL() string { return c.baseURL }

// String renders the client without the token.
func (c *Client) String() string {
	return fmt.Sprintf("youtrack.Client{BaseURL: %q, Token: %q, MaxRetries: %d}",
		c.baseURL, tokenMask(c.token), c.maxRetries)
}

// requestURL builds an absolute URL by concatenating the trimmed base URL with
// a path that starts with "/api/". Concatenation is deliberate: resolving path
// as a reference against the base would drop the instance context path.
func (c *Client) requestURL(path string, query url.Values) string {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return target
}

// fieldsQuery builds the common query form: a fields selector plus extras.
func fieldsQuery(fields string, extra url.Values) url.Values {
	q := url.Values{}
	for k, v := range extra {
		// YouTrack ignores empty parameters but sending them wastes bytes and
		// confuses the query parser for the "query" parameter.
		if len(v) == 0 || v[0] == "" {
			continue
		}
		q[k] = v
	}
	if fields != "" {
		q.Set("fields", fields)
	}
	return q
}

// get issues a GET and decodes the response into out.
func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// post issues a POST with a JSON body and decodes the response into out.
func (c *Client) post(ctx context.Context, path string, query url.Values, body, out any) error {
	return c.do(ctx, http.MethodPost, path, query, body, out)
}

// do performs one request through the limiter and the retry loop, then decodes
// the response body into out when out is non-nil.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("youtrack: encoding the request body for %s %s: %w", method, path, err)
		}
		payload = encoded
	}

	//nolint:bodyclose // send returns an unread body; it is closed below and on every retry.
	resp, err := c.send(ctx, method, path, query, payload)
	if err != nil {
		return err
	}
	defer closeBody(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return c.apiError(resp, method, path)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		drain(resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("youtrack: decoding the response of %s %s: %w", method, path, err)
	}
	return nil
}

// apiError reads the failing body, redacts it and builds the typed error.
func (c *Client) apiError(resp *http.Response, method, path string) error {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody*4))
	msg := ""
	if err == nil {
		msg = truncate(redactToken(string(raw), c.token), maxErrorBody)
	}
	return &APIError{Status: resp.StatusCode, Method: method, Path: path, Body: msg}
}

// newRequest builds one attempt. It is a method so that the auth headers are
// set in exactly one place. contentType is sent only when there is a payload;
// a multipart upload passes its own boundary-carrying type, which is why this
// is a parameter and not a constant.
func (c *Client) newRequest(ctx context.Context, method, target string, payload []byte, accept, contentType string) (*http.Request, error) {
	var reader io.Reader = http.NoBody
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("youtrack: building the %s request: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", accept)
	if payload != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

// Accept header values: JSON for the REST surface, anything for a binary
// attachment download. contentTypeJSON is the request type of every call that
// sends a JSON body; an attachment upload deliberately does not use it.
const (
	acceptJSON      = "application/json"
	acceptAny       = "*/*"
	contentTypeJSON = "application/json"
)

// closeBody closes a response body, ignoring the close error: the request has
// already produced its result by then.
func closeBody(body io.ReadCloser) {
	_ = body.Close()
}

// drain reads the rest of a body so the underlying connection is reusable.
func drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 1<<20))
}

// sleepContext is the default Sleep seam: it waits for d but gives up as soon
// as ctx is done.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return ctxErr(err)
		}
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("youtrack: waiting to retry: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
