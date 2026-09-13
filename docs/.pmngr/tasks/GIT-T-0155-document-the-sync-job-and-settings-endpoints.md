---
id: GIT-T-0155
type: task
title: Document the sync job and settings endpoints
status: done
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:19:03Z
updated: 2026-09-13T15:13:55Z
started: 2026-09-13T15:13:16Z
closed: 2026-09-13T15:13:55Z
---

## Description

Document the six endpoints in `docs/07-cli-and-api.md` beside the existing sync section, with request and response examples, the problem codes, the valid state transitions for retry and cancel, and the `persisted` semantics. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Every endpoint has parameters, an example response and an error table.
- [x] The state-transition rules for retry and cancel are documented.
- [x] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

`docs/07-cli-and-api.md` §5.5, "Background jobs and the engine settings": the six
routes in the reference list, a parameter table for the listing, worked examples
for the listing, the single read, retry, cancel and both settings verbs, the
allowed-transition table and the `sync_job_not_found` /
`sync_job_not_retryable` / `sync_engine_not_running` error table. The
`persisted` semantics are stated, including that they are `false` for the engine
half today because the configuration file has no `sync.engine` section yet.
