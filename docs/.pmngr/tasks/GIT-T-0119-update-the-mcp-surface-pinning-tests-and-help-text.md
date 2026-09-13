---
id: GIT-T-0119
type: task
title: Update the MCP surface-pinning tests and help text
status: in_review
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 1
created: 2026-09-13T13:18:14Z
updated: 2026-09-13T15:38:27Z
started: 2026-09-13T15:38:07Z
---

## Description

Update the surface-pinning tests that enumerate the tool set — `internal/mcp/tools_test.go:16` (`readTools`), `:21` (`writeTools`), `TestToolSurface` :26 — and `cmd/gintrack/mcp_test.go:28,37-40`, plus the long help in `cmd/gintrack/mcp.go:36-38` and the tool list in `AGENTS.md`. Without this the build fails on the pinned surface.

## Acceptance Criteria

- [x] The pinned read and write tool lists include the new tool and the tests pass.
- [ ] `gintrack mcp --list-tools` and its long help report the correct counts.
- [x] `AGENTS.md` lists the new tool; `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes.

## Notes

The `internal/mcp` half is done: `writeTools` in `internal/mcp/tools_test.go`
now pins all four YouTrack tools, `TestToolSurface` passes, and `AGENTS.md` says
7 read-only / 22 with `--allow-write` and lists all twenty-two.
`docs/08-mcp-server.md` §4 has the four new rows, the counts and §4.16–4.18.

The second criterion is left unticked: the long help in `cmd/gintrack/mcp.go:33-38`
still enumerates thirteen tools, and `cmd/` is owned by another agent this wave.
`cmd/gintrack/mcp_test.go` needed no change — it asserts presence and absence of
named tools, not a count — and `go test -race ./cmd/gintrack/...` is unaffected
by the four additions.
