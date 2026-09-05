---
id: GIT-US-0021
type: story
title: Sync a repository with fetch, rebase and push
status: in_review
priority: critical
parent: GIT-EP-0005
milestone: GIT-M-0005
author: team
labels: [git, server, web]
estimate: 8
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0020 }
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

- [x] Sync performs fetch, rebase-or-merge and push, in that order.
- [x] The strategy (rebase or merge) is configurable per project.
- [x] Per-repository status shows ahead/behind counts and uncommitted changes.
- [x] A dry-run preview lists incoming and outgoing commits before acting.
- [x] The operation is cancellable and honours context deadlines.
- [x] Browser mode syncs via isomorphic-git and reports the CORS proxy requirement.
- [x] A rejected push is explained with the actual reason and a suggested next step.
- [x] An interrupted sync leaves a recoverable tree; no partial state is hidden.
- [x] Integration tests run against a local bare repository in a temp directory.

## Notes

Implemented as `gitops.Sync` over the extended `gitops.Backend` (`SyncStatus`,
`Fetch`, `Integrate`, `Push`, `Abort`, `Continue`, `Commits`), served as
`/api/v1/sync/*`, driven from the CLI by `gintrack sync` and from the web app by
the `/sync` panel. Browser-only mode syncs through isomorphic-git over the File
System Access handles (`web/src/git/fsa-fs.ts`, `web/src/git/browser-sync.ts`).

Deliberately left to the stories that own them, and documented where they are
described:

- the structured conflict resolver — reading base/ours/theirs and continuing
  from a resolution — is GIT-US-0022; this story detects a conflict, names
  every path, leaves the rebase resumable and offers abort;
- credential storage and the token prompt are GIT-US-0023; this story surfaces
  `git_auth_required` with what to do about it and never writes a credential;
- branch policy (doc 06 §4.3: `user-branch`, `autoPr`, host URL templates) and
  `git.dirtyPolicy`'s `stash` and `ask` values;
- browser-mode commit-on-save (doc 06 §6.0) and the dedicated git Web Worker
  (§6.4): the sync half ships, the debounced commit still needs the companion;
- per-repository `git:` overrides in `workspaces[].repos[]`; the strategy is
  configurable per workspace (`git.pullStrategy`), per run (`--strategy`,
  `PATCH /api/v1/sync/settings`) and forced to `merge` in the browser.
