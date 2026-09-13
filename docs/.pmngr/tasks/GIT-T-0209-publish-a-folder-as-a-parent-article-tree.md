---
id: GIT-T-0209
type: task
title: Publish a folder as a parent article tree
status: todo
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:49Z
updated: 2026-09-13T13:20:49Z
---

## Description

Extend the publish handler with the recursive mode: walk the KB tree depth-first, create or reuse one parent article per directory so the remote hierarchy mirrors the local one through `parentArticle`, publish parents before their children and keep sibling order. Note that `ordinal` is documented read-only and is silently ignored if sent, so ordering must come from creation order.

## Acceptance Criteria

- [ ] A folder publish creates the parent article tree and reuses existing parents on re-publish.
- [ ] Parents are always published before their children and sibling order is preserved.
- [ ] `ordinal` is not relied on for ordering.
- [ ] `go test -race ./internal/server/...` covers a two-level tree on first publish and re-publish.
