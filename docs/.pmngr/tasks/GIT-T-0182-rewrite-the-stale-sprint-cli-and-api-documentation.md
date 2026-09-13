---
id: GIT-T-0182
type: task
title: Rewrite the stale sprint CLI and API documentation
status: todo
priority: medium
parent: GIT-US-0092
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:19:41Z
updated: 2026-09-13T13:19:41Z
---

## Description

Replace the stale specification at `docs/07-cli-and-api.md` §4.6 (line 731), which describes `board`, `sprint` and `retro` commands that do not exist, with the sprint command tree as built, and document the extended `POST /api/v1/sprints/{id}/close` and the new `POST /api/v1/sprints/{id}/transfer` plus the `sprint.changed` WS topic in §5.5. Note explicitly that `board` and `retro` commands are still not implemented rather than leaving them described as if they were.

## Acceptance Criteria

- [ ] §4.6 documents only what exists, with the flags and exit codes, and says what does not.
- [ ] §5.5 documents both endpoints with their request and response shapes and error codes, and the WS topic is in the contract section.
- [ ] `make lint` passes.
