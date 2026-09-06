package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
