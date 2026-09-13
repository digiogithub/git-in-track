---
id: GIT-US-0074
type: story
title: Sync job progress events on the WebSocket hub
status: backlog
priority: high
parent: GIT-EP-0015
milestone: GIT-M-0011
author: mcp
labels: [server, web, docs]
estimate: 5
created: 2026-09-13T13:13:43Z
updated: 2026-09-13T13:13:43Z
---

## Description

As a user watching an import run, I want the queue and its progress to update live in the web app, so that I can see what the companion is doing instead of reloading a page and hoping.

Publish `sync.job.queued`, `sync.job.started`, `sync.job.progress`, `sync.job.done` and `sync.job.failed` on the existing hub (`internal/server/hub.go`, `Publish` `:167`), from a thin observer the server registers on the engine so that `internal/syncengine` keeps no dependency on `internal/server`. The payload carries the job id, kind, coalescing key, state, attempt, processed and total counts and, for failures, the redacted error. This mirrors the shapes already in use — `sync.progress` (`internal/server/sync.go:239`) and `git.commit` (`internal/server/git.go:544`) — and inherits the hub's behaviour: `Publish` never blocks, clients hold a 256-event buffer that drops on overflow (`hub.go:31`, `deliver` `:104`) and a 1000-event replay ring supports `since` (`:27`, `:234`). A long import must therefore emit coalesced progress, not one event per item, or slow clients will simply lose frames.

On the frontend, bridge the new topics into TanStack Query the way `useBacklogEvents` already does (`web/src/features/backlog/queries.ts:130-148`): a `useSyncJobEvents` hook subscribes to `sync.job.*` and invalidates the jobs query, so both the queue table and any toast react to the same stream.

## Acceptance Criteria

- [ ] The five `sync.job.*` topics are published from the server-side observer, and `internal/syncengine` does not import `internal/server`.
- [ ] Progress events are coalesced (at most a few per second per job) so a large batch cannot overflow a client's 256-event buffer.
- [ ] Event payloads never contain a credential and carry the redacted error on failure.
- [ ] Clients resuming with `since` receive the events they missed from the replay ring, consistent with the existing contract.
- [ ] `docs/07-cli-and-api.md` §5.6 documents each topic and its payload alongside the existing `sync.progress` and `git.commit` entries.
- [ ] A `useSyncJobEvents` hook subscribes to the topics and invalidates the jobs query; it unsubscribes cleanly on unmount.
- [ ] `go test -race ./internal/server/...` covers publication for each state, and Vitest covers the hook's subscribe, invalidate and cleanup behaviour.

## Notes

The WebSocket endpoint is `GET /api/v1/events` (`internal/server/events.go:44`) with `subscribe`/`unsubscribe`/`resume`/`ping` client frames (`clientFrame` `:34`); the wire contract is documented at `docs/07-cli-and-api.md:2353-2452`. Existing publishers are collected in `internal/server/events.go` (`publishWrite` `:256`, `publishDelta` `:372`) and are the style to follow.

Do NOT emit an event per imported item: the hub drops on overflow by design, and a dropped frame is worse than a coarser one. Do NOT make the engine publish directly — keeping the transport out of the package is what allows the fake-handler unit tests to exist.
