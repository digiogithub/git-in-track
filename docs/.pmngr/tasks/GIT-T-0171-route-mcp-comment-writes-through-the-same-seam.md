---
id: GIT-T-0171
type: task
title: Route MCP comment writes through the same seam
status: todo
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [mcp, server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:26Z
updated: 2026-09-13T13:19:26Z
---

## Description

Make an agent-written comment reach the same enqueue seam through `Options.AfterWrite` and `s.publishAgentWrite` (`internal/server/mcp.go:87`, `:260`), with no second implementation of the decision or the enqueue. Verify through the in-memory MCP client harness that `add_comment` in auto mode produces one push job.

## Acceptance Criteria

- [ ] An MCP `add_comment` in auto mode enqueues exactly one push through the shared seam.
- [ ] No push-decision logic is duplicated in `internal/mcp`.
- [ ] `go test -race ./internal/mcp/... ./internal/server/...` covers it through the client harness.
