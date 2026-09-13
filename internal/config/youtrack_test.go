package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// theToken is the credential every test in this file asserts is never rendered.
const theToken = "perm:jose.gintrack.s3cr3t-value"

// TestYouTrackTokenPrecedence covers the documented chain flag > env > file >
// none, which is the whole point of having three places to put a token.
func TestYouTrackTokenPrecedence(t *testing.T) {
	t.Parallel()

	const key = "GIT"
	file := "file-token"

	tests := []struct {
		name       string
		env        string
		flag       string
		stored     bool
		wantToken  string
		wantSource TokenSource
	}{
		{name: "nothing configured", wantSource: TokenSourceNone},
		{name: "file only", stored: true, wantToken: file, wantSource: TokenSourceFile},
		{name: "env beats the file", stored: true, env: "env-token", wantToken: "env-token", wantSource: TokenSourceEnv},
		{name: "env with no file", env: "env-token", wantToken: "env-token", wantSource: TokenSourceEnv},
		{
			name: "flag beats env and file", stored: true, env: "env-token", flag: "flag-token",
			wantToken: "flag-token", wantSource: TokenSourceFlag,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if tc.stored {
				cfg := Default()
				cfg.SetYouTrackToken(key, file)
				if err := Save(path, cfg); err != nil {
					t.Fatalf("save: %v", err)
				}
			}
			env := func(name string) string {
				switch name {
				case EnvYouTrackToken:
					return tc.env
				case "GINTRACK_CONFIG":
					return path
				default:
					return ""
				}
			}
			res, err := Resolve(Flags{ConfigPath: path, YouTrackToken: tc.flag}, env)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			token, source := res.Config.YouTrackToken(key)
			if token != tc.wantToken {
				t.Errorf("token = %q, want %q", token, tc.wantToken)
			}
			if source != tc.wantSource {
				t.Errorf("source = %q, want %q", source, tc.wantSource)
			}
			if got := res.Config.HasYouTrackToken(key); got != (tc.wantToken != "") {
				t.Errorf("HasYouTrackToken = %v, want %v", got, tc.wantToken != "")
			}
		})
	}
}

// TestYouTrackTokenRoundTrip stores a token, saves, reloads and asserts both the
// value and the mode of the file that holds it.
func TestYouTrackTokenRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state", "config.yaml")
	cfg := Default()
	cfg.SetYouTrackToken("git", theToken) // lower case on the way in
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600: the file holds a credential", perm)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	token, source := reloaded.YouTrackToken("GIT")
	if token != theToken || source != TokenSourceFile {
		t.Fatalf("reloaded token = %q from %q, want the stored one from the file", token, source)
	}

	reloaded.ClearYouTrackToken("GIT")
	if reloaded.HasYouTrackToken("GIT") {
		t.Error("the token survived ClearYouTrackToken")
	}
	if reloaded.Integrations.YouTrack != nil {
		t.Error("the empty integrations map should be dropped, not left behind")
	}
}

// TestYouTrackTokenIsNeverRendered is the leak assertion: no marshaler, no
// String method and no clone of the configuration may put the token in a string.
func TestYouTrackTokenIsNeverRendered(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.SetYouTrackToken("GIT", theToken)
	cfg.SetYouTrackTokenOverride("perm:override.secret.value", TokenSourceEnv)

	asJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	renderings := map[string]string{
		"json":              string(asJSON),
		"credential String": cfg.Integrations.YouTrack["GIT"].String(),
		"tokens String":     cfg.YouTrackTokens().String(),
		"clone json":        mustJSON(t, cfg.Clone()),
	}
	for name, rendering := range renderings {
		if strings.Contains(rendering, "s3cr3t-value") || strings.Contains(rendering, "override.secret") {
			t.Errorf("%s leaked the token: %s", name, rendering)
		}
	}

	// The override lives only in this process: saving and reloading must not
	// resurrect it from the file.
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // a temporary file this test wrote
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "override.secret") {
		t.Error("the environment override was written to the configuration file")
	}
	if !strings.Contains(string(data), "s3cr3t-value") {
		t.Error("the stored token was not written to the configuration file")
	}
}

// mustJSON marshals a value for a leak assertion.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}

// TestYouTrackTokensSnapshotIsIndependent proves Clone hands out a copy rather
// than a shared map.
func TestYouTrackTokensSnapshotIsIndependent(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.SetYouTrackToken("GIT", "one")
	snapshot := cfg.YouTrackTokens()
	other := snapshot.Clone()
	other.Set("GIT", "two")

	if token, _ := snapshot.For("GIT"); token != "one" {
		t.Errorf("the original snapshot changed: %q", token)
	}
	if token, _ := other.For("GIT"); token != "two" {
		t.Errorf("the clone did not change: %q", token)
	}
	other.Clear("GIT")
	if other.Has("GIT") {
		t.Error("Clear left the token behind")
	}
}

// TestValidateIntegrations covers the refusals of the credential section.
func TestValidateIntegrations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		creds map[string]YouTrackCredential
		want  string
	}{
		{name: "no section"},
		{name: "valid", creds: map[string]YouTrackCredential{"GIT": {Token: theToken}}},
		{
			name:  "empty token",
			creds: map[string]YouTrackCredential{"GIT": {Token: "  "}},
			want:  "integrations.youtrack.GIT.token",
		},
		{
			name:  "lower-case key is not a project key",
			creds: map[string]YouTrackCredential{"git": {Token: theToken}},
			want:  "is not a project key",
		},
		{
			name:  "empty key",
			creds: map[string]YouTrackCredential{"": {Token: theToken}},
			want:  "must not be empty",
		},
		{
			name:  "key with a space",
			creds: map[string]YouTrackCredential{"GIT TRACK": {Token: theToken}},
			want:  "is not a project key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := Default()
			cfg.Integrations.YouTrack = tc.creds
			err := cfg.Validate()
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want == "":
				return
			case err == nil:
				t.Fatalf("expected an error naming %q", tc.want)
			case !errors.Is(err, ErrInvalid):
				t.Fatalf("error does not unwrap to ErrInvalid: %v", err)
			case !strings.Contains(err.Error(), tc.want):
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "s3cr3t-value") {
				t.Errorf("the validation error leaked the token: %v", err)
			}
		})
	}
}
