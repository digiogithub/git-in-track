---
id: GIT-T-0068
type: task
title: Publish import progress events on the hub
status: done
priority: medium
parent: GIT-US-0050
milestone: GIT-M-0011
author: mcp
labels: [server, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:17:00Z
updated: 2026-09-13T15:58:39Z
started: 2026-09-13T15:58:15Z
closed: 2026-09-13T15:58:39Z
---

## Description

Publish `sync.job.progress` with `{jobId, done, total, currentId}` after each batch through `Hub.Publish` (`internal/server/hub.go:167`), alongside the engine's own `queued`, `started`, `done` and `failed` events, and accumulate per-issue failures into the job result rather than failing the job. Document the progress payload in `docs/07-cli-and-api.md` §5.6 next to the existing `sync.progress` contract.

## Acceptance Criteria

- [x] `sync.job.progress` carries `{jobId, done, total, currentId}` and is emitted once per batch.
- [x] Per-issue failures accumulate in the job result and do not fail the job.
- [x] The event is documented in `docs/07-cli-and-api.md` §5.6.
- [x] `go test -race ./internal/server/...` asserts the emitted sequence for a two-batch import.

## Notes

The payload is a **superset** of the engine's own `sync.job.*` shape rather than
a second one: it carries `jobId`, `done`, `total`, `currentId` and `failed`, and
repeats the first two as `id` and `processed`, the names the engine's queue
events already use. One client-side reader therefore handles both sources
without branching on which produced the frame, which matters because both
publish on the same topic.

It is published once per batch (an import) or once per page (a knowledge-base
job) and is deliberately **not** throttled: the 500 ms coalescing in
`syncjobevents.go` applies to the engine's own transitions, where a thousand
jobs can narrate themselves, while a handler frame is already coarse and
throttling it would leave a bar stuck.
