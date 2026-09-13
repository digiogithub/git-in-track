---
id: GIT-T-0109
type: task
title: Implement the retry policy with backoff, jitter and Retry-After
status: done
priority: medium
parent: GIT-US-0067
milestone: GIT-M-0011
author: mcp
labels: [server, performance, agent-ok]
estimate: 3
created: 2026-09-13T13:17:58Z
updated: 2026-09-13T14:14:34Z
started: 2026-09-13T14:14:03Z
closed: 2026-09-13T14:14:34Z
---

## Description

Add `RetryPolicy{MaxAttempts, Base, Max, Jitter}` defaulting to 5 attempts growing 500 ms, 1.5 s, 4 s and capped, following the shape of `internal/gitops/sync.go` (`defaultBackoff` `:755`, `sleep(ctx, d)` `:766`, the push-retry loop `:661-684`). A `Retry-After` from the classifier always overrides the computed delay. Retries are scheduled on the injectable clock and set the job's `NextAttempt` so the UI can show when it will run.

## Acceptance Criteria

- [x] Delays follow the configured growth with jitter and respect the cap.
- [x] `Retry-After` overrides the computed delay in both seconds and HTTP-date form.
- [x] `NextAttempt` is set on every scheduled retry.
- [x] `go test -race ./internal/syncengine/...` verifies the delay sequence with a fake clock.

## Notes

The ladder is `Base * 3^(attempt-1)` capped at `Max`: 500 ms, 1.5 s, 4.5 s, 13.5 s, then the 30 s cap. `Jitter` is the symmetric fraction the delay is spread by (default 0.2); a negative `Jitter` turns it off, which is what the test that pins the exact ladder uses.
