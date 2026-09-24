---
id: GIT-US-0151
type: story
title: Return an empty conflicts list for already-applied requirement updates
status: todo
priority: medium
parent: GIT-EP-0026
milestone: GIT-M-0015
author: mcp
labels: [core, mcp, agent-ok]
estimate: 2
created: 2026-09-24T21:55:53Z
updated: 2026-09-24T21:55:53Z
---

## Description

The rev protocol in AGENTS.md says an **empty** `conflicts[]` on `stale_revision` means the file already holds the proposed change, so the caller should stop. GIT-US-0129 (PR #58) found that requirement updates never return an empty `conflicts[]`. The web app and agents therefore cannot tell "already applied" from a real conflict.

## Acceptance Criteria

- [ ] `requirement.update` (vault, MCP `update_requirement`, HTTP PATCH) returns `stale_revision` with an empty `conflicts[]` when every proposed field already equals the current value. This matches `update_item`.
- [ ] Table tests cover an already-applied change, a partial overlap and a real conflict.
- [ ] The web requirement detail treats an empty `conflicts[]` as "already saved" and reloads without an error.
- [ ] docs/08 and docs/07 are updated if they describe it.
