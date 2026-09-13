---
id: GIT-T-0175
type: task
title: Add engine settings to server.Options and internal/config
status: in_review
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:31Z
updated: 2026-09-13T15:16:22Z
started: 2026-09-13T15:16:00Z
---

## Description

Add a `SyncEngine` configuration struct to `internal/config/config.go` with workers, batch size, rate limit, max attempts and journal retention, validated in `internal/config/validate.go:53`, and surface it on `server.Options` (`internal/server/server.go:59-131`) next to `Git config.Git` and `Tunnel config.Tunnel`. Defaults are 2 workers, batch 20, 5 req/s, 5 attempts and 7 days.

## Acceptance Criteria

- [ ] The struct exists in both places with the documented defaults and range validation.
- [x] An out-of-range value fails config validation with a message naming the key.
- [x] `go test -race ./internal/config/... ./internal/server/...` passes.

## Notes

**Half done, and the missing half is not this agent's to write.** The struct is
`server.SyncEngine` in `internal/server/syncengine.go`, carried on
`Options.SyncEngine` next to `Git` and `Tunnel`, with `withDefaults` (2 workers,
batch 20, 5 req/s, 5 attempts, 7 days retention, 5 s drain) and a `Validate`
that refuses an out-of-range value with a `settingsError` naming the field —
which is what both `server.New` and `PATCH /api/v1/sync/settings` report.

`internal/config` was **not touched**: it belongs to another agent this wave.
So there is no `sync.engine` section in `config.yaml`, nothing to validate in
`internal/config/validate.go`, and two consequences elsewhere:
`PATCH /api/v1/sync/settings` always answers `persisted: false` (GIT-T-0151), and
`gintrack serve` resolves the four settings flag > env > default with no file
layer (GIT-T-0180). Both are documented in docs/07 §4.1 and §5.5.

Moving the struct into `internal/config` later is a rename: the field names and
the defaults are already the ones that section should take.
