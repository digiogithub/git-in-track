---
id: GIT-T-0123
type: task
title: Add the gintrack youtrack import command
status: todo
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 3
created: 2026-09-13T13:18:20Z
updated: 2026-09-13T13:18:20Z
---

## Description

Create `cmd/gintrack/youtrack.go` with a `youtrack` parent command and an `import <query|ids...>` subcommand, registered in `root.go:85-100`, using the arg validators from `cmd/gintrack/exit.go` so a bad invocation exits 2. Flags `--depth`, `--comments`, `--attachments`, `--links`, `--dry-run` and `--json`. `RunE` resolves config with `flags.resolve()`, builds a printer with `flags.printer(cmd, asJSON)` and opens the vault with `openVault(...)`; the table lists issue, action and target id, and human lines go to stderr in JSON mode. No business logic in the command file.

## Acceptance Criteria

- [ ] The command and its flags work and a bad invocation exits 2 through the shared validators.
- [ ] Table output lists issue, action and item id; `--json` emits the raw result with notes on stderr.
- [ ] A long import is polled rather than blocked on, and an interrupt is honoured.
- [ ] `go test -race ./cmd/gintrack/...` covers the command through the in-process harness.
