---
id: GIT-T-0068
type: task
title: Publish import progress events on the hub
status: todo
priority: medium
parent: GIT-US-0050
milestone: GIT-M-0011
author: mcp
labels: [server, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:17:00Z
updated: 2026-09-13T13:17:00Z
---

## Description

Publish `sync.job.progress` with `{jobId, done, total, currentId}` after each batch through `Hub.Publish` (`internal/server/hub.go:167`), alongside the engine's own `queued`, `started`, `done` and `failed` events, and accumulate per-issue failures into the job result rather than failing the job. Document the progress payload in `docs/07-cli-and-api.md` §5.6 next to the existing `sync.progress` contract.

## Acceptance Criteria

- [ ] `sync.job.progress` carries `{jobId, done, total, currentId}` and is emitted once per batch.
- [ ] Per-issue failures accumulate in the job result and do not fail the job.
- [ ] The event is documented in `docs/07-cli-and-api.md` §5.6.
- [ ] `go test -race ./internal/server/...` asserts the emitted sequence for a two-batch import.
