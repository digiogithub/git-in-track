---
id: GIT-T-0071
type: task
title: Add the gintrack inbox command tree
status: todo
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [cli]
estimate: 4
created: 2026-09-13T13:17:03Z
updated: 2026-09-13T13:17:03Z
---

## Description

Create `cmd/gintrack/inbox.go` with `newInboxCommand(flags *globalFlags)` and the subcommands `list`, `add`, `accept`, `reject` and `snooze`, registered in `cmd/gintrack/root.go:85-100`. Follow the shape of `cmd/gintrack/item.go:21-41`: the arg validators from `cmd/gintrack/exit.go:86-106`, `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`, table output through `cmd/gintrack/output/output.go` with human notes on stderr, and pure formatters in `cmd/gintrack/format.go`. No business logic in the command file.

## Acceptance Criteria

- [ ] All five subcommands work, support `--json` where a listing is produced, and use the shared exit codes (2 for a bad invocation).
- [ ] `inbox list --status` filters and a snoozed item whose date has passed shows under `pending`.
- [ ] `go test -race ./cmd/gintrack/...` covers the tree with the in-process harness (`harness_test.go:28`).
- [ ] `docs/07-cli-and-api.md` §4 documents the command tree.
