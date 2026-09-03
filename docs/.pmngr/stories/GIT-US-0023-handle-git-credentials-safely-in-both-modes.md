---
id: GIT-US-0023
type: story
title: Handle git credentials safely in both modes
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: critical
parent: GIT-EP-0005
milestone: GIT-M-0005
estimate: 5
labels: [git, security]
links:
  - kind: blocked_by
    target: GIT-US-0021
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

- [ ] Native mode uses the system credential helper and SSH agent; nothing is stored.
- [ ] Browser mode holds a token in memory only, cleared on tab close or sign-out.
- [ ] No credential is ever written to `localStorage`, IndexedDB, or disk — test-enforced.
- [ ] Tokens are redacted from all logs, errors and UI output.
- [ ] A token is scoped to the remote it was entered for and never reused elsewhere.
- [ ] The user is warned before any credential would traverse a configured CORS proxy.
- [ ] SSH and HTTPS remotes are both supported natively.
- [ ] Authentication failures produce a specific, actionable message.
- [ ] `gitleaks` and secret scanning stay clean on the repository.
