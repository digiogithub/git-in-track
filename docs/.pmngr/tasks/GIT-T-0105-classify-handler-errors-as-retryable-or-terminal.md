---
id: GIT-T-0105
type: task
title: Classify handler errors as retryable or terminal
status: todo
priority: medium
parent: GIT-US-0067
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:53Z
updated: 2026-09-13T13:17:53Z
---

## Description

Add an error classifier to `internal/syncengine` that maps a handler error to retryable or terminal, plus an optional `Retry-After` duration. Recognise the typed errors from `internal/youtrack` through `errors.As` on small interfaces declared in the engine, so the package keeps no import of the YouTrack client: 429 and 5xx and transport errors retry, while 401, 403, 404 and validation errors are terminal and consume no attempts.

## Acceptance Criteria

- [ ] Classification is interface-based and `internal/syncengine` does not import `internal/youtrack`.
- [ ] Terminal errors fail on the first attempt; retryable ones schedule a retry.
- [ ] `go test -race ./internal/syncengine/...` covers each class in a table-driven test.
