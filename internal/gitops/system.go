package gitops

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// systemBackend shells out to the git binary. It is the default whenever a
// usable one is on PATH, because it inherits everything the user already
// configured: credential helpers, hooks, signing, LFS and `includeIf` rules
// (docs/06 section 7.1).
type systemBackend struct {
	path    string
	git     string
	version string
	opts    Options
}

// openSystem binds the system backend to a working tree.
func openSystem(path string, opts Options) (Backend, error) {
	bin, version, err := resolveGit(opts.GitBinary)
	if err != nil {
		return nil, wrap("open", CodeUnsupported, err,
			"the system git backend is not usable")
	}
	b := &systemBackend{path: path, git: bin, version: version, opts: opts}
	out, err := b.run(context.Background(), "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(out) != "true" {
		return nil, wrap("open", CodeNotARepository, err,
			"%s is not inside a git working tree", path)
	}
	return b, nil
}

// Name reports the backend name.
func (b *systemBackend) Name() string { return string(KindSystem) }

// Path reports the working tree.
func (b *systemBackend) Path() string { return b.path }

// Capabilities describes what the system binary brings.
func (b *systemBackend) Capabilities() Capabilities {
	return Capabilities{
		Backend:           string(KindSystem),
		Version:           b.version,
		Hooks:             true,
		Signing:           true,
		CredentialHelpers: true,
		PathspecCommit:    true,
	}
}

// Identity resolves the author from the overrides, then from `git config`.
func (b *systemBackend) Identity(ctx context.Context) (Identity, error) {
	if id := (Identity{Name: b.opts.AuthorName, Email: b.opts.AuthorEmail}); id.Valid() {
		return id, nil
	}
	name, _ := b.run(ctx, "config", "--get", "user.name")
	email, _ := b.run(ctx, "config", "--get", "user.email")
	id := Identity{Name: strings.TrimSpace(name), Email: strings.TrimSpace(email)}
	if !id.Valid() {
		return Identity{}, noIdentity("identity", b.path)
	}
	return id, nil
}

// Status reports the branch and the dirty set, read from the porcelain v1
// format, which is stable across every git version we support.
func (b *systemBackend) Status(ctx context.Context) (Status, error) {
	out := Status{Staged: []string{}, Modified: []string{}, Untracked: []string{}}

	branch, err := b.run(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		// An unborn branch has no HEAD to resolve; the symbolic ref still names it.
		if sym, symErr := b.run(ctx, "symbolic-ref", "--short", "HEAD"); symErr == nil {
			branch = sym
		} else {
			return Status{}, wrap("status", CodeCommitFailed, err, "read HEAD of %s", b.path)
		}
	}
	out.Branch = strings.TrimSpace(branch)
	out.Detached = out.Branch == "HEAD"

	raw, err := b.run(ctx, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return Status{}, wrap("status", CodeCommitFailed, err, "read the status of %s", b.path)
	}
	for _, line := range strings.Split(raw, "\n") {
		if len(line) < 4 {
			continue
		}
		index, worktree, path := line[0], line[1], strings.TrimSpace(line[3:])
		// A rename reports "old -> new"; the new path is the one that matters.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+4:]
		}
		path = strings.Trim(path, `"`)
		switch {
		case index == '?' && worktree == '?':
			out.Untracked = append(out.Untracked, path)
		default:
			if index != ' ' {
				out.Staged = append(out.Staged, path)
			}
			if worktree != ' ' {
				out.Modified = append(out.Modified, path)
			}
		}
	}
	out.Staged, out.Modified, out.Untracked =
		normalisePaths(out.Staged), normalisePaths(out.Modified), normalisePaths(out.Untracked)
	out.Clean = len(out.Staged)+len(out.Modified)+len(out.Untracked) == 0
	return out, nil
}

// Commit stages exactly the requested paths and commits them with the message
// on stdin, so no shell quoting can mangle it. Hooks run, and a hook that
// refuses the commit is reported with its own output (AC 8).
func (b *systemBackend) Commit(ctx context.Context, req CommitRequest) (CommitResult, error) {
	paths := normalisePaths(req.Paths)
	if len(paths) == 0 && !req.AllowEmpty {
		return CommitResult{Empty: true}, nil
	}
	author := req.Author
	if !author.Valid() {
		resolved, err := b.Identity(ctx)
		if err != nil {
			return CommitResult{}, err
		}
		author = resolved
	}

	// `add --all --` stages a modification, a creation and a deletion alike,
	// and only for the paths given, which is what AC 5 asks for.
	if len(paths) > 0 {
		args := append([]string{"add", "--all", "--"}, paths...)
		if _, err := b.run(ctx, args...); err != nil {
			return CommitResult{}, wrap("commit", CodeCommitFailed, err,
				"stage %d file(s) in %s", len(paths), b.path)
		}
	}
	changed, err := b.hasStagedChanges(ctx, paths)
	if err != nil {
		return CommitResult{}, err
	}
	if !changed && !req.AllowEmpty {
		return CommitResult{Empty: true, Paths: paths, Author: author}, nil
	}

	args := []string{"commit", "--file=-", "--cleanup=verbatim"}
	if req.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if req.Sign {
		args = append(args, "--gpg-sign")
	}
	// Limiting the commit to the pathspec is what keeps an unrelated staged
	// change out of our commit; it is the one thing go-git cannot do.
	if len(paths) > 0 {
		args = append(args, "--only", "--")
		args = append(args, paths...)
	}
	env := append(b.env(),
		"GIT_AUTHOR_NAME="+author.Name, "GIT_AUTHOR_EMAIL="+author.Email,
		"GIT_COMMITTER_NAME="+author.Name, "GIT_COMMITTER_EMAIL="+author.Email,
	)
	if _, err := b.runWith(ctx, env, req.Message.String(), args...); err != nil {
		return CommitResult{}, b.commitError(ctx, err, len(paths))
	}

	sha, err := b.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return CommitResult{}, wrap("commit", CodeCommitFailed, err, "read the new commit of %s", b.path)
	}
	return CommitResult{
		SHA:     strings.TrimSpace(sha),
		Subject: req.Message.Subject,
		Author:  author,
		Paths:   paths,
	}, nil
}

// hasStagedChanges reports whether the given paths differ between the index and
// HEAD. With no commit yet, anything in the index counts.
func (b *systemBackend) hasStagedChanges(ctx context.Context, paths []string) (bool, error) {
	if _, err := b.run(ctx, "rev-parse", "--verify", "HEAD"); err != nil {
		out, lsErr := b.run(ctx, "diff", "--cached", "--name-only")
		if lsErr != nil {
			return false, wrap("commit", CodeCommitFailed, lsErr, "inspect the index of %s", b.path)
		}
		return strings.TrimSpace(out) != "", nil
	}
	args := append([]string{"diff", "--cached", "--name-only", "--"}, paths...)
	out, err := b.run(ctx, args...)
	if err != nil {
		return false, wrap("commit", CodeCommitFailed, err, "inspect the index of %s", b.path)
	}
	return strings.TrimSpace(out) != "", nil
}

// commitError classifies a failed `git commit`. A repository with an executable
// pre-commit or commit-msg hook gets the dedicated code, because "your hook
// refused this" and "git broke" need different reactions from the user.
func (b *systemBackend) commitError(ctx context.Context, err error, count int) *Error {
	detail := outputOf(err)
	if b.hasCommitHook(ctx) {
		return &Error{
			Code: CodeHookFailed,
			Op:   "commit",
			Message: "a git hook refused the commit in " + b.path +
				" (the files on disk were not touched)",
			Detail: detail,
			Err:    err,
		}
	}
	return &Error{
		Code:    CodeCommitFailed,
		Op:      "commit",
		Message: "could not commit " + plural(count) + " in " + b.path + " (the files on disk were not touched)",
		Detail:  detail,
		Err:     err,
	}
}

// hasCommitHook reports whether the repository has an executable hook that can
// refuse a commit.
func (b *systemBackend) hasCommitHook(ctx context.Context) bool {
	dir := strings.TrimSpace(mustRun(b.run(ctx, "rev-parse", "--git-path", "hooks")))
	if dir == "" {
		return false
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(b.path, dir)
	}
	for _, name := range []string{"pre-commit", "commit-msg", "prepare-commit-msg"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return true
		}
	}
	return false
}

// env is the environment every git invocation runs with: the caller's, plus
// the switches of credentials.go that keep git non-interactive. A prompt inside
// a background commit or a sync would hang the companion forever, so a missing
// credential has to fail instead of asking (GIT-US-0023).
func (b *systemBackend) env() []string {
	return nonInteractiveEnv(os.Environ())
}

// run executes git in the working tree and returns its standard output.
func (b *systemBackend) run(ctx context.Context, args ...string) (string, error) {
	return b.runWith(ctx, b.env(), "", args...)
}

// runWith executes git with an explicit environment and stdin.
func (b *systemBackend) runWith(ctx context.Context, env []string, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, b.git, args...) //nolint:gosec // b.git comes from LookPath, args are built here
	cmd.Dir = b.path
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &commandError{
			args:   args,
			output: redactSecrets(strings.TrimSpace(stderr.String() + "\n" + stdout.String())),
			err:    err,
		}
	}
	return stdout.String(), nil
}

// mustRun drops the error of a diagnostic call; the caller only wants the text.
func mustRun(out string, _ error) string { return out }

// plural renders a file count for an error message.
func plural(n int) string {
	if n == 1 {
		return "1 file"
	}
	return itoa(n) + " files"
}

// itoa avoids a strconv import for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// commandError carries what a failed git invocation printed, which is the only
// actionable part of a hook refusal.
type commandError struct {
	args   []string
	output string
	err    error
}

// Error implements the error interface.
func (e *commandError) Error() string {
	msg := "git " + strings.Join(e.args, " ") + ": " + e.err.Error()
	if e.output != "" {
		msg += ": " + e.output
	}
	return msg
}

// Unwrap reports the exec failure.
func (e *commandError) Unwrap() error { return e.err }

// outputOf returns what a failed git invocation printed.
func outputOf(err error) string {
	var cmdErr *commandError
	if errors.As(err, &cmdErr) {
		return strings.TrimSpace(cmdErr.output)
	}
	return ""
}
