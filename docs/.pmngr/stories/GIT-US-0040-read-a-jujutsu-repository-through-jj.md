---
id: GIT-US-0040
type: story
title: Read a Jujutsu repository through jj
status: in_review
priority: high
parent: GIT-EP-0010
milestone: GIT-M-0010
author: team
labels: [core, server]
estimate: 8
created: 2026-09-06T00:00:00Z
updated: 2026-09-06T00:00:00Z
---

## Description

As a user whose backlog lives in a Jujutsu repository, I want the product to
read that repository with jj itself, so that the status panel, the sync
indicator, the conflict resolver and the metrics describe what jj says instead
of what git happens to see behind it.

`GIT-US-0038` stopped the damage by refusing every git write behind a jj
working tree, and `GIT-US-0039` (ADR-022) generalized `gitops.Backend` so a jj
backend can satisfy it without pretending to have an index, a `MERGE_HEAD` or a
branch. What is left is the backend itself. This story is its **read half**;
the writes stay refused and are `GIT-US-0041`.

Reading through git is wrong in three ways that no amount of correction after
the fact can fix. Git's `HEAD` sits at `@-`, so the branch it reports is not the
line of work. jj keeps git's index synchronized with `@`, so every file of the
working-copy commit reads as staged. And a non-colocated repository has no git
working tree at all, so git can serve nothing there — no status, no conflicts,
and until now no metrics either.

Reading must also be free of side effects. Almost every jj command snapshots the
working copy before it runs, which creates a new operation in the operation log
and a new working-copy commit. An indexer or a status poll that did that would
rewrite the user's repository as a side effect of looking at it, so every
invocation this backend makes passes `--ignore-working-copy`.

## Acceptance Criteria

- [x] A `jj` backend is registered alongside `system` and `go-git` and is chosen
      for a jj working tree of either layout, colocated or not, whenever a
      usable jj binary is installed.
- [x] The backend refuses to open on a jj older than `core.MinJujutsuVersion`
      with a dedicated code, rather than running commands whose output it has
      not been verified against.
- [x] `Status` reports the real bookmark as the line of work — `Kind` is
      `bookmark`, `Anonymous` is false and `PushTarget` is the bookmark — and
      falls back to `@` as a `working-copy` line when no bookmark is reachable.
- [x] The dirty set comes from `jj diff --summary -r @`, never from git's index:
      `Staged` is always empty because jj has no staging area, added paths are
      reported as untracked and modified or deleted ones as modified.
- [x] `SyncStatus` reports the tracked remote, the remote-tracking bookmark and
      ahead/behind counters computed from a revset against it, and its `State`
      is the truthful one — `up_to_date`, `ahead`, `behind`, `diverged`,
      `dirty` or `conflicted` — instead of the `jujutsu` placeholder.
- [x] `SyncStatus.Integration` reports `Unfinished: false` and
      `Undo: operation_log`, because a jj operation is never half-finished and
      `jj undo` is what takes one back.
- [x] `Commits` lists commits over a jj revset, translating the git-shaped refs
      the callers pass (`HEAD`, `origin/main`) into `@` and `main@origin`.
- [x] `History` works for both layouts, so the burndown and the CFD of
      `GIT-US-0028` are correct for a non-colocated repository for the first
      time, and byte-identical to today's for a colocated one.
- [x] `ConflictFile` reads the three sides out of the conflict jj records inside
      the commit and reports `Markers: jj`, which is the only way to read them
      in a repository with no git index.
- [x] No read creates a jj operation: every invocation passes
      `--ignore-working-copy`, and a test compares `jj op log` before and after
      a full pass of the read surface.
- [x] Every write — `Commit`, `Fetch`, `Integrate`, `Push`, `Undo`, `Resume`,
      `ResolvePath` — is still refused with `vcs_jujutsu_write_refused` and the
      jj command that does it safely.
- [x] `internal/core` still compiles to WASM, and `make lint`, `make wasm`,
      `make wasm-smoke`, `make build` and `make test` pass.
- [x] `docs/02-architecture.md`, `docs/06-git-sync.md` section 14 and
      `docs/07-cli-and-api.md` describe the backend, and an ADR records where
      the file history is read from.

## Notes

Reference: jj 0.41.0. Machine-readable output comes from `jj log --no-graph -T`
with `\x1f` separators, the same unit separator the git backends already use.

`History` is deliberately not read with `jj file show`: that would cost one
process per blob, and the metrics read thousands. jj's git backend stores every
commit as a real git commit, so the history is walked in process with go-git
over the object store `.jj/repo/store/git_target` points at, starting from the
commit id jj reports for `@`. ADR-023 records it.
