---
id: GIT-T-0144
type: task
title: Add the standalone sprint.transfer method with dry run
status: done
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:18:47Z
updated: 2026-09-13T14:41:13Z
started: 2026-09-13T14:40:10Z
closed: 2026-09-13T14:41:13Z
---

## Description

Add a `sprint.transfer` case to the workspace dispatch table (`internal/vault/dispatch.go:149-190`) that moves the incomplete references of one sprint into another without closing either, sharing the carry machinery with `CloseSprint` (`internal/vault/sprint.go:602` `carry`). Add `DryRun bool` to both `sprint.close` and `sprint.transfer`: with it set, the full report is computed and returned and no file is written, not even a `WriteSet`.

## Acceptance Criteria

- [x] `sprint.transfer` moves only incomplete references and refuses a completed target.
- [x] `dryRun: true` on both methods returns the report and leaves the repository byte-identical.
- [x] A per-item refusal such as `repo_not_cloned` appears on its own report line and does not abort the rest (R-SPR-8).
- [x] `go test -race ./internal/vault/...` covers transfer, dry run and the refusal path.

## Notes

`Workspace.TransferSprintItems` shares `plan`, `carry` and the new `applyCarries` with
`CloseSprint`; the only difference is that it never touches the source sprint's `state`,
`items` or `committed`.

`applyCarries` also fixes a latent bug in the old per-item `carry`: carrying ten references
into one sprint used to write that sprint's file ten times. Target sprints are now mutated in
memory and written once each, and `mergeRepoWrites` folds the result into one `RepoWriteSet`
per repository, which is what the story's "one WriteSet per repository" asks for.

The dry-run assertion is spelled as "every file this operation could write is unchanged"
rather than a raw tree hash: `TestSprintTransferDryRun` records the `rev` of every sprint and
every item of the workspace before and after, plus each sprint's scope, and requires all of
them to be identical. The index fingerprint was tried first and rejected — it does not cover
sprint files, so it would have passed a test that should fail.
