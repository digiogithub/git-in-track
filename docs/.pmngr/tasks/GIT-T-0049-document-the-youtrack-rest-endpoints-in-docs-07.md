---
id: GIT-T-0049
type: task
title: Document the YouTrack REST endpoints in docs/07
status: todo
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:16:30Z
updated: 2026-09-13T13:16:30Z
---

## Description

Document all five endpoints in `docs/07-cli-and-api.md` next to the git and sync sections, with request and response examples, the problem codes they can return, and an explicit statement that the token is write-only across the API. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Each endpoint has a method, path, parameters, example response and error table.
- [ ] The documentation states that no endpoint ever returns the token.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.
