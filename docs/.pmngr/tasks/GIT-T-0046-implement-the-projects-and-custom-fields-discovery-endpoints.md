---
id: GIT-T-0046
type: task
title: Implement the projects and custom-fields discovery endpoints
status: done
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:27Z
updated: 2026-09-13T14:42:42Z
started: 2026-09-13T14:42:29Z
closed: 2026-09-13T14:42:42Z
---

## Description

Implement `GET /api/v1/youtrack/projects?q=` returning a bounded, filtered list of `{id, shortName, name, archived}` for the settings autosuggest, and `GET /api/v1/youtrack/fields?project=` returning the custom field settings so the field-mapping UI offers real names. Both are thin wrappers over `internal/youtrack`; the client-side limiter already caps outbound rate, but cap the page size here too so a keystroke cannot fan out.

## Acceptance Criteria

- [x] Both endpoints return typed JSON, bounded in size, and 409-style `youtrack_not_configured` when no integration exists.
- [x] Repeated calls are safe to make on keystrokes and never exceed the client rate limit.
- [x] `go test -race ./internal/server/...` covers both against an `httptest` stub.

## Notes

The page is capped at 100 and the cap is reported as `limit`, so a client can tell a full page from the whole truth. `youtrackState` caches the client per project, which is what makes the rate limiter shared rather than per request — a test asserts two calls return the same client.

`/fields` also returns `gintrackFields`, the left-hand side of a field mapping, so the settings card gets both halves of the mapping from one call instead of hard-coding the list in the frontend.
