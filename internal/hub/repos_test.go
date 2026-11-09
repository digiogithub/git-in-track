package hub

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSSH(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		ok   bool
		want sshTarget
	}{
		{"git@github.com:acme/web.git", true, sshTarget{user: "git", host: "github.com", path: "acme/web.git"}},
		{"github.com:acme/web.git", true, sshTarget{host: "github.com", path: "acme/web.git"}},
		{"ssh://git@gitea.local:2222/acme/web.git", true, sshTarget{user: "git", host: "gitea.local", port: "2222", path: "/acme/web.git"}},
		{"https://github.com/acme/web.git", false, sshTarget{}},
		{"/srv/git/web.git", false, sshTarget{}},
		{"relative/path", false, sshTarget{}},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, ok := parseSSH(tc.raw)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("parseSSH(%q) = %+v, %v; want %+v, %v", tc.raw, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestRemoteURLAndSSHConfig(t *testing.T) {
	t.Parallel()
	cfg := &Config{DataDir: "/data", Organization: Organization{ID: "acme", Projects: []RepoSpec{
		{ID: "web", URL: "ssh://git@gitea.local:2222/acme/web.git", DeployKey: "/keys/web"},
		{ID: "api", URL: "git@github.com:acme/api.git"},
		{ID: "docs", URL: "https://github.com/acme/docs.git"},
	}}}

	t.Run("deploy key rewrites to the alias", func(t *testing.T) {
		t.Parallel()
		if got := remoteURL(cfg.Organization.Projects[0]); got != "gintrack-hub-web:acme/web.git" {
			t.Fatalf("remoteURL = %q", got)
		}
	})
	t.Run("no deploy key keeps the url", func(t *testing.T) {
		t.Parallel()
		for _, r := range cfg.Organization.Projects[1:] {
			if got := remoteURL(r); got != r.URL {
				t.Fatalf("remoteURL(%s) = %q, want %q", r.ID, got, r.URL)
			}
		}
	})
	t.Run("ssh config has one host per deploy key", func(t *testing.T) {
		t.Parallel()
		text, ok := sshConfig(cfg)
		if !ok {
			t.Fatal("sshConfig reported no host")
		}
		for _, want := range []string{
			"Host gintrack-hub-web", "HostName gitea.local", "User git", "Port 2222",
			"IdentityFile /keys/web", "IdentitiesOnly yes", "UserKnownHostsFile /data/known_hosts",
		} {
			if !strings.Contains(text, want) {
				t.Errorf("ssh config lacks %q:\n%s", want, text)
			}
		}
		if strings.Contains(text, "gintrack-hub-api") {
			t.Errorf("a repository without a deploy key got an alias:\n%s", text)
		}
	})
}

func TestRedact(t *testing.T) {
	t.Parallel()
	got := redact("fatal: could not read from https://user:s3cret@host/r.git")
	if strings.Contains(got, "s3cret") || !strings.Contains(got, "https://***@host") {
		t.Fatalf("redact = %q", got)
	}
}

// gitBin returns the git executable or skips the test.
func gitBin(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("these tests need a git binary")
	}
	return bin
}

// git runs git in dir with a fixed identity and fails the test on error.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), gitBin(t), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// bareRemote creates a bare repository holding one commit with the given
// files, and returns its path.
func bareRemote(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	git(t, root, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "--quiet", bare, seed)
	for name, text := range files {
		full := filepath.Join(seed, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, seed, "add", "-A")
	git(t, seed, "commit", "--quiet", "-m", "seed")
	git(t, seed, "push", "--quiet", "origin", "HEAD:main")
	return bare
}

func TestPrepareClones(t *testing.T) {
	t.Parallel()
	bare := bareRemote(t, map[string]string{"README.md": "hello\n"})
	cfg := &Config{DataDir: t.TempDir(), Organization: Organization{ID: "acme",
		Projects: []RepoSpec{{ID: "web", URL: bare}}}}

	if err := Prepare(context.Background(), cfg, gitBin(t)); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.RepoDir("web"), "README.md")); err != nil {
		t.Fatalf("clone is missing its file: %v", err)
	}
	t.Run("an existing clone is reused", func(t *testing.T) {
		if err := Prepare(context.Background(), cfg, gitBin(t)); err != nil {
			t.Fatalf("second Prepare: %v", err)
		}
	})
	t.Run("a non-empty non-repository directory is refused", func(t *testing.T) {
		dir := cfg.RepoDir("other")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		bad := &Config{DataDir: cfg.DataDir, Organization: Organization{ID: "acme",
			Projects: []RepoSpec{{ID: "other", URL: bare}}}}
		err := Prepare(context.Background(), bad, gitBin(t))
		if err == nil || !strings.Contains(err.Error(), "not a git working tree") {
			t.Fatalf("Prepare = %v, want a refusal", err)
		}
	})
	t.Run("a clone failure names the repository", func(t *testing.T) {
		missing := &Config{DataDir: t.TempDir(), Organization: Organization{ID: "acme",
			Projects: []RepoSpec{{ID: "gone", URL: filepath.Join(t.TempDir(), "nope.git")}}}}
		err := Prepare(context.Background(), missing, gitBin(t))
		if err == nil || !strings.Contains(err.Error(), "clone repository gone") {
			t.Fatalf("Prepare = %v", err)
		}
	})
}
