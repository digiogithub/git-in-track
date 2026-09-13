---
id: GIT-T-0092
type: task
title: Document the inbox REST, WS and CLI surfaces and update the CHANGELOG
status: todo
priority: medium
parent: GIT-US-0071
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:17:34Z
updated: 2026-09-13T13:17:34Z
---

## Description

Document `GET /api/v1/inbox`, `POST /api/v1/items/{id}/triage` and the `inbox.changed` WS topic in `docs/07-cli-and-api.md` (§5 for the endpoints, the WS contract section at line 2353 for the topic), and record the epic in `CHANGELOG.md` under Unreleased — the `triage` category, the `inbox` block, the new endpoints, tools and commands, plus the note that a project created before this change has no triage status and therefore no inbox until one is added to its workflow.

## Acceptance Criteria

- [ ] Both endpoints and the WS topic are documented with their request and response shapes and their error codes.
- [ ] The CHANGELOG entry includes the "existing projects have no triage status yet" note.
- [ ] `make lint` passes.
