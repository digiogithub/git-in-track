---
id: GIT-T-0221
type: task
title: Update the MCP surface tests for the KB sync tools
status: in_review
priority: medium
parent: GIT-US-0094
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 1
created: 2026-09-13T13:21:50Z
updated: 2026-09-13T15:39:37Z
started: 2026-09-13T15:39:05Z
---

## Description

Update the pinned tool lists in `internal/mcp/tools_test.go:16-26` and `cmd/gintrack/mcp_test.go:28,37-40`, the long help in `cmd/gintrack/mcp.go:36-38` and the tool list in `AGENTS.md` to include both KB sync tools and the corrected counts.

## Acceptance Criteria

- [x] The pinned lists and counts include both tools and the tests pass.
- [ ] `gintrack mcp --list-tools` reports the correct totals.
- [x] `AGENTS.md` lists both tools; `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes.

## Notes

`writeTools` in `internal/mcp/tools_test.go` pins both KB sync tools and
`TestToolSurface` passes; `AGENTS.md` says 7 read-only / 22 with `--allow-write`
and lists all twenty-two; `docs/08-mcp-server.md` §4 carries the rows and §4.18.

`cmd/gintrack/mcp_test.go` needed no change — it asserts named tools present and
absent, not a count.

Second criterion left unticked: the long help in `cmd/gintrack/mcp.go:33-38`
still enumerates thirteen tools, and `cmd/` is owned by another agent this wave.
`--list-tools` itself prints whatever the registry holds, so its output is
already correct; only the prose beside it is stale.
