---
id: GIT-T-0107
type: task
title: Filter and order sprint listings by derived status
status: todo
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:55Z
updated: 2026-09-13T13:17:55Z
---

## Description

Add `Status []core.SprintStatus` to `SprintListParams` (`internal/vault/sprint.go`) and make `Workspace.Sprints` (`:314`) filter on the derived value and default to the order current → upcoming → draft → completed, ties broken by start date then id. Thread the parameter through `GET /api/v1/sprints` in `internal/server/sprints.go` and include the derived status in every sprint payload.

## Acceptance Criteria

- [ ] `GET /api/v1/sprints?status=current,upcoming` filters correctly and the default ordering matches the rule above.
- [ ] Every sprint payload carries the derived status.
- [ ] `go test -race ./internal/vault/... ./internal/server/...` covers filtering and ordering.
