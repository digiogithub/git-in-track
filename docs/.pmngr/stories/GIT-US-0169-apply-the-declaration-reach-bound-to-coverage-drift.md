---
id: GIT-US-0169
type: story
title: Apply the declaration-reach bound to coverage drift detection
status: backlog
priority: low
parent: GIT-EP-0029
milestone: GIT-M-0015
author: mcp
labels: [core, agent-ok]
created: 2026-09-29T17:39:29Z
updated: 2026-09-29T17:39:29Z
---

## Description

GIT-US-0168 bounds impact tier 1 so a `decl` reason whose declaration has more than `DefaultMaxDeclUsers` users is dropped (and counted as `dropped`). Coverage drift detection in `internal/trace/reqcoverage.go` also calls `TraceTouching`, but it still counts every `decl` hit whatever its `users` count, so a change to a widely used type can still mark many requirements as drifted.

## Acceptance Criteria

- [ ] Coverage drift applies the same users bound as impact tier 1 (shared constant or option, not a copy).
- [ ] Drift that the bound suppresses stays visible (a count or reason), never silently lost.
- [ ] Table-driven tests cover a widely used and a narrowly used declaration.
- [ ] docs/03 §21 describes the rule for coverage as well as impact.

## Notes

Found while implementing GIT-US-0168; kept out of that story's scope.
