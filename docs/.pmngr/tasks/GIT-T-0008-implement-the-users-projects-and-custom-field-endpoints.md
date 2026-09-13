---
id: GIT-T-0008
type: task
title: Implement the users, projects and custom-field endpoints
status: todo
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:15:19Z
updated: 2026-09-13T13:15:19Z
---

## Description

Add `Me(ctx)` over `GET /api/users/me?fields=id,login,fullName,email`, `Projects(ctx, query, page)` over `GET /api/admin/projects?fields=id,shortName,name,archived&query=&$top=&$skip=`, `Project(ctx, key)` over `GET /api/admin/projects/{key}` and `CustomFieldSettings(ctx, projectID)` over `GET /api/admin/projects/{id}/customFieldSettings?fields=field(name,fieldType(id)),bundle(id),canBeEmpty`. All list endpoints return a bare JSON array, never an envelope. Decode into typed structs in `internal/youtrack/types.go`.

## Acceptance Criteria

- [ ] All four methods are implemented with the field selectors above and typed results.
- [ ] A 404 from `Me` is surfaced distinctly so the caller can suggest a missing context path in the base URL.
- [ ] `go test -race ./internal/youtrack/...` covers each method against recorded JSON fixtures.
