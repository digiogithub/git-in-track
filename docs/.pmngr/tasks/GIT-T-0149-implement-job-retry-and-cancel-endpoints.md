---
id: GIT-T-0149
type: task
title: Implement job retry and cancel endpoints
status: todo
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:18:55Z
updated: 2026-09-13T13:18:55Z
---

## Description

Add `POST /api/v1/sync/jobs/{id}/retry`, which re-queues a failed or cancelled job with its attempt count reset, and `POST /api/v1/sync/jobs/{id}/cancel`, which works on queued and running jobs. A job in a state that cannot make the transition returns a problem document naming the current state rather than silently succeeding.

## Acceptance Criteria

- [ ] Retry works only from `failed` and `cancelled`; cancel works only from `queued` and `running`.
- [ ] An invalid transition returns a problem document naming the current state.
- [ ] Both actions emit the corresponding `sync.job.*` event.
- [ ] `go test -race ./internal/server/...` covers valid and invalid transitions.
