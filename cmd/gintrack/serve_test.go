package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/digiogithub/git-in-track/internal/config"
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

// TestServeSyncEngineFlagsAreDeclared pins the four engine flags and their
// defaults, which are the ones documented in docs/07 section 4.1.
func TestServeSyncEngineFlagsAreDeclared(t *testing.T) {
	t.Parallel()

	root := newRootCommand(buildInfo{Version: "test"})
	serve, _, err := root.Find([]string{"serve"})
	if err != nil {
		t.Fatalf("find serve: %v", err)
	}
	for name, want := range map[string]string{
		"sync-workers":      "2",
		"sync-batch":        "20",
		"sync-rate":         "5",
		"sync-max-attempts": "5",
	} {
		flag := serve.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("serve has no --%s flag", name)
		}
		if flag.DefValue != want {
			t.Errorf("--%s default = %q, want %q", name, flag.DefValue, want)
		}
	}
}

// TestSyncEngineSettingsPrecedence covers the documented chain in full: the
// flag beats the environment, which beats the configuration file, which beats
// the shipped default.
func TestSyncEngineSettingsPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		env         map[string]string
		file        config.SyncEngine
		wantWorkers int
		wantRate    float64
		wantErr     string
	}{
		{name: "defaults", wantWorkers: server.DefaultSyncWorkers, wantRate: server.DefaultSyncRate},
		{
			name:        "the file beats the default",
			file:        config.SyncEngine{Workers: 7, Rate: 2.5},
			wantWorkers: 7, wantRate: 2.5,
		},
		{
			name:        "the environment beats the file",
			env:         map[string]string{envSyncWorkers: "6"},
			file:        config.SyncEngine{Workers: 7, Rate: 2.5},
			wantWorkers: 6, wantRate: 2.5,
		},
		{
			name:        "the flag beats the file and the environment",
			args:        []string{"--sync-workers", "3"},
			env:         map[string]string{envSyncWorkers: "6"},
			file:        config.SyncEngine{Workers: 7, Rate: 2.5},
			wantWorkers: 3, wantRate: 2.5,
		},
		{
			name:        "the environment beats the default",
			env:         map[string]string{envSyncWorkers: "6", envSyncRate: "12.5"},
			wantWorkers: 6, wantRate: 12.5,
		},
		{
			name:        "the flag beats the environment",
			args:        []string{"--sync-workers", "3"},
			env:         map[string]string{envSyncWorkers: "6"},
			wantWorkers: 3, wantRate: server.DefaultSyncRate,
		},
		{
			name:    "an impossible value fails the command",
			args:    []string{"--sync-workers", "0"},
			wantErr: "workers",
		},
		{
			name:    "an unparsable environment value fails the command",
			env:     map[string]string{envSyncBatch: "many"},
			wantErr: envSyncBatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			flags := &serveFlags{}
			cmd := newServeCommand(buildInfo{Version: "test"})
			// The flags are parsed but the command is never run: this asserts
			// the resolution, not the listener.
			cmd.RunE = func(*cobra.Command, []string) error { return nil }
			if err := cmd.Flags().Parse(tt.args); err != nil {
				t.Fatalf("parse %v: %v", tt.args, err)
			}
			for name, target := range map[string]any{
				"sync-workers": &flags.syncWorkers, "sync-batch": &flags.syncBatch,
				"sync-max-attempts": &flags.syncMaxAttempts,
			} {
				value, err := cmd.Flags().GetInt(name)
				if err != nil {
					t.Fatalf("read --%s: %v", name, err)
				}
				*(target.(*int)) = value
			}
			rate, err := cmd.Flags().GetFloat64("sync-rate")
			if err != nil {
				t.Fatalf("read --sync-rate: %v", err)
			}
			flags.syncRate = rate

			cfg := &config.Config{Sync: config.Sync{Engine: tt.file}}
			got, err := syncEngineSettings(cmd, flags, cfg, func(key string) string {
				return tt.env[key]
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want one naming %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("syncEngineSettings(): %v", err)
			}
			if got.Workers != tt.wantWorkers || got.Rate != tt.wantRate {
				t.Fatalf("settings = %+v, want %d workers at %g req/s", got, tt.wantWorkers, tt.wantRate)
			}
		})
	}
}

// TestServeAgentFlagIsDeclared pins `--agent` and its default. The proxy relays
// a chat turn to a local agent process that can act on the backlog, so it is
// opt-in exactly as the tunnel is.
func TestServeAgentFlagIsDeclared(t *testing.T) {
	t.Parallel()

	root := newRootCommand(buildInfo{Version: "test"})
	serve, _, err := root.Find([]string{"serve"})
	if err != nil {
		t.Fatalf("find serve: %v", err)
	}
	flag := serve.Flags().Lookup("agent")
	if flag == nil {
		t.Fatal("serve has no --agent flag")
	}
	if flag.DefValue != "false" {
		t.Errorf("--agent default = %q, want false", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "/api/v1/agent") {
		t.Errorf("--agent usage does not name the route: %q", flag.Usage)
	}
}

// TestBannerPrintsTheAgentTargetWithoutItsToken is the whole point of the
// banner line: an operator has to see which adapter the companion will talk to,
// and nobody may see the credential it will talk to it with.
func TestBannerPrintsTheAgentTargetWithoutItsToken(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Agent.Enabled = true
	cfg.Agent.Pando.URL = "http://127.0.0.1:8090"
	cfg.Agent.Pando.Token = "pando-secret-token-value"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("the fixture configuration is invalid: %v", err)
	}

	opts := server.Options{
		Bind:      "127.0.0.1",
		Token:     "companion-token",
		Workspace: "default",
		Agent:     true,
		Pando:     cfg.PandoTargets(),
	}
	srv, err := server.New(opts)
	if err != nil {
		t.Fatalf("server.New(): %v", err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	printBanner(cmd, buildInfo{Version: "test"}, srv, opts.Token, opts)

	out := stdout.String()
	if !strings.Contains(out, "agent:      http://127.0.0.1:8090/api/v1/agui (backlog-assistant)") {
		t.Errorf("the banner does not name the agent upstream:\n%s", out)
	}
	if strings.Contains(out, "pando-secret-token-value") {
		t.Errorf("the banner printed the upstream token:\n%s", out)
	}
}

// TestBannerWarnsWhenTheAgentHasNoUpstream keeps `--agent` with no configured
// URL from looking like a working feature.
func TestBannerWarnsWhenTheAgentHasNoUpstream(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Agent.Pando.URL = ""
	opts := server.Options{
		Bind:      "127.0.0.1",
		Token:     "companion-token",
		Workspace: "default",
		Agent:     true,
		Pando:     cfg.PandoTargets(),
	}
	srv, err := server.New(opts)
	if err != nil {
		t.Fatalf("server.New(): %v", err)
	}
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	printBanner(cmd, buildInfo{Version: "test"}, srv, opts.Token, opts)

	if !strings.Contains(stdout.String(), "agent.pando.url is not set") {
		t.Errorf("the banner does not warn about the missing upstream:\n%s", stdout.String())
	}
}
