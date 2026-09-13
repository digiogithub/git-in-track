---
id: GIT-T-0223
type: task
title: Document the KB sync tools and commands
status: todo
priority: medium
parent: GIT-US-0094
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 1
created: 2026-09-13T13:22:01Z
updated: 2026-09-13T13:22:01Z
---

## Description

Document both KB sync tools in the `docs/08-mcp-server.md` §4 tool table and `gintrack youtrack kb push|pull` in `docs/07-cli-and-api.md` §4, each with an example, including the non-zero exit on conflict so CI authors can rely on it. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Both surfaces are documented with examples and the exit-code behaviour is stated.
- [ ] `CHANGELOG.md` has an entry.
- [ ] `make lint` passes.
