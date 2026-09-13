---
id: GIT-T-0140
type: task
title: Expand a bulk transfer option into per-item carry decisions on close
status: todo
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 4
created: 2026-09-13T13:18:42Z
updated: 2026-09-13T13:18:42Z
---

## Description

Add `Transfer *SprintTransfer` (`{Mode: next|backlog|none, Target string}`) to `SprintCloseParams` (`internal/vault/sprint.go:122-136`) and expand it, inside `CloseSprint`, into a `CarryNext`/`CarryBacklog`/`CarryLeave` decision for every reference `SummarizeClose` (`internal/core/sprintview.go:237`) grades as unfinished, leaving explicit per-item decisions to win over the bulk mode. Refuse a target whose derived status is `completed`. Finished references are never touched.

## Acceptance Criteria

- [ ] `transfer: {mode: next, target}` moves every unfinished reference into the target sprint and nothing else.
- [ ] `mode: backlog` returns them to each project's first `todo` status; `mode: none` matches today's behaviour.
- [ ] An explicit per-item decision overrides the bulk mode, and a completed target is refused.
- [ ] `go test -race ./internal/vault/...` covers all four combinations.
