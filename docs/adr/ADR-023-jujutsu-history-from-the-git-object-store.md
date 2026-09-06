# ADR-023 — A Jujutsu repository's file history is read from the git object store, not with `jj file show`

- **Status:** Accepted
- **Date:** 2026-09-06
- **Phase:** Post-1.0 (Jujutsu support, `GIT-EP-0010`)
- **Related:** [ADR-017](ADR-017-metrics-history-from-git-not-a-stored-time-series.md), [ADR-021](ADR-021-jujutsu-is-a-first-class-vcs-kind.md), [ADR-022](ADR-022-a-vcs-neutral-backend-interface.md)
- **Implements:** `GIT-US-0040` — Read a Jujutsu repository through jj

## Context

`GIT-US-0040` reads a Jujutsu repository with jj instead of with git, because
git reads three things wrongly behind jj: `HEAD` sits at `@-`, the index is kept
synchronized with `@`, and a non-colocated repository has no git working tree at
all. Status, sync status, conflicts and the commit log all follow from that and
are answered by `jj` invocations.

`Backend.History` does not. It reads **every revision of every backlog file**,
because the burndown and the CFD of `GIT-US-0028` are reconstructed from the
history rather than stored as a time series
([ADR-017](ADR-017-metrics-history-from-git-not-a-stored-time-series.md)). A
real backlog is hundreds of item files with several revisions each, and the
metrics rebuild that on every cache miss.

The jj-native route is `jj log --no-graph -T … -r 'files(<path>)'` to find the
revisions and `jj file show -r <rev> <path>` to read each one. `jj file show`
prints one file per invocation with no separator between several, so there is no
batching: the walk costs **one operating-system process per revision per file**.
The system-git backend solves the same problem with one `git log` per path plus
a single `git cat-file --batch` for every blob, and the go-git backend does it
entirely in process. Matching jj command-for-command here would make the metrics
of a mid-sized backlog take minutes.

The relevant fact is that jj's default backend *is* git: every jj commit is a
real git commit in the object store that `.jj/repo/store/git_target` points at —
the workspace's own `.git` in a colocated repository, a bare repository inside
`.jj` otherwise. Reading that store is not reading "the git side of a jj
repository" the way `GIT-US-0038` warned against; it is reading jj's own
storage.

## Decision

**`History` walks the git object store jj commits into, in process with go-git,
starting from the commit id jj reports for `@`.**

Three parts of that sentence carry weight.

- **The store, not the working tree.** The path comes from `jujutsuStore`, which
  already resolves `git_target` for detection, so both layouts are covered by
  one code path — including the non-colocated one, which has no git working tree
  and therefore had no history at all before this story.
- **The commit jj reports, not git's `HEAD`.** `jj log -r @` with
  `--ignore-working-copy` gives the working-copy commit. Git's `HEAD` points at
  `@-` in a colocated repository and at nothing useful in a non-colocated one,
  so starting there would silently drop the newest revision of every file.
- **go-git, not the git binary.** The walk needs no working tree, no
  configuration and no credentials, and go-git filters a log by path and reads
  blobs in process. The same walker serves the go-git backend, so there is one
  implementation of the traversal, the deduplication and the leading-deletion
  rule.

## Consequences

- The metrics are correct and fast in both layouts. A non-colocated jj
  repository gets a burndown and a CFD for the first time; a colocated one's are
  byte-identical to what the git backends produced, which is what keeps the
  charts stable across the change.
- The history is the only read of a jj repository that does not go through the
  `jj` binary. It is called out in the code and in doc 06 §14.5 so that nobody
  mistakes it for the git-shaped reading the epic exists to end.
- It is still side-effect free: reading an object store creates no jj operation,
  and the one jj command involved (`jj log -r @`) carries
  `--ignore-working-copy` like every other read.
- The revisions are the ones jj recorded. A commit jj has not yet written to the
  store cannot exist — jj writes commits as it creates them — so there is no
  window in which the history is behind what `jj log` shows.
- If jj ever gains a non-git backend, this walk stops applying. `jujutsuStore`
  already answers the empty string when it cannot resolve a git store, and
  `History` then reports an empty history rather than failing, which the metrics
  render as unknown days instead of as an empty backlog. Serving such a
  repository would need the `jj file show` route, with its cost accepted or
  batched by a future jj release.

## Alternatives considered

- **`jj log -r 'files(<path>)'` plus `jj file show` per revision.** Uniform with
  every other read and the obvious first choice, but one process per blob. On a
  backlog of 400 items with an average of six revisions that is 2 400 processes
  per metrics rebuild. Rejected on cost, not on correctness.
- **Delegating `History` to the git backend for colocated repositories only.**
  Fast where it applies, but it leaves the non-colocated layout with no metrics
  — the exact gap this story set out to close — and it means two histories with
  different starting points and different bugs.
- **Storing a time series of our own.** Already rejected by
  [ADR-017](ADR-017-metrics-history-from-git-not-a-stored-time-series.md): it is
  derived data, and derived data that is stored drifts.
