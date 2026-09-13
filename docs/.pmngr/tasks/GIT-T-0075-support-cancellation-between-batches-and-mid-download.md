---
id: GIT-T-0075
type: task
title: Support cancellation between batches and mid-download
status: todo
priority: medium
parent: GIT-US-0050
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:10Z
updated: 2026-09-13T13:17:10Z
---

## Description

Observe the job context between batches and inside the attachment download loop so `Engine.Cancel` takes effect promptly. A cancelled import leaves already-committed batches intact, removes any in-flight `.part` file, and reports how many issues were processed before the cancellation as the job result. Cancellation is a terminal state, not a failure, and must not be retried by the backoff loop.

## Acceptance Criteria

- [ ] Cancel stops the job between batches and mid-download within one batch.
- [ ] Committed batches survive and in-flight temp files are removed.
- [ ] The job ends in `cancelled` with a progress count and is not retried.
- [ ] `go test -race ./internal/server/...` covers cancellation at both observation points.
