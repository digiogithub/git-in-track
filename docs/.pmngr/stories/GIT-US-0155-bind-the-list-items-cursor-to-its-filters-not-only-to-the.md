---
id: GIT-US-0155
type: story
title: Bind the list_items cursor to its filters, not only to the sort
status: todo
priority: medium
author: mcp
labels: [mcp, agent-ok]
estimate: 2
created: 2026-09-24T23:02:40Z
updated: 2026-09-24T23:02:40Z
---

## Description

While writing the MCP pagination spec (GIT-US-0136, PR #74), the agent found that the `list_items` cursor is bound to the sort order, not to the filters. A filter changed mid-walk is therefore not detected, and the walk silently returns rows from a different result set. AGENTS.md and docs/08 describe the cursor as filter-bound ("never change a filter mid-walk"), and the tools are meant to refuse a changed filter.

## Acceptance Criteria

- [ ] A failing test: walking `list_items` with a cursor and changing `status`, `type`, `label` or `milestone` is accepted today.
- [ ] The cursor fingerprint covers every filter and the sort. A mismatch is refused with the existing cursor error, the same way the other paginated tools behave.
- [ ] `list_requirements`, `spec_coverage` and the other cursor tools are checked for the same gap.
- [ ] docs/08 matches the behaviour. If GIT-SP-0004 is merged by then, its requirement is updated with a MODIFIED Spec Delta.
