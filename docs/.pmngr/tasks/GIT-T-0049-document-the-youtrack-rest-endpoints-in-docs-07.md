---
id: GIT-T-0049
type: task
title: Document the YouTrack REST endpoints in docs/07
status: done
priority: medium
parent: GIT-US-0052
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 1
created: 2026-09-13T13:16:30Z
updated: 2026-09-13T14:43:27Z
started: 2026-09-13T14:42:51Z
closed: 2026-09-13T14:43:27Z
---

## Description

Document all five endpoints in `docs/07-cli-and-api.md` next to the git and sync sections, with request and response examples, the problem codes they can return, and an explicit statement that the token is write-only across the API. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Each endpoint has a method, path, parameters, example response and error table.
- [x] The documentation states that no endpoint ever returns the token.
- [ ] `make lint` passes and `CHANGELOG.md` is updated.

## Notes

`docs/07-cli-and-api.md` §5.5 gains a "YouTrack" subsection with all five endpoints, their parameters, example requests and responses, the six-row problem-code table and the reason an upstream failure is a 502 rather than the status YouTrack returned. The `code` catalog in §5.4 lists the new codes.

`make lint` passes with 0 issues. The box stays unticked for `CHANGELOG.md` only: that file was owned by another agent this wave and is off limits to this one.
