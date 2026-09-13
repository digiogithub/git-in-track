---
id: GIT-T-0101
type: task
title: Make sprint dates optional with both-or-neither validation
status: done
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 3
created: 2026-09-13T13:17:47Z
updated: 2026-09-13T14:00:50Z
started: 2026-09-13T14:00:18Z
closed: 2026-09-13T14:00:50Z
---

## Description

Relax `Sprint.Validate` (`internal/core/sprint.go`, the `E-SPRINT-DATES` switch): a sprint with neither `start` nor `end` is a valid draft; exactly one of the two, or `end < start`, stays an error. Make `TotalDays` and `RemainingDays` (`:165-198`) return 0 for a draft rather than computing from a zero date, and make `Overlaps` (`:189`) already-correctly return false when either sprint is dateless — add a test pinning that so it cannot regress.

## Acceptance Criteria

- [x] A dateless sprint validates clean; one date alone and an inverted range both produce `E-SPRINT-DATES`.
- [x] `TotalDays`, `RemainingDays` and `Overlaps` behave sanely for drafts, with tests.
- [x] Existing sprint fixtures with both dates are unaffected.
- [x] `go test -race ./internal/core/...` passes.

## Notes

`(*Sprint).IsDraft()` was added as the single predicate the validation, the day arithmetic and the overlap rule all share.
