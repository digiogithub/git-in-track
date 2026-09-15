---
id: GIT-T-0206
type: task
title: Document the search settings endpoints and the model pin
status: in_review
priority: medium
parent: GIT-US-0091
milestone: GIT-M-0013
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:20:36Z
updated: 2026-09-15T16:04:32Z
started: 2026-09-15T16:04:17Z
---

## Description

Document the three endpoints in `docs/07-cli-and-api.md` with their request and response shapes and the `persisted` semantics, and add an operations note explaining that changing the embedding model silently degrades recall until a full reindex, because Pando skips chunks whose vector length differs from the query's with no dimension guard and no error. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] All three endpoints are documented with shapes and the `persisted` semantics.
- [ ] The embedding-model operations note is present and states the failure mode explicitly.
- [ ] `CHANGELOG.md` records the settings surface.
