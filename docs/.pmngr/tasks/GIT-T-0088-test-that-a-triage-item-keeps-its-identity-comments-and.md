---
id: GIT-T-0088
type: task
title: Test that a triage item keeps its identity, comments and acceptance path
status: todo
priority: medium
parent: GIT-US-0071
milestone: GIT-M-0012
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-13T13:17:29Z
updated: 2026-09-13T13:17:29Z
---

## Description

Complement the exclusion suite with the cases the exclusion must not swallow: a triage item is still readable by id through `item.get`, still accepts and lists comments, still appears in `inbox.list`, and — once accepted — appears in the backlog and in a matching board column within the same index generation. Put the accept case in `internal/vault` so it exercises the real write path.

## Acceptance Criteria

- [ ] Tests cover read-by-id, comments, inbox listing and the accept-then-appears transition.
- [ ] The accept case asserts the item is in the board column its new status maps to.
- [ ] `go test -race ./internal/core/... ./internal/vault/...` passes.
