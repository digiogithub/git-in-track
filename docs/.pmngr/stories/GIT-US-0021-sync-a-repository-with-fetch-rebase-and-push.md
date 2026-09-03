---
id: GIT-US-0021
type: story
title: Sync a repository with fetch, rebase and push
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: critical
parent: GIT-EP-0005
milestone: GIT-M-0005
estimate: 8
labels: [git, server, web]
links:
  - kind: blocked_by
    target: GIT-US-0020
---

## Description

As a team member, I want a sync action that brings in everyone else's changes and publishes
mine, so that git is the sync mechanism without me needing a terminal.

Sync is fetch, then rebase (or merge, configurable), then push, with a preview of what will
happen before anything is done, a clear per-repository status (ahead, behind, diverged,
dirty), and a progress indicator with cancellation. In browser-only mode the same flow runs
on isomorphic-git over the File System Access handles, which requires a CORS proxy for most
hosts (risk R3) — this is surfaced honestly in the UI, not hidden.

Every failure mode leaves a working tree the user can recover from, with an explanation of
what to do next.

## Acceptance Criteria

- [ ] Sync performs fetch, rebase-or-merge and push, in that order.
- [ ] The strategy (rebase or merge) is configurable per project.
- [ ] Per-repository status shows ahead/behind counts and uncommitted changes.
- [ ] A dry-run preview lists incoming and outgoing commits before acting.
- [ ] The operation is cancellable and honours context deadlines.
- [ ] Browser mode syncs via isomorphic-git and reports the CORS proxy requirement.
- [ ] A rejected push is explained with the actual reason and a suggested next step.
- [ ] An interrupted sync leaves a recoverable tree; no partial state is hidden.
- [ ] Integration tests run against a local bare repository in a temp directory.
