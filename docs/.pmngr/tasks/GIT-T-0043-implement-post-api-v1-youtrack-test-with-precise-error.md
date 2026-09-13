---
id: GIT-T-0043
type: task
title: Implement POST /api/v1/youtrack/test with precise error mapping
status: todo
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 2
created: 2026-09-13T13:16:22Z
updated: 2026-09-13T13:16:22Z
---

## Description

Implement the connection test: call `youtrack.Client.Me` with a bounded timeout, returning the login and full name on success. Map the typed client errors to distinct problem documents — 401 to `youtrack_unauthorized`, 403 to a permissions message, 404 to a hint that the base URL is probably missing its context path, transport failure to `youtrack_unreachable` — and accept an unsaved URL and token in the request body so the user can test before saving.

## Acceptance Criteria

- [ ] Success returns the resolved YouTrack user; each failure mode returns its own code and an actionable message.
- [ ] The handler honours request cancellation and finishes inside the router's 30 s timeout (`server.go:355`).
- [ ] No problem `detail` ever contains the token, asserted by a test against an `httptest` YouTrack stub.
