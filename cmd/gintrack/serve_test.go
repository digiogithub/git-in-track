package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/server"
)

// TestServeTunnelFlagIsDeclared pins the flag and, above all, its default: the
// tunnel is opt-in, so `gintrack serve` never publishes anything by itself.
func TestServeTunnelFlagIsDeclared(t *testing.T) {
	t.Parallel()

	root := newRootCommand(buildInfo{Version: "test"})
	serve, _, err := root.Find([]string{"serve"})
	if err != nil {
		t.Fatalf("find serve: %v", err)
	}
	flag := serve.Flags().Lookup("tunnel")
	if flag == nil {
		t.Fatal("serve has no --tunnel flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--tunnel default = %q, want false", flag.DefValue)
	}
}

// TestServeRefusesATunnelWithoutAToken is the command-line half of the refusal
// enforced by internal/server: whichever way the tunnel is asked for, a server
// with authentication disabled must not start at all.
func TestServeRefusesATunnelWithoutAToken(t *testing.T) {
	tests := []struct {
		name string
		// config is written to GINTRACK_CONFIG before the command runs.
		config string
		args   []string
	}{
		{
			name: "the flag on a server without a token",
			args: []string{"serve", "--tunnel", "--token", "none"},
		},
		{
			name:   "the configuration key on a server without a token",
			config: "version: 1\nserver:\n  tunnel:\n    enabled: true\n",
			args:   []string{"serve", "--token", "none"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if tc.config != "" {
				if err := os.MkdirAll(filepath.Dir(h.Config), 0o755); err != nil {
					t.Fatalf("create the configuration directory: %v", err)
				}
				if err := os.WriteFile(h.Config, []byte(tc.config), 0o600); err != nil {
					t.Fatalf("write the configuration: %v", err)
				}
			}

			stdout, stderr, code := h.run(tc.args...)
			if code == exitOK {
				t.Fatalf("the server started with a tunnel and no token\nstdout:\n%s", stdout)
			}
			if !strings.Contains(stderr, "requires a bearer token") {
				t.Errorf("stderr does not explain the refusal:\n%s", stderr)
			}
			if strings.Contains(stdout, "listening on") {
				t.Errorf("the server listened before refusing:\n%s", stdout)
			}
		})
	}
}

// stubTunnelReporter answers announceTunnel with a fixed status.
type stubTunnelReporter struct{ status server.TunnelInfo }

func (s stubTunnelReporter) TunnelStatus() server.TunnelInfo { return s.status }

// TestAnnounceTunnelPrintsAnOpenLink pins the two lines a tunnel prints. The
// `open:` link is the only way a remote browser gets the token: the web app
// reads it from `?token=` or from Settings and nowhere else, so a bare hostname
// leaves the visitor on an empty workspace behind the "needs an access token"
// banner.
func TestAnnounceTunnelPrintsAnOpenLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status server.TunnelInfo
		token  string
		want   []string
		unwant []string
	}{
		{
			name:   "a tunnel with a token prints the hostname and the link",
			status: server.TunnelInfo{State: "connected", URL: "https://example.trycloudflare.com"},
			token:  "s3cret",
			want: []string{
				"tunnel:     https://example.trycloudflare.com   (public; the token is still required)\n",
				"open:       https://example.trycloudflare.com/?token=s3cret",
			},
		},
		{
			// Unreachable through `serve`, which refuses the combination, but
			// the printer must not invent a `?token=` link with no token.
			name:   "a tunnel without a token prints no link",
			status: server.TunnelInfo{State: "connected", URL: "https://example.trycloudflare.com"},
			want:   []string{"tunnel:     https://example.trycloudflare.com"},
			unwant: []string{"open:", "token="},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			announceTunnel(t.Context(), cmd, stubTunnelReporter{status: tc.status}, tc.token)

			for _, want := range tc.want {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("stdout does not contain %q:\n%s", want, stdout.String())
				}
			}
			for _, unwant := range tc.unwant {
				if strings.Contains(stdout.String(), unwant) {
					t.Errorf("stdout contains %q, which it must not:\n%s", unwant, stdout.String())
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr is not empty:\n%s", stderr.String())
			}
		})
	}
}

// TestAnnounceTunnelReportsAFailureOnStderr keeps a tunnel that never opens out
// of stdout: nothing was published, so there is no link to print.
func TestAnnounceTunnelReportsAFailureOnStderr(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	status := server.TunnelInfo{State: "error", Err: "broker refused"}
	announceTunnel(t.Context(), cmd, stubTunnelReporter{status: status}, "s3cret")

	if !strings.Contains(stderr.String(), "broker refused") {
		t.Errorf("stderr does not name the failure:\n%s", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("a failed tunnel printed to stdout:\n%s", stdout.String())
	}
}
