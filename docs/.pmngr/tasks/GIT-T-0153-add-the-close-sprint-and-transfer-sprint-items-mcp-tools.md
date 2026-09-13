---
id: GIT-T-0153
type: task
title: Add the close_sprint and transfer_sprint_items MCP tools
status: done
priority: medium
parent: GIT-US-0085
milestone: GIT-M-0012
author: mcp
labels: [mcp, agent-ok]
estimate: 3
created: 2026-09-13T13:19:00Z
updated: 2026-09-13T14:41:22Z
started: 2026-09-13T14:40:11Z
closed: 2026-09-13T14:41:22Z
---

## Description

Create `internal/mcp/tools_sprints.go` with `registerSprintTools(s)` defining `close_sprint` and `transfer_sprint_items`, both write tools, registered from `registerTools` (`internal/mcp/tools.go:24`). Handlers validate with `invalidField`, call `requiredRev` before writing, dispatch through `dispatch[T]`, project the report through `internal/mcp/wire.go` and `s.announce` on write. Default `dryRun` to true in the tool description's guidance so an agent is nudged to look before it moves work. Update `internal/mcp/tools_test.go:16-26` and `cmd/gintrack/mcp_test.go:28-40`.

## Acceptance Criteria

- [x] Both tools appear with write annotations and are hidden on a read-only server.
- [x] The returned report carries per-outcome counts, the target and every refusal.
- [ ] The surface-pinning tests are updated and pass.
- [x] A behaviour test through the in-memory client harness covers a dry run and a real transfer.

## Notes

`TestToolSurface` in `internal/mcp` is updated and passes. The third criterion stays unticked
for the same reason as `GIT-T-0045`: `cmd/gintrack/mcp_test.go:157`,
`internal/server/mcp_test.go:130` and `:148` still pin thirteen tools and now read eighteen,
and those files belong to other agents this wave.

Both descriptions tell the model to call with `dryRun: true` first and show the counts before
repeating with `false`; the field itself defaults to `false`, because a tool whose default is
"do nothing" makes an honest caller pay an extra round trip for every real write. A dry run is
never announced through `AfterWrite` — nothing changed, so there is nothing to commit or
publish.

The report is compact on purpose: counts, the destination and one line per decision, rather
than the whole `SprintCloseReport` with its board cards, which an agent pays for by the token
and can read from `get_item` when it needs it.
