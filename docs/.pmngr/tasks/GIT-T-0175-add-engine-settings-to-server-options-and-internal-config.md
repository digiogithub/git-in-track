---
id: GIT-T-0175
type: task
title: Add engine settings to server.Options and internal/config
status: done
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:31Z
updated: 2026-09-13T16:02:45Z
started: 2026-09-13T15:16:00Z
closed: 2026-09-13T16:02:45Z
---

## Description

Add a `SyncEngine` configuration struct to `internal/config/config.go` with workers, batch size, rate limit, max attempts and journal retention, validated in `internal/config/validate.go:53`, and surface it on `server.Options` (`internal/server/server.go:59-131`) next to `Git config.Git` and `Tunnel config.Tunnel`. Defaults are 2 workers, batch 20, 5 req/s, 5 attempts and 7 days.

## Acceptance Criteria

- [x] The struct exists in both places with the documented defaults and range validation.
- [x] An out-of-range value fails config validation with a message naming the key.
- [x] `go test -race ./internal/config/... ./internal/server/...` passes.

## Notes

Both halves now exist. `config.SyncEngine` is the `sync.engine` section of the
configuration file — workers, batchSize, rate, maxAttempts and retention — with
`config.DefaultSyncEngine()` taking its values **from `internal/syncengine`'s own
constants** rather than restating them, so the file, the flags and the engine
cannot drift. `Config.validateSyncEngine` refuses an out-of-range value with the
dotted key that carries it (`sync.engine.workers: 0 is outside the range 1-64`),
against the same ranges the running engine applies, so a value the file accepts
can never be refused later by `PATCH /api/v1/sync/settings`.

`server.SyncEngine` stays where it is and keeps the three fields only a running
process knows — `CacheDir`, `DrainTimeout`, `Debounce`. `SyncEngineFrom` converts
the file section into it and `section()` converts it back, which is what
`syncState.persist` now writes: reload, replace one section, save, report. So
`PATCH /api/v1/sync/settings` answers `persisted: true` whenever the companion
has a configuration file.

`retention` deliberately has no flag and no environment variable: it is
housekeeping, not something an operator tunes per run.
