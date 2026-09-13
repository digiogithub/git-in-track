---
id: GIT-T-0001
type: task
title: Add core.External to the model and to Item, Comment and KB pages
status: todo
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:14:40Z
updated: 2026-09-13T13:14:40Z
---

## Description

Add an `External{System, ID, URL, Key, SyncedAt}` struct to `internal/core/model.go` beside `Link` (`:145`), using `Timestamp` (`:153`) for `SyncedAt`. Add a `External []External` field to `Item` (`:324`), `Comment` (`:377`) and the KB page type in `internal/core/kb.go`. Add validation in `internal/core/validate.go` requiring a non-empty `system` and `id` and a well-formed `url` when present, without constraining `system` to an enum so unknown systems round-trip.

## Acceptance Criteria

- [ ] `core.External` exists with the five fields and compiles under `GOOS=js GOARCH=wasm`.
- [ ] Validation rejects an entry missing `system` or `id` and accepts an unknown `system`.
- [ ] `go test -race ./internal/core/...` passes with table-driven validation tests.
