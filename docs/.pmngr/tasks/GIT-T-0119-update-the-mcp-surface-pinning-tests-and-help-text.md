---
id: GIT-T-0119
type: task
title: Update the MCP surface-pinning tests and help text
status: todo
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 1
created: 2026-09-13T13:18:14Z
updated: 2026-09-13T13:18:14Z
---

## Description

Update the surface-pinning tests that enumerate the tool set — `internal/mcp/tools_test.go:16` (`readTools`), `:21` (`writeTools`), `TestToolSurface` :26 — and `cmd/gintrack/mcp_test.go:28,37-40`, plus the long help in `cmd/gintrack/mcp.go:36-38` and the tool list in `AGENTS.md`. Without this the build fails on the pinned surface.

## Acceptance Criteria

- [ ] The pinned read and write tool lists include the new tool and the tests pass.
- [ ] `gintrack mcp --list-tools` and its long help report the correct counts.
- [ ] `AGENTS.md` lists the new tool; `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes.
