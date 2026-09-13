---
id: GIT-T-0124
type: task
title: Prune completed jobs after the retention window
status: todo
priority: medium
parent: GIT-US-0070
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:18:20Z
updated: 2026-09-13T13:18:20Z
---

## Description

Prune `done` jobs older than a configurable retention window, default 7 days, both at start-up and periodically on the injectable clock, so the journal cannot grow without bound on a long-running companion. Failed and dead-lettered jobs are never pruned automatically — they are cleared explicitly by the user.

## Acceptance Criteria

- [ ] `done` jobs older than the window are removed at start-up and during a long run.
- [ ] Failed and dead-lettered jobs survive pruning.
- [ ] The retention window is configurable and validated.
- [ ] `go test -race ./internal/syncengine/...` covers pruning with a fake clock.
