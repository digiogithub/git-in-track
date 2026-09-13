---
id: GIT-T-0008
type: task
title: Implement the users, projects and custom-field endpoints
status: done
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:15:19Z
updated: 2026-09-13T14:05:16Z
started: 2026-09-13T14:05:04Z
closed: 2026-09-13T14:05:16Z
---

## Description

Add `Me(ctx)` over `GET /api/users/me?fields=id,login,fullName,email`, `Projects(ctx, query, page)` over `GET /api/admin/projects?fields=id,shortName,name,archived&query=&$top=&$skip=`, `Project(ctx, key)` over `GET /api/admin/projects/{key}` and `CustomFieldSettings(ctx, projectID)` over `GET /api/admin/projects/{id}/customFieldSettings?fields=field(name,fieldType(id)),bundle(id),canBeEmpty`. All list endpoints return a bare JSON array, never an envelope. Decode into typed structs in `internal/youtrack/types.go`.

## Acceptance Criteria

- [x] All four methods are implemented with the field selectors above and typed results.
- [x] A 404 from `Me` is surfaced distinctly so the caller can suggest a missing context path in the base URL.
- [x] `go test -race ./internal/youtrack/...` covers each method against recorded JSON fixtures.

## Notes

Two additions beyond the description, both needed by the next wave and both cheap here: `AllProjects(ctx, query)` walks the project pages, and `VersionBundleValues(ctx, bundleID)` reads a version bundle — the closest YouTrack analogue to a git-in-track milestone, reached through the `Fix versions` bundle id that `CustomFieldSettings` returns.

The admin project `query` parameter is a substring search, not the issue query language, so it deliberately does not go through `EnsureOrderBy`.
