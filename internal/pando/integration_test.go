package pando

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestIntegrationAgainstRealPando exercises this client against a Pando that is
// actually running, which is the only way to notice that Pando changed a tool's
// result format under us. It is opt-in and never required: with
// GINTRACK_PANDO_INTEGRATION unset it skips, so CI and every other developer
// are unaffected.
//
//	GINTRACK_PANDO_INTEGRATION=1 \
//	GINTRACK_PANDO_MCP_URL=http://127.0.0.1:9777/mcp \
//	GINTRACK_PANDO_MCP_TOKEN=... \
//	go test ./internal/pando/ -run TestIntegration -v
func TestIntegrationAgainstRealPando(t *testing.T) {
	if os.Getenv("GINTRACK_PANDO_INTEGRATION") == "" {
		t.Skip("set GINTRACK_PANDO_INTEGRATION=1 to run against a live Pando")
	}
	url := os.Getenv("GINTRACK_PANDO_MCP_URL")
	if url == "" {
		url = "http://127.0.0.1:9777/mcp"
	}
	c, err := New(Options{
		MCPURL:  url,
		Token:   os.Getenv("GINTRACK_PANDO_MCP_TOKEN"),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx := context.Background()
	if err := c.Health(ctx); err != nil {
		t.Fatalf("Health() error = %v", err)
	}

	projects, err := c.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects() error = %v", err)
	}
	t.Logf("ListProjects() returned %d projects", len(projects))

	hits, err := c.SearchKB(ctx, "git-in-track roadmap", KBSearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("SearchKB() error = %v", err)
	}
	for _, h := range hits {
		if h.FilePath == "" {
			t.Errorf("a KB hit came back with no file path: %+v", h)
		}
	}
	t.Logf("SearchKB() returned %d hits", len(hits))

	if len(projects) > 0 {
		code, err := c.SearchCode(ctx, projects[0].ProjectID, "http handler", CodeSearchOptions{Limit: 3})
		if err != nil {
			t.Fatalf("SearchCode() error = %v", err)
		}
		t.Logf("SearchCode(%s) returned %d hits", projects[0].ProjectID, len(code))
	}
}
