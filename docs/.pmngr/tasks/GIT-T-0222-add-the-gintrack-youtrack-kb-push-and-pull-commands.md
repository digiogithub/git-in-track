---
id: GIT-T-0222
type: task
title: Add the gintrack youtrack kb push and pull commands
status: todo
priority: medium
parent: GIT-US-0094
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 3
created: 2026-09-13T13:21:54Z
updated: 2026-09-13T13:21:54Z
---

## Description

Add a `kb` subcommand group under the `youtrack` command in `cmd/gintrack/youtrack.go` with `push <path>` and `pull <path>`, each taking `--recursive`, `--wait` and `--json`. Without `--wait` the command prints the job id; with `--wait` it follows the job and prints a table of page, action and article id, and exits non-zero when any page failed or conflicted so a CI step can fail on it.

## Acceptance Criteria

- [ ] Both subcommands work with all three flags and validate args through `cmd/gintrack/exit.go`.
- [ ] `--wait` returns a non-zero exit code on any failure or conflict.
- [ ] `--json` emits the raw result with human lines on stderr.
- [ ] `go test -race ./cmd/gintrack/...` covers both commands through the in-process harness.
