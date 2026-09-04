package gitops

import (
	"bufio"
	"context"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// Credentials in native mode (story GIT-US-0023, docs/06-git-sync.md section 8.1).
//
// The companion stores no secret of its own. It authenticates the way the
// user's own `git` does: the configured `credential.helper` over HTTPS and the
// SSH agent over SSH. Nothing in this package writes a token to a file, to a
// keychain or to the git configuration, and nothing prompts for one — every
// invocation is deliberately non-interactive, so a missing credential fails
// with an actionable message in milliseconds instead of hanging a background
// process on a terminal that nobody is watching.
//
// The two runtimes differ only in who calls the helper:
//
//   - the system backend inherits it for free, because it is `git` itself that
//     runs the helper (docs/06 section 7.3);
//   - the go-git backend has no helper support, so it shells out to
//     `git credential fill` when a git binary exists, and falls back to a token
//     in the environment for CI. The value lives in a local variable for the
//     duration of one fetch or push and is never held anywhere else.

// The environment variables every git invocation is pinned to. Together they
// make it impossible for a credential prompt to block us: git will not open the
// terminal, will not run an askpass helper (the user's GUI one included), and
// ssh runs in batch mode, so an unusable key fails instead of asking for a
// passphrase.
const (
	// envTerminalPrompt disables git's own terminal prompt.
	envTerminalPrompt = "GIT_TERMINAL_PROMPT=0"
	// envAskPass is deliberately empty: git treats an empty GIT_ASKPASS as "no
	// askpass program" and, because the variable is set, never falls back to
	// core.askPass or SSH_ASKPASS.
	envAskPass = "GIT_ASKPASS="
	// envSSHAskPass blanks the ssh-side askpass for the same reason.
	envSSHAskPass = "SSH_ASKPASS="
	// envOptionalLocks keeps a status call from taking the index lock.
	envOptionalLocks = "GIT_OPTIONAL_LOCKS=0"
	// envGCMInteractive stops Git Credential Manager from opening a window.
	envGCMInteractive = "GCM_INTERACTIVE=never"
)

// batchModeOption is what turns an ssh passphrase or host-key question into an
// immediate failure. It is appended to whatever GIT_SSH_COMMAND the user
// already configured rather than replacing it, so a custom ssh binary, a
// `-F` config or a jump host keeps working.
const batchModeOption = " -o BatchMode=yes"

// nonInteractiveEnv returns base with the credential-related variables pinned.
// exec uses the last value of a duplicated key, so appending is enough.
func nonInteractiveEnv(base []string) []string {
	out := make([]string, 0, len(base)+6)
	out = append(out, base...)
	out = append(out,
		envTerminalPrompt,
		envAskPass,
		envSSHAskPass,
		envOptionalLocks,
		envGCMInteractive,
		"GIT_SSH_COMMAND="+sshCommand(base),
	)
	return out
}

// sshCommand extends the caller's GIT_SSH_COMMAND with batch mode, or builds
// the minimal one when there is none.
func sshCommand(base []string) string {
	current := ""
	for _, entry := range base {
		if value, ok := strings.CutPrefix(entry, "GIT_SSH_COMMAND="); ok {
			current = value
		}
	}
	current = strings.TrimSpace(current)
	if current == "" {
		current = "ssh"
	}
	if strings.Contains(current, "BatchMode") {
		return current
	}
	return current + batchModeOption
}

// secretPatterns are the shapes a credential takes in text we did not write:
// the userinfo of a remote URL, and the query parameters hosts use for tokens.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/@\s]+@`),
	regexp.MustCompile(`(?i)\b((?:access_|private_|api_)?token|password)=[^\s&"']+`),
	regexp.MustCompile(`(?i)\b(authorization:\s*(?:basic|bearer))\s+\S+`),
}

// redactSecrets removes every credential shape from free text before it reaches
// a log, an error message, a progress event or the UI. It is applied to
// everything git prints, because a remote URL with a token in it is exactly
// what an authentication failure quotes back at us.
func redactSecrets(text string) string {
	if text == "" {
		return text
	}
	out := secretPatterns[0].ReplaceAllString(text, "$1***@")
	out = secretPatterns[1].ReplaceAllString(out, "$1=***")
	return secretPatterns[2].ReplaceAllString(out, "$1 ***")
}

// hostOf reports the host of a remote URL, for both `https://host/o/r.git` and
// the scp-like `git@host:o/r.git`. It never returns the userinfo.
func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	if _, rest, ok := strings.Cut(raw, "@"); ok {
		host, _, _ := strings.Cut(rest, ":")
		return host
	}
	return ""
}

// isSSHRemote reports whether a remote URL is spoken over SSH.
func isSSHRemote(raw string) bool {
	if strings.HasPrefix(raw, "ssh://") || strings.HasPrefix(raw, "git+ssh://") {
		return true
	}
	return !strings.Contains(raw, "://") && strings.Contains(raw, "@") && strings.Contains(raw, ":")
}

// transportContext is what a transport failure needs to be actionable: which
// repository, which remote and which host it was talking to.
type transportContext struct {
	// Op is the operation that failed, "fetch" or "push".
	Op string
	// Path is the working tree.
	Path string
	// Remote is the remote name, normally "origin".
	Remote string
	// URL is the remote URL; it is redacted before it reaches a message.
	URL string
}

// describe renders "origin (https://host/o/r.git) in /path" for a message.
func (c transportContext) describe() string {
	out := c.Remote
	if out == "" {
		out = "the remote"
	}
	if c.URL != "" {
		out += " (" + redactURL(c.URL) + ")"
	}
	if c.Path != "" {
		out += " in " + c.Path
	}
	return out
}

// host reports the remote host, falling back to a readable placeholder.
func (c transportContext) host() string {
	if h := hostOf(c.URL); h != "" {
		return h
	}
	return "the git host"
}

// authError builds the credential failure of a transport. There are two of
// them and they need different reactions, so they get different wording:
// nothing answered for this host, or something answered and was refused.
func (c transportContext) authError(err error, output, text string) *Error {
	unanswered := containsAny(text, "could not read username", "could not read password",
		"terminal prompts disabled", "no askpass", "authentication required")
	switch {
	case isSSHRemote(c.URL):
		return &Error{
			Code: CodeAuthRequired, Op: c.Op, Err: err, Detail: redactSecrets(output),
			Message: "the SSH key offered to " + c.host() + " was refused for " + c.describe() +
				": gintrack stores no key of its own and asks your ssh-agent, so add the key it " +
				"needs with `ssh-add` (or check `ssh -T git@" + c.host() +
				"`) and sync again — nothing was changed locally",
		}
	case unanswered:
		return &Error{
			Code: CodeAuthRequired, Op: c.Op, Err: err, Detail: redactSecrets(output),
			Message: "no git credential helper answered for " + c.host() + " when syncing " +
				c.describe() + ": gintrack never stores or asks for a token, it runs git " +
				"non-interactively so it can never hang on a prompt. Configure a helper " +
				"(`git config --global credential.helper …`), run `git -C " + c.pathOrDot() +
				" fetch " + c.remoteOrOrigin() + "` once on a terminal to let it save one, or use " +
				"an SSH remote with an agent key — nothing was changed locally",
		}
	default:
		return &Error{
			Code: CodeAuthRequired, Op: c.Op, Err: err, Detail: redactSecrets(output),
			Message: c.host() + " refused the credentials your git credential helper supplied for " +
				c.describe() + ": the token is missing, expired or lacks `contents: read/write` " +
				"on this repository. Replace it in your helper (`git credential reject` then " +
				"`git -C " + c.pathOrDot() + " fetch " + c.remoteOrOrigin() +
				"`) and sync again — nothing was changed locally",
		}
	}
}

// pathOrDot renders the repository for a copy-pasteable command.
func (c transportContext) pathOrDot() string {
	if c.Path == "" {
		return "."
	}
	return c.Path
}

// remoteOrOrigin renders the remote for a copy-pasteable command.
func (c transportContext) remoteOrOrigin() string {
	if c.Remote == "" {
		return "origin"
	}
	return c.Remote
}

// ------------------------------------------------------------ go-git auth ---

// authFor resolves the transport credentials go-git needs for one remote URL.
//
// It returns nil when nothing is available, which is not a failure: the
// transport then tries anonymously and, if the host refuses, classifyTransport
// turns that into the actionable message above. Nothing resolved here is
// cached, logged or written anywhere.
func authFor(ctx context.Context, rawURL, gitBinary string) transport.AuthMethod {
	if rawURL == "" {
		return nil
	}
	if isSSHRemote(rawURL) {
		return sshAgentAuth(rawURL)
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil
	}
	if user, secret := credentialFill(ctx, gitBinary, rawURL); secret != "" {
		return &githttp.BasicAuth{Username: user, Password: secret}
	}
	if user, secret := environmentToken(rawURL); secret != "" {
		return &githttp.BasicAuth{Username: user, Password: secret}
	}
	return nil
}

// sshAgentAuth binds go-git to the running ssh-agent, which is the path that
// works with hardware keys and passphrase-protected keys without prompting
// (docs/06 section 7.2). Without an agent we offer nothing rather than reading
// a private key file ourselves.
func sshAgentAuth(rawURL string) transport.AuthMethod {
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		return nil
	}
	user := "git"
	if before, _, ok := strings.Cut(rawURL, "@"); ok {
		user = before[strings.LastIndexAny(before, "/:")+1:]
	}
	if user == "" {
		user = "git"
	}
	auth, err := gitssh.NewSSHAgentAuth(user)
	if err != nil {
		return nil
	}
	// The default callback verifies against ~/.ssh/known_hosts and refuses an
	// unknown host, which is the strict behavior docs/06 section 7.2 requires.
	return auth
}

// credentialFill asks the user's own credential helper, the way git does. The
// helper never learns anything it did not already know: it is given the
// protocol, host and path of the remote and nothing else.
//
// The answer is returned to the caller and is otherwise dropped: it is not
// stored, not logged and not put in a command line, which is why the exchange
// goes over stdin and stdout.
func credentialFill(ctx context.Context, gitBinary, rawURL string) (username, secret string) {
	name := gitBinary
	if name == "" {
		name = "git"
	}
	// LookPath rather than resolveGit: filling a credential needs no minimum
	// git version, and probing one would run a command of its own.
	bin, err := exec.LookPath(name)
	if err != nil {
		return "", ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", ""
	}
	// A URL that already carries a credential is the user's own configuration;
	// go-git sends it itself, so there is nothing to fill.
	if u.User != nil {
		return "", ""
	}
	request := "protocol=" + u.Scheme + "\nhost=" + u.Host + "\n"
	if path := strings.TrimPrefix(u.Path, "/"); path != "" {
		request += "path=" + path + "\n"
	}
	cmd := exec.CommandContext(ctx, bin, "credential", "fill") //nolint:gosec // bin comes from LookPath or explicit configuration
	cmd.Env = nonInteractiveEnv(os.Environ())
	cmd.Stdin = strings.NewReader(request + "\n")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	return parseCredentialFill(string(out))
}

// parseCredentialFill reads the `key=value` lines a helper answers with.
func parseCredentialFill(out string) (username, secret string) {
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		switch key {
		case "username":
			username = value
		case "password":
			secret = value
		}
	}
	return username, secret
}

// environmentToken is the headless fallback of docs/06 section 8.1: a token in
// the environment, for CI, where there is no helper and no keychain. It is read
// on every call and never copied anywhere.
func environmentToken(rawURL string) (username, secret string) {
	host := hostOf(rawURL)
	names := []string{"GINTRACK_TOKEN"}
	switch {
	case strings.Contains(host, "github"):
		names = append(names, "GITHUB_TOKEN")
	case strings.Contains(host, "gitlab"):
		names = append(names, "GITLAB_TOKEN")
	}
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return basicUsernameFor(host), value
		}
	}
	return "", ""
}

// basicUsernameFor is the username half a host expects next to a token in HTTP
// basic auth (docs/06 section 7.3).
func basicUsernameFor(host string) string {
	switch {
	case strings.Contains(host, "github"):
		return "x-access-token"
	case strings.Contains(host, "gitlab"):
		return "oauth2"
	default:
		return "token"
	}
}
