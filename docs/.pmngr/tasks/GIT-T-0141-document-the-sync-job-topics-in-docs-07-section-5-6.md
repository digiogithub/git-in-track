---
id: GIT-T-0141
type: task
title: Document the sync.job.* topics in docs/07 section 5.6
status: done
priority: medium
parent: GIT-US-0074
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:18:44Z
updated: 2026-09-13T15:15:11Z
started: 2026-09-13T15:14:36Z
closed: 2026-09-13T15:15:11Z
---

## Description

Document the five `sync.job.*` topics in `docs/07-cli-and-api.md` §5.6 beside `sync.progress` and `git.commit`, with a payload example for each, a note on progress coalescing and a note that clients may miss frames on overflow and should reconcile from `GET /api/v1/sync/jobs`. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Each topic is documented with an example payload.
- [x] The overflow and reconciliation behaviour is stated.
- [x] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

`docs/07-cli-and-api.md` §5.6, between `conflict.resolved` and `tunnel.changed`:
the five topics with their shared payload shape and three worked examples
(`queued`, `progress`, `failed`), the coalescing rule, the statement that a
terminal event is never throttled, and the paragraph telling clients that the
stream is a live hint and never the source of truth — after a `stream.overflow`,
a `resume.gap` or any reconnect they reconcile from `GET /api/v1/sync/jobs`.
`inbox.changed` and `sprint.changed` were documented in the same pass.
