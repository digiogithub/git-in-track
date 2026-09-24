---
id: GIT-US-0152
type: story
title: Never report a refused item patch as already applied
status: todo
priority: medium
author: mcp
labels: [core, mcp, agent-ok]
estimate: 2
created: 2026-09-24T22:12:47Z
updated: 2026-09-24T22:12:47Z
---

## Description

GIT-US-0151 (PR #63) found that a stale requirement patch the store would refuse anyway, such as a blank title, came back with an empty `conflicts[]`. The rev protocol reads an empty list as "already applied, stop", so a refused change looked saved. The requirement path is fixed. `update_item`'s `conflictWith` has the same flaw.

## Acceptance Criteria

- [ ] A failing test reproduces it: a stale `item.update` whose patch would be refused (for example an invalid field value) returns an empty `conflicts[]` today.
- [ ] `conflicts[]` is empty only when every proposed field is already on disk. Otherwise it names the fields the patch carries.
- [ ] The vault, MCP `update_item` and HTTP tests cover already applied, partial overlap, and refused patch.
- [ ] docs/08 is updated if it describes the case.
