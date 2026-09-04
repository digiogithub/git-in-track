package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeConfig writes a configuration file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, "version: 1\nserver:\n  bind: 10.0.0.1\n  port: 8000\n  token: from-file\nlog:\n  level: warn\n")

	tests := []struct {
		name      string
		env       map[string]string
		flags     Flags
		wantPort  int
		wantBind  string
		wantToken string
		wantLevel string
	}{
		{
			name:      "the file wins over the defaults",
			env:       map[string]string{EnvConfig: path},
			wantPort:  8000,
			wantBind:  "10.0.0.1",
			wantToken: "from-file",
			wantLevel: "warn",
		},
		{
			name:      "the environment wins over the file",
			env:       map[string]string{EnvConfig: path, EnvPort: "9100", EnvBind: "0.0.0.0", EnvToken: "from-env", EnvLogLevel: "debug"},
			wantPort:  9100,
			wantBind:  "0.0.0.0",
			wantToken: "from-env",
			wantLevel: "debug",
		},
		{
			name:      "the flags win over the environment",
			env:       map[string]string{EnvConfig: path, EnvPort: "9100", EnvBind: "0.0.0.0", EnvToken: "from-env", EnvLogLevel: "debug"},
			flags:     Flags{Port: 7000, Bind: "127.0.0.2", Token: "from-flag", LogLevel: "error"},
			wantPort:  7000,
			wantBind:  "127.0.0.2",
			wantToken: "from-flag",
			wantLevel: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := Resolve(tt.flags, fakeEnv(tt.env))
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if res.Path != path {
				t.Errorf("path = %q, want %q", res.Path, path)
			}
			if !res.Exists {
				t.Error("the file was reported as missing")
			}
			c := res.Config
			if c.Server.Port != tt.wantPort || c.Server.Bind != tt.wantBind || c.Server.Token != tt.wantToken {
				t.Errorf("server = %+v, want port %d bind %q token %q", c.Server, tt.wantPort, tt.wantBind, tt.wantToken)
			}
			if c.Log.Level != tt.wantLevel {
				t.Errorf("log level = %q, want %q", c.Log.Level, tt.wantLevel)
			}
		})
	}
}

func TestResolveWithoutAFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	res, err := Resolve(Flags{ConfigPath: path}, fakeEnv(nil))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Exists {
		t.Error("a missing file was reported as existing")
	}
	if res.Config.Server.Port != DefaultPort {
		t.Errorf("port = %d, want the default", res.Config.Server.Port)
	}
}

func TestResolveFlagBeatsEnvironmentForThePath(t *testing.T) {
	t.Parallel()

	fromFlag := writeConfig(t, "server:\n  port: 4444\n")
	fromEnv := writeConfig(t, "server:\n  port: 5555\n")

	res, err := Resolve(Flags{ConfigPath: fromFlag}, fakeEnv(map[string]string{EnvConfig: fromEnv}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Path != fromFlag || res.Config.Server.Port != 4444 {
		t.Errorf("resolved %q port %d, want %q port 4444", res.Path, res.Config.Server.Port, fromFlag)
	}
}

func TestResolveWorkspaceOverride(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, "defaultWorkspace: work\nworkspaces:\n  - name: work\n  - name: oss\n")

	res, err := Resolve(Flags{}, fakeEnv(map[string]string{EnvConfig: path}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Workspace != "work" {
		t.Errorf("workspace = %q, want work", res.Workspace)
	}

	res, err = Resolve(Flags{}, fakeEnv(map[string]string{EnvConfig: path, EnvWorkspace: "oss"}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Workspace != "oss" {
		t.Errorf("workspace = %q, want oss from the environment", res.Workspace)
	}

	res, err = Resolve(Flags{Workspace: "fresh"}, fakeEnv(map[string]string{EnvConfig: path, EnvWorkspace: "oss"}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Workspace != "fresh" {
		t.Errorf("workspace = %q, want the flag to win", res.Workspace)
	}
	if _, ok := res.Config.WorkspaceNamed("fresh"); !ok {
		t.Error("naming a new workspace did not create it")
	}
}

func TestResolveRejectsABadPortInTheEnvironment(t *testing.T) {
	t.Parallel()

	_, err := Resolve(Flags{}, fakeEnv(map[string]string{EnvConfig: writeConfig(t, "version: 1\n"), EnvPort: "http"}))
	if err == nil {
		t.Fatal("a non-numeric GINTRACK_PORT was accepted")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("errors.Is(err, ErrInvalid) = false for %v", err)
	}
}

func TestResolveRejectsAnInvalidFile(t *testing.T) {
	t.Parallel()

	path := writeConfig(t, "server:\n  port: 70000\n")
	_, err := Resolve(Flags{ConfigPath: path}, fakeEnv(nil))
	if err == nil {
		t.Fatal("an out-of-range port was accepted")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("errors.Is(err, ErrInvalid) = false for %v", err)
	}
}

func TestEnvReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv(EnvWorkspace, "from-process")
	if got := Env()(EnvWorkspace); got != "from-process" {
		t.Errorf("Env()(%q) = %q", EnvWorkspace, got)
	}
}
