package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
	"github.com/digiogithub/git-in-track/internal/pando/supervisor"
)

const pandoTestToken = "super-secret-instance-token"

// pandoHarness registers the fixture, opts it in to a managed Pando and
// returns the instance directory a supervisor would use for it.
func pandoHarness(t *testing.T, optIn bool) (*harness, string) {
	t.Helper()

	h := newHarness(t)
	h.register()
	cfg, err := config.Load(h.Config)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Repos[0].SemanticSearch = optIn
	cfg.Search.Pando.Mode = config.PandoModeManaged
	if err := config.Save(h.Config, cfg); err != nil {
		t.Fatal(err)
	}
	return h, supervisor.InstanceDir(cfg.CacheDir(h.Config), supervisor.InstanceKey(cfg.Repos[0].Path))
}

func writePandoState(t *testing.T, dir string, st supervisor.Status) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "state.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(pandoTestToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPandoStatus(t *testing.T) {
	const dead = 2147483000
	self := os.Getpid()
	tests := []struct {
		name   string
		optIn  bool
		state  *supervisor.Status
		want   string
		wantIn []string
	}{
		{"found", true, &supervisor.Status{State: supervisor.StateReady, SupervisorPID: self, PID: self, Port: 4242,
			Version: "pando v1.2.3", MCPURL: "http://127.0.0.1:4242/mcp", TokenFile: "/cache/pando/x/token"},
			"ready", []string{"pid: ", "port: 4242", "pando v1.2.3", "http://127.0.0.1:4242/mcp", "/cache/pando/x/token"}},
		{"stale state file", true, &supervisor.Status{State: supervisor.StateReady, SupervisorPID: dead, PID: dead, Port: 4242},
			"stopped (stale)", []string{"gone"}},
		{"not running", true, nil, "not running", nil},
		{"not opted in", false, nil, "disabled", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, dir := pandoHarness(t, tc.optIn)
			if tc.state != nil {
				writePandoState(t, dir, *tc.state)
			}
			out := h.mustRun("pando", "status")
			if strings.Contains(out, pandoTestToken) {
				t.Errorf("the token was printed:\n%s", out)
			}
			for _, want := range append([]string{"mode: managed", "acme-api  " + tc.want}, tc.wantIn...) {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}

			raw := h.mustRun("pando", "status", "--json", "--repo", "acme-api")
			if strings.Contains(raw, pandoTestToken) {
				t.Errorf("the token was printed in JSON:\n%s", raw)
			}
			var payload pandoStatusPayload
			if err := json.Unmarshal([]byte(raw), &payload); err != nil {
				t.Fatalf("decode: %v\n%s", err, raw)
			}
			if len(payload.Instances) != 1 || payload.Instances[0].State != tc.want {
				t.Errorf("instances = %+v, want one in state %q", payload.Instances, tc.want)
			}
		})
	}

	t.Run("unknown repository", func(t *testing.T) {
		h, _ := pandoHarness(t, true)
		if _, _, code := h.run("pando", "status", "--repo", "nope"); code != exitNotFound {
			t.Errorf("exit = %d, want %d", code, exitNotFound)
		}
	})
}

func TestPandoControl(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		verb     string
		wantCode int
		wantOut  string
	}{
		{"start", http.StatusOK, `{"repo":"acme-api","action":"start","managed":{"state":"starting","pid":7}}`,
			"start", exitOK, "acme-api: start -> starting (pid 7)"},
		{"refused", http.StatusConflict, `{"detail":"acme-api: no managed instance is running."}`,
			"restart", exitConflict, "no managed instance is running"},
		{"bad token", http.StatusUnauthorized, `{}`, "stop", exitFailure, "refused the token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotAuth string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			h, _ := pandoHarness(t, true)
			stdout, stderr, code := h.run("pando", tc.verb, "--companion-url", srv.URL, "--token", "tok")
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d\n%s%s", code, tc.wantCode, stdout, stderr)
			}
			if !strings.Contains(stdout+stderr, tc.wantOut) {
				t.Errorf("output lacks %q:\n%s%s", tc.wantOut, stdout, stderr)
			}
			if want := "/api/v1/search/managed/acme-api/" + tc.verb; gotPath != want || gotAuth != "Bearer tok" {
				t.Errorf("request = %s %q, want %s with the bearer token", gotPath, gotAuth, want)
			}
		})
	}

	t.Run("serve is not running", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		h, _ := pandoHarness(t, true)
		_, stderr, code := h.run("pando", "start", "--companion-url", url)
		if code != exitFailure || !strings.Contains(stderr, "gintrack serve is not running") {
			t.Errorf("exit = %d, stderr = %q, want a clear not-running error", code, stderr)
		}
	})

	t.Run("nothing opted in", func(t *testing.T) {
		h, _ := pandoHarness(t, false)
		if _, _, code := h.run("pando", "start"); code != exitUsage {
			t.Errorf("exit = %d, want %d", code, exitUsage)
		}
	})
}
