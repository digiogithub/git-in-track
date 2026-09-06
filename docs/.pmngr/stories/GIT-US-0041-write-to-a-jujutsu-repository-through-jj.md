---
id: GIT-US-0041
type: story
title: Write to a Jujutsu repository through jj
status: in_review
priority: high
parent: GIT-EP-0010
milestone: GIT-M-0010
author: team
labels: [core, cli, web]
estimate: 13
created: 2026-09-06T00:00:00Z
updated: 2026-09-06T00:00:00Z
---

## Description

As a user whose backlog lives in a Jujutsu repository, I want commit on save,
sync and conflict resolution to work through jj, so that a jj repository is a
first-class repository rather than a read-only special case.

`GIT-US-0038` stopped the damage, `GIT-US-0039` (ADR-022) made `gitops.Backend`
VCS-neutral and `GIT-US-0040` (ADR-023) delivered the read half. This story is
the **write half**, and it is the last one of `GIT-EP-0010`.

The failure this epic exists to prevent frames every decision here: a write that
orphans the working-copy commit, or records commits no bookmark can reach, is
worse than no write at all. Doing it "the jj way" therefore means two things
that git has no equivalent of.

First, **the working copy is a commit**. There is no index, so "commit exactly
these paths" is `jj commit -m … -- <paths>`, which records those paths of `@` as
a new commit and leaves everything else in a fresh working-copy commit on top.
Nothing is orphaned, and no edit outside the requested paths is captured.

Second, **a bookmark does not move when a commit is made**. `jj commit` alone
would therefore leave every commit on save unreachable from any bookmark, and
`jj git push` would never publish them — the exact outcome the epic's audit
called out. The commit is followed by a fast-forward `jj bookmark move`, which
is what a jj user would do by hand and which jj refuses when it would not be a
fast-forward.

Everything else follows from jj's own model: `jj git fetch`, `jj rebase -d …`,
`jj git push -b <bookmark>` with its dry run, `jj undo` over the operation log,
and no `Resume` at all, because a jj operation is never half-finished.

## Acceptance Criteria

- [x] `Capabilities.writes` is true for a jj repository driven by the jj
      backend, and stays false for the read-only guard of `GIT-US-0038` (no jj
      binary installed).
- [x] `Commit` records exactly the requested paths with
      `jj commit -m <message> -- <paths>`: every other uncommitted change stays
      in the new working-copy commit, and an empty commit is reported as
      `Empty` without writing anything.
- [x] Commit on save fast-forwards the bookmark of the line of work onto the new
      commit, so the work is reachable from a bookmark and `jj git push` would
      publish it. A non-fast-forward move is refused, never forced.
- [x] A graph-integrity test proves that after a commit on save the working-copy
      commit is intact and unabandoned, the change graph has no orphan, the
      bookmark advanced exactly one commit, and a dry-run `jj git push` reports
      the new commit as what it would publish.
- [x] `Fetch` runs `jj git fetch --remote <remote>` and reports the
      remote-tracking bookmark before and after.
- [x] `Integrate` runs `jj rebase -b @ -d <upstream>` for the rebase strategy
      and, for the merge strategy, creates the merge commit and moves the
      bookmark onto it. Conflicts are reported with `CodeConflict`,
      `Unfinished: false`, `Undo: operation_log` and `Resume: ""`.
- [x] `Push` runs `jj git push --remote <remote> -b <bookmark>`, honors
      `DryRun`, and classifies a rejection as `git_push_rejected` so the sync
      retry ladder answers it with fetch + integrate + push as it does for git.
- [x] `Undo` runs `jj undo`; `Resume` fails with `git_unsupported` and says that
      jj leaves no unfinished operation to continue, which is what
      `Integration.Resume: ResumeNone` already announces.
- [x] `ResolvePath` writes the merged file the resolver of `GIT-US-0022`
      produces, snapshots it and squashes it into the conflicted commit, so the
      conflict is resolved where jj records it and the bookmark commit becomes
      pushable again.
- [x] Ahead/behind survive a bookmark that jj marks conflicted after a fetch,
      which is the state the sync preflight meets when both sides moved.
- [x] The sync preflight treats a jj repository as syncable: it no longer
      refuses it, its messages name jj commands, and a dirty working copy does
      not block the run, because in jj the working copy is a commit.
- [x] Nothing prompts and nothing leaks: every jj invocation inherits the
      non-interactive environment of `GIT-US-0023` and every line jj prints goes
      through the secret redaction before it reaches an error, an event or the
      UI.
- [x] The server, `gintrack sync` and the web sync and conflict surfaces treat a
      jj repository as writable, and report a read-only one from
      `capabilities.writes` rather than from "is it jj".
- [x] `internal/core` still compiles to WASM, and `make lint`, `make wasm`,
      `make wasm-smoke`, `make build` and `make test` pass.
- [x] `docs/02-architecture.md`, `docs/06-git-sync.md` section 14,
      `docs/07-cli-and-api.md` and the CHANGELOG describe the write half, and an
      ADR records the commit-and-move-the-bookmark decision.
