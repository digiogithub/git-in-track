---
id: GIT-US-0038
type: story
title: Detect a Jujutsu repository and stop writing behind it
status: done
priority: critical
parent: GIT-EP-0010
milestone: GIT-M-0010
author: team
labels: [core, cli, web]
estimate: 5
created: 2026-09-06T00:00:00Z
updated: 2026-09-06T00:00:00Z
closed: 2026-09-06T00:00:00Z
---

## Description

As someone whose repositories are managed with Jujutsu, we want the product to
recognize that fact and refuse every git write it would otherwise make behind
jj's back, so that using git-in-track on a jj repository cannot lose work and
the UI stops describing jj's healthy steady state as a failure.

This is the safety and truth-telling layer of `GIT-EP-0010`. It builds no jj
backend (that is `GIT-US-0040`/`GIT-US-0041`) and does not touch the `Backend`
interface (`GIT-US-0039`). It closes the one hole the epic's audit found to be
actively dangerous, and it stops three lies.

**The dangerous path.** Commit on save runs `git add -- <paths>` followed by
`git commit --only -- <paths>`. In a colocated jj repository git's `HEAD` sits
at `@-`, the *parent* of the working-copy commit, so the commit lands on `@-`,
moves no bookmark, and the next `jj` command re-parents `@` onto it and abandons
the previous working-copy commit as an orphan. The commit is unreachable from
any bookmark, so `jj git push` would never publish it. Only commit-on-save being
off by default has kept this from biting anyone. It must become impossible, not
discouraged.

**The three lies.** `git rev-parse --abbrev-ref HEAD` answers the literal
`HEAD`, so the status reports `Detached: true` and the sync panel paints a
destructive "Detached HEAD" badge over what is jj's normal state. jj keeps
git's index synchronized with `@` while `HEAD` is at `@-`, so the working copy
can read as permanently staged and therefore permanently dirty. And sync's
preflight refuses every jj repository with "check out the branch you want to
sync first", advice that is wrong in a repository that has no checked-out
branch by design.

Detection has to come first because none of the above can be conditional on
something the product cannot see: `config.Detect` reported `Git: true` for a
colocated jj repository, indistinguishable from plain git.

## Acceptance Criteria

- [x] `internal/core` carries the VCS vocabulary — kind, layout, labels and the
      refusal wording — as WASM-safe pure logic; `internal/gitops` carries the
      filesystem and binary plumbing that decides which one a folder is.
- [x] Both jj layouts are detected: colocated (the git repository is the
      workspace's own `.git`) and non-colocated (the git store lives inside
      `.jj/`), including the legacy `.jj/repo/store/git` layout.
- [x] Repository detection reports the kind of its own (`git`, `jj`, `none`)
      instead of reporting a colocated jj repository as plain git, and
      registration no longer refuses a jj repository that has no git working
      tree.
- [x] The `jj` binary and its version are resolved the way `git`'s are, with a
      documented minimum of 0.41.
- [x] Every git write path — commit on save, sync (fetch, integrate, push),
      abort, continue and conflict resolution — is refused in a jj repository
      with `vcs_jujutsu_write_refused` and a message naming the `jj` command to
      run instead. No `git commit`, `git rebase` or `git push` can reach a jj
      repository.
- [x] A test drives a real temporary jj repository, asks for a commit on save
      and a sync, and proves the refusal happens before any git process runs.
- [x] A jj repository is not shown as detached and not shown as permanently
      dirty: its state is `jujutsu`, rendered as "Managed by Jujutsu — reads
      work, writes go through jj" and never as a destructive badge.
- [x] The kind is surfaced by `gintrack ls`, `gintrack doctor`, `gintrack add`,
      the repository payloads of the API and the sync panel.
- [x] `docs/02-architecture.md`, `docs/06-git-sync.md` and
      `docs/07-cli-and-api.md` describe the behavior, and an ADR records
      treating jj as a first-class VCS kind.

## Notes

Reference: jj 0.41.0. Read-only verification was done against the product
owner's real jj repositories; every write experiment ran in throwaway
repositories under `/tmp`.
