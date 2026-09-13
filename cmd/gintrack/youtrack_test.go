package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/git-in-track/internal/config"
)

// cliToken is the credential the redaction assertions hunt for.
const cliToken = "perm:jose.gintrack.s3cr3t-value"

// cliProjectKey is the project key of the fixture backlog.
const cliProjectKey = "DEMO"

// ytCLIStub is a stub YouTrack instance for the command tests.
type ytCLIStub struct {
	server *httptest.Server
	status int
}

// newYTCLIStub starts a stub instance and stops it with the test.
func newYTCLIStub(t *testing.T, status int) *ytCLIStub {
	t.Helper()

	stub := &ytCLIStub{status: status}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if stub.status != 0 && stub.status != http.StatusOK {
			w.WriteHeader(stub.status)
			_, _ = w.Write([]byte(`{"error":"denied"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/api/users/me") {
			_, _ = w.Write([]byte(`{"id":"1-1","login":"jose","fullName":"Jose F. Rives"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"0-1","shortName":"ACME","name":"ACME API"}`))
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// URL is the base URL of the stub instance.
func (s *ytCLIStub) URL() string { return s.server.URL }

// projectYAML is the fixture's project.yaml inside the harness repository.
func (h *harness) projectYAML() string {
	return filepath.Join(h.Repo, "docs", ".pmngr", "project.yaml")
}

// readFile reads a file the commands wrote.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a temporary file the harness created
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestYouTrackConnectWritesBothHalves is the happy path: the link is committed,
// the token is not, and neither output carries the credential.
func TestYouTrackConnectWritesBothHalves(t *testing.T) {
	stub := newYTCLIStub(t, http.StatusOK)
	h := newHarness(t)
	h.register()
	h.Stdin = strings.NewReader(cliToken + "\n")

	stdout := h.mustRun("youtrack", "connect", "--url", stub.URL(), "--project", "ACME", "--json")
	payload := decode[map[string]any](t, stdout)

	if payload["login"] != "jose" || payload["project"] != "ACME" {
		t.Fatalf("payload = %v", payload)
	}
	if payload["projectKey"] != cliProjectKey {
		t.Errorf("projectKey = %v, want %s", payload["projectKey"], cliProjectKey)
	}
	if payload["tokenInput"] != "stdin" || payload["tokenSource"] != "file" {
		t.Errorf("token provenance = %v/%v", payload["tokenInput"], payload["tokenSource"])
	}
	if strings.Contains(stdout, "s3cr3t-value") {
		t.Fatalf("--json output leaked the token: %s", stdout)
	}

	yaml := readFile(t, h.projectYAML())
	if !strings.Contains(yaml, stub.URL()) || !strings.Contains(yaml, "project: ACME") {
		t.Fatalf("project.yaml has no link:\n%s", yaml)
	}
	if strings.Contains(yaml, "s3cr3t-value") {
		t.Fatalf("the token was written to project.yaml:\n%s", yaml)
	}
	// The surgical writer left the rest of the file alone.
	if !strings.Contains(yaml, "# Fixture project used by the internal/core tests.") {
		t.Errorf("the write lost the file's comments:\n%s", yaml)
	}
	if !strings.Contains(yaml, "id: in_progress") {
		t.Errorf("the write lost the workflow:\n%s", yaml)
	}

	stored, err := config.Load(h.Config)
	if err != nil {
		t.Fatalf("load the configuration: %v", err)
	}
	if token, source := stored.YouTrackToken(cliProjectKey); token != cliToken || source != config.TokenSourceFile {
		t.Fatalf("stored token = %q from %q", token, source)
	}
	info, err := os.Stat(h.Config)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("configuration mode = %o, want 600", perm)
	}
}

// TestYouTrackConnectTokenPrecedence covers flag > env > stdin.
func TestYouTrackConnectTokenPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		flag  string
		env   string
		stdin string
		want  string
		input string
	}{
		{name: "flag wins", flag: "flag-token", env: "env-token", stdin: "stdin-token", want: "flag-token", input: "flag"},
		{name: "env beats stdin", env: "env-token", stdin: "stdin-token", want: "env-token", input: "env"},
		{name: "stdin is the fallback", stdin: "stdin-token", want: "stdin-token", input: "stdin"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := newYTCLIStub(t, http.StatusOK)
			h := newHarness(t)
			h.register()
			if tc.env != "" {
				t.Setenv(config.EnvYouTrackToken, tc.env)
			}
			if tc.stdin != "" {
				h.Stdin = strings.NewReader(tc.stdin)
			}
			args := []string{"youtrack", "connect", "--url", stub.URL(), "--project", "ACME", "--json"}
			if tc.flag != "" {
				args = append(args, "--token", tc.flag)
			}
			payload := decode[map[string]any](t, h.mustRun(args...))
			if payload["tokenInput"] != tc.input {
				t.Errorf("tokenInput = %v, want %q", payload["tokenInput"], tc.input)
			}
			stored, err := config.Load(h.Config)
			if err != nil {
				t.Fatalf("load the configuration: %v", err)
			}
			if token, _ := stored.YouTrackToken(cliProjectKey); token != tc.want {
				t.Errorf("stored token = %q, want %q", token, tc.want)
			}
		})
	}
}

// TestYouTrackConnectRefusedProbeWritesNothing proves each of the three
// refusals is named and leaves both files untouched.
func TestYouTrackConnectRefusedProbeWritesNothing(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, want: "401"},
		{name: "forbidden", status: http.StatusForbidden, want: "403"},
		{name: "not found", status: http.StatusNotFound, want: "404"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := newYTCLIStub(t, tc.status)
			h := newHarness(t)
			h.register()
			before := readFile(t, h.projectYAML())
			configBefore := readFile(t, h.Config)

			_, stderr, code := h.run("youtrack", "connect",
				"--url", stub.URL(), "--project", "ACME", "--token", cliToken)
			if code != exitFailure {
				t.Fatalf("exit = %d, want %d\n%s", code, exitFailure, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr does not say which refusal it was: %s", stderr)
			}
			if strings.Contains(stderr, "s3cr3t-value") {
				t.Errorf("stderr leaked the token: %s", stderr)
			}
			if readFile(t, h.projectYAML()) != before {
				t.Error("a refused connect still wrote project.yaml")
			}
			if readFile(t, h.Config) != configBefore {
				t.Error("a refused connect still wrote the configuration")
			}
		})
	}
}

// TestYouTrackConnectNeedsAToken proves the command refuses rather than
// prompting when no token can be found anywhere.
func TestYouTrackConnectNeedsAToken(t *testing.T) {
	h := newHarness(t)
	h.register()

	_, stderr, code := h.run("youtrack", "connect", "--url", "https://yt.example.com", "--project", "ACME")
	if code != exitUsage {
		t.Fatalf("exit = %d, want %d\n%s", code, exitUsage, stderr)
	}
	if !strings.Contains(stderr, config.EnvYouTrackToken) {
		t.Errorf("the refusal does not name the environment variable: %s", stderr)
	}
}

// TestYouTrackStatus covers the configured, unconfigured and probe-failure
// cases, and the --offline and --json shapes.
func TestYouTrackStatus(t *testing.T) {
	t.Run("unconfigured", func(t *testing.T) {
		h := newHarness(t)
		h.register()

		stdout, _, code := h.run("youtrack", "status", "--json")
		if code != exitNotFound {
			t.Fatalf("exit = %d, want %d", code, exitNotFound)
		}
		payload := decode[map[string]any](t, stdout)
		if payload["configured"] != false || payload["hasToken"] != false {
			t.Errorf("payload = %v", payload)
		}
		if payload["tokenSource"] != "none" {
			t.Errorf("tokenSource = %v, want none", payload["tokenSource"])
		}
	})

	t.Run("connected", func(t *testing.T) {
		stub := newYTCLIStub(t, http.StatusOK)
		h := newHarness(t)
		h.register()
		h.Stdin = strings.NewReader(cliToken)
		h.mustRun("youtrack", "connect", "--url", stub.URL(), "--project", "ACME")

		stdout := h.mustRun("youtrack", "status", "--json")
		payload := decode[map[string]any](t, stdout)
		if payload["ok"] != true || payload["login"] != "jose" {
			t.Fatalf("payload = %v", payload)
		}
		if payload["tokenSource"] != "file" || payload["hasToken"] != true {
			t.Errorf("token = %v from %v", payload["hasToken"], payload["tokenSource"])
		}
		if strings.Contains(stdout, "s3cr3t-value") {
			t.Fatalf("--json output leaked the token: %s", stdout)
		}

		text := h.mustRun("youtrack", "status")
		for _, want := range []string{"instance: " + stub.URL(), "mapping:  DEMO -> ACME", "token:    present (file)", "probe:    ok as jose"} {
			if !strings.Contains(text, want) {
				t.Errorf("status output has no %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "s3cr3t-value") {
			t.Fatalf("text output leaked the token: %s", text)
		}
	})

	t.Run("offline skips the network", func(t *testing.T) {
		stub := newYTCLIStub(t, http.StatusOK)
		h := newHarness(t)
		h.register()
		h.Stdin = strings.NewReader(cliToken)
		h.mustRun("youtrack", "connect", "--url", stub.URL(), "--project", "ACME")
		stub.server.Close() // any probe from here on must fail

		stdout := h.mustRun("youtrack", "status", "--offline", "--json")
		payload := decode[map[string]any](t, stdout)
		if payload["probed"] != false {
			t.Errorf("--offline still probed: %v", payload)
		}
		if payload["configured"] != true {
			t.Errorf("--offline lost the configuration: %v", payload)
		}
	})

	t.Run("probe failure", func(t *testing.T) {
		stub := newYTCLIStub(t, http.StatusOK)
		h := newHarness(t)
		h.register()
		h.Stdin = strings.NewReader(cliToken)
		h.mustRun("youtrack", "connect", "--url", stub.URL(), "--project", "ACME")
		stub.status = http.StatusUnauthorized

		stdout, stderr, code := h.run("youtrack", "status", "--json")
		if code != exitFailure {
			t.Fatalf("exit = %d, want %d\n%s", code, exitFailure, stderr)
		}
		payload := decode[map[string]any](t, stdout)
		if payload["ok"] != false || payload["probed"] != true {
			t.Errorf("payload = %v", payload)
		}
		if strings.Contains(stdout+stderr, "s3cr3t-value") {
			t.Fatalf("a failed probe leaked the token: %s %s", stdout, stderr)
		}
	})
}

// TestYouTrackStatusUnknownProject proves naming a project that does not exist
// is a not-found, not a panic.
func TestYouTrackStatusUnknownProject(t *testing.T) {
	h := newHarness(t)
	h.register()

	_, stderr, code := h.run("youtrack", "status", "--project-key", "NOPE")
	if code != exitNotFound {
		t.Fatalf("exit = %d, want %d\n%s", code, exitNotFound, stderr)
	}
}
