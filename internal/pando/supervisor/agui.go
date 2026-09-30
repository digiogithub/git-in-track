package supervisor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/digiogithub/git-in-track/internal/pando"
)

// defaultAGUIPath is Pando's AG-UI route prefix.
const defaultAGUIPath = "/api/v1/agui"

// prepareRun writes what the next child of this instance reads and returns its
// command-line arguments. For KindMCP that is the generated .pando.toml; for
// KindAGUI it is the files Options.Files renders, plus the flags that put the
// listener on the chosen port behind the instance's token.
func (s *Supervisor) prepareRun(port int) ([]string, error) {
	if s.opts.Kind != KindAGUI {
		cfg := generatedConfig(s.opts, s.dir, port, s.token)
		if err := writeFileAtomic(filepath.Join(s.dir, configFileName), cfg, 0o600); err != nil {
			return nil, fmt.Errorf("write .pando.toml: %w", err)
		}
		args := []string{"mcp-server", "--no-stdio", "--cwd", s.dir}
		if s.opts.Debug {
			args = append(args, "--debug")
		}
		return args, nil
	}
	files, err := s.opts.Files(port)
	if err != nil {
		return nil, fmt.Errorf("render the AG-UI configuration: %w", err)
	}
	if _, ok := files[configFileName]; !ok {
		return nil, fmt.Errorf("render the AG-UI configuration: no %s", configFileName)
	}
	for name, content := range files {
		rel := filepath.FromSlash(path.Clean("/" + name))[1:]
		if rel == "" || strings.HasPrefix(name, "/") || slices.Contains(strings.Split(name, "/"), "..") {
			return nil, fmt.Errorf("render the AG-UI configuration: bad file name %q", name)
		}
		dest := filepath.Join(s.dir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			return nil, fmt.Errorf("create the directory of %s: %w", name, err)
		}
		if err := writeFileAtomic(dest, content, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}
	// The token comes from the file, never the command line, where `ps` shows it.
	args := []string{
		"agui-serve", "--cwd", s.dir, "--host", "127.0.0.1", "--port", strconv.Itoa(port),
		"--no-tls", "--token-file", filepath.Join(s.dir, tokenFileName),
	}
	if s.opts.Debug {
		args = append(args, "--debug")
	}
	return args, nil
}

// healthCheck returns the function that decides whether the child is healthy.
// full is true for the first check after a start. For KindMCP both are the MCP
// health call. For KindAGUI the cheap check is the unauthenticated /healthz and
// the full one also reads /info with our token, which is what tells our child
// from another process that owns the port (a 401 there).
func (s *Supervisor) healthCheck(mcpURL, base string) (check func(ctx context.Context, full bool) error, closeFn func(), err error) {
	if s.opts.Kind != KindAGUI {
		client, err := pando.New(pando.Options{MCPURL: mcpURL, Token: s.token, Timeout: s.opts.HealthTimeout})
		if err != nil {
			return nil, nil, fmt.Errorf("build the pando client: %w", err)
		}
		return func(ctx context.Context, _ bool) error { return client.Health(ctx) },
			func() { _ = client.Close() }, nil
	}
	hc := &http.Client{Timeout: s.opts.HealthTimeout, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	get := func(ctx context.Context, route, token string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+s.opts.AGUIPath+route, http.NoBody)
		if err != nil {
			return fmt.Errorf("build the request: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := hc.Do(req)
		if err != nil {
			return fmt.Errorf("GET %s: %w", route, err)
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s answered %d", route, resp.StatusCode)
		}
		return nil
	}
	return func(ctx context.Context, full bool) error {
			if err := get(ctx, "/healthz", ""); err != nil {
				return err
			}
			if full {
				return get(ctx, "/info", s.token)
			}
			return nil
		},
		hc.CloseIdleConnections, nil
}
