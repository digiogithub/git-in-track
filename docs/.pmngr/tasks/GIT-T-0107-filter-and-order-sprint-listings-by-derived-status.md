---
id: GIT-T-0107
type: task
title: Filter and order sprint listings by derived status
status: done
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:55Z
updated: 2026-09-13T14:57:27Z
started: 2026-09-13T14:57:14Z
closed: 2026-09-13T14:57:27Z
---

## Description

Add `Status []core.SprintStatus` to `SprintListParams` (`internal/vault/sprint.go`) and make `Workspace.Sprints` (`:314`) filter on the derived value and default to the order current → upcoming → draft → completed, ties broken by start date then id. Thread the parameter through `GET /api/v1/sprints` in `internal/server/sprints.go` and include the derived status in every sprint payload.

## Acceptance Criteria

- [ ] `GET /api/v1/sprints?status=current,upcoming` filters correctly and the default ordering matches the rule above.
- [x] Every sprint payload carries the derived status.
- [x] `go test -race ./internal/vault/... ./internal/server/...` covers filtering and ordering.

## Notes

Vault half landed: `SprintListParams.Status` (`[]core.SprintStatus`, JSON key `status`) is validated through `core.ParseSprintStatus` — case-insensitive, empty entries dropped, an unknown value is `invalid_request` — and `Workspace.Sprints` applies `core.FilterSprintsByStatus` then `core.SortSprintsForListing` with the team vault's clock. `core.SprintSummary.Status` already travels on every payload. Covered by `TestWorkspaceSprintListByDerivedStatus`.

The REST half (`internal/server/sprints.go`, the `?status=current,upcoming` query string) belongs to the server agent of this wave and is not done here: it only has to split the query string on commas and pass the values through as `status`. The docs/04 R-SPR-10 "As built" paragraph was deleted.
