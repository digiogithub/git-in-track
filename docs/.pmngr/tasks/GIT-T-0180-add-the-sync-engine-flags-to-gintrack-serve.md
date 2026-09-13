---
id: GIT-T-0180
type: task
title: Add the sync engine flags to gintrack serve
status: todo
priority: medium
parent: GIT-US-0084
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:19:39Z
updated: 2026-09-13T13:19:39Z
---

## Description

Add `--sync-workers`, `--sync-batch`, `--sync-rate` and `--sync-max-attempts` to `gintrack serve`, wired through `config.Resolve` and `applyFlags` (`internal/config/load.go:146`, `:224`) so the documented flag > env > file > default precedence holds and an invalid value fails before the server binds a port.

## Acceptance Criteria

- [ ] The four flags exist, are validated and reach the engine.
- [ ] Flag > env > file > default precedence is covered by a test.
- [ ] An invalid value exits non-zero before the listener is opened.
- [ ] `go test -race ./cmd/...` passes.
