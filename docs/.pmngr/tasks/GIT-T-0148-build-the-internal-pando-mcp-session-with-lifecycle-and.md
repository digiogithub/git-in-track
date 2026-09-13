---
id: GIT-T-0148
type: task
title: Build the internal/pando MCP session with lifecycle and timeouts
status: todo
priority: medium
parent: GIT-US-0077
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:18:54Z
updated: 2026-09-13T13:18:54Z
---

## Description

Create `internal/pando` with a `Client` that opens one MCP session over streamable HTTP using `github.com/modelcontextprotocol/go-sdk/mcp`, already a direct dependency. The session is established lazily on first use, guarded by a mutex, and rebuilt when a call fails with a transport error so a Pando restart heals itself. Every call takes a context with a configurable per-call timeout, and `Health(ctx)` probes reachability without blocking longer than its own timeout. The package imports nothing from `internal/core`.

## Acceptance Criteria

- [ ] One session is shared across calls and rebuilt after a transport failure.
- [ ] Timeouts are honoured and produce a typed error distinguishable from a tool error.
- [ ] `Health` returns promptly whether or not Pando is up.
- [ ] `go test -race ./internal/pando/...` covers lazy connect, reconnect, timeout and health against an in-process MCP stub.
