---
id: GIT-T-0045
type: task
title: Add the three inbox MCP tools and update the surface tests
status: done
priority: medium
parent: GIT-US-0056
milestone: GIT-M-0012
author: mcp
labels: [mcp, agent-ok]
estimate: 3
created: 2026-09-13T13:16:26Z
updated: 2026-09-13T14:40:55Z
started: 2026-09-13T14:40:07Z
closed: 2026-09-13T14:40:55Z
---

## Description

Create `internal/mcp/tools_inbox.go` with `registerInboxTools(s)` defining `create_inbox_item` (write), `list_inbox` (read, `Untrusted: true`) and `triage_inbox_item` (write, `Idempotent: false`), each a `register(s, toolDef{...}, handler)` call added from `registerTools` (`internal/mcp/tools.go:24`). Handlers validate with `invalidField`, call `requiredRev` before any write, dispatch through `dispatch[T]`, project results through `internal/mcp/wire.go`, and `s.announce` on write. No business logic in this package. Then update the surface-pinning tests: `internal/mcp/tools_test.go:16-26` and `cmd/gintrack/mcp_test.go:28-40`.

## Acceptance Criteria

- [x] The three tools appear with correct read/write annotations, and the two write tools are not advertised at all on a read-only server.
- [x] Every returned item carries `id` and `rev`; `list_inbox` carries the untrusted-content note and meta key.
- [ ] `TestToolSurface` and the CLI tool-list tests are updated and pass.
- [x] A behaviour test through the in-memory client harness (`internal/mcp/mcp_test.go:57`) covers create, list and each triage action.

## Notes

`TestToolSurface` (`internal/mcp/tools_test.go`) is updated and passes. The third criterion
stays unticked because the tool-count assertions outside `internal/mcp` were not this agent's
to edit this wave: `cmd/gintrack/mcp_test.go:157`, `internal/server/mcp_test.go:130` and
`internal/server/mcp_test.go:148` still pin thirteen tools and now read eighteen. The fix is
the number in those three lines. `AGENTS.md` ("6 tools read-only, 13 with `--allow-write`")
now reads seven and eighteen and needs the same one-line correction.

The tests run against a new fixture, `internal/mcp/testdata/project-inbox/`, because the
shared `project-basic` fixture deliberately declares no triage status and is read by
`internal/core` and `internal/server` tests that would have to change with it. `newHarness`
gained `newHarnessWith(t, allowWrite, mounts)`; its own behaviour is unchanged.
