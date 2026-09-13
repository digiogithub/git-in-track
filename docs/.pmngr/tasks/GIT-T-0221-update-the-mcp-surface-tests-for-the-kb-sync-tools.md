---
id: GIT-T-0221
type: task
title: Update the MCP surface tests for the KB sync tools
status: todo
priority: medium
parent: GIT-US-0094
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 1
created: 2026-09-13T13:21:50Z
updated: 2026-09-13T13:21:50Z
---

## Description

Update the pinned tool lists in `internal/mcp/tools_test.go:16-26` and `cmd/gintrack/mcp_test.go:28,37-40`, the long help in `cmd/gintrack/mcp.go:36-38` and the tool list in `AGENTS.md` to include both KB sync tools and the corrected counts.

## Acceptance Criteria

- [ ] The pinned lists and counts include both tools and the tests pass.
- [ ] `gintrack mcp --list-tools` reports the correct totals.
- [ ] `AGENTS.md` lists both tools; `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes.
