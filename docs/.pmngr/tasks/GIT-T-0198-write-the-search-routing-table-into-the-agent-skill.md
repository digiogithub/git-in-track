---
id: GIT-T-0198
type: task
title: Write the search routing table into the agent skill
status: done
priority: medium
parent: GIT-US-0088
milestone: GIT-M-0013
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:20:17Z
updated: 2026-09-15T16:44:13Z
started: 2026-09-15T16:17:44Z
closed: 2026-09-15T16:44:13Z
---

## Description

Extend the Pando skill file shipped by `gintrack agent init` with the routing table: structured lookups go to the gintrack MCP tools `get_item`, `list_items` and `search_items`; "which stories or pages talk about X" goes to `search_semantic` or `kb_search_documents`; "where is this implemented" goes to `code_hybrid_search` with the repository's project id. One example question per row. Mirror the table in `docs/20-agent-interface.md` so a human setting this up sees the same guidance.

## Acceptance Criteria

- [ ] The skill file contains the four-row table with an example per row.
- [ ] The same table appears in `docs/20-agent-interface.md`.
- [ ] A note explains why `hybrid_search_remembrances` is not used: its merge sorts incommensurable scores.
