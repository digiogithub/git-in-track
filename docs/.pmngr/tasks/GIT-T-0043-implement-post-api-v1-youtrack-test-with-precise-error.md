---
id: GIT-T-0043
type: task
title: Implement POST /api/v1/youtrack/test with precise error mapping
status: done
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 2
created: 2026-09-13T13:16:22Z
updated: 2026-09-13T14:42:41Z
started: 2026-09-13T14:42:23Z
closed: 2026-09-13T14:42:41Z
---

## Description

Implement the connection test: call `youtrack.Client.Me` with a bounded timeout, returning the login and full name on success. Map the typed client errors to distinct problem documents — 401 to `youtrack_unauthorized`, 403 to a permissions message, 404 to a hint that the base URL is probably missing its context path, transport failure to `youtrack_unreachable` — and accept an unsaved URL and token in the request body so the user can test before saving.

## Acceptance Criteria

- [x] Success returns the resolved YouTrack user; each failure mode returns its own code and an actionable message.
- [x] The handler honours request cancellation and finishes inside the router's 30 s timeout (`server.go:355`).
- [x] No problem `detail` ever contains the token, asserted by a test against an `httptest` YouTrack stub.

## Notes

`POST /api/v1/youtrack/test?key=<projectKey>`, with an optional `{url, token}` body that tests a connection the user has typed but not saved; nothing is written either way. On success it also resolves the linked YouTrack project, and stays silent about it rather than failing the whole probe when the token authenticates but cannot see it.

The handler derives a 15 s context from the request's, so cancellation propagates and the call cannot outlive the router's 30 s. The stub in the test echoes the credential back in its error body, which is what proves the redaction in `internal/youtrack` survives the trip into a problem document.
