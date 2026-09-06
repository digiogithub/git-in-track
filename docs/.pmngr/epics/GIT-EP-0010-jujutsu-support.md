---
id: GIT-EP-0010
type: epic
title: Jujutsu (jj) repository support
status: in_progress
priority: high
milestone: GIT-M-0010
author: team
labels: [core, cli, web]
estimate: 34
created: 2026-09-06T00:00:00Z
updated: 2026-09-06T00:00:00Z
---

## Description

Post-1.0. Users manage repositories with Jujutsu, which uses git as a storage
backend but a different model on top of it. A colocated jj repository keeps
git's `HEAD` detached at the working-copy parent as normal operation, so every
git-shaped assumption in the product misreads it.

An audit against two real jj repositories established:

- Sync refuses every jj repository in preflight with "detached HEAD: check out
  the branch you want to sync first", and the UI paints a destructive
  "Detached HEAD" badge for what is jj's healthy steady state.
- jj keeps git's index synced to `@` while `HEAD` sits at `@-`, so the whole
  working copy reads as staged and the tree is considered permanently dirty.
- Commit on save is actively dangerous: `git commit` lands on the parent of the
  working-copy commit, moves no bookmark, and on the next jj command jj
  re-parents `@` onto it and abandons the previous working-copy commit as an
  orphan. The commits are unreachable from any bookmark, so `jj git push` would
  never publish them. Only the feature being off by default has prevented this.
- History and metrics already work: colocated jj exports every commit as a real
  git commit with author dates.
- Detection does not exist: `config.Detect` sets `Git: true` for a colocated
  repository, indistinguishable from plain git, and refuses a non-colocated jj
  repository outright.

The product owner asked for full compatibility, writes included. The
`Backend` interface must be generalized first: it currently exposes git's index
stages, `MERGE_HEAD`-style operation state and `--continue`, none of which exist
in jj, where the working copy is a commit, bookmarks are not branches,
conflicts are recorded inside commits, and `jj undo` replaces both the reflog
and `--abort`.

## Notes

Reference: jj 0.41.0. Machine-readable output comes from `jj log --no-graph -T`
with explicit separators.
