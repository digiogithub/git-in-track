package hub

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// This file turns the configured repositories into clones on disk.
//
// Authentication to the forge uses one deploy key per repository. gitops pins
// GIT_SSH_COMMAND for every git it runs (internal/gitops/credentials.go), so a
// per-repository core.sshCommand would be overridden. Instead the hub writes an
// ssh configuration with one host alias per repository, points the clone's
// remote at the alias, and sets GIT_SSH_COMMAND="ssh -F <that file>" for the
// whole process: every later fetch and push, whoever runs it, picks the right
// key from the remote URL alone.

// aliasPrefix starts every ssh host alias the hub writes.
const aliasPrefix = "gintrack-hub-"

// sshTarget is the parsed form of an ssh remote.
type sshTarget struct {
	user, host, port, path string
}

// isSSHURL reports whether raw is an ssh remote: ssh://… or scp-like
// [user@]host:path.
func isSSHURL(raw string) bool {
	_, ok := parseSSH(raw)
	return ok
}

func parseSSH(raw string) (sshTarget, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "ssh://") {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return sshTarget{}, false
		}
		return sshTarget{user: u.User.Username(), host: u.Hostname(), port: u.Port(), path: u.Path}, true
	}
	if strings.Contains(raw, "://") || filepath.IsAbs(raw) {
		return sshTarget{}, false
	}
	hostPart, path, ok := strings.Cut(raw, ":")
	if !ok || hostPart == "" || path == "" || strings.Contains(hostPart, "/") {
		return sshTarget{}, false
	}
	t := sshTarget{host: hostPart, path: path}
	if user, host, hasUser := strings.Cut(hostPart, "@"); hasUser {
		t.user, t.host = user, host
	}
	return t, t.host != ""
}

// remoteURL is the URL the clone's origin uses: the configured one, or for an
// ssh remote with a deploy key, the same path behind the repository's alias.
func remoteURL(r RepoSpec) string {
	t, ok := parseSSH(r.URL)
	if !ok || r.DeployKey == "" {
		return r.URL
	}
	return aliasPrefix + r.ID + ":" + strings.TrimPrefix(t.path, "/")
}

// sshConfig renders the ssh configuration for every repository with a deploy
// key, and reports false when there is none.
func sshConfig(cfg *Config) (string, bool) {
	var b strings.Builder
	b.WriteString("# Written by gintrack-hub; regenerated on every start.\n")
	found := false
	for _, r := range cfg.Repos() {
		t, ok := parseSSH(r.URL)
		if !ok || r.DeployKey == "" {
			continue
		}
		found = true
		fmt.Fprintf(&b, "\nHost %s%s\n", aliasPrefix, r.ID)
		fmt.Fprintf(&b, "  HostName %s\n", t.host)
		if t.user != "" {
			fmt.Fprintf(&b, "  User %s\n", t.user)
		}
		if t.port != "" {
			fmt.Fprintf(&b, "  Port %s\n", t.port)
		}
		fmt.Fprintf(&b, "  IdentityFile %s\n", r.DeployKey)
		b.WriteString("  IdentitiesOnly yes\n")
		b.WriteString("  StrictHostKeyChecking accept-new\n")
		fmt.Fprintf(&b, "  UserKnownHostsFile %s\n", filepath.Join(cfg.DataDir, "known_hosts"))
	}
	return b.String(), found
}

// Prepare makes the data directory ready: it writes the ssh configuration,
// exports GIT_SSH_COMMAND for the process when one is needed, and clones every
// repository that is not on disk yet. An existing clone is reused as it is;
// the periodic sync brings it up to date.
func Prepare(ctx context.Context, cfg *Config, git string) error {
	if git == "" {
		git = "git"
	}
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "repos"), 0o750); err != nil {
		return fmt.Errorf("create the data directory: %w", err)
	}
	env := os.Environ()
	if text, ok := sshConfig(cfg); ok {
		path := filepath.Join(cfg.DataDir, "ssh_config")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		command := "ssh -F " + path
		if err := os.Setenv("GIT_SSH_COMMAND", command); err != nil {
			return fmt.Errorf("set GIT_SSH_COMMAND: %w", err)
		}
		env = append(env, "GIT_SSH_COMMAND="+command)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	for _, r := range cfg.Repos() {
		if err := ensureClone(ctx, git, env, cfg.RepoDir(r.ID), *r); err != nil {
			return err
		}
	}
	return nil
}

// ensureClone clones r into dir unless a git working tree is already there.
func ensureClone(ctx context.Context, git string, env []string, dir string, r RepoSpec) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("repository %s: %w", r.ID, err)
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("repository %s: %s exists, is not empty and is not a git working tree", r.ID, dir)
	}
	args := []string{"clone", "--quiet"}
	if r.Branch != "" {
		args = append(args, "--branch", r.Branch)
	}
	args = append(args, "--", remoteURL(r), dir)
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone repository %s: %w: %s", r.ID, err, redact(strings.TrimSpace(string(out))))
	}
	return nil
}

// userinfoRE matches the credentials of a URL, which git quotes back when a
// clone fails.
var userinfoRE = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/@\s]+@`)

// redact removes URL credentials from text that is about to be logged or
// returned.
func redact(text string) string { return userinfoRE.ReplaceAllString(text, "${1}***@") }
