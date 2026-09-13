---
id: GIT-T-0180
type: task
title: Add the sync engine flags to gintrack serve
status: done
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:19:39Z
updated: 2026-09-13T16:02:52Z
started: 2026-09-13T15:16:08Z
closed: 2026-09-13T16:02:52Z
---

## Description

Add `--sync-workers`, `--sync-batch`, `--sync-rate` and `--sync-max-attempts` to `gintrack serve`, wired through `config.Resolve` and `applyFlags` (`internal/config/load.go:146`, `:224`) so the documented flag > env > file > default precedence holds and an invalid value fails before the server binds a port.

## Acceptance Criteria

- [x] The four flags exist, are validated and reach the engine.
- [x] Flag > env > file > default precedence is covered by a test.
- [x] An invalid value exits non-zero before the listener is opened.
- [x] `go test -race ./cmd/...` passes.

## Notes

The file layer is in. `syncEngineSettings` in `cmd/gintrack/serve.go` now falls
back to `cfg.Sync.Engine` — itself already defaulted by `config.Default()` and
already carrying the `GINTRACK_SYNC_*` layer through `config.applySyncEnv` —
whenever the flag was not typed and the variable is not set, so the chain is
flag > environment > file > default in full.

`config.Flags` gained `SyncWorkers`, `SyncBatch`, `SyncRate` and
`SyncMaxAttempts` and `applyFlags` folds them in, so a command other than
`serve` that resolves configuration sees the same precedence; `serve` keeps its
own resolution because it is the only command that has to validate before a
listener opens, and `TestSyncEngineSettingsPrecedence` now covers all four
layers as a table.
