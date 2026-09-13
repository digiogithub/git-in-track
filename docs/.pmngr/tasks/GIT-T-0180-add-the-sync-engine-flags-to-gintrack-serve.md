---
id: GIT-T-0180
type: task
title: Add the sync engine flags to gintrack serve
status: in_review
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:19:39Z
updated: 2026-09-13T15:16:40Z
started: 2026-09-13T15:16:08Z
---

## Description

Add `--sync-workers`, `--sync-batch`, `--sync-rate` and `--sync-max-attempts` to `gintrack serve`, wired through `config.Resolve` and `applyFlags` (`internal/config/load.go:146`, `:224`) so the documented flag > env > file > default precedence holds and an invalid value fails before the server binds a port.

## Acceptance Criteria

- [x] The four flags exist, are validated and reach the engine.
- [ ] Flag > env > file > default precedence is covered by a test.
- [x] An invalid value exits non-zero before the listener is opened.
- [x] `go test -race ./cmd/...` passes.

## Notes

The four flags are declared on `newServeCommand` and resolved by
`syncEngineSettings` in `cmd/gintrack/serve.go`, which also validates the result
and returns before `server.New` is called — so `--sync-workers 0` fails the
command with a non-zero exit and nothing ever listens. The journal directory
comes from `index.cacheDir`, which the configuration file does already declare.

The precedence criterion is **partly** met and left unticked for that reason:
`flag > env > default` is implemented and table-tested
(`TestSyncEngineSettingsPrecedence`), with `GINTRACK_SYNC_WORKERS`,
`GINTRACK_SYNC_BATCH`, `GINTRACK_SYNC_RATE` and `GINTRACK_SYNC_MAX_ATTEMPTS`.
The **file** layer is missing because it does not exist: `internal/config` has no
`sync.engine` section and belongs to another agent this wave, so nothing was
wired through `applyFlags`. Adding the section makes this a two-line change in
`syncEngineSettings`. docs/07 §4.1 states the gap.
