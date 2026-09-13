---
id: GIT-T-0131
type: task
title: Add the custom field discovery endpoint
status: todo
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:32Z
updated: 2026-09-13T13:18:32Z
---

## Description

Add `GET /api/v1/youtrack/fields` in `internal/server/youtrack.go`. It calls `GET /api/admin/projects/{id}/customFieldSettings?fields=field(name,fieldType(id)),bundle(id,$type),canBeEmpty` and then the matching enum, state or version bundle value endpoint for each bundle-backed field, returning `{fields: [{name, type, values: [{name, isResolved?, archived?}]}]}`. The route is gated on `features.youtrack` and a linked project and goes through the shared rate limiter.

## Acceptance Criteria

- [ ] The endpoint returns each custom field with its type and, for bundle-backed fields, its values.
- [ ] Enum, state and version bundles are all resolved; state values carry `isResolved`.
- [ ] The route is gated and returns 404 for an unlinked project.
- [ ] `go test -race ./internal/server/...` covers the projection with an httptest upstream.
