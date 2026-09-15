package pando

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNewRefusesNonLoopbackURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"mcp host name", Options{MCPURL: "http://pando.example.com:9777/mcp"}},
		{"mcp public ip", Options{MCPURL: "http://10.1.2.3:9777/mcp"}},
		{"mcp ipv6", Options{MCPURL: "http://[2001:db8::1]:9777/mcp"}},
		{"rest host name", Options{RESTURL: "https://pando.example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(tc.opts)
			if !errors.Is(err, ErrRemoteRefused) {
				t.Fatalf("New() error = %v, want ErrRemoteRefused", err)
			}
			if !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("New() error = %v, want it to unwrap to ErrInvalidOptions", err)
			}
		})
	}
}

func TestNewAcceptsLoopbackURLs(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"http://127.0.0.1:9777/mcp",
		"http://localhost:9777/mcp",
		"http://[::1]:9777/mcp",
		"http://127.10.20.30:9777/mcp",
	} {
		if _, err := New(Options{MCPURL: raw}); err != nil {
			t.Fatalf("New(%q) error = %v", raw, err)
		}
	}
}

// The refusal is not overridable by anything except AllowRemote, which is the
// single, explicit escape hatch this version does not offer to the operator.
func TestNewAllowRemoteIsTheOnlyOverride(t *testing.T) {
	t.Parallel()
	opts := Options{MCPURL: "http://pando.example.com:9777/mcp", AllowRemote: true}
	if _, err := New(opts); err != nil {
		t.Fatalf("New() with AllowRemote error = %v", err)
	}
}

func TestNewRejectsBadURLs(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"ftp://127.0.0.1/mcp", "http://", "://nope"} {
		if _, err := New(Options{MCPURL: raw}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("New(%q) error = %v, want ErrInvalidOptions", raw, err)
		}
	}
}

func TestNewDefaultsTimeout(t *testing.T) {
	t.Parallel()
	c, err := New(Options{MCPURL: "http://127.0.0.1:9777/mcp"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if c.Timeout() != DefaultTimeout {
		t.Fatalf("Timeout() = %v, want %v", c.Timeout(), DefaultTimeout)
	}
	if _, err := New(Options{MCPURL: "http://127.0.0.1:9777/mcp", Timeout: -1}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("New() with a negative timeout error = %v, want ErrInvalidOptions", err)
	}
}

func TestNotConfiguredWithoutMCPURL(t *testing.T) {
	t.Parallel()
	c, err := New(Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := c.Health(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Health() error = %v, want ErrNotConfigured", err)
	}
	if _, err := c.SearchKB(context.Background(), "q", KBSearchOptions{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("SearchKB() error = %v, want ErrNotConfigured", err)
	}
}

func TestHealthReachable(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "s3cret")
	c := newTestClient(t, f, nil)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}

func TestHealthUnreachable(t *testing.T) {
	t.Parallel()
	// A port nothing listens on: the connection is refused immediately, so
	// this also proves Health does not sit on its deadline.
	c, err := New(Options{MCPURL: "http://127.0.0.1:1/mcp", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	start := time.Now()
	err = c.Health(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Health() error = %v, want ErrUnreachable", err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("Health() took %v, want it to fail fast", elapsed)
	}
}

func TestUnauthorized(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "right-token")
	c := newTestClient(t, f, func(o *Options) { o.Token = "wrong-token" })
	err := c.Health(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Health() error = %v, want ErrUnauthorized", err)
	}
	if _, err := c.ListProjects(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ListProjects() error = %v, want ErrUnauthorized", err)
	}
}

func TestTimeoutIsDistinctFromUnreachable(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	release := make(chan struct{})
	defer close(release)
	f.setTool(toolCodeProjects, func(ctx context.Context, _ map[string]any) (*mcpsdk.CallToolResult, error) {
		// Hold the call open until the client gives up, then unwind with the
		// request context so the fake server never outlives the test.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return textResult("No indexed projects found."), nil
		}
	})
	c := newTestClient(t, f, func(o *Options) { o.Timeout = 150 * time.Millisecond })

	start := time.Now()
	_, err := c.ListProjects(context.Background())
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("ListProjects() error = %v, want ErrTimeout", err)
	}
	if errors.Is(err, ErrToolFailed) {
		t.Fatalf("a timeout must not look like a tool error: %v", err)
	}
	if errors.Is(err, ErrUnreachable) {
		t.Fatalf("a timeout must not look like an unreachable Pando: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("ListProjects() took %v, want the deadline to bound it", elapsed)
	}
}

// The session is opened lazily on the first call that needs it and then shared:
// two searches must not produce two initialize handshakes, because Pando's
// session map has no eviction.
func TestSessionIsLazyAndShared(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeProjects, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(projectsTOON), nil
	})
	c := newTestClient(t, f, nil)

	c.mu.Lock()
	lazy := c.session == nil
	c.mu.Unlock()
	if !lazy {
		t.Fatal("New() opened a session; it must be lazy")
	}

	if _, err := c.ListProjects(context.Background()); err != nil {
		t.Fatalf("first ListProjects() error = %v", err)
	}
	c.mu.Lock()
	first := c.session
	c.mu.Unlock()

	if _, err := c.ListProjects(context.Background()); err != nil {
		t.Fatalf("second ListProjects() error = %v", err)
	}
	c.mu.Lock()
	second := c.session
	c.mu.Unlock()
	if first != second {
		t.Fatal("the session was rebuilt between two healthy calls")
	}
	if f.callCount() != 2 {
		t.Fatalf("call count = %d, want 2", f.callCount())
	}
}

// A Pando restart invalidates the session id. The next call must heal itself
// rather than surface the restart as a permanent failure.
func TestSessionReconnectsAfterRestart(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	f.setTool(toolCodeProjects, func(context.Context, map[string]any) (*mcpsdk.CallToolResult, error) {
		return textResult(projectsTOON), nil
	})
	c := newTestClient(t, f, nil)

	if _, err := c.ListProjects(context.Background()); err != nil {
		t.Fatalf("first ListProjects() error = %v", err)
	}
	c.mu.Lock()
	before := c.session
	c.mu.Unlock()

	f.restart()

	got, err := c.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects() after restart error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d projects, want 2", len(got))
	}
	c.mu.Lock()
	after := c.session
	c.mu.Unlock()
	if before == after {
		t.Fatal("the session was not rebuilt after the server restarted")
	}
}

func TestHealthReconnectsAfterRestart(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	c := newTestClient(t, f, nil)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	f.restart()
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health() after restart error = %v", err)
	}
}

func TestCloseIsIdempotentAndFinal(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "")
	c := newTestClient(t, f, nil)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := c.Health(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Health() after Close() error = %v, want ErrNotConfigured", err)
	}
}

func TestSanitizeProjectID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"/www/git-in-track", "www_git-in-track"},
		{"/www/MCP/Pando/pando", "www_MCP_Pando_pando"},
		{"/www/Github/plane", "www_Github_plane"},
		{"C:\\Users\\jose\\My Repo", "C_Users_jose_My_Repo"},
		{"already_sane", "already_sane"},
		{"dots.and spaces/here", "dots_and_spaces_here"},
		{"///", ""},
		{"weird!chars@here", "weirdcharshere"},
	} {
		if got := SanitizeProjectID(tc.in); got != tc.want {
			t.Errorf("SanitizeProjectID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestErrorsDoNotLeakTheToken(t *testing.T) {
	t.Parallel()
	f := newFakePando(t, "right-token")
	c := newTestClient(t, f, func(o *Options) { o.Token = "super-secret" })
	err := c.Health(context.Background())
	if err == nil {
		t.Fatal("Health() succeeded with a wrong token")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("the error rendered the token: %v", err)
	}
}
