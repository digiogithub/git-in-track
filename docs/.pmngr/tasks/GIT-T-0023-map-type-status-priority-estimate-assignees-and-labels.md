---
id: GIT-T-0023
type: task
title: Map type, status, priority, estimate, assignees and labels
status: done
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:15:51Z
updated: 2026-09-13T14:34:21Z
started: 2026-09-13T14:34:03Z
closed: 2026-09-13T14:34:21Z
---

## Description

In `internal/youtrack/mapping/item.go`, fill `IssueToDraft` and `IssueToPatch`: the `Type` field maps Epic, User Story, Task and Bug to `core.ItemType`, a Version-bundle value maps to `milestone`, `State` maps to a `project.yaml` status id through the supplied `field_map`, `Priority` maps to one of `critical|high|medium|low`, the `Estimation` period (`presentation` such as `3d 4h`, or `minutes`) converts to `estimate` points, `Assignee` (`login`) fills `assignees[]` and `tags[].name` fills `labels[]`. Unknown values fall back to the project defaults and are returned as warnings. Set `external: [{system: "youtrack", id: idReadable, url: base + "/issue/" + idReadable}]` on every draft.

## Acceptance Criteria

- [x] All six field groups map, with the `field_map` overriding the built-in defaults.
- [x] Unknown values produce a `Warning` and a documented fallback, never a silent drop.
- [x] `external` is set on every draft and patch.
- [x] Table-driven tests cover each group plus the unknown-value path; `go test -race ./internal/youtrack/...` passes.

## Notes

Landed as `item.go` and `fieldmap.go`. Two decisions a reviewer should know:

- The field map is a `mapping.FieldMap` struct the caller supplies, not a `project.yaml` reader: `internal/config` is owned by another agent this wave, so the mapper takes the data and the config plumbing stays for the import story. `DefaultFieldMap()` works against a stock YouTrack (default bundles for Type, State and Priority).
- `Estimation` becomes points by dividing the period by `MinutesPerPoint` (one eight-hour working day by default, configurable, as are the day and week lengths). `minutes` wins over `presentation`; an unparseable presentation sets **no** estimate and warns, because an estimate of 0 is a statement and guessing one would be worse than leaving it unset.
- The milestone comes back in `Relations.Milestone` as the version **name**, not on the draft: this package cannot resolve a version name into a `core.ItemID`.
