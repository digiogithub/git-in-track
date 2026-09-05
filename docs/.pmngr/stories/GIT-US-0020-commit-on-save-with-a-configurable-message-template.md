---
id: GIT-US-0020
type: story
title: Commit on save with a configurable message template
status: in_review
priority: high
parent: GIT-EP-0005
milestone: GIT-M-0005
author: team
labels: [git, server]
estimate: 5
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0014 }
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

- [x] Commit on save is disabled by default and configurable per project.
- [x] Rapid successive edits produce one commit, not one per keystroke.
- [x] The message template supports `{{action}}`, `{{id}}`, `{{type}}`, `{{title}}`.
- [x] The commit author comes from git configuration; missing identity is reported clearly.
- [x] Only the files the edit actually touched are staged.
- [x] The backend is selectable between go-git and the system `git` binary.
- [x] A failed commit surfaces an actionable error and never loses the file content.
- [x] Pre-commit hooks are honoured in system-git mode and a hook failure is reported.

## Notes

Implemented in `internal/gitops` (the two backends, the message template and the
batching committer), `internal/server` (the write-path hook and the
`/api/v1/git/*` routes), `internal/config`, `cmd/gintrack` (`serve` wiring and
the `doctor` environment check) and the web app (the provider members, the
browser-side message renderer and the Settings card).

Browser-only mode stores the settings and renders the message with the same
format, but commits nothing yet: git in the browser is isomorphic-git, which
GIT-US-0021 owns (docs/06 section 6.0). The Settings card reports that rather
than offering a switch that would do nothing.

Scope note on "configurable per project": the settings are configured per
workspace (the `git:` section of the companion configuration, or the browser's
per-workspace entry) and apply to every repository that workspace serves. The
per-repository override that docs/06 section 13 sketches
(`workspaces[].repos[].git`) is not implemented; it belongs with the per-repo
branch policy of GIT-US-0021, which needs the same mechanism.

Two documentation conflicts were settled while implementing, both recorded in
docs/06 section 3.3: the configuration key is `commitDebounce` (a Go duration,
matching `index.debounce`) with `commitDebounceMs` on the JSON surfaces, and the
short placeholder form `{{action}} {{id}}: {{title}}` is supported alongside the
`{{.ItemID}}` field form rather than instead of it.
