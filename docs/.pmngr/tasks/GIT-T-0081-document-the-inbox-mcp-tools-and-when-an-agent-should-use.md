---
id: GIT-T-0081
type: task
title: Document the inbox MCP tools and when an agent should use them
status: todo
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [docs, mcp, agent-ok]
estimate: 1
created: 2026-09-13T13:17:18Z
updated: 2026-09-13T13:17:18Z
---

## Description

Add the three inbox tools to the table in `docs/08-mcp-server.md` §4 and write a short paragraph on when an agent should call `create_inbox_item` instead of `create_story` — namely whenever the work has not been agreed with a human. Mirror the guidance in the tool's own MCP description (`internal/mcp/tools_inbox.go`) and in the long help of `cmd/gintrack/mcp.go:36-38`, and fix the stale tool count in `docs/07-cli-and-api.md:896-916` while in the file.

## Acceptance Criteria

- [ ] `docs/08-mcp-server.md` §4 lists the three tools with their annotations and the guidance paragraph.
- [ ] The MCP tool description and the CLI long help carry the same guidance.
- [ ] `docs/07-cli-and-api.md` §4.9 lists the correct number of tools and write tools.
