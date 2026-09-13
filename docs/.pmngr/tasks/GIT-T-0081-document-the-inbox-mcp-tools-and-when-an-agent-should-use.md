---
id: GIT-T-0081
type: task
title: Document the inbox MCP tools and when an agent should use them
status: done
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [docs, mcp, agent-ok]
estimate: 1
created: 2026-09-13T13:17:18Z
updated: 2026-09-13T17:23:39Z
started: 2026-09-13T17:23:25Z
closed: 2026-09-13T17:23:39Z
---

## Description

Add the three inbox tools to the table in `docs/08-mcp-server.md` §4 and write a short paragraph on when an agent should call `create_inbox_item` instead of `create_story` — namely whenever the work has not been agreed with a human. Mirror the guidance in the tool's own MCP description (`internal/mcp/tools_inbox.go`) and in the long help of `cmd/gintrack/mcp.go:36-38`, and fix the stale tool count in `docs/07-cli-and-api.md:896-916` while in the file.

## Acceptance Criteria

- [x] `docs/08-mcp-server.md` §4 lists the three tools with their annotations and the guidance paragraph.
- [x] The MCP tool description and the CLI long help carry the same guidance.
- [x] `docs/07-cli-and-api.md` §4.9 lists the correct number of tools and write tools.

## Notes

Two of the three were already satisfied and were verified rather than assumed. `docs/08-mcp-server.md` §4 lists `list_inbox` (read), `create_inbox_item` (write) and `triage_inbox_item` (write) in the tool table with their core methods and token costs, and §4.11 carries the guidance paragraph: use it rather than `create_story` when you are reporting something rather than planning it, because `status`, `parent` and `type` are not the submitter's to choose. `internal/mcp/tools_inbox.go` carries the same sentence in the tool's own MCP description. `docs/07-cli-and-api.md` §4.9 says twenty-two tools, seven read-only and fifteen writes, matching `gintrack mcp --list-tools --allow-write`.

The gap was the CLI long help: `cmd/gintrack/mcp.go` listed the tool names but carried none of the guidance. It now has a paragraph naming the three inbox tools and stating the rule in the same words the MCP description and docs/08 use — reach for `create_inbox_item` rather than `create_story` whenever the work has not been agreed with a human; it files the item as pending and leaves the type, the parent and the status to whoever triages it.
