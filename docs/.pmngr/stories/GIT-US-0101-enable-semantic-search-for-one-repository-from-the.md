---
id: GIT-US-0101
type: story
title: Enable semantic search for one repository from the workspace list
status: in_review
priority: high
parent: GIT-EP-0021
milestone: GIT-M-0014
author: mcp
labels: [server, web]
estimate: 5
created: 2026-09-17T09:31:54Z
updated: 2026-09-17T09:47:03Z
started: 2026-09-17T09:33:18Z
---

## Description

As a user, I want to see in the workspace list whether each repository has semantic search, and switch it on with a button when its Pando code index is `off` or `unavailable`, without going to Settings.

The state already exists in `GET /api/v1/search/settings` (`indexed[].code.status`). The action registers that one repository with Pando and reindexes it (code project + KB), reporting progress on the existing `search.progress` topic. When Pando is not configured (`configured: false`) the button is a link to the Settings card instead.

## Acceptance Criteria

- [ ] A per-repository action (e.g. `POST /api/v1/search/reindex` with a `repo` scope, or a dedicated route) registers + reindexes only that repository and answers 202 with a job.
- [ ] The workspace list shows a semantic-search badge per repository (on / indexing / off / unavailable) and the enable button for off/unavailable.
- [ ] Unconfigured Pando: the control links to Settings; browser-only runtime (no companion): the control is hidden.
- [ ] Progress is followed through the provider event seam, not polled; the badge updates when the job ends.
- [ ] Tests: server route scoping, component states.
- [ ] docs/07 and the search docs updated.
