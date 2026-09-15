---
id: GIT-T-0170
type: task
title: Merge exact and semantic hits with fallback in handleSearch
status: done
priority: medium
parent: GIT-US-0082
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:19:24Z
updated: 2026-09-15T16:43:26Z
started: 2026-09-15T16:04:07Z
closed: 2026-09-15T16:43:26Z
---

## Description

Change `handleSearch` (`internal/server/api.go:75`) to select the backend per request: use the Pando searcher when it is configured, healthy and the corpus has been exported, otherwise the core index. When Pando answers, run the core index too and emit exact id and title matches first in their existing order, then semantic hits not already present, each tagged with its origin. Never sort the two score spaces together numerically — KB fusion scores and core scores are incommensurable. A Pando failure mid-request falls back to the core result with the same response shape, logging the degradation once per state change rather than per request.

## Acceptance Criteria

- [ ] Exact matches lead the result list in their current relative order, followed by deduplicated semantic hits.
- [ ] Scores from the two backends are never compared against each other.
- [ ] A Pando failure yields the core result with an unchanged response shape.
- [ ] The degradation is logged once per state change, and `go test -race ./internal/server/...` covers selection, merge, dedup and fallback.
