---
id: GIT-US-0063
type: story
title: "Core sync engine: queue, worker pool and keyed batching"
status: backlog
priority: high
parent: GIT-EP-0015
milestone: GIT-M-0011
author: mcp
labels: [server, performance]
estimate: 13
created: 2026-09-13T13:12:52Z
updated: 2026-09-13T13:12:52Z
---

## Description

As a developer building integrations, I want a real background job engine in the companion, so that an import or a comment push runs off the HTTP request that started it, respects a shared rate limit and survives the response being closed.

Nothing exists to plug into: git sync is synchronous inside its handler (`internal/server/sync.go:145`), there is no worker pool, no queue and no persistent job store, and the whole companion runs seven non-test goroutines. Build `internal/syncengine/` as a native package with `Engine`, `Job{ID, Kind, Key, Payload, Attempts, State, CreatedAt, …}`, `Kind` as a named string, a `Handler` registry keyed by `Kind`, and the methods `Register(kind, handler)`, `Enqueue(ctx, job)`, `Cancel(id)`, `Pending()`, `Flush(ctx)` and `Close(ctx)`. Jobs of the same `Kind` and coalescing `Key` batch together up to a configurable size, and a single shared limiter caps outbound work regardless of worker count.

Copy the proven local pattern rather than inventing one: `internal/gitops/committer.go` gives per-key `batch` structs (`:91`), `Enqueue` with a coalescing key (`:157`, `:181`), `arm` → `time.AfterFunc` (`:189`, `:205`), a debounce capped at a maximum (`:12`, `:18`), `fire` (`:211`), in-flight accounting over a `sync.Cond` (`:119`-`:135`), `Flush` (`:240`), idempotent `Close` (`:277`) and `Pending` (`:267`). Crucially it wraps the caller context in `context.WithoutCancel` (`committer.go:167`) so an HTTP response closing cannot cancel the work it caused — the same idiom appears at `internal/server/tunnel.go:198` and `internal/server/server.go:304`. For the supervised worker loop with restart and stale-event fencing, `internal/tunnel/tunnel.go` is the template: `generation atomic.Uint64` (`:114`), `set(generation, mutate)` (`:321`), `applyEvent` dropping stale generations (`:395`).

Time must be injectable so tests use a fake clock instead of sleeping, and the package must be race-clean under load.

## Acceptance Criteria

- [ ] `internal/syncengine` exposes `Engine`, `Job`, `Kind`, `Handler`, `Register`, `Enqueue`, `Cancel`, `Pending`, `Flush`, `Close` with documented semantics.
- [ ] Worker pool size, batch size and rate limit are configurable, defaulting to 2 workers, batch 20 and 5 req/s.
- [ ] Jobs sharing a `Kind` and `Key` coalesce into one batch handed to the handler as a slice; jobs with different keys never merge.
- [ ] `Enqueue` detaches the caller context with `context.WithoutCancel`, so a cancelled HTTP request does not cancel queued work; a test proves it.
- [ ] `Cancel(id)` removes a queued job and signals a running one through its context; `Flush` waits for the queue to drain; `Close` is idempotent and safe to call twice.
- [ ] The shared limiter caps total outbound rate across all workers, proved with a fake clock rather than wall-clock sleeps.
- [ ] Job state transitions are `queued → running → done|failed|cancelled` and no other transition is reachable.
- [ ] `go test -race ./internal/syncengine/...` passes, including a concurrent enqueue/cancel/close stress test, with no wall-clock sleeps.

## Notes

The engine knows nothing about YouTrack: handlers are registered by the caller, so the package has no dependency on `internal/youtrack` and stays unit-testable with a fake handler. It is native-only and must never be imported from `internal/core`, which compiles to WASM.

Performance targets are CI-enforced (`docs/02-architecture.md` §9): a cold index of 10 000 items stays under 2 s native, so the engine must not hold the vault mutex while working — every write goes through the normal vault call, one item at a time, each carrying its own `rev`.

Do NOT add an embedded database (no bolt, no sqlite) — that is explicitly out of scope and would need its own ADR. Do NOT add `errgroup`, `semaphore` or `singleflight`: none is used anywhere in the codebase today and the standard library covers this. Persistence is the next story; this one keeps state in memory.
