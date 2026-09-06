---
id: GIT-US-0039
type: story
title: Generalize the backend interface beyond git
status: in_review
priority: critical
parent: GIT-EP-0010
milestone: GIT-M-0010
author: team
labels: [core, server]
estimate: 8
created: 2026-09-06T00:00:00Z
updated: 2026-09-06T00:00:00Z
---

## Description

As the product, we want `gitops.Backend` to be expressed in concepts that exist
in every version-control system we drive, so that a Jujutsu backend can satisfy
it honestly instead of faking git machinery jj does not have.

`GIT-US-0038` made a jj repository safe by refusing every git write behind it.
That is a floor, not a feature: the epic asked for full compatibility, writes
included, and the backend that will deliver it (`GIT-US-0040`/`GIT-US-0041`)
cannot be written against today's interface without lying. Four concepts in it
are git's, not version control's.

**A staging area.** `Commit` is documented as "stage exactly these paths and
commit them", and `Capabilities.PathspecCommit` names git's pathspec. jj has no
index at all: `jj commit -m … -- <paths>` and `jj squash -- <paths>` act on the
working-copy commit directly. The contract the product actually depends on is
"commit exactly these paths and nothing else"; staging is one implementation of
it.

**Operation state.** `SyncStatus.Operation` is a `MERGE_HEAD`/`rebase-merge`
marker, `ResolvePath` refuses while it is empty, and `Abort`/`Continue` are
named after `--abort`/`--continue`. jj has no half-finished operation: `jj
rebase` always completes, conflicts are recorded *inside* the resulting commits,
and `jj undo`/`jj op restore` takes back a whole operation from the operation
log. What the product needs to know is whether an integration has settled, how
it can be taken back, and whether there is anything left to carry forward.

**Index conflict stages.** `ConflictFile` is specified as "read index stages
1/2/3". Colocated jj exports those stages, non-colocated jj has no git index to
read, and jj's own conflict markers (`%%%%%%%`, `+++++++`) are not git's. The
payload the resolver consumes — base, ours, theirs, the working file — is
already neutral; only its stated source was not.

**Branches.** `Status.Branch`/`Detached` and a push of `HEAD:refs/heads/<branch>`
assume a checked-out branch that moves on commit. jj has bookmarks, which do not
move on commit, and its `HEAD` equivalent is `@-`.

This story is a refactor: no user-visible behavior changes, the HTTP surface
keeps every field it had, and both git backends stay exactly as capable as they
are today. It implements no jj backend.

## Acceptance Criteria

- [x] `Backend.Commit` is specified as "commit exactly these paths", with
      staging named as one backend's way of doing it; the capability is
      `ScopedCommit` and no longer names git's pathspec.
- [x] `SyncStatus.Operation` is replaced by an `Integration` value that reports
      whether an integration is unfinished, how it is undone (`abort` or
      `operation_log`) and how it is carried forward (`continue` or nothing),
      so that `jj undo` is a legitimate implementation of undo.
- [x] `Backend.Abort`/`Backend.Continue` become `Undo`/`Resume`, specified
      against that value rather than against git's flags, and every caller —
      the sync pipeline, `internal/server`, `gintrack sync` — uses them.
- [x] `ConflictFile` is specified as "the three sides, however the backend can
      produce them", and `ConflictVersions` carries the marker dialect of the
      working file so a reader never assumes git's.
- [x] `Status`/`SyncStatus` carry a `Line` — the current line of work, its kind
      (`branch`, `bookmark`, `working-copy`, none) and where a push publishes
      it — instead of a branch name plus a detached flag.
- [x] The `system` and `go-git` backends keep every capability they have today
      and pass the existing test suite unchanged in meaning.
- [x] The JSON of `/api/v1/git/status`, `/api/v1/sync/status`, the conflict
      routes and every `gitops.CodeOf` code is unchanged; the new information is
      additive.
- [x] `internal/core` still compiles to WASM, and `make lint`, `make wasm`,
      `make wasm-smoke`, `make build` and `make test` pass.
- [x] `docs/02-architecture.md`, `docs/06-git-sync.md` and
      `docs/07-cli-and-api.md` describe the generalized interface, and an ADR
      records it.

## Notes

Reference: jj 0.41.0. No jj backend is written here; the deliverable is an
interface one can satisfy without pretending to have an index, a `MERGE_HEAD`
or a branch.
