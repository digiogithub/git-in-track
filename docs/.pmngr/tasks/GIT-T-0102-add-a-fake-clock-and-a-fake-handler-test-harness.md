---
id: GIT-T-0102
type: task
title: Add a fake clock and a fake handler test harness
status: todo
priority: medium
parent: GIT-US-0063
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:17:48Z
updated: 2026-09-13T13:17:48Z
---

## Description

Add an internal test harness under `internal/syncengine`: a fake clock with manual advance driving both the debounce timers and the rate limiter, and a scriptable fake handler that can succeed, fail, block or record the batches it received. Every engine test uses these instead of wall-clock sleeps, so the suite stays fast and deterministic under `-race`.

## Acceptance Criteria

- [ ] The fake clock drives timers and the limiter, and advancing it deterministically fires pending work.
- [ ] The fake handler records batch contents and can be scripted per attempt.
- [ ] No engine test calls `time.Sleep`, and `go test -race ./internal/syncengine/...` runs in a few seconds.
