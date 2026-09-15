---
id: GIT-T-0152
type: task
title: Implement SearchKB, SearchCode and ListProjects with result parsing
status: done
priority: medium
parent: GIT-US-0077
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:19:00Z
updated: 2026-09-15T16:45:11Z
started: 2026-09-15T15:20:14Z
closed: 2026-09-15T16:45:11Z
---

## Description

Add the three call wrappers. `SearchKB` calls `kb_search_documents` with `query`, a limit capped at Pando's maximum of 20, optional `tags` and `exclude_outdated`, and parses the result into `{filePath, chunk, score, rank, tags, createdAt, updatedAt, metadata}`. `SearchCode` calls `code_hybrid_search` with the configured `project_id`, honouring the limit cap of 50 and setting `include_docs` explicitly rather than relying on its default, which excludes Markdown. `ListProjects` calls `code_list_projects`. Unknown fields in a result are tolerated, and a tool-level error is returned as a typed error rather than an empty result set.

## Acceptance Criteria

- [ ] Each wrapper sends the documented parameters and parses the documented result shape.
- [ ] Limits above Pando's caps are clamped rather than rejected by the server.
- [ ] A tool-level error is distinguishable from an empty result.
- [ ] `go test -race` covers parsing, clamping, unknown fields and the error path with recorded payloads.
