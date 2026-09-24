---
id: GIT-US-0156
type: story
title: Bind HTTP, browser and spec-tool cursors to their filters
status: todo
priority: medium
author: mcp
labels: [server, web, mcp, agent-ok]
estimate: 3
created: 2026-09-24T23:06:50Z
updated: 2026-09-24T23:06:50Z
---

## Description

GIT-US-0155 (PR #75) bound the MCP `list_items` and `list_inbox` cursors to every filter and the sort. Other paths are still open:

- `GET /api/v1/items` and `/api/v1/inbox`, and the browser vault path, accept a filter changed mid-walk.
- `list_requirements` and `spec_coverage` (Phase 11) have not been checked.

## Acceptance Criteria

- [ ] Failing tests for each path that accepts a changed filter today.
- [ ] Each cursor is bound to its filters and sort, and a mismatch returns the existing invalid-cursor error. Share one helper; do not duplicate the MCP one.
- [ ] If GIT-SP-0004 (#74) is merged by then, add a MODIFIED Spec Delta for its cursor requirement.
- [ ] docs/07 and docs/08 are updated. `make test` and `make lint` pass.
