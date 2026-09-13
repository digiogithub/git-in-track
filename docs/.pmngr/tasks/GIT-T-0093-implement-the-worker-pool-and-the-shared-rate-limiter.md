---
id: GIT-T-0093
type: task
title: Implement the worker pool and the shared rate limiter
status: done
priority: medium
parent: GIT-US-0063
milestone: GIT-M-0011
author: mcp
labels: [server, performance, agent-ok]
estimate: 5
created: 2026-09-13T13:17:36Z
updated: 2026-09-13T14:06:02Z
started: 2026-09-13T14:05:52Z
closed: 2026-09-13T14:06:02Z
---

## Description

Add a worker pool of configurable size (default 2) pulling batches from the queue, with in-flight accounting over a `sync.Cond` as in `internal/gitops/committer.go:119-135`, and a single shared token-bucket limiter (default 5 req/s) that every worker passes through so total outbound rate is independent of worker count. Use the generation-fencing idiom of `internal/tunnel/tunnel.go` (`generation atomic.Uint64` `:114`, `set` `:321`, stale drop in `applyEvent` `:395`) so a worker restarted after a settings change cannot apply a stale result.

## Acceptance Criteria

- [x] Pool size and rate are configurable and changing them takes effect on a running engine.
- [x] Total throughput is capped by the limiter regardless of worker count, proved with a fake clock.
- [x] Results from a superseded generation are dropped rather than applied.
- [x] `go test -race ./internal/syncengine/...` includes a concurrent stress test with no data races and no goroutine leaks.

## Notes

Fencing is two-layered. `Engine.generation` retires the pool when `SetWorkers` or `Close` bumps it; a worker already inside a handler still finishes its batch, because abandoning it would lose the jobs in it. Per-result fencing is finer and lives on `jobRecord.gen`: cancelling or re-queueing a job bumps its stamp, and the worker's outcome is dropped for any job whose stamp moved under it.
