---
id: GIT-US-0014
type: story
title: Expose the REST API for items and knowledge base pages
status: done
priority: critical
parent: GIT-EP-0003
milestone: GIT-M-0003
author: team
labels: [server, core]
estimate: 8
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T05:48:51Z
started: 2026-09-04T05:06:40Z
closed: 2026-09-04T05:48:51Z
links:
  - { kind: blocked_by, target: GIT-US-0012 }
---

## Description

As the web app and, later, the MCP server, I need a stable HTTP API over the vault, so that
every client shares one implementation of reading and writing items.

The API covers projects, items (list, get, create, update, delete), knowledge-base pages,
search, and health. Writes carry the `rev` the client read and are rejected with `409` and
a structured conflict body when stale, which is the optimistic locking the whole
multi-writer design rests on. Listing endpoints paginate with cursors and support the same
filters as the UI.

Errors are a consistent problem-detail shape carrying the file path and field for
validation failures, so the UI can point at the exact input that is wrong.

## Acceptance Criteria

- [ ] `GET /api/health` reports version, mode and the open roots, and is unauthenticated.
- [ ] CRUD endpoints exist for every item type and write canonical files.
- [ ] Every response carries a `rev`; stale writes return `409` with a conflict body.
- [ ] List endpoints support cursor pagination and the full filter set.
- [ ] `GET /api/search` covers items and knowledge-base pages.
- [ ] Validation errors return a consistent problem detail with path and field.
- [ ] Requests are authenticated by token; unauthenticated requests get `401`.
- [ ] Every handler honours `r.Context()` cancellation and a request timeout.
- [ ] The API is documented in `docs/` with request and response examples.
