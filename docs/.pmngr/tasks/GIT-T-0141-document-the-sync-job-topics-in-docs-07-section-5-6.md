---
id: GIT-T-0141
type: task
title: Document the sync.job.* topics in docs/07 section 5.6
status: todo
priority: medium
parent: GIT-US-0074
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:18:44Z
updated: 2026-09-13T13:18:44Z
---

## Description

Document the five `sync.job.*` topics in `docs/07-cli-and-api.md` §5.6 beside `sync.progress` and `git.commit`, with a payload example for each, a note on progress coalescing and a note that clients may miss frames on overflow and should reconcile from `GET /api/v1/sync/jobs`. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Each topic is documented with an example payload.
- [ ] The overflow and reconciliation behaviour is stated.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.
