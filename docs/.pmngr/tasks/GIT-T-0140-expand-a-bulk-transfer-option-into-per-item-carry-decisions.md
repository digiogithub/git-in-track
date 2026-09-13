---
id: GIT-T-0140
type: task
title: Expand a bulk transfer option into per-item carry decisions on close
status: done
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 4
created: 2026-09-13T13:18:42Z
updated: 2026-09-13T14:41:02Z
started: 2026-09-13T14:40:08Z
closed: 2026-09-13T14:41:02Z
---

## Description

Add `Transfer *SprintTransfer` (`{Mode: next|backlog|none, Target string}`) to `SprintCloseParams` (`internal/vault/sprint.go:122-136`) and expand it, inside `CloseSprint`, into a `CarryNext`/`CarryBacklog`/`CarryLeave` decision for every reference `SummarizeClose` (`internal/core/sprintview.go:237`) grades as unfinished, leaving explicit per-item decisions to win over the bulk mode. Refuse a target whose derived status is `completed`. Finished references are never touched.

## Acceptance Criteria

- [x] `transfer: {mode: next, target}` moves every unfinished reference into the target sprint and nothing else.
- [x] `mode: backlog` returns them to each project's first `todo` status; `mode: none` matches today's behaviour.
- [x] An explicit per-item decision overrides the bulk mode, and a completed target is refused.
- [x] `go test -race ./internal/vault/...` covers all four combinations.

## Notes

The expansion is `(sprintContext).plan` in `internal/vault/sprint.go`: it walks
`report.Incomplete` only, skips every reference an explicit `carry` entry names, and refuses
an unknown mode with `invalid_request`. `report.Unresolved` is deliberately not expanded —
"nothing could grade this" is not a reason to move someone's work.

A completed target is refused twice over: once for the whole operation when the bulk mode
resolves one (`sprint_target_completed`, before anything is written), and once per decision
so an explicit `next` into a completed sprint is reported on its own report line.
