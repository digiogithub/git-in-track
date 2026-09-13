---
id: GIT-T-0089
type: task
title: Implement the queue, keyed batching and the debounce timer
status: todo
priority: medium
parent: GIT-US-0063
milestone: GIT-M-0011
author: mcp
labels: [server, performance, agent-ok]
estimate: 5
created: 2026-09-13T13:17:30Z
updated: 2026-09-13T13:17:30Z
---

## Description

Implement `Enqueue(ctx, job)` coalescing jobs by `Kind` plus `Key` into a per-key batch capped at a configurable size, armed with a `time.AfterFunc` debounce capped at a maximum, exactly as `internal/gitops/committer.go` does (`batch` `:91`, `Enqueue` `:157`, coalescing key `:181`, `arm` `:189`, `fire` `:211`, `DefaultDebounce` `:12`, `maxDebounce` `:18`). `Enqueue` must detach the caller context with `context.WithoutCancel` (`committer.go:167`) so a closing HTTP response cannot cancel the work it started.

## Acceptance Criteria

- [ ] Jobs with the same kind and key batch together; different keys never merge; a batch fires at the cap without waiting for the debounce.
- [ ] A cancelled caller context does not cancel the enqueued job, proved by a test.
- [ ] Timers are driven by the injectable clock, so tests do not sleep.
- [ ] `go test -race ./internal/syncengine/...` covers coalescing, the cap and the debounce ceiling.
