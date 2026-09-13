---
id: GIT-T-0063
type: task
title: Register the youtrack.import job kind and its handler
status: done
priority: medium
parent: GIT-US-0050
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:16:52Z
updated: 2026-09-13T15:58:33Z
started: 2026-09-13T15:58:13Z
closed: 2026-09-13T15:58:33Z
---

## Description

Add `internal/server/youtrack_import.go` registering a `youtrack.import` `Kind` and `Handler` with the sync engine, wired where the engine is constructed in `Server.Start`. The handler decodes the job payload, resolves the issue set through the YouTrack client with `$top`/`$skip` paging that always appends `order by: created asc`, and calls `youtrack.import.run` in batches whose size comes from the engine settings (default 20). Hand work to the engine with `context.WithoutCancel(ctx)` so a closing HTTP response cannot cancel it.

## Acceptance Criteria

- [x] The `youtrack.import` kind is registered and enqueued through `Engine.Enqueue`.
- [x] Paging always appends an ordering clause and is covered by a test asserting the composed query.
- [x] Issues are processed in batches of the configured size, one vault call per batch.
- [x] `go test -race ./internal/server/...` covers batching with a fake client and a fake engine.

## Notes

Landed as `internal/server/youtrackimport.go`, with the shared plumbing of the
four YouTrack kinds in `internal/server/youtrackjobs.go`.

Registration happens in `server.New` rather than in `Server.Start`: `Start`
replays the journal, so a kind registered after it would leave a restored job
undispatchable. `New` is the last moment at which the server exists and nothing
is running.

`Server.EnqueueYouTrackImport` is the enqueue seam, and it detaches the context
with `context.WithoutCancel` so a closing HTTP response cannot cancel the work
it started. `POST /api/v1/youtrack/import` calls it.
