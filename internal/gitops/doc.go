// Package gitops wraps git behind one interface with two implementations: an
// in-process go-git backend and a shell-out backend that drives the system git
// binary, chosen at construction time.
//
// Responsibilities:
//
//   - status, log, stage and commit with the message template and the
//     machine-readable trailers specified in docs/06-git-sync.md;
//   - fetch, integrate (rebase or merge), push, and conflict inspection and
//     resolution, all cancellable through the context;
//   - credential handling: the system backend inherits credential helpers, the
//     SSH agent and GIT_ASKPASS, while go-git supports the SSH agent and
//     token-in-URL only and reports git_auth_failed with actionable detail.
//
// This package is native-only. Git in the browser is isomorphic-git, driven from
// the web app, and shares no code with this package; what the two must agree on
// is the commit message format, which is documented, not shared.
//
// Implementation lands in Phase 4 (docs/07 section 6.4).
package gitops
