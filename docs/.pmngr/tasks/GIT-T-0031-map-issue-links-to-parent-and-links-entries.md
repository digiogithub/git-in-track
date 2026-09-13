---
id: GIT-T-0031
type: task
title: Map issue links to parent and links entries
status: done
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:16:03Z
updated: 2026-09-13T14:34:53Z
started: 2026-09-13T14:34:35Z
closed: 2026-09-13T14:34:53Z
---

## Description

Add `internal/youtrack/mapping/links.go` with `MapLinks(issue) (parent string, children []string, links []core.Link, warnings []Warning)`. Filter out every entry whose `issues` array is empty first — YouTrack returns one entry per (linkType, direction) pair, most of them empty. `linkType.name == "Subtask"` with `direction: OUTWARD` yields children and `INWARD` yields the parent. Depend and Duplicate map onto `blocks`, `blocked_by`, `duplicates` and `duplicated_by`, Relates onto `relates_to`; any other link type becomes a warning. Never invent a kind `core.LinkKind.Valid()` (`internal/core/model.go:116-124`) rejects.

## Acceptance Criteria

- [x] Entries with an empty `issues` array are filtered before anything else.
- [x] Subtask OUTWARD yields children, INWARD yields the parent.
- [x] Only the five valid `core.LinkKind` values are emitted; unknown types produce warnings.
- [x] Table-driven tests use a fixture with the real multi-entry link shape; `go test -race ./internal/youtrack/...` passes.

## Notes

Landed as `links.go` with the exact signature above, filtering through the client's `youtrack.NonEmptyLinks` first. Beyond the criteria: a link type whose `LinkType.Aggregation` flag is true also forms the hierarchy, so a renamed Subtask type still works; a second inward Subtask keeps the first parent and warns (an item has one parent); a hierarchy entry with direction `BOTH` names neither and is reported rather than guessed at; self-references and duplicate (kind, target) pairs are dropped; output is sorted so a golden file is stable. Targets are YouTrack `idReadable` values and come back in `Relations`, never on the draft.
