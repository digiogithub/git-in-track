---
id: GIT-T-0003
type: task
title: Support external in ItemDraft and ItemPatch with add/remove semantics
status: todo
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:14:50Z
updated: 2026-09-13T13:14:50Z
---

## Description

Add `External` to `ItemDraft` (`internal/core/store.go:118`) and `AddExternal`/`RemoveExternal` to `ItemPatch` (`:154`), applied in `applyPatch` (`:754`) with exactly the set semantics used for `assignees`, `labels` and `links`, so two writers touching different systems do not clobber each other. Identity for the set is `(system, id)`; re-adding an existing pair updates `url` and `synced_at` in place rather than duplicating.

## Acceptance Criteria

- [ ] Creating with `ItemDraft.External` and patching with add/remove both work and keep the list stable and deduplicated by `(system, id)`.
- [ ] A patch that only adds an external reference does not touch any other field, and the write stays a single rev-checked write.
- [ ] `go test -race ./internal/core/...` covers add, remove, re-add and concurrent-patch cases.
