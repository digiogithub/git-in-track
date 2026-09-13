---
id: GIT-T-0007
type: task
title: Add the shared rate limiter and the retry loop with Retry-After
status: todo
priority: medium
parent: GIT-US-0046
milestone: GIT-M-0011
author: mcp
labels: [server, performance, agent-ok]
estimate: 3
created: 2026-09-13T13:15:14Z
updated: 2026-09-13T13:15:14Z
---

## Description

Add a single token-bucket limiter to `Client`, shared by all concurrent callers, defaulting to 5 requests per second and disabled when the rate is zero. Wrap `do` in a retry loop covering 429, 500, 502, 503, 504 and transport errors, up to `MaxRetries` (default 4), sleeping for `Retry-After` when present — parsing both the delta-seconds and the HTTP-date form and clamping negatives to zero — otherwise `base * 2^attempt + jitter` from 500 ms. Drain and close the response body before every retry so the connection is reused. Time comes from the injectable `Now`/sleep seam so tests never sleep for real.

## Acceptance Criteria

- [ ] Concurrent goroutines are throttled to the configured rate, proved with a fake clock.
- [ ] `Retry-After` in seconds and in HTTP-date form both honoured; a non-429 4xx fails on the first attempt.
- [ ] Bodies are drained before retrying and no response body is leaked.
- [ ] `go test -race ./internal/youtrack/...` passes with no wall-clock sleeps.
