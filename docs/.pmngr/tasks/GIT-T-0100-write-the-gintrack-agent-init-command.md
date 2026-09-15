---
id: GIT-T-0100
type: task
title: Write the gintrack agent init command
status: in_progress
priority: medium
parent: GIT-US-0069
milestone: GIT-M-0013
author: mcp
labels: [cli]
estimate: 3
created: 2026-09-13T13:17:47Z
updated: 2026-09-15T15:18:54Z
started: 2026-09-15T15:18:54Z
---

## Description

Add `cmd/gintrack/agent.go` with an `agent` command and an `init` subcommand, following the thin-cobra convention of the other commands. It writes a `.pando.toml` from an embedded template with `[AGUI]`, `[MCPServers.gintrack]` (streamable HTTP to the companion `/mcp` endpoint) and `[Remembrances]` (corpus path, auto-import, watch), taking the companion URL and the corpus directory from the resolved config. Existing files are left untouched unless `--force` is passed, and no token value is ever written into a file inside the repository — the template references the environment instead.

## Acceptance Criteria

- [ ] `gintrack agent init` writes the file into a temp directory and prints the next step.
- [ ] Re-running without `--force` exits non-zero, changes nothing and says which file exists.
- [ ] No secret appears in the generated file.
- [ ] `go test -race ./cmd/gintrack/...` covers generation, `--force` and the refusal.
