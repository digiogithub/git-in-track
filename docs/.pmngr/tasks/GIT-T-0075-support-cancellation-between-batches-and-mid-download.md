---
id: GIT-T-0075
type: task
title: Support cancellation between batches and mid-download
status: done
priority: medium
parent: GIT-US-0050
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:10Z
updated: 2026-09-13T15:59:08Z
started: 2026-09-13T15:58:44Z
closed: 2026-09-13T15:59:08Z
---

## Description

Observe the job context between batches and inside the attachment download loop so `Engine.Cancel` takes effect promptly. A cancelled import leaves already-committed batches intact, removes any in-flight `.part` file, and reports how many issues were processed before the cancellation as the job result. Cancellation is a terminal state, not a failure, and must not be retried by the backoff loop.

## Acceptance Criteria

- [x] Cancel stops the job between batches and mid-download within one batch.
- [x] Committed batches survive and in-flight temp files are removed.
- [x] The job ends in `cancelled` with a progress count and is not retried.
- [x] `go test -race ./internal/server/...` covers cancellation at both observation points.

## Notes

There are four observation points, not two: between jobs of a batch, between
import batches, between issues of an attachment pass, and between attachments.
Each is a plain `ctx.Err()` check whose error is returned **unwrapped**, which
is what makes `syncengine.Classify` read it as `cancelled` rather than as a
failure worth another attempt — wrapping it with `Terminal` would have been
wrong in the other direction, since a cancelled job is not a failed one.

`classifyYouTrackJobError` returns `context.Canceled` untouched for the same
reason.

The progress count is the last `sync.job.progress` frame the run published plus
a log line naming how far it got; the engine's own `sync.job.done` carries the
`cancelled` state.
