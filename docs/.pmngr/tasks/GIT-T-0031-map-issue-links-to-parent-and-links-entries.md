---
id: GIT-T-0031
type: task
title: Map issue links to parent and links entries
status: todo
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:16:03Z
updated: 2026-09-13T13:16:03Z
---

## Description

Add `internal/youtrack/mapping/links.go` with `MapLinks(issue) (parent string, children []string, links []core.Link, warnings []Warning)`. Filter out every entry whose `issues` array is empty first — YouTrack returns one entry per (linkType, direction) pair, most of them empty. `linkType.name == "Subtask"` with `direction: OUTWARD` yields children and `INWARD` yields the parent. Depend and Duplicate map onto `blocks`, `blocked_by`, `duplicates` and `duplicated_by`, Relates onto `relates_to`; any other link type becomes a warning. Never invent a kind `core.LinkKind.Valid()` (`internal/core/model.go:116-124`) rejects.

## Acceptance Criteria

- [ ] Entries with an empty `issues` array are filtered before anything else.
- [ ] Subtask OUTWARD yields children, INWARD yields the parent.
- [ ] Only the five valid `core.LinkKind` values are emitted; unknown types produce warnings.
- [ ] Table-driven tests use a fixture with the real multi-entry link shape; `go test -race ./internal/youtrack/...` passes.
