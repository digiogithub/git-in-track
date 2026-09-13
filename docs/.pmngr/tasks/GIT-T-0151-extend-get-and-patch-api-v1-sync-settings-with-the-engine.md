---
id: GIT-T-0151
type: task
title: Extend GET and PATCH /api/v1/sync/settings with the engine knobs
status: done
priority: medium
parent: GIT-US-0078
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:00Z
updated: 2026-09-13T15:13:54Z
started: 2026-09-13T15:13:14Z
closed: 2026-09-13T15:13:54Z
---

## Description

Extend the existing settings handler (`internal/server/sync.go:379`) with workers, batch size, rate limit and retry policy, validating ranges and applying the change to the running engine, not only to the stored config. Persist with the reload-mutate-save pattern of `gitState.persist` (`internal/server/git.go:336-352`) and return `persisted bool` as `handleGitSettingsPatch` does (`:374-393`).

## Acceptance Criteria

- [x] All four knobs are readable and writable, with out-of-range values rejected by a problem document.
- [x] A PATCH changes the behaviour of the running engine without a restart, proved by a test.
- [x] `persisted` is false when the server has no config path, matching the git settings contract.
- [ ] `go test -race ./internal/server/...` covers validation, live application and persistence.

## Notes

`GET /api/v1/sync/settings` is new (only `PATCH` existed) and answers both halves
of the sync configuration in one document. `workers`, `batchSize` and `rate`
reach the **running** engine through `SetWorkers`, `SetBatchSize` and `SetRate`;
the test asserts it against `Engine.Snapshot()`, not against the response.
`maxAttempts` is fixed when the engine is built — the engine exposes no setter —
so it is recorded and applies from the next start, which docs/07 §5.5 states.

The last criterion is unticked on purpose: **persistence is not covered because
it is not implemented**. The configuration file has no `sync.engine` section
(that is the `internal/config` half of GIT-T-0175, which belongs to another
owner), so `syncState.persist` writes nothing and `persisted` is always false.
The reload-mutate-save seam is in place for whoever adds the section.
