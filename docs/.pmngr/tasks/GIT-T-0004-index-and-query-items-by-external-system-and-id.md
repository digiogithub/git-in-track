---
id: GIT-T-0004
type: task
title: Index and query items by external system and id
status: todo
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [core, performance, agent-ok]
estimate: 3
created: 2026-09-13T13:14:56Z
updated: 2026-09-13T13:14:56Z
---

## Description

Maintain a lookup from `(system, external id)` to `core.ItemID` in `internal/core/index.go`, populated by `Build` (`:445`) and kept correct by `ApplyFileEvents` (`:843`) when an item's external list changes or the item is deleted. Expose it as a direct lookup method and as a field on `core.Filter` (`internal/core/query.go:33`) so callers can both resolve one id and list everything from a system. The importer depends on this being O(1).

## Acceptance Criteria

- [ ] Lookup and filtering by external system and id both work and stay correct after add, update and delete file events.
- [ ] Building the index of 10 000 items stays inside the documented performance budget (`docs/02-architecture.md` §9).
- [ ] `go test -race ./internal/core/...` covers the incremental-update paths, not just a cold build.
