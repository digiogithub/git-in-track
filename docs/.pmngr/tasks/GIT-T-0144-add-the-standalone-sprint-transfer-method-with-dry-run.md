---
id: GIT-T-0144
type: task
title: Add the standalone sprint.transfer method with dry run
status: todo
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:18:47Z
updated: 2026-09-13T13:18:47Z
---

## Description

Add a `sprint.transfer` case to the workspace dispatch table (`internal/vault/dispatch.go:149-190`) that moves the incomplete references of one sprint into another without closing either, sharing the carry machinery with `CloseSprint` (`internal/vault/sprint.go:602` `carry`). Add `DryRun bool` to both `sprint.close` and `sprint.transfer`: with it set, the full report is computed and returned and no file is written, not even a `WriteSet`.

## Acceptance Criteria

- [ ] `sprint.transfer` moves only incomplete references and refuses a completed target.
- [ ] `dryRun: true` on both methods returns the report and leaves the repository byte-identical (asserted by hashing the tree before and after).
- [ ] A per-item refusal such as `repo_not_cloned` appears on its own report line and does not abort the rest (R-SPR-8).
- [ ] `go test -race ./internal/vault/...` covers transfer, dry run and the refusal path.
