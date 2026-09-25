package pando

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultTimeout is the per-call deadline used when Options.Timeout is zero.
// It is deliberately short: every call here sits inside an HTTP handler, and a
// Pando that is embedding a corpus must not be allowed to hold that handler
// open indefinitely.
const DefaultTimeout = 20 * time.Second

// clientName and clientVersion identify this client in the MCP initialize
// handshake, which is what shows up in Pando's session log.
const (
	clientName    = "git-in-track"
	clientVersion = "0.1.0"
)

// Options configures a Client. It is a plain struct rather than a
// config.Config value on purpose: this package is a transport seam and must
// not depend on the companion's configuration shape.
type Options struct {
	// MCPURL is Pando's streamable-HTTP MCP endpoint, for example
	// "http://127.0.0.1:9777/mcp". Empty disables every tool call: they
	// return ErrNotConfigured.
	MCPURL string
	// Token is the bearer token Pando's MCP HTTP transport requires
	// (MCPServer.HttpToken in Pando's configuration).
	Token string
	// RESTURL is the base URL of Pando's REST API, for example
	// "http://127.0.0.1:9778". Empty disables ReindexKB, which then returns
	// ErrNotConfigured.
	RESTURL string
	// RESTToken is sent as X-Pando-Token on REST calls.
	RESTToken string
	// ProjectID is the default indexed project for SearchCode. It is Pando's
	// sanitized identifier; SanitizeProjectID derives it from a path.
	ProjectID string
	// Timeout bounds one call end to end. Zero means DefaultTimeout.
	Timeout time.Duration
	// AllowRemote permits a URL whose host is not a loopback address. It
	// exists for a future Pando deployment that is actually authenticated and
	// is off by default; see the package doc for why loopback is the rule.
	AllowRemote bool
}

// Client is a typed, bounded client for Pando's search surface. It is safe for
// concurrent use: one MCP session is shared by every caller and rebuilt under a
// mutex when it dies.
//
// No MCP type appears in its exported surface. Replacing the transport with
// Pando's future REST search routes is a change to this file and search.go,
// and to nothing above the package boundary.
type Client struct {
	opts    Options
	timeout time.Duration
	// http carries the MCP bearer token; rest deliberately does not, so the
	// MCP token can never leak onto the REST surface, which uses its own.
	http *http.Client
	rest *http.Client

	// authFailed records that a request came back 401 or 403. The MCP SDK
	// flattens a non-2xx response into a bare error string, so the status is
	// captured in the round tripper instead of being matched on text.
	authFailed atomic.Bool

	mu      sync.Mutex
	session *mcpsdk.ClientSession
	closed  bool
}

// New validates opts and returns a client. It opens no connection: the MCP
// session is established on the first call that needs it.
//
// A URL whose host is not a loopback address is refused unless
// Options.AllowRemote is set, and nothing else in this package can override
// that refusal.
func New(opts Options) (*Client, error) {
	if err := checkEndpoint("search.pando.mcpUrl", opts.MCPURL, opts.AllowRemote); err != nil {
		return nil, err
	}
	if err := checkEndpoint("search.pando.restUrl", opts.RESTURL, opts.AllowRemote); err != nil {
		return nil, err
	}
	if opts.Timeout < 0 {
		return nil, fmt.Errorf("%w: search.pando.timeout must not be negative", ErrInvalidOptions)
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	c := &Client{opts: opts, timeout: timeout}
	c.http = &http.Client{Transport: &authTransport{client: c, base: http.DefaultTransport}}
	c.rest = &http.Client{}
	return c, nil
}

// Timeout reports the per-call deadline in force, after defaulting.
func (c *Client) Timeout() time.Duration { return c.timeout }

// ProjectID reports the default code project SearchCode uses when it is passed
// an empty project id.
func (c *Client) ProjectID() string { return c.opts.ProjectID }

// Close ends the MCP session, if one is open. It is idempotent, and a closed
// client refuses further calls with ErrNotConfigured.
func (c *Client) Close() error {
	c.mu.Lock()
	sess := c.session
	c.session = nil
	c.closed = true
	c.mu.Unlock()
	if sess == nil {
		return nil
	}
	if err := sess.Close(); err != nil {
		return fmt.Errorf("close the Pando session: %w", err)
	}
	return nil
}

// Health reports whether Pando is reachable and speaking MCP. It performs the
// initialize handshake if no session is open and then lists tools, so a server
// that accepts TCP but is not an MCP endpoint is reported as unhealthy. It
// never blocks longer than the configured timeout.
func (c *Client) Health(ctx context.Context) error {
	if c.opts.MCPURL == "" {
		return fmt.Errorf("%w: no Pando MCP URL", ErrNotConfigured)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	sess, fresh, err := c.acquire(ctx)
	if err != nil {
		return c.classify(ctx, err)
	}
	if _, err := sess.ListTools(ctx, nil); err != nil {
		if !fresh && isSessionDead(err) {
			c.discard(sess)
			sess, _, err2 := c.acquire(ctx)
			if err2 != nil {
				return c.classify(ctx, err2)
			}
			if _, err2 := sess.ListTools(ctx, nil); err2 != nil {
				return c.classify(ctx, err2)
			}
			return nil
		}
		return c.classify(ctx, err)
	}
	return nil
}

// call runs one tool and returns its text content. A session that died since
// the last call is rebuilt once, so a Pando restart heals itself instead of
// surfacing as a permanent failure.
func (c *Client) call(ctx context.Context, name string, args map[string]any) (*toolResult, error) {
	if c.opts.MCPURL == "" {
		return nil, fmt.Errorf("%w: no Pando MCP URL", ErrNotConfigured)
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	for attempt := 0; attempt < 2; attempt++ {
		sess, fresh, err := c.acquire(ctx)
		if err != nil {
			return nil, c.classify(ctx, err)
		}
		res, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			if !fresh && isSessionDead(err) && attempt == 0 {
				c.discard(sess)
				continue
			}
			return nil, c.classify(ctx, err)
		}
		out, err := newToolResult(name, res)
		if err != nil {
			return nil, err
		}
		if err := c.resolveCached(ctx, sess, out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: %s: the Pando session could not be rebuilt", ErrUnreachable, name)
}

// resolveCached replaces a cached-response stub with the full text Pando
// cached (see cache.go). A stub that cannot be paged back fails the call,
// unless the result also carries structuredContent.metadata, which Pando keeps
// intact on a stub: a decoder that reads the metadata still works, and one
// that needs the text gets the fixed error instead of the stub.
func (c *Client) resolveCached(ctx context.Context, sess *mcpsdk.ClientSession, r *toolResult) error {
	stub, isStub, err := parseCachedStub(r.text)
	if !isStub {
		return nil
	}
	if err == nil {
		var text string
		if text, err = c.followCache(ctx, sess, r.tool, stub); err == nil {
			r.text = text
			return nil
		}
	}
	if IsUnavailable(err) && !errors.Is(err, ErrUnreadable) {
		return err // the transport failed: that is the answer, metadata or not
	}
	if len(r.metadata) == 0 {
		return err
	}
	r.text, r.unread = "", err
	return nil
}

// acquire returns the shared session, opening one if needed. The boolean
// reports whether this call created it, which is how call() knows a reconnect
// would be pointless.
func (c *Client) acquire(ctx context.Context) (*mcpsdk.ClientSession, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, false, fmt.Errorf("%w: the Pando client is closed", ErrNotConfigured)
	}
	if c.session != nil {
		return c.session, false, nil
	}
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:   c.opts.MCPURL,
		HTTPClient: c.http,
		// Pando answers GET /mcp with 405, so the standalone SSE stream would
		// only produce noise; this client needs nothing but request/response.
		DisableStandaloneSSE: true,
		// One reconnect attempt is enough: the caller's deadline is short and
		// this package rebuilds a dead session itself.
		MaxRetries: -1,
	}
	sdkClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: clientName, Version: clientVersion}, nil)
	sess, err := sdkClient.Connect(ctx, transport, nil)
	if err != nil {
		return nil, false, fmt.Errorf("connect to the Pando MCP endpoint: %w", err)
	}
	c.session = sess
	return sess, true, nil
}

// discard drops a session that has died so the next call opens a new one.
func (c *Client) discard(dead *mcpsdk.ClientSession) {
	c.mu.Lock()
	if c.session == dead {
		c.session = nil
	}
	c.mu.Unlock()
	_ = dead.Close()
}

// classify maps a transport failure onto this package's sentinels.
func (c *Client) classify(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	}
	if c.authFailed.Load() {
		return fmt.Errorf("%w: Pando rejected the bearer token: %w", ErrUnauthorized, err)
	}
	return fmt.Errorf("%w: %w", ErrUnreachable, err)
}

// isSessionDead reports whether err means "this session is gone, open another"
// rather than "Pando is down".
func isSessionDead(err error) bool {
	switch {
	case errors.Is(err, mcpsdk.ErrSessionMissing),
		errors.Is(err, mcpsdk.ErrConnectionClosed),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, net.ErrClosed):
		return true
	}
	return strings.Contains(err.Error(), "connection reset by peer")
}

// authTransport adds the bearer token Pando's MCP HTTP transport requires and
// records an authentication failure, which the SDK would otherwise flatten into
// an unmatched error string.
type authTransport struct {
	client *Client
	base   http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token := t.client.opts.Token; token != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("call the Pando MCP endpoint: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		t.client.authFailed.Store(true)
	default:
		t.client.authFailed.Store(false)
	}
	return resp, nil
}

// checkEndpoint validates one configured URL. An empty URL is valid: it means
// the feature is switched off.
func checkEndpoint(field, raw string, allowRemote bool) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %s is not a URL: %w", ErrInvalidOptions, field, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: %s must be http or https, got %q", ErrInvalidOptions, field, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: %s has no host", ErrInvalidOptions, field)
	}
	if allowRemote {
		return nil
	}
	if !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("%w: %s points at %q; Pando must be reached over loopback (see internal/pando doc)",
			ErrRemoteRefused, field, u.Hostname())
	}
	return nil
}

// isLoopbackHost reports whether host names the local machine. A name that is
// not an IP literal is accepted only when it is exactly "localhost": resolving
// arbitrary names here would make the guard depend on DNS, which the operator
// of a remote Pando controls.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// SanitizeProjectID derives a Pando code-project identifier from a path or a
// name, using Pando's own rule (internal/llm/tools/remembrances_code.go,
// sanitizeProjectID): alphanumerics, underscore and hyphen survive; slash,
// backslash, space and dot collapse into a single underscore; everything else
// is dropped; leading and trailing underscores are trimmed. "/www/git-in-track"
// becomes "www_git-in-track".
func SanitizeProjectID(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			out = append(out, c)
		case c == '/', c == '\\', c == ' ', c == '.':
			if len(out) > 0 && out[len(out)-1] != '_' {
				out = append(out, '_')
			}
		}
	}
	for len(out) > 0 && out[0] == '_' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == '_' {
		out = out[:len(out)-1]
	}
	return string(out)
}

func statusText(code int) string {
	if t := http.StatusText(code); t != "" {
		return fmt.Sprintf("%d %s", code, t)
	}
	return fmt.Sprintf("%d", code)
}
