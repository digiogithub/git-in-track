---
id: GIT-T-0052
type: task
title: Implement bounded, cycle-safe subtask recursion
status: todo
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:36Z
updated: 2026-09-13T13:16:36Z
---

## Description

Add the expansion step that walks Subtask links outward up to `depth` levels, keeping a visited set keyed on `idReadable` so a cycle cannot loop, and resolving the parent of each imported issue. Parents and `links[]` targets outside the resulting set are recorded as warnings on the issue result instead of being written as dangling references. Depth 0 means the selected issues only.

## Acceptance Criteria

- [ ] `depth` bounds the recursion and 0 imports only the selected issues.
- [ ] A cyclic Subtask graph terminates and is reported.
- [ ] Out-of-set parents and link targets become warnings, never dangling references.
- [ ] `go test -race ./internal/vault/...` covers depth 0, 1 and 2 and a cyclic fixture.
