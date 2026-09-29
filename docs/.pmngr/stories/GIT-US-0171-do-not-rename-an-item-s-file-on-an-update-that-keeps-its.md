---
id: GIT-US-0171
type: story
title: Do not rename an item's file on an update that keeps its title
status: backlog
priority: low
author: mcp
labels: [core, mcp, agent-ok]
created: 2026-09-29T18:44:42Z
updated: 2026-09-29T18:44:42Z
---

## Description

On 2026-09-29 a status-only `update_item` on GIT-US-0162 renamed its file from `…-to-files-for-customisation.md` (61-byte slug, hand-created) to `…-to-files-for.md` (the 60-byte truncation of docs/03 §3.4). docs/03 R-SLUG-1/R-SLUG-2 say the slug is cosmetic and a file SHOULD be renamed when its *title* changes; a stale or over-long slug is only warning `W-SLUG-STALE`. Renaming on an unrelated update turns a two-line status diff into a delete plus an add and breaks links to the path.

## Acceptance Criteria

- [ ] An update that does not change `title` keeps the existing filename, even when its slug is stale or longer than 60 bytes.
- [ ] A title change still renames per R-SLUG-2.
- [ ] Table-driven test covers status-only update on an over-long slug and a title rename.
- [ ] docs/03 §3.4 states the rule explicitly if it is not already clear.

## Notes

Observed while closing out GIT-US-0162 through the MCP server.
