---
id: GIT-T-0020
type: task
title: Scaffold the mapping package and the custom-field value reducer
status: todo
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:15:44Z
updated: 2026-09-13T13:15:44Z
---

## Description

Create `internal/youtrack/mapping/mapping.go` with the package doc, the `Warning` type and the exported entry points `IssueToDraft`, `IssueToPatch` and `CommentsToDrafts` as stubs, plus `internal/youtrack/mapping/fields.go` holding `reduceFieldValue(v Value) (string, bool)` and `fieldByName(issue, name)`. The reducer follows the documented order `name`, then `login` or `fullName`, then `localizedName`, then `presentation`, then `idReadable`, then `id`, maps arrays element-wise and passes scalars through, and keeps the raw `$type` so a caller can tell a period from an enum. No HTTP, no vault, no `internal/server` imports.

## Acceptance Criteria

- [ ] `internal/youtrack/mapping` compiles with the three entry points and imports nothing from `net/http`, `internal/vault` or `internal/server`.
- [ ] `reduceFieldValue` implements the documented reduction order and handles arrays and scalars.
- [ ] `go test -race ./internal/youtrack/...` passes with a unit test per field kind in the `$type` table.
