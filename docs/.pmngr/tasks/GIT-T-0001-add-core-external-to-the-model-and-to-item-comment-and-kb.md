---
id: GIT-T-0001
type: task
title: Add core.External to the model and to Item, Comment and KB pages
status: done
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:14:40Z
updated: 2026-09-13T14:00:16Z
started: 2026-09-13T14:00:10Z
closed: 2026-09-13T14:00:16Z
---

## Description

Add an `External{System, ID, URL, Key, SyncedAt}` struct to `internal/core/model.go` beside `Link` (`:145`), using `Timestamp` (`:153`) for `SyncedAt`. Add a `External []External` field to `Item` (`:324`), `Comment` (`:377`) and the KB page type in `internal/core/kb.go`. Add validation in `internal/core/validate.go` requiring a non-empty `system` and `id` and a well-formed `url` when present, without constraining `system` to an enum so unknown systems round-trip.

## Acceptance Criteria

- [x] `core.External` exists with the five fields and compiles under `GOOS=js GOARCH=wasm`.
- [x] Validation rejects an entry missing `system` or `id` and accepts an unknown `system`.
- [x] `go test -race ./internal/core/...` passes with table-driven validation tests.

## Notes

Implemented in `internal/core/model.go` (`External`, `ExternalRef`, `NewExternalRef`) and in the
new `internal/core/external.go` (set algebra, normalisation, `validateExternal`), called from
`ValidateItem`. On `KBPage` the field is named `ExternalRefs` because `KBPage.External` was
already taken by the list of outbound URLs found in the body; the front-matter key is still
`external`. Diagnostic codes: `E-EXT-FIELDS`, `W-EXT-URL`, `W-EXT-DUP`.
