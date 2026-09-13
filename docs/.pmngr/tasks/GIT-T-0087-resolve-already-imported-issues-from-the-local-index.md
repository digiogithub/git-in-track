---
id: GIT-T-0087
type: task
title: Resolve already-imported issues from the local index
status: done
priority: medium
parent: GIT-US-0054
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:28Z
updated: 2026-09-13T16:03:42Z
started: 2026-09-13T16:03:26Z
closed: 2026-09-13T16:03:42Z
---

## Description

Enrich each search result with `linked: {itemId} | null`, resolved locally by `(external.system, external.id)` through the index rather than a second remote call, so the picker can show which issues are already imported. Resolution is one index lookup per page, not per row, and must not hold the vault mutex across the upstream request.

## Acceptance Criteria

- [ ] Every result carries `linked`, resolved from the index in a single lookup per page.
- [ ] No additional YouTrack request is made for enrichment.
- [ ] The vault mutex is not held across the network call.
- [ ] `go test -race ./internal/server/...` covers a mixed page of linked and unlinked issues.
