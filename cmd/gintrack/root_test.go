package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// runCommand executes the command tree with the given arguments and returns its
// standard output.
func runCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	root := newRootCommand(buildInfo{Version: "1.2.3", Commit: "9f2c1ab", Date: "2026-09-01T10:22:41Z", BuiltBy: "test"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	out, err := runCommand(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	for _, want := range []string{"gintrack 1.2.3", "commit:   9f2c1ab", "built:    2026-09-01T10:22:41Z", "core:     schema v1"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lost %q:\n%s", want, out)
		}
	}
}

func TestVersionCommandJSON(t *testing.T) {
	t.Parallel()

	out, err := runCommand(t, "version", "--json")
	if err != nil {
		t.Fatalf("version --json: %v", err)
	}
	var payload versionInfoPayload
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if payload.Version != "1.2.3" || payload.Commit != "9f2c1ab" || payload.Schema != 1 {
		t.Errorf("payload = %#v", payload)
	}
	if payload.Go == "" || payload.OS == "" || payload.Arch == "" {
		t.Errorf("payload = %#v, want the toolchain reported", payload)
	}
}

func TestCompletionCommand(t *testing.T) {
	t.Parallel()

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			out, err := runCommand(t, "completion", shell)
			if err != nil {
				t.Fatalf("completion %s: %v", shell, err)
			}
			if !strings.Contains(out, "gintrack") {
				t.Errorf("the script does not mention gintrack:\n%s", out)
			}
		})
	}

	if _, err := runCommand(t, "completion", "tcsh"); err == nil {
		t.Error("completion tcsh succeeded, want an error")
	}
}

func TestUnknownLogLevelIsRejected(t *testing.T) {
	t.Parallel()

	if _, err := runCommand(t, "--log-level", "loud", "version"); err == nil {
		t.Error("an unknown log level was accepted")
	}
}

func TestServeFlagsAreDeclared(t *testing.T) {
	t.Parallel()

	root := newRootCommand(buildInfo{Version: "test"})
	serve, _, err := root.Find([]string{"serve"})
	if err != nil {
		t.Fatalf("find serve: %v", err)
	}
	for name, want := range map[string]string{
		"port":    "7317",
		"bind":    "127.0.0.1",
		"no-open": "false",
		"token":   "",
	} {
		f := serve.Flags().Lookup(name)
		if f == nil {
			t.Errorf("serve has no --%s flag", name)
			continue
		}
		if f.DefValue != want {
			t.Errorf("--%s default = %q, want %q", name, f.DefValue, want)
		}
	}
}

func TestResolveToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		flag  string
		empty bool
		fixed string
	}{
		{name: "none disables authentication", flag: "none", empty: true},
		{name: "explicit token is kept", flag: "s3cret", fixed: "s3cret"},
		{name: "empty generates one", flag: ""},
		{name: "new generates one", flag: "new"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveToken(tt.flag)
			if err != nil {
				t.Fatalf("resolveToken(%q): %v", tt.flag, err)
			}
			switch {
			case tt.empty && got != "":
				t.Errorf("token = %q, want it empty", got)
			case tt.fixed != "" && got != tt.fixed:
				t.Errorf("token = %q, want %q", got, tt.fixed)
			case !tt.empty && tt.fixed == "" && len(got) < 40:
				t.Errorf("generated token %q is too short", got)
			}
		})
	}
}
