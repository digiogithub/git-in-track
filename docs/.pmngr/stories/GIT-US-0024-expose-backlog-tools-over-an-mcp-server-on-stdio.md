---
id: GIT-US-0024
type: story
title: Expose backlog tools over an MCP server on stdio
status: in_review
priority: critical
parent: GIT-EP-0006
milestone: GIT-M-0006
author: team
labels: [mcp, core]
estimate: 8
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0014 }
---

## Description

As an AI agent, I want a typed interface to the backlog and the knowledge base, so that I
can pick up and complete work without parsing Markdown by hand or guessing at file layout.

`gintrack mcp` speaks MCP over stdio, and the companion serves the same tools over
streamable HTTP. Tools: `list_items`, `search_items`, `get_item`, `create_epic`,
`create_story`, `create_task`, `update_item`, `add_comment`, `move_on_board`,
`list_kb_pages`, `get_kb_page`, `search_kb`.

Responses are shaped for agents rather than for humans: compact JSON, front matter only
unless the body is requested, cursor pagination with bounded page sizes, and a `rev` on
every item. Item bodies are returned as data; the server never treats repository content as
instructions.

## Acceptance Criteria

- [x] `gintrack mcp` serves over stdio and is usable from a standard MCP client.
- [x] The same tools are available over streamable HTTP from `gintrack serve`.
- [x] Every listed tool is implemented with a documented input and output schema.
- [x] Responses omit the body unless requested and stay compact.
- [x] List and search results paginate with cursors and a bounded page size.
- [x] Every returned item carries a `rev`.
- [x] Writes go through the same core validation as the UI; invalid input is rejected with
      a structured error.
- [x] Path arguments are confined to the vault root; traversal attempts are rejected.
- [x] Tool schemas are documented in `docs/` with worked examples.

## Notes

Implemented in `internal/mcp`, a thin adapter over `internal/vault`: its whole dependency on
the rest of the product is one interface, `Dispatch(ctx, method, params) (any, error)`, which
`*vault.Workspace` satisfies. No tool contains business logic.

- Twelve tools, named verb first, served identically over stdio (`gintrack mcp`) and over
  streamable HTTP (`gintrack serve --mcp-http`, `POST /mcp`, behind the existing bearer token
  and origin checks). Write tools are absent from `tools/list` unless writes are enabled.
- Schemas are inferred from the Go type of each handler's input and output by the official Go
  MCP SDK and validated in both directions, so a schema cannot drift from its handler
  ([ADR-015](../../adr/ADR-015-official-go-mcp-sdk-and-verb-noun-tools.md)).
- `update_item` routes a status change through the core's `item.move`, so an undeclared
  transition is refused by the same workflow validation the web UI runs.
- Path arguments are confined lexically (no absolute paths, no `..`, no backslashes, no NUL)
  and again after symlink resolution against the mounted roots.
- Repository content is returned as inert data: nothing in a body is interpreted, every tool
  that can return such text says so in its description, the result is marked
  `_meta["dev.git-in-track/contentTrust"]`, and the handshake instructions repeat the rule.
- Writes made over HTTP reach the event stream with `"origin": "mcp"` and the commit-on-save
  queue, exactly like a write made over REST.

Out of scope, by story: making `rev` mandatory on an agent write (`GIT-US-0025`) and the
`AGENTS.md` conventions (`GIT-US-0026`). `docs/08-mcp-server.md` marks everything else it
specifies — resources, prompts, dry-run, the audit log, rate limits — as *planned*.
