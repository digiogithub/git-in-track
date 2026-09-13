---
id: GIT-US-0050
type: story
title: Import job kind in the sync engine
status: backlog
priority: high
parent: GIT-EP-0012
milestone: GIT-M-0011
author: mcp
labels: [server, performance]
estimate: 5
created: 2026-09-13T13:11:50Z
updated: 2026-09-13T13:11:50Z
---

## Description

As a user importing a large query result, I want the import to run in the background with progress and a cancel button, so that the browser never waits on hundreds of YouTrack requests and a mistake can be stopped halfway.

Register a `youtrack.import` job kind with the sync engine from GIT-EP-0015: a `Handler` that receives `{project, ids[] | query, depth, includeLinks, includeComments, includeAttachments}`, resolves the issue set through the YouTrack client in `$top`/`$skip` pages always paired with `order by: created asc`, and then calls `youtrack.import.run` in batches (default 20 issues) so each batch is one vault call and one commit. Progress is published per batch on the hub as `sync.job.progress` with `{jobId, done, total, currentId}` alongside the engine's own `queued|started|done|failed` events, and the frontend bridges them into TanStack Query exactly as `useBacklogEvents` (`web/src/features/backlog/queries.ts:130-148`) already does for writes.

Attachments are downloaded when requested: `GET /api/issues/{id}/attachments`, then the signed relative `attachment.url` resolved against the instance base URL with the Bearer header still sent and redirects followed, streamed to `.pmngr/attachments/<ITEM-ID>/<name>.<pid>.<ts>.part` and renamed into place so a partial download never looks complete, skipping a file that already exists at the right size. The item's `attachments[]` front matter lists the resulting filenames. Cancellation must be observed between batches and mid-download, and a cancelled job leaves the batches already committed intact and reports how far it got.

## Acceptance Criteria

- [ ] A `youtrack.import` `Kind` and `Handler` are registered with `internal/syncengine` and enqueued through `Engine.Enqueue`.
- [ ] Issue resolution pages with `$top`/`$skip` and always appends `order by: created asc`; comments and attachments are paged explicitly.
- [ ] Issues are processed in configurable batches (default 20), each batch one `youtrack.import.run` call and one commit.
- [ ] `sync.job.progress` carries `{jobId, done, total, currentId}` and per-issue failures accumulate into the job result instead of failing the job.
- [ ] Attachment download streams to a `.part` file and renames on success; an existing file of the right size is skipped.
- [ ] Cancel stops the job between batches and mid-download, keeps committed batches and reports progress at the point of cancellation.
- [ ] `go test -race ./internal/server/...` covers batching, progress emission and cancellation with a fake clock and a fake client.

## Notes

Depends on GIT-EP-0015 for `Engine`, `Job`, `Kind`, `Handler`, retry/backoff, the shared rate limiter and the journal, and on GIT-EP-0011 for the client. Do not re-plan the engine here — this story only adds a job kind.

Stable sort is not optional: without `order by:` a `$skip` walk over a live instance silently skips and duplicates issues (scratchpad YouTrack report §3.3). Use `context.WithoutCancel(ctx)` when handing work to the engine, as `internal/gitops/committer.go:167` and `internal/server/tunnel.go:198` do, so a closing HTTP response cannot cancel the work it started.

Do NOT invent a second queue or a per-import goroutine pool; do NOT write job state anywhere but the engine's journal in `config.CacheDir`.
