---
id: GIT-US-0024
type: story
title: Expose backlog tools over an MCP server on stdio
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: critical
parent: GIT-EP-0006
milestone: GIT-M-0006
estimate: 8
labels: [mcp, core]
links:
  - kind: blocked_by
    target: GIT-US-0014
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

- [ ] `gintrack mcp` serves over stdio and is usable from a standard MCP client.
- [ ] The same tools are available over streamable HTTP from `gintrack serve`.
- [ ] Every listed tool is implemented with a documented input and output schema.
- [ ] Responses omit the body unless requested and stay compact.
- [ ] List and search results paginate with cursors and a bounded page size.
- [ ] Every returned item carries a `rev`.
- [ ] Writes go through the same core validation as the UI; invalid input is rejected with
      a structured error.
- [ ] Path arguments are confined to the vault root; traversal attempts are rejected.
- [ ] Tool schemas are documented in `docs/` with worked examples.
