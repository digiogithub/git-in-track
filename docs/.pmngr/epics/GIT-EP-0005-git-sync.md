---
id: GIT-EP-0005
type: epic
title: Git sync
status: in_progress
priority: critical
milestone: GIT-M-0005
author: team
labels: [git, server, web, security]
estimate: 26
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-EP-0003 }
---

## Description

Phase 4. Git is the only sync mechanism in git-in-track, and this epic makes it usable from
inside the product: commit on save with a message template, explicit sync as fetch plus
rebase or merge plus push, a conflict UI for text and front-matter conflicts, and
credential handling that never persists a secret.

Native mode uses go-git (or the system `git` binary, configurable). Browser-only mode uses
isomorphic-git over File System Access handles, with the documented CORS-proxy caveat.

## Acceptance Criteria

- [ ] Two clones edited concurrently are reconciled through the UI, including one real
      conflict, without touching a terminal.
- [ ] No credential is written to disk or to `localStorage` by git-in-track, proven by a
      test.
- [ ] Commit on save produces one commit per logical edit, not one per keystroke.
- [ ] A push failure leaves a recoverable working tree and an actionable message.
- [ ] The CORS limitation for browser-only mode is documented with a working proxy recipe.

## Notes

Stories: GIT-US-0020 … GIT-US-0023. See docs/11-roadmap.md, milestone 5.
Primary risks: R3 (CORS for browser git), R5 (front-matter merges).
