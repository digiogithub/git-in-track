---
id: GIT-US-0156
type: story
title: Bind HTTP, browser and spec-tool cursors to their filters
status: in_progress
priority: medium
milestone: GIT-M-0015
assignees: [claude]
author: mcp
labels: [server, web, mcp, agent-ok]
estimate: 3
created: 2026-09-24T23:06:50Z
updated: 2026-09-25T11:15:00Z
started: 2026-09-25T11:15:00Z
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

## Spec Delta

### MODIFIED GIT-SP-0004.R5 — The item-query cursor is bound to its filter and sort

IF a cursor of the core's item query — the one behind `list_items`, `list_inbox`, `GET /api/v1/items`, `GET /api/v1/inbox` and the browser's `item.list` and `inbox.list` — is presented with a different filter or sort than the call that issued it, THEN the core SHALL refuse the cursor with `invalid_cursor` instead of returning a page of another result set.

#### Scenario: the sort changed mid-walk
- **GIVEN** a page sorted by `id` returned a `nextCursor`
- **WHEN** the cursor is passed back with the sort `-updated`
- **THEN** the query is refused with `invalid_cursor`

#### Scenario: a filter changed mid-walk over REST
- **GIVEN** `GET /api/v1/items?limit=1&sort=id` returned a `nextCursor`
- **WHEN** the cursor is passed back with `status=todo` added
- **THEN** the response is `400` with the code `invalid_cursor`

#### Scenario: a relative updatedSince survives the clock
- **GIVEN** a page filtered by `updatedSince: 7d` returned a `nextCursor`
- **WHEN** the cursor is passed back with the same `7d` a minute later
- **THEN** the walk continues
