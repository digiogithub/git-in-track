---
id: GIT-T-0069
type: task
title: Add the gintrack youtrack cobra command skeleton
status: todo
priority: medium
parent: GIT-US-0058
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:17:02Z
updated: 2026-09-13T13:17:02Z
---

## Description

Add `cmd/gintrack/youtrack.go` with a `youtrack` parent command and `connect` and `status` subcommands, registered with the root command alongside the existing ones. Keep the file thin: flag definitions, argument validation and calls into `internal/config` and `internal/youtrack`. Add a `--json` flag to both subcommands.

## Acceptance Criteria

- [ ] `gintrack youtrack --help`, `connect --help` and `status --help` print English help consistent with the other commands.
- [ ] Flags are defined and validated; missing required arguments exit non-zero with a useful message.
- [ ] `go test -race ./cmd/...` covers argument parsing.
