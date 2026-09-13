---
id: GIT-T-0124
type: task
title: Prune completed jobs after the retention window
status: done
priority: medium
parent: GIT-US-0070
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:18:20Z
updated: 2026-09-13T14:14:46Z
started: 2026-09-13T14:14:28Z
closed: 2026-09-13T14:14:46Z
---

## Description

Prune `done` jobs older than a configurable retention window, default 7 days, both at start-up and periodically on the injectable clock, so the journal cannot grow without bound on a long-running companion. Failed and dead-lettered jobs are never pruned automatically — they are cleared explicitly by the user.

## Acceptance Criteria

- [x] `done` jobs older than the window are removed at start-up and during a long run.
- [x] Failed and dead-lettered jobs survive pruning.
- [x] The retention window is configurable and validated.
- [x] `go test -race ./internal/syncengine/...` covers pruning with a fake clock.

## Notes

Cancelled jobs are pruned on the same window as done ones: they are finished bookkeeping too, and leaving them would let a long run grow without bound through the one state nothing ever clears. Failed and dead-lettered jobs are untouched by the sweep.
