---
id: GIT-T-0135
type: task
title: Coalesce progress events so slow clients do not overflow
status: done
priority: medium
parent: GIT-US-0074
milestone: GIT-M-0011
author: mcp
labels: [server, performance, agent-ok]
estimate: 2
created: 2026-09-13T13:18:36Z
updated: 2026-09-13T15:15:09Z
started: 2026-09-13T15:14:35Z
closed: 2026-09-13T15:15:09Z
---

## Description

Throttle `sync.job.progress` to a few events per second per job, coalescing intermediate counts, because hub clients hold a 256-event buffer that drops on overflow (`internal/server/hub.go:31`, `deliver` `:104`). A terminal event is always published even if a progress event was just suppressed, so the final state is never the one that gets dropped.

## Acceptance Criteria

- [x] A batch of a thousand items emits a bounded number of progress events.
- [x] `done` and `failed` are never suppressed by the throttle.
- [x] `go test -race ./internal/server/...` covers the throttle with a fake clock.

## Notes

The throttle is keyed on the **coalescing group** — the job's kind and key, which
is what the engine hands a handler as one batch — rather than on the job id: a
thousand jobs of one import share one group, so the bound holds however many
jobs there are. One progress event every 500 ms per group, carrying the running
`processed`/`total` of that group.

A terminal transition always publishes its own topic and is never throttled; the
progress event that accompanies it is. The test drives a thousand jobs through a
fake clock and asserts a thousand terminal events and a handful of progress
ones.

Worth stating for the reader: this bounds the *progress* narration, not every
`sync.job.*` frame. A queue with a thousand jobs still publishes one `queued`
and one terminal event per job, so a slow client can still overflow — which is
why docs/07 §5.6 tells clients to reconcile from `GET /api/v1/sync/jobs` after a
gap rather than trusting the stream.
