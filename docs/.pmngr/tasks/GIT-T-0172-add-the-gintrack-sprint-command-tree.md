---
id: GIT-T-0172
type: task
title: Add the gintrack sprint command tree
status: done
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [cli]
estimate: 4
created: 2026-09-13T13:19:28Z
updated: 2026-09-13T16:42:06Z
started: 2026-09-13T16:41:34Z
closed: 2026-09-13T16:42:06Z
---

## Description

Create `cmd/gintrack/sprint.go` with `newSprintCommand(flags *globalFlags)` and the subcommands `list`, `show`, `start`, `close` and `transfer`, registered in `cmd/gintrack/root.go:85-100`. Follow the shape of `cmd/gintrack/item.go:21-41`: arg validators from `cmd/gintrack/exit.go:86-106`, `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`, output via `cmd/gintrack/output/output.go` with human notes on stderr, pure formatters in `cmd/gintrack/format.go`. No business logic in the command file.

## Acceptance Criteria

- [x] All five subcommands work with the documented flags (`--status`, `--board`, `--json`, `--force`, `--transfer`, `--target`, `--to`).
- [x] `sprint list` groups by derived status, and JSON output carries the derived status and the snapshot when present.
- [x] Bad invocations exit 2 and operational failures use the shared exit codes.
- [x] `go test -race ./cmd/gintrack/...` covers the tree with the in-process harness.

## Notes

Landed in `cmd/gintrack/sprint.go`, dispatching `sprint.list|get|start|close|transfer` through the shared `openSpace`/`dispatch` seam in `inbox.go`. Every subcommand also takes `--team`. `list` groups current → upcoming → draft → completed on the **derived** status, and the `--json` payload carries both `state` (stored) and `status` (derived) plus `snapshot` once a sprint is closed. `docs/07` §4.16 documents the tree.

Like the existing `gintrack item`, a bare `gintrack sprint` prints help and exits 0; every genuinely bad invocation exits 2.
