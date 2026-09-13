---
id: GIT-T-0046
type: task
title: Implement the projects and custom-fields discovery endpoints
status: todo
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:27Z
updated: 2026-09-13T13:16:27Z
---

## Description

Implement `GET /api/v1/youtrack/projects?q=` returning a bounded, filtered list of `{id, shortName, name, archived}` for the settings autosuggest, and `GET /api/v1/youtrack/fields?project=` returning the custom field settings so the field-mapping UI offers real names. Both are thin wrappers over `internal/youtrack`; the client-side limiter already caps outbound rate, but cap the page size here too so a keystroke cannot fan out.

## Acceptance Criteria

- [ ] Both endpoints return typed JSON, bounded in size, and 409-style `youtrack_not_configured` when no integration exists.
- [ ] Repeated calls are safe to make on keystrokes and never exceed the client rate limit.
- [ ] `go test -race ./internal/server/...` covers both against an `httptest` stub.
