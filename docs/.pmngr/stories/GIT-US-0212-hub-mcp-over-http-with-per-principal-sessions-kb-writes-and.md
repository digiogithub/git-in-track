---
id: GIT-US-0212
type: story
title: Hub MCP over HTTP with per-principal sessions, KB writes and memory tools
status: in_review
priority: medium
parent: GIT-EP-0034
author: claude-code
assignees: [claude-code]
labels: [mcp, server, agent-ok]
estimate: 5
created: 2026-11-08T17:59:20Z
updated: 2026-11-09T04:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0209 }
  - { kind: blocked_by, target: GIT-US-0211 }
---

## Description

Serve the MCP tools from the hub with one `sdk.Server` per session bound to the principal (`internal/mcp/transport.go:41`), and register hub-only tools: `write_kb_page` (rev required for an existing page), `add_memory`, `propose_memory`, `update_memory`, `forget_memory`, `search_memory`, `accept_memory`, `reject_memory`, `project_context`. Local tool counts of `gintrack mcp` do not change.

## Acceptance Criteria

- [ ] `Agent:` and the author come from the session's principal, not from the handshake.
- [ ] `write_kb_page` with an empty rev on an existing page fails with `already_exists`.
- [ ] `add_memory` without `write` degrades to `proposed` and says so.
- [ ] `cmd/gintrack/mcp_test.go` counts unchanged; hub tools documented in docs/08.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
