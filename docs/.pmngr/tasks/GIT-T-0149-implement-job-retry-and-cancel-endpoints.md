---
id: GIT-T-0149
type: task
title: Implement job retry and cancel endpoints
status: done
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:18:55Z
updated: 2026-09-13T15:13:52Z
started: 2026-09-13T15:13:08Z
closed: 2026-09-13T15:13:52Z
---

## Description

Add `POST /api/v1/sync/jobs/{id}/retry`, which re-queues a failed or cancelled job with its attempt count reset, and `POST /api/v1/sync/jobs/{id}/cancel`, which works on queued and running jobs. A job in a state that cannot make the transition returns a problem document naming the current state rather than silently succeeding.

## Acceptance Criteria

- [x] Retry works only from `failed` and `cancelled`; cancel works only from `queued` and `running`.
- [x] An invalid transition returns a problem document naming the current state.
- [x] Both actions emit the corresponding `sync.job.*` event.
- [x] `go test -race ./internal/server/...` covers valid and invalid transitions.

## Notes

A **failed** job is re-queued in place through `Engine.RetryDeadLetter`: same id,
same history, fresh attempt budget. A **cancelled** job cannot be — the engine's
state machine has no edge out of `cancelled`, by design — so it is re-queued as a
new job with the same kind, key and payload and the answer carries the new id.
This is documented in docs/07 §5.5. An invalid transition answers
`sync_job_not_retryable` (409) whose detail names the current state.
