---
id: GIT-T-0023
type: task
title: Map type, status, priority, estimate, assignees and labels
status: todo
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:15:51Z
updated: 2026-09-13T13:15:51Z
---

## Description

In `internal/youtrack/mapping/item.go`, fill `IssueToDraft` and `IssueToPatch`: the `Type` field maps Epic, User Story, Task and Bug to `core.ItemType`, a Version-bundle value maps to `milestone`, `State` maps to a `project.yaml` status id through the supplied `field_map`, `Priority` maps to one of `critical|high|medium|low`, the `Estimation` period (`presentation` such as `3d 4h`, or `minutes`) converts to `estimate` points, `Assignee` (`login`) fills `assignees[]` and `tags[].name` fills `labels[]`. Unknown values fall back to the project defaults and are returned as warnings. Set `external: [{system: "youtrack", id: idReadable, url: base + "/issue/" + idReadable}]` on every draft.

## Acceptance Criteria

- [ ] All six field groups map, with the `field_map` overriding the built-in defaults.
- [ ] Unknown values produce a `Warning` and a documented fallback, never a silent drop.
- [ ] `external` is set on every draft and patch.
- [ ] Table-driven tests cover each group plus the unknown-value path; `go test -race ./internal/youtrack/...` passes.
