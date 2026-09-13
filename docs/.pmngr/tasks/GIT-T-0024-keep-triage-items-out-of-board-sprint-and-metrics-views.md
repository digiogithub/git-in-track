---
id: GIT-T-0024
type: task
title: Keep triage items out of board, sprint and metrics views
status: done
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:15:52Z
updated: 2026-09-13T14:08:35Z
started: 2026-09-13T14:08:28Z
closed: 2026-09-13T14:08:35Z
---

## Description

Make every planning surface skip items in the `triage` category. Touch `internal/core/boardview.go` (card candidates and column mapping), `internal/core/sprintview.go` — `SummarizeSprint` at `:8`, `BuildSprintView` at `:81` and `isSprintCandidate` at `:155` — and `internal/core/metrics.go` (`BuildSprintMetrics` and the observation walk), filtering on the resolved status category rather than re-reading files. A sprint whose `items` list names a triage reference must report it as unresolved rather than as work, so the exclusion cannot be smuggled in through a hand-edited sprint file.

## Acceptance Criteria

- [x] Board views, sprint views, sprint candidates and sprint metrics return identical results whether or not triage items exist in the index.
- [x] A triage reference listed by a sprint file is reported as unresolved and never counted as done or as points.
- [x] The exclusion is a filter over the built index, adding no extra pass over the corpus (the `docs/02 §9` index budget is unchanged).
- [x] `go test -race ./internal/core/...` passes with new cases for each surface.

## Notes

**`sprintview.go` needed no change at all**, which is the point of where the filter went.
`SummarizeSprint`, `BuildSprintView` and `isSprintCandidate` all consume the cards produced by
`walkCards` (`boardview.go`), so dropping triage candidates there — on both the live-source and the
committed-snapshot path — removes them from boards, sprint scopes, sprint candidates and sprint
metrics in one place. A sprint that names a triage reference then finds no card for it, so
`SummarizeSprint` counts it through the existing "no card, no status" branch: unresolved, never
work, never points, never done. The exclusion is a `continue` inside a walk that already existed, so
there is no extra pass.

`metrics.go` got one extra guard: an observation reconstructed from git whose category was `triage`
that day is neither scope nor work. `bandOf` deliberately has no triage band.
