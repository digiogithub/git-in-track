---
id: GIT-US-0112
type: story
title: List changed files between two refs in every git backend
status: done
priority: high
parent: GIT-EP-0024
milestone: GIT-M-0015
author: claude
labels: [git, agent-ok]
estimate: 5
created: 2026-09-24T12:10:07Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
---

## Description

As the trace and impact engines, I need to know which files (and which line ranges) changed between two refs, or between a ref and the working tree, so suspect detection and impact can work from a diff. `internal/gitops` has no diff primitive today.

## Acceptance Criteria

- [x] `gitops` exposes `ChangedFiles(from, to string) ([]FileChange, error)` where `to` may be the worktree; each `FileChange` carries path, old path on rename, status (added/modified/deleted/renamed) and changed line ranges on the new side.
- [x] Implemented for the go-git backend, the system-git shell-out and the jj backend, with identical results on the same history.
- [x] Refs accept branch names, SHAs and `origin/main`-style remotes; an unknown ref is a typed error.
- [x] Table-driven tests against fixture repositories for each backend (jj tests skip when `jj` is absent).
- [x] docs/06-git-sync.md documents the primitive.

## Notes

No dependency on the spec data model; can start immediately. Native only: never imported from `internal/core` or `internal/vault`.
