// Package gitops wraps git behind one interface with two implementations: an
// in-process go-git backend and a shell-out backend that drives the system git
// binary, chosen by the `git.backend` setting (auto, go-git or system).
//
// What this package does today (story GIT-US-0020):
//
//   - resolve the backend and report its Capabilities, so the UI can hide what
//     a backend cannot do instead of failing at the last step (ADR-006);
//   - resolve the commit Identity from the git configuration chain, and refuse
//     to invent one;
//   - render a commit message from a configurable text/template with the
//     placeholders and the machine-readable trailers of docs/06-git-sync.md
//     section 3.3;
//   - stage exactly the paths a write touched and commit them, honouring hooks
//     and signing in system-git mode;
//   - batch rapid writes with a Committer, so one logical edit is one commit
//     even when the editor saves on every keystroke.
//
// What lands later: fetch, integrate (rebase or merge), push, conflict
// inspection and resolution with GIT-US-0021 and GIT-US-0022, and credential
// handling with GIT-US-0023. Those methods are deliberately absent from the
// Backend interface rather than declared and unimplemented.
//
// This package is native-only: it uses os/exec and the filesystem, so it must
// never be imported from internal/core, which compiles to WebAssembly. Git in
// the browser is isomorphic-git, driven from the web app, and shares no code
// with this package; what the two must agree on is the commit message format,
// which is documented (docs/06 section 3.3) and mirrored in
// web/src/git/message.ts, not shared.
package gitops
