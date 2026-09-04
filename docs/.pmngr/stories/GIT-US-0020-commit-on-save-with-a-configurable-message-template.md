---
id: GIT-US-0020
type: story
title: Commit on save with a configurable message template
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: high
parent: GIT-EP-0005
milestone: GIT-M-0005
estimate: 5
labels: [git, server]
links:
  - kind: blocked_by
    target: GIT-US-0014
---

## Description

As a user, I want my edits optionally committed as I make them, so that my backlog has a
useful history without me remembering to commit after every change.

Commit on save is **off by default**. When enabled, edits are batched over a short window so
a burst of keystrokes becomes one commit, and the message is rendered from a configurable
template such as `{{action}} {{id}}: {{title}}`. The author is taken from the git
configuration, never invented.

`internal/gitops` wraps go-git for the native path and can shell out to the system `git`
binary when configured, which matters for users whose credential helpers, hooks or signing
setup only work with real git.

## Acceptance Criteria

- [ ] Commit on save is disabled by default and configurable per project.
- [ ] Rapid successive edits produce one commit, not one per keystroke.
- [ ] The message template supports `{{action}}`, `{{id}}`, `{{type}}`, `{{title}}`.
- [ ] The commit author comes from git configuration; missing identity is reported clearly.
- [ ] Only the files the edit actually touched are staged.
- [ ] The backend is selectable between go-git and the system `git` binary.
- [ ] A failed commit surfaces an actionable error and never loses the file content.
- [ ] Pre-commit hooks are honoured in system-git mode and a hook failure is reported.
