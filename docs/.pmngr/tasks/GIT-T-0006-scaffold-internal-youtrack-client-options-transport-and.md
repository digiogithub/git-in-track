---
id: GIT-T-0006
type: task
title: "Scaffold internal/youtrack: Client, Options, transport and typed errors"
status: todo
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, security, agent-ok]
estimate: 3
created: 2026-09-13T13:15:09Z
updated: 2026-09-13T13:15:09Z
---

## Description

Create `internal/youtrack/client.go` with `Client`, `Options{BaseURL, Token, HTTPClient, Rate, MaxRetries, Now}` and a private `do(ctx, method, path, query, body, out)` that sets `Authorization: Bearer <token>` and `Accept: application/json`, adds `Content-Type` only for writes, and builds the URL as `strings.TrimRight(base, "/") + path` so a context path such as `/youtrack` survives. Add `ErrUnauthorized`, `ErrForbidden` and `ErrNotFound` plus an `APIError` carrying status and a redacted body, and a `redact` helper used everywhere the token could leak.

## Acceptance Criteria

- [ ] A request against an `httptest.Server` mounted at a context path hits the expected absolute path.
- [ ] 401, 403 and 404 map to the typed errors; other statuses become `APIError`.
- [ ] No error, log line or `String()` output contains the token, asserted by a test.
- [ ] `make lint` passes including `bodyclose`, `noctx` and `wrapcheck`.
