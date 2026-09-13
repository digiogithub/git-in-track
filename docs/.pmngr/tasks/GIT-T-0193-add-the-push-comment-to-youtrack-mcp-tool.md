---
id: GIT-T-0193
type: task
title: Add the push_comment_to_youtrack MCP tool
status: done
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 2
created: 2026-09-13T13:20:01Z
updated: 2026-09-13T15:39:21Z
started: 2026-09-13T15:39:03Z
closed: 2026-09-13T15:39:21Z
---

## Description

Register `push_comment_to_youtrack` in `internal/mcp/tools_youtrack.go` as a write tool with `In` `{project, itemId, commentPath?, all?}`, validating with `invalidField`, checking any path with `s.guard.Check`, dispatching the vault operation and announcing the write. Update the surface-pinning tests in `internal/mcp/tools_test.go` and `cmd/gintrack/mcp_test.go` and the tool list in `AGENTS.md`.

## Acceptance Criteria

- [x] The tool is registered as a write tool and hidden on a read-only server.
- [x] Path arguments pass through the path guard.
- [x] Surface-pinning tests and `AGENTS.md` are updated.
- [x] `go test -race ./internal/mcp/...` passes with a behaviour test through the client harness.

## Notes

`commentPath` goes through `s.guard.Check` before the core is asked anything,
like a knowledge-base path: it is user-supplied and the guard is what keeps it
inside the mounted roots.

`cmd/gintrack/mcp_test.go` needed no change — it asserts named tools present and
absent, not a count — so the four additions leave it passing untouched.

The result reports `pushed`, `skipped` and `failed` as always-present lists, and
`pushed` means *queued*: the push itself is a background job named by `jobId`.
