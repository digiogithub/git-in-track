---
id: GIT-T-0007
type: task
title: Add the shared rate limiter and the retry loop with Retry-After
status: done
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, performance, agent-ok]
estimate: 3
created: 2026-09-13T13:15:14Z
updated: 2026-09-13T14:04:29Z
started: 2026-09-13T14:04:15Z
closed: 2026-09-13T14:04:29Z
---

## Description

Add a single token-bucket limiter to `Client`, shared by all concurrent callers, defaulting to 5 requests per second and disabled when the rate is zero. Wrap `do` in a retry loop covering 429, 500, 502, 503, 504 and transport errors, up to `MaxRetries` (default 4), sleeping for `Retry-After` when present — parsing both the delta-seconds and the HTTP-date form and clamping negatives to zero — otherwise `base * 2^attempt + jitter` from 500 ms. Drain and close the response body before every retry so the connection is reused. Time comes from the injectable `Now`/sleep seam so tests never sleep for real.

## Acceptance Criteria

- [x] Concurrent goroutines are throttled to the configured rate, proved with a fake clock.
- [x] `Retry-After` in seconds and in HTTP-date form both honoured; a non-429 4xx fails on the first attempt.
- [x] Bodies are drained before retrying and no response body is leaked.
- [x] `go test -race ./internal/youtrack/...` passes with no wall-clock sleeps.

## Notes

One deliberate deviation from the description, so the zero value of `Options` stays safe: `Rate: 0` selects the 5 req/s default and a **negative** `Rate` disables throttling. Disabling on zero would have made the zero-value client unthrottled, which is the opposite of the intended default. Same convention for `MaxRetries`: 0 selects 4, negative disables retrying.

`TestRetryDrainsBodies` counts TCP connections through `Server.ConnState` and asserts one connection serves all three attempts, which only holds when each retried body is fully drained and closed.
