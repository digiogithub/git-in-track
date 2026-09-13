---
id: GIT-T-0172
type: task
title: Add the gintrack sprint command tree
status: todo
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [cli]
estimate: 4
created: 2026-09-13T13:19:28Z
updated: 2026-09-13T13:19:28Z
---

## Description

Create `cmd/gintrack/sprint.go` with `newSprintCommand(flags *globalFlags)` and the subcommands `list`, `show`, `start`, `close` and `transfer`, registered in `cmd/gintrack/root.go:85-100`. Follow the shape of `cmd/gintrack/item.go:21-41`: arg validators from `cmd/gintrack/exit.go:86-106`, `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`, output via `cmd/gintrack/output/output.go` with human notes on stderr, pure formatters in `cmd/gintrack/format.go`. No business logic in the command file.

## Acceptance Criteria

- [ ] All five subcommands work with the documented flags (`--status`, `--board`, `--json`, `--force`, `--transfer`, `--target`, `--to`).
- [ ] `sprint list` groups by derived status, and JSON output carries the derived status and the snapshot when present.
- [ ] Bad invocations exit 2 and operational failures use the shared exit codes.
- [ ] `go test -race ./cmd/gintrack/...` covers the tree with the in-process harness.
