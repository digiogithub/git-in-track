---
id: GIT-EP-0015
type: epic
title: "Sync engine: queue, workers, batches and settings"
status: done
priority: high
milestone: GIT-M-0011
author: mcp
labels: [server, web, docs, performance]
created: 2026-09-13T13:07:58Z
updated: 2026-09-15T19:37:31Z
started: 2026-09-15T19:37:12Z
closed: 2026-09-15T19:37:31Z
---

## Description

git-in-track has no background job abstraction: git sync runs inside its HTTP handler and the only async pattern is the debounced commit batcher in `internal/gitops/committer.go`. Integrations need a real engine: a persistent job queue with named job kinds (import, push comment, publish page, pull page), a worker pool with a configurable size, batching of homogeneous jobs against one YouTrack instance, a shared rate limiter, retries with exponential backoff honouring `Retry-After`, per-job state (`queued / running / done / failed / cancelled`), cancellation, and progress events on the WebSocket hub. State survives a restart through a JSON journal in the cache directory; no embedded database.

Settings UI exposes workers, batch size, rate limit, retry policy, auto-push toggles and a live view of the queue with retry/cancel.

## Acceptance Criteria

- [ ] `internal/syncengine` package: `Engine`, `Job`, `Kind`, `Handler` registry, `Enqueue`, `Cancel`, `Flush`, `Close`; lifecycle wired in `Server.Start` with `context.WithoutCancel`.
- [ ] Worker pool size, batch size and rate limit configurable at runtime; defaults 2 workers, batch 20, 5 req/s.
- [ ] Retry with backoff (500 ms, 1.5 s, 4 s, ... capped), max attempts configurable, `Retry-After` respected; failed jobs kept for inspection.
- [ ] Journal in `config.CacheDir` (`jobs.json`), replayed on start; completed jobs pruned after N days.
- [ ] Events `sync.job.queued|started|progress|done|failed` on the hub, documented in `docs/07-cli-and-api.md` §5.6.
- [ ] REST `GET /api/v1/sync/jobs`, `POST /api/v1/sync/jobs/{id}/retry|cancel`, `GET|PATCH /api/v1/sync/settings`.
- [ ] Settings card with the knobs and a queue table; toasts for job completion and failure.
- [ ] Unit tests with a fake clock and a fake handler; race detector clean.

## Notes

Copy the shape of `gitops.Committer` (keyed batches, `time.AfterFunc`, inflight accounting) and `internal/tunnel` (generation fencing) rather than inventing a new one.
