---
id: GIT-T-0085
type: task
title: "Define the syncengine types: Job, Kind, Handler and the registry"
status: todo
priority: medium
parent: GIT-US-0063
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:17:24Z
updated: 2026-09-13T13:17:24Z
---

## Description

Create `internal/syncengine/` with `Kind` as a named string, `Job{ID, Kind, Key, Payload, State, Attempts, CreatedAt, UpdatedAt, LastError, NextAttempt}`, a `State` enum covering `queued`, `running`, `done`, `failed` and `cancelled`, a `Handler` interface taking a batch of jobs, and `Engine` with `Register(kind, handler)`. Include an injectable clock seam so every later test can use a fake. Document in the package comment that handlers must be idempotent, since a job can be retried or replayed after a restart.

## Acceptance Criteria

- [ ] The types and the registry exist, with duplicate `Register` for one kind rejected.
- [ ] Only the documented state transitions are reachable, enforced in one place.
- [ ] The clock is injectable and defaults to the real one.
- [ ] `go test -race ./internal/syncengine/...` covers registration and the transition guard.
