---
id: GIT-T-0208
type: task
title: Register the KB publish job kind for a single page
status: todo
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:44Z
updated: 2026-09-13T13:20:44Z
---

## Description

Add `internal/server/youtrack_kb.go` registering a `youtrack.kb.publish` `Kind` and `Handler`. For a page with no YouTrack `external` reference it creates the article with `POST /api/articles` including `project` in the body — `project` is read-only afterwards and can only be set at creation — and for a linked page it updates with `POST /api/articles/{id}`. On success it writes the article id and url into the page front matter through `kb.write` (`internal/vault/vault.go:1356`), quoting the page rev.

## Acceptance Criteria

- [ ] An unlinked page is created with `project` in the creation body; a linked page is updated.
- [ ] The article id and url are written back through `kb.write` rev-guarded.
- [ ] A stale rev on the write-back is reported, never forced.
- [ ] `go test -race ./internal/server/...` covers create, update and the stale-rev path with a fake client.
