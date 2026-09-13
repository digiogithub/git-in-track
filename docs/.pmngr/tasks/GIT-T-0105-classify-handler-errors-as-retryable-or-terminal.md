---
id: GIT-T-0105
type: task
title: Classify handler errors as retryable or terminal
status: done
priority: medium
parent: GIT-US-0067
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:53Z
updated: 2026-09-13T14:14:32Z
started: 2026-09-13T14:13:57Z
closed: 2026-09-13T14:14:32Z
---

## Description

Add an error classifier to `internal/syncengine` that maps a handler error to retryable or terminal, plus an optional `Retry-After` duration. Recognise the typed errors from `internal/youtrack` through `errors.As` on small interfaces declared in the engine, so the package keeps no import of the YouTrack client: 429 and 5xx and transport errors retry, while 401, 403, 404 and validation errors are terminal and consume no attempts.

## Acceptance Criteria

- [x] Classification is interface-based and `internal/syncengine` does not import `internal/youtrack`.
- [x] Terminal errors fail on the first attempt; retryable ones schedule a retry.
- [x] `go test -race ./internal/syncengine/...` covers each class in a table-driven test.

## Notes

The interfaces the classifier reads are `StatusCoder`, `RetryableError`, `TerminalError`, `RetryAfterProvider` and `RetryAfterHeaderProvider`, plus the `ErrTerminal` and `ErrRetry` sentinels and the `Terminal(err)` / `RetryAfter(err, d)` wrappers.

`youtrack.APIError` carries its status in an exported **field** (`Status`), not a method, so it does not satisfy `StatusCoder` as it stands. A handler therefore wraps it — `syncengine.Terminal(err)` for a rejected token, `syncengine.RetryAfter(err, d)` for a throttle — or the caller sets `Options.Classify`. Adding a `StatusCode() int` method to `APIError` would make the default classifier read it with no wrapper at all; that is a one-line change in a package this story does not own.
