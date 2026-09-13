---
id: GIT-T-0123
type: task
title: Add the gintrack youtrack import command
status: done
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 3
created: 2026-09-13T13:18:20Z
updated: 2026-09-13T16:39:27Z
started: 2026-09-13T16:39:08Z
closed: 2026-09-13T16:39:27Z
---

## Description

Create `cmd/gintrack/youtrack.go` with a `youtrack` parent command and an `import <query|ids...>` subcommand, registered in `root.go:85-100`, using the arg validators from `cmd/gintrack/exit.go` so a bad invocation exits 2. Flags `--depth`, `--comments`, `--attachments`, `--links`, `--dry-run` and `--json`. `RunE` resolves config with `flags.resolve()`, builds a printer with `flags.printer(cmd, asJSON)` and opens the vault with `openVault(...)`; the table lists issue, action and target id, and human lines go to stderr in JSON mode. No business logic in the command file.

## Acceptance Criteria

- [x] The command and its flags work and a bad invocation exits 2 through the shared validators.
- [x] Table output lists issue, action and item id; `--json` emits the raw result with notes on stderr.
- [ ] A long import is polled rather than blocked on, and an interrupt is honoured.
- [x] `go test -race ./cmd/gintrack/...` covers the command through the in-process harness.

## Notes

Landed in `cmd/gintrack/youtracksync.go`, extending the existing `youtrack` parent command; `root.go` needed no change. One argument that is not a readable issue id is a query, one or more issue ids are an id list — `issueIDArgs` decides, and the vault refuses "both" with a field-level `invalid_request` the command only renders. `minArgs` was added to `exit.go` beside `exactArgs` and `rangeArgs`, because `cobra.MinimumNArgs` exits 1.

The third criterion is deliberately unticked. `youtrack.import.run` is a synchronous vault method — it is the REST layer, not the vault, that queues an import — so there is no job for the command to poll. It runs the import in-process in a companion that never binds a port, and an interrupt cancels the context the import is running under. Polling would require the command to queue through the engine instead of dispatching the method, which would make it the only surface that imports differently from the MCP tool.
