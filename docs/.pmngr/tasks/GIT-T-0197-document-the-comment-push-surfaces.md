---
id: GIT-T-0197
type: task
title: Document the comment push surfaces
status: todo
priority: medium
parent: GIT-US-0079
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:20:09Z
updated: 2026-09-13T13:20:09Z
---

## Description

Document `push_comment_to_youtrack` in the `docs/08-mcp-server.md` §4 tool table and `gintrack youtrack push-comments` in `docs/07-cli-and-api.md` §4, each with an example, and state that a local delete never deletes remotely. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Both surfaces are documented with examples.
- [ ] The delete asymmetry is stated where a reader will find it.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.
