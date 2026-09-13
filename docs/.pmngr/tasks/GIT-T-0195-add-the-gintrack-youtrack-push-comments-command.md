---
id: GIT-T-0195
type: task
title: Add the gintrack youtrack push-comments command
status: todo
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:20:06Z
updated: 2026-09-13T13:20:06Z
---

## Description

Add `push-comments <item>` under the `youtrack` command in `cmd/gintrack/youtrack.go` with `--all`, `--comment <path>`, `--wait` and `--json`. Without `--wait` it prints the job id; with `--wait` it follows the job and prints a table of comment, remote id and result. Exit codes and arg validation come from `cmd/gintrack/exit.go`.

## Acceptance Criteria

- [ ] The subcommand works with all four flags and validates args through the shared helpers.
- [ ] `--wait` prints the result table and returns a non-zero exit code when any comment failed.
- [ ] `--json` emits the raw result with human lines on stderr.
- [ ] `go test -race ./cmd/gintrack/...` covers it through the in-process harness.
