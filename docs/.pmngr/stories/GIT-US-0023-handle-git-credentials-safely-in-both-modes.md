---
id: GIT-US-0023
type: story
title: Handle git credentials safely in both modes
status: in_review
priority: critical
parent: GIT-EP-0005
milestone: GIT-M-0005
author: team
labels: [git, security]
estimate: 5
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0021 }
---

## Description

As a user, I want git-in-track to authenticate to my hosts without ever becoming a place my
credentials can leak from, so that installing it does not widen my attack surface.

Native mode stores nothing: it delegates to the user's existing git credential helper and
SSH agent, exactly as the `git` CLI does. Browser-only mode has no such facility, so it asks
for a personal access token per session, keeps it in memory only — never `localStorage`,
never IndexedDB — and scopes it to the single remote it was entered for.

Tokens are redacted everywhere: logs, error messages, telemetry (of which there is none) and
UI surfaces. A CORS proxy never receives a credential unless the user explicitly configured
that proxy.

## Acceptance Criteria

- [x] Native mode uses the system credential helper and SSH agent; nothing is stored.
- [x] Browser mode holds a token in memory only, cleared on tab close or sign-out.
- [x] No credential is ever written to `localStorage`, IndexedDB, or disk — test-enforced.
- [x] Tokens are redacted from all logs, errors and UI output.
- [x] A token is scoped to the remote it was entered for and never reused elsewhere.
- [x] The user is warned before any credential would traverse a configured CORS proxy.
- [x] SSH and HTTPS remotes are both supported natively.
- [x] Authentication failures produce a specific, actionable message.
- [x] `gitleaks` and secret scanning stay clean on the repository.

## Notes

As built:

- **Native.** `internal/gitops/credentials.go` pins every git invocation
  non-interactive (`GIT_TERMINAL_PROMPT=0`, empty `GIT_ASKPASS`/`SSH_ASKPASS`,
  `GCM_INTERACTIVE=never`, ssh `BatchMode=yes` appended to the user's own
  `GIT_SSH_COMMAND`), so a missing credential fails with `git_auth_required` in
  milliseconds instead of hanging. The system backend inherits the user's
  helper; the go-git backend calls `git credential fill`, then the ssh-agent for
  SSH remotes, then `GINTRACK_TOKEN`/`GITHUB_TOKEN`/`GITLAB_TOKEN` for headless
  use. The keychain, terminal prompt and plaintext-file steps that docs/06 §8.1
  used to describe are deliberately not implemented, and the document now says
  so.
- **Browser.** `web/src/git/credentials.ts` holds a token in a module-level map
  keyed by the remote's origin, prompted for by
  `web/src/features/sync/CredentialPrompt.tsx` only when `onAuth` fires. The
  prompt names the configured CORS proxy that would see the token. Nothing is
  persisted, `onAuthFailure` drops a refused token, and unmount, sign-out,
  reload and "Forget tokens" clear the map.
- **Redaction.** `redactSecrets` masks URL userinfo, `token=`/`password=`
  parameters and `Authorization` headers in everything git prints, before it can
  reach an error, the API problem document, a `sync.progress` event or a log
  line.
- **Exit criterion.** `TestNoCredentialReachesDisk` (Go, both backends) and
  `credentials.test.ts` → "no credential is ever persisted" (web) are the
  test-enforced halves of "never written to disk or `localStorage`".
