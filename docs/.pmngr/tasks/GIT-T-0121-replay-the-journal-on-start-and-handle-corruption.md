---
id: GIT-T-0121
type: task
title: Replay the journal on start and handle corruption
status: todo
priority: medium
parent: GIT-US-0070
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:16Z
updated: 2026-09-13T13:18:16Z
---

## Description

Read the journal when the engine starts: `queued` jobs return to the queue, jobs recorded as `running` are re-queued as `queued` keeping their attempt count, and `done` jobs are kept only for the retention window. A corrupt, truncated or unreadable journal is renamed aside with a timestamped suffix and logged, and the engine starts empty — the journal is derived state and the Markdown files remain the source of truth.

## Acceptance Criteria

- [ ] Queued and failed jobs survive a simulated restart with their attempt counts intact.
- [ ] A `running` job is re-queued rather than lost or double-counted.
- [ ] A corrupt journal is moved aside and the engine still starts, logging what happened.
- [ ] `go test -race ./internal/syncengine/...` covers replay, the running case and the corrupt case.
