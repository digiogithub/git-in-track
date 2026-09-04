package gitops

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Credential handling in native mode (story GIT-US-0023).
//
// Two properties are load-bearing and are asserted against real temporary
// repositories rather than against a mock:
//
//   - a host that asks for a credential we do not have fails fast with an
//     actionable message, and never blocks on a prompt;
//   - nothing this package touches ever writes a credential to disk.

// theSecret is the token every fixture below embeds. No assertion may ever find
// it outside the repository's own configuration, which is the user's file and
// not ours.
const theSecret = "s3cr3t-token-value"

func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "a remote URL loses its userinfo",
			in:   "fatal: Authentication failed for 'https://jose:" + theSecret + "@git.example.test/acme/web.git/'",
			want: "fatal: Authentication failed for 'https://***@git.example.test/acme/web.git/'",
		},
		{
			name: "a token-only userinfo is removed too",
			in:   "remote: https://" + theSecret + "@github.com/acme/web.git",
			want: "remote: https://***@github.com/acme/web.git",
		},
		{
			name: "a token query parameter is masked",
			in:   "GET /info/refs?service=git-upload-pack&access_token=" + theSecret,
			want: "GET /info/refs?service=git-upload-pack&access_token=***",
		},
		{
			name: "a password parameter is masked",
			in:   "password=" + theSecret + " host=example.test",
			want: "password=*** host=example.test",
		},
		{
			name: "an authorization header is masked",
			in:   "Authorization: Basic " + theSecret,
			want: "Authorization: Basic ***",
		},
		{
			name: "an ssh remote has nothing to redact",
			in:   "git@github.com:acme/web.git",
			want: "git@github.com:acme/web.git",
		},
		{
			name: "an empty string stays empty",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactSecrets(tc.in); got != tc.want {
				t.Fatalf("redactSecrets(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(redactSecrets(tc.in), theSecret) {
				t.Fatalf("redactSecrets left the secret in %q", tc.in)
			}
		})
	}
}

func TestNonInteractiveEnv(t *testing.T) {
	cases := []struct {
		name string
		base []string
		want []string
	}{
		{
			name: "an empty environment gets every switch",
			base: nil,
			want: []string{
				"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=",
				"GCM_INTERACTIVE=never", "GIT_SSH_COMMAND=ssh -o BatchMode=yes",
			},
		},
		{
			name: "a configured ssh command is extended, not replaced",
			base: []string{"GIT_SSH_COMMAND=ssh -F /home/u/.ssh/work"},
			want: []string{"GIT_SSH_COMMAND=ssh -F /home/u/.ssh/work -o BatchMode=yes"},
		},
		{
			name: "a ssh command that is already batch mode is left alone",
			base: []string{"GIT_SSH_COMMAND=ssh -o BatchMode=yes -v"},
			want: []string{"GIT_SSH_COMMAND=ssh -o BatchMode=yes -v"},
		},
		{
			name: "a user askpass is overridden so it cannot open a window",
			base: []string{"GIT_ASKPASS=/usr/bin/ksshaskpass", "SSH_ASKPASS=/usr/bin/ksshaskpass"},
			want: []string{"GIT_ASKPASS=", "SSH_ASKPASS="},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nonInteractiveEnv(tc.base)
			for _, want := range tc.want {
				if !lastValueIs(got, want) {
					t.Fatalf("nonInteractiveEnv(%v) = %v, want %q to win", tc.base, got, want)
				}
			}
		})
	}
}

// lastValueIs reports whether the last entry for the key of want is want, which
// is the value exec passes on for a duplicated key.
func lastValueIs(env []string, want string) bool {
	key, _, _ := strings.Cut(want, "=")
	last := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, key+"=") {
			last = entry
		}
	}
	return last == want
}

func TestParseCredentialFill(t *testing.T) {
	cases := []struct {
		name          string
		out           string
		wantUser      string
		wantHasSecret bool
	}{
		{
			name:          "a helper that answers",
			out:           "protocol=https\nhost=example.test\nusername=jose\npassword=" + theSecret + "\n",
			wantUser:      "jose",
			wantHasSecret: true,
		},
		{
			name:          "a helper that knows nothing",
			out:           "protocol=https\nhost=example.test\n",
			wantUser:      "",
			wantHasSecret: false,
		},
		{
			name:          "noise between the pairs is ignored",
			out:           "\nusername=x-access-token\nnot a pair\npassword=" + theSecret + "\n",
			wantUser:      "x-access-token",
			wantHasSecret: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			user, secret := parseCredentialFill(tc.out)
			if user != tc.wantUser {
				t.Fatalf("username = %q, want %q", user, tc.wantUser)
			}
			if (secret != "") != tc.wantHasSecret {
				t.Fatalf("secret present = %v, want %v", secret != "", tc.wantHasSecret)
			}
		})
	}
}

func TestEnvironmentToken(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		url      string
		wantUser string
		wantSet  bool
	}{
		{
			name:     "the generic variable serves any host",
			env:      map[string]string{"GINTRACK_TOKEN": theSecret},
			url:      "https://git.example.test/acme/web.git",
			wantUser: "token",
			wantSet:  true,
		},
		{
			name:     "github takes the x-access-token username",
			env:      map[string]string{"GITHUB_TOKEN": theSecret},
			url:      "https://github.com/acme/web.git",
			wantUser: "x-access-token",
			wantSet:  true,
		},
		{
			name:     "gitlab takes the oauth2 username",
			env:      map[string]string{"GITLAB_TOKEN": theSecret},
			url:      "https://gitlab.com/acme/web.git",
			wantUser: "oauth2",
			wantSet:  true,
		},
		{
			name:    "a host-specific variable does not serve another host",
			env:     map[string]string{"GITHUB_TOKEN": theSecret},
			url:     "https://gitlab.com/acme/web.git",
			wantSet: false,
		},
		{
			name:    "nothing configured resolves nothing",
			url:     "https://git.example.test/acme/web.git",
			wantSet: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range []string{"GINTRACK_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"} {
				t.Setenv(name, "")
			}
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			user, secret := environmentToken(tc.url)
			if (secret != "") != tc.wantSet {
				t.Fatalf("secret present = %v, want %v", secret != "", tc.wantSet)
			}
			if tc.wantSet && user != tc.wantUser {
				t.Fatalf("username = %q, want %q", user, tc.wantUser)
			}
		})
	}
}

func TestHostOf(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"https", "https://git.example.test/acme/web.git", "git.example.test"},
		{"https with credentials", "https://jose:" + theSecret + "@git.example.test/a.git", "git.example.test"},
		{"scp-like ssh", "git@github.com:acme/web.git", "github.com"},
		{"ssh scheme", "ssh://git@github.com:22/acme/web.git", "github.com"},
		{"a local path has no host", "/srv/git/web.git", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hostOf(tc.in); got != tc.want {
				t.Fatalf("hostOf(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsSSHRemote(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"scp-like", "git@github.com:acme/web.git", true},
		{"ssh scheme", "ssh://git@github.com/acme/web.git", true},
		{"https", "https://github.com/acme/web.git", false},
		{"a local path", "/srv/git/web.git", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSSHRemote(tc.in); got != tc.want {
				t.Fatalf("isSSHRemote(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// unauthorizedHost is a git host that refuses everything with a 401, which is
// what a private repository does to a client with no credential.
func unauthorizedHost(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// TestAuthRequiredIsFastAndActionable is the "never hang on a prompt" half of
// the story: a host that asks for a credential nobody can supply has to fail
// with git_auth_required in milliseconds, saying which repository, which host
// and what to do about it.
func TestAuthRequiredIsFastAndActionable(t *testing.T) {
	host := unauthorizedHost(t)
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			repo := newRepo(t)
			g := gitRunner(t, repo)
			trackRemote(t, g, host+"/acme/web.git")

			backend := open(t, repo, kind)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			started := time.Now()
			_, err := backend.Fetch(ctx, FetchRequest{})
			elapsed := time.Since(started)

			if err == nil {
				t.Fatal("a 401 host must not fetch successfully")
			}
			if got := CodeOf(err); got != CodeAuthRequired {
				t.Fatalf("code = %q, want %q (error: %v)", got, CodeAuthRequired, err)
			}
			if elapsed > 20*time.Second {
				t.Fatalf("the fetch took %s: it waited on something interactive", elapsed)
			}
			message := err.Error()
			for _, want := range []string{repo, "127.0.0.1", "nothing was changed locally"} {
				if !strings.Contains(message, want) {
					t.Fatalf("the message does not name %q: %s", want, message)
				}
			}
		})
	}
}

// TestNoCredentialReachesDisk is the milestone-5 exit criterion in Go: with a
// token in the remote URL and a syncing pipeline that fails against it, nothing
// under the user's home or configuration directory may end up holding it, and
// no report, message or progress event may echo it.
func TestNoCredentialReachesDisk(t *testing.T) {
	host := unauthorizedHost(t)
	for _, kind := range backends(t) {
		t.Run(string(kind), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

			repo := newRepo(t)
			remote := strings.Replace(host, "http://", "http://jose:"+theSecret+"@", 1) + "/acme/web.git"
			g := gitRunner(t, repo)
			trackRemote(t, g, remote)

			backend := open(t, repo, kind)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			var events []string
			opts := syncOpts(StrategyMerge)
			opts.Progress = func(p Progress) { events = append(events, p.Message) }
			res, err := Sync(ctx, backend, opts)
			if err == nil {
				t.Fatal("the sync against an unauthorized host must fail")
			}

			t.Run("the failure never echoes the token", func(t *testing.T) {
				surfaces := append([]string{err.Error(), res.Message, res.Before.RemoteURL,
					res.After.RemoteURL}, events...)
				for _, text := range surfaces {
					if strings.Contains(text, theSecret) {
						t.Fatalf("a reported surface carries the token: %s", text)
					}
				}
			})

			t.Run("the remote URL is reported redacted", func(t *testing.T) {
				status, statusErr := backend.SyncStatus(ctx)
				if statusErr != nil {
					t.Fatalf("SyncStatus: %v", statusErr)
				}
				if strings.Contains(status.RemoteURL, theSecret) {
					t.Fatalf("the status carries the token: %s", status.RemoteURL)
				}
				if !strings.Contains(status.RemoteURL, "***@") {
					t.Fatalf("the status URL is not redacted: %s", status.RemoteURL)
				}
			})

			t.Run("nothing under the home directory holds the token", func(t *testing.T) {
				if found := grepTree(t, home); found != "" {
					t.Fatalf("%s holds the token", found)
				}
			})

			t.Run("only the repository configuration the user wrote holds it", func(t *testing.T) {
				// The token is in `.git/config` because the user put it there
				// with `git remote add`. Anything else under the working tree
				// would be ours, and there must be none.
				found := []string{}
				walkFiles(t, repo, func(path string, text string) {
					if strings.Contains(text, theSecret) {
						found = append(found, filepath.ToSlash(mustRel(t, repo, path)))
					}
				})
				for _, path := range found {
					if path != ".git/config" {
						t.Fatalf("%s holds the token and we wrote it", path)
					}
				}
			})
		})
	}
}

// grepTree returns the first file under root whose bytes hold the token.
func grepTree(t *testing.T, root string) string {
	t.Helper()
	hit := ""
	walkFiles(t, root, func(path, text string) {
		if hit == "" && strings.Contains(text, theSecret) {
			hit = path
		}
	})
	return hit
}

// walkFiles reads every regular file under root and hands its text to visit.
func walkFiles(t *testing.T, root string, visit func(path, text string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil //nolint:nilerr // an unreadable entry cannot hold our token
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // path comes from the test's own temporary tree
		if readErr != nil {
			return nil //nolint:nilerr // same
		}
		visit(path, string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// mustRel makes a path relative for a readable failure.
func mustRel(t *testing.T, base, path string) string {
	t.Helper()
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

// trackRemote gives a fixture repository a remote its branch tracks, without
// ever talking to it: the tracking ref is written locally, which is enough for
// the pipeline to get as far as the fetch.
func trackRemote(t *testing.T, g func(args ...string), url string) {
	t.Helper()
	g("remote", "add", "origin", url)
	g("config", "branch.main.remote", "origin")
	g("config", "branch.main.merge", "refs/heads/main")
	g("update-ref", "refs/remotes/origin/main", "HEAD")
}
