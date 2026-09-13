---
id: GIT-T-0045
type: task
title: Add the three inbox MCP tools and update the surface tests
status: todo
priority: medium
parent: GIT-US-0056
milestone: GIT-M-0012
author: mcp
labels: [mcp, agent-ok]
estimate: 3
created: 2026-09-13T13:16:26Z
updated: 2026-09-13T13:16:26Z
---

## Description

Create `internal/mcp/tools_inbox.go` with `registerInboxTools(s)` defining `create_inbox_item` (write), `list_inbox` (read, `Untrusted: true`) and `triage_inbox_item` (write, `Idempotent: false`), each a `register(s, toolDef{...}, handler)` call added from `registerTools` (`internal/mcp/tools.go:24`). Handlers validate with `invalidField`, call `requiredRev` before any write, dispatch through `dispatch[T]`, project results through `internal/mcp/wire.go`, and `s.announce` on write. No business logic in this package. Then update the surface-pinning tests: `internal/mcp/tools_test.go:16-26` and `cmd/gintrack/mcp_test.go:28-40`.

## Acceptance Criteria

- [ ] The three tools appear with correct read/write annotations, and the two write tools are not advertised at all on a read-only server.
- [ ] Every returned item carries `id` and `rev`; `list_inbox` carries the untrusted-content note and meta key.
- [ ] `TestToolSurface` and the CLI tool-list tests are updated and pass.
- [ ] A behaviour test through the in-memory client harness (`internal/mcp/mcp_test.go:57`) covers create, list and each triage action.
