package gitops

import (
	"errors"
	"fmt"
)

// The machine codes this package fails with. They are the `code` of the
// problem document the API returns and the `ProviderErrorCode` the web app
// switches on, so a caller never has to match on a message.
const (
	// CodeNotARepository means the path is not inside a git working tree.
	CodeNotARepository = "git_not_a_repository"
	// CodeNoIdentity means neither the configuration nor the overrides supply a
	// user.name and user.email, so no commit can be attributed (docs/06 §3.3).
	CodeNoIdentity = "git_no_identity"
	// CodeHookFailed means a repository hook refused the commit. The hook's own
	// output is carried in the detail.
	CodeHookFailed = "git_hook_failed"
	// CodeCommitFailed is any other commit failure. The working tree is
	// untouched.
	CodeCommitFailed = "git_commit_failed"
	// CodeUnsupported means the resolved backend cannot do what was asked, for
	// example signing with go-git.
	CodeUnsupported = "git_unsupported"
	// CodeTemplateInvalid means the configured message template does not parse
	// or does not render.
	CodeTemplateInvalid = "git_template_invalid"
)

// The codes the sync pipeline adds (GIT-US-0021, docs/06 section 12). Every one
// of them describes a state the working tree can be recovered from.
const (
	// CodeNoRemote means the repository has no remote to sync against.
	CodeNoRemote = "git_no_remote"
	// CodeNoUpstream means the branch tracks no remote branch yet.
	CodeNoUpstream = "git_no_upstream"
	// CodeUnexpectedBranch means HEAD is detached or is not the branch the
	// repository is configured to sync; we never switch branches by ourselves.
	CodeUnexpectedBranch = "git_unexpected_branch"
	// CodeInProgress means a rebase or merge is half-finished and has to be
	// continued or aborted before anything else runs.
	CodeInProgress = "git_operation_in_progress"
	// CodeDirtyTree means uncommitted changes to tracked files block the
	// integration. Nothing was fetched.
	CodeDirtyTree = "git_dirty_tree"
	// CodeFetchFailed is any fetch failure that is neither auth nor network.
	CodeFetchFailed = "git_fetch_failed"
	// CodeAuthRequired means the host refused the credentials. Credential
	// storage itself is GIT-US-0023; this code is what asks for it.
	CodeAuthRequired = "git_auth_required"
	// CodeNetwork means the host was unreachable. Local work is untouched.
	CodeNetwork = "git_network_unavailable"
	// CodeHostKey means an SSH host key is not trusted; never auto-accepted.
	CodeHostKey = "git_host_key_unverified"
	// CodeConflict means an integration stopped on conflicted paths. The
	// rebase or merge is still in progress and is resumable.
	CodeConflict = "git_conflict"
	// CodeIntegrateFailed is any other rebase or merge failure.
	CodeIntegrateFailed = "git_integrate_failed"
	// CodePushRejected means the remote refused the push: a non-fast-forward
	// race, or a policy such as a protected branch. Local commits are intact.
	CodePushRejected = "git_push_rejected"
	// CodePushFailed is any other push failure.
	CodePushFailed = "git_push_failed"
	// CodeCancelled means the caller cancelled the run or its deadline expired.
	CodeCancelled = "git_cancelled"
	// CodeSyncFailed is the fallback classification of a sync failure.
	CodeSyncFailed = "git_sync_failed"
	// CodeNotFound means the path asked about is not conflicted, normally
	// because the integration moved on while the resolver was open
	// (GIT-US-0022).
	CodeNotFound = "not_found"
)

// ErrGit is the sentinel behind every failure of this package, so a caller can
// classify one with errors.Is without inspecting the fields.
var ErrGit = errors.New("git operation failed")

// Error is a git failure with a machine code and an actionable message.
type Error struct {
	// Code is one of the constants above.
	Code string
	// Op is the operation that failed: "commit", "status", "open".
	Op string
	// Message is what to tell the user; it is written to be actionable.
	Message string
	// Detail is the underlying output, for example what a hook printed. It is
	// never a credential: this package never handles one.
	Detail string
	// Err is the wrapped cause, when there is one.
	Err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	msg := e.Op + ": " + e.Message
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// Unwrap reports the cause, falling back to ErrGit so that errors.Is(err,
// ErrGit) classifies every failure of this package.
func (e *Error) Unwrap() error {
	if e.Err != nil {
		return e.Err
	}
	return ErrGit
}

// Is makes every Error match ErrGit even when it wraps a concrete cause.
func (e *Error) Is(target error) bool { return target == ErrGit }

// CodeOf returns the machine code of an error, or the empty string when it did
// not come from this package.
func CodeOf(err error) string {
	var gitErr *Error
	if errors.As(err, &gitErr) {
		return gitErr.Code
	}
	return ""
}

// failf builds an Error with no wrapped cause.
func failf(op, code, format string, args ...any) *Error {
	return &Error{Code: code, Op: op, Message: fmt.Sprintf(format, args...)}
}

// wrap builds an Error around a cause.
func wrap(op, code string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Op: op, Message: fmt.Sprintf(format, args...), Err: err}
}

// noIdentity is the one error the user is most likely to hit, so its wording is
// fixed and tested: it names the exact commands that fix it.
func noIdentity(op, path string) *Error {
	return &Error{
		Code: CodeNoIdentity,
		Op:   op,
		Message: "no git identity is configured for " + path +
			`: run "git config user.name" and "git config user.email" in the repository, ` +
			"or set git.authorName and git.authorEmail in the gintrack configuration",
	}
}
