---
id: GIT-T-0103
type: task
title: Exempt drafts from the overlap rule and improve the refusal message
status: todo
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [core]
estimate: 2
created: 2026-09-13T13:17:52Z
updated: 2026-09-13T13:17:52Z
---

## Description

Update `checkSprintDates` (`internal/vault/sprint.go:748`) so a create or update that leaves a sprint dateless is never refused, and so a dated collision fails with `sprint_overlap` carrying a message that names the other sprint, its range, and the escape hatch: remove the dates to keep it a draft. Keep the on-disk condition as the `W-SPRINT-OVERLAP` warning (docs/04 §8.4) — validation describes, a write decides (R-SPR-6).

## Acceptance Criteria

- [ ] A dateless create or a date removal is accepted even when another sprint covers the same period.
- [ ] A dated overlap is refused with `sprint_overlap` and a message naming the other sprint and its range.
- [ ] The on-disk warning is unchanged.
- [ ] `go test -race ./internal/vault/...` covers both directions.
