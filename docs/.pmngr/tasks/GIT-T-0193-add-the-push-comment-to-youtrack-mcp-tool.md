---
id: GIT-T-0193
type: task
title: Add the push_comment_to_youtrack MCP tool
status: todo
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 2
created: 2026-09-13T13:20:01Z
updated: 2026-09-13T13:20:01Z
---

## Description

Register `push_comment_to_youtrack` in `internal/mcp/tools_youtrack.go` as a write tool with `In` `{project, itemId, commentPath?, all?}`, validating with `invalidField`, checking any path with `s.guard.Check`, dispatching the vault operation and announcing the write. Update the surface-pinning tests in `internal/mcp/tools_test.go` and `cmd/gintrack/mcp_test.go` and the tool list in `AGENTS.md`.

## Acceptance Criteria

- [ ] The tool is registered as a write tool and hidden on a read-only server.
- [ ] Path arguments pass through the path guard.
- [ ] Surface-pinning tests and `AGENTS.md` are updated.
- [ ] `go test -race ./internal/mcp/...` passes with a behaviour test through the client harness.
