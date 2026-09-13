---
id: GIT-T-0103
type: task
title: Exempt drafts from the overlap rule and improve the refusal message
status: done
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 2
created: 2026-09-13T13:17:52Z
updated: 2026-09-13T14:57:11Z
started: 2026-09-13T14:56:59Z
closed: 2026-09-13T14:57:11Z
---

## Description

Update `checkSprintDates` (`internal/vault/sprint.go:748`) so a create or update that leaves a sprint dateless is never refused, and so a dated collision fails with `sprint_overlap` carrying a message that names the other sprint, its range, and the escape hatch: remove the dates to keep it a draft. Keep the on-disk condition as the `W-SPRINT-OVERLAP` warning (docs/04 §8.4) — validation describes, a write decides (R-SPR-6).

## Acceptance Criteria

- [x] A dateless create or a date removal is accepted even when another sprint covers the same period.
- [x] A dated overlap is refused with `sprint_overlap` and a message naming the other sprint and its range.
- [x] The on-disk warning is unchanged.
- [x] `go test -race ./internal/vault/...` covers both directions.

## Notes

`checkSprintDates` now enforces both-or-neither (exactly one date is `invalid_request` naming the draft escape hatch), returns early for `(*core.Sprint).IsDraft()`, and formats the refusal with `core.SprintOverlapMessage`. `parseSprintDate` accepts an empty value as the zero date, so `sprint.create` with no dates and `sprint.update` with `start: ""`/`end: ""` both produce a draft. Covered by `TestWorkspaceSprintDrafts` in `internal/vault/sprint_test.go`; the docs/04 R-SPR-9 "As built" paragraph was deleted.
