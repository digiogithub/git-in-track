---
id: GIT-T-0069
type: task
title: Add the gintrack youtrack cobra command skeleton
status: done
priority: medium
parent: GIT-US-0058
milestone: GIT-M-0011
author: mcp
labels: [cli, agent-ok]
estimate: 2
created: 2026-09-13T13:17:02Z
updated: 2026-09-13T14:43:28Z
started: 2026-09-13T14:42:55Z
closed: 2026-09-13T14:43:28Z
---

## Description

Add `cmd/gintrack/youtrack.go` with a `youtrack` parent command and `connect` and `status` subcommands, registered with the root command alongside the existing ones. Keep the file thin: flag definitions, argument validation and calls into `internal/config` and `internal/youtrack`. Add a `--json` flag to both subcommands.

## Acceptance Criteria

- [x] `gintrack youtrack --help`, `connect --help` and `status --help` print English help consistent with the other commands.
- [x] Flags are defined and validated; missing required arguments exit non-zero with a useful message.
- [x] `go test -race ./cmd/...` covers argument parsing.

## Notes

`cmd/gintrack/youtrack.go`, registered in `newRootCommand`. `connect` takes `--url`, `--project`, `--project-key`, `--token`, `--push-comments`, `--kb-sync`, `--kb-sync-direction` and `--json`; `status` takes `--project-key`, `--offline` and `--json`. A missing `--url`, `--project` or token exits 2 with a message naming what to supply.
