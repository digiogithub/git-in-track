---
id: GIT-T-0153
type: task
title: Add the close_sprint and transfer_sprint_items MCP tools
status: todo
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [mcp, agent-ok]
estimate: 3
created: 2026-09-13T13:19:00Z
updated: 2026-09-13T13:19:00Z
---

## Description

Create `internal/mcp/tools_sprints.go` with `registerSprintTools(s)` defining `close_sprint` and `transfer_sprint_items`, both write tools, registered from `registerTools` (`internal/mcp/tools.go:24`). Handlers validate with `invalidField`, call `requiredRev` before writing, dispatch through `dispatch[T]`, project the report through `internal/mcp/wire.go` and `s.announce` on write. Default `dryRun` to true in the tool description's guidance so an agent is nudged to look before it moves work. Update `internal/mcp/tools_test.go:16-26` and `cmd/gintrack/mcp_test.go:28-40`.

## Acceptance Criteria

- [ ] Both tools appear with write annotations and are hidden on a read-only server.
- [ ] The returned report carries per-outcome counts, the target and every refusal.
- [ ] The surface-pinning tests are updated and pass.
- [ ] A behaviour test through the in-memory client harness covers a dry run and a real transfer.
