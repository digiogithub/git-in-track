---
id: GIT-T-0119
type: task
title: Update the MCP surface-pinning tests and help text
status: done
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 1
created: 2026-09-13T13:18:14Z
updated: 2026-09-13T16:56:26Z
started: 2026-09-13T15:38:07Z
closed: 2026-09-13T16:56:26Z
---

## Description

Update the surface-pinning tests that enumerate the tool set — `internal/mcp/tools_test.go:16` (`readTools`), `:21` (`writeTools`), `TestToolSurface` :26 — and `cmd/gintrack/mcp_test.go:28,37-40`, plus the long help in `cmd/gintrack/mcp.go:36-38` and the tool list in `AGENTS.md`. Without this the build fails on the pinned surface.

## Acceptance Criteria

- [x] The pinned read and write tool lists include the new tool and the tests pass.
- [x] `gintrack mcp --list-tools` and its long help report the correct counts.
- [x] `AGENTS.md` lists the new tool; `go test -race ./internal/mcp/... ./cmd/gintrack/...` passes.

## Notes

The `internal/mcp` half landed earlier: `writeTools` in `internal/mcp/tools_test.go` pins all four YouTrack tools, `TestToolSurface` passes, and `AGENTS.md` says 7 read-only / 22 with `--allow-write` and lists all twenty-two. `docs/08-mcp-server.md` §4 has the four new rows, the counts and §4.16–4.18.

The second criterion is now closed. The long help in `cmd/gintrack/mcp.go` states the counts explicitly — "twenty-two of them with writes enabled, seven without" — and then enumerates the seven read-only tools and the fifteen that need writes, matching what the registry advertises. `--list-tools` itself needed no change: it prints whatever the registry holds, so it was already correct; only the prose beside it was stale.

`cmd/gintrack/mcp_test.go` needed no change — it asserts presence and absence of named tools, not a count.
