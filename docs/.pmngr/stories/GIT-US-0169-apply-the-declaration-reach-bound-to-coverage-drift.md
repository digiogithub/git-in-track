---
id: GIT-US-0169
type: story
title: Apply the declaration-reach bound to coverage drift detection
status: done
priority: low
parent: GIT-EP-0029
milestone: GIT-M-0015
assignees: [claude]
author: mcp
labels: [core, agent-ok]
created: 2026-09-29T17:39:29Z
updated: 2026-09-29T22:21:36Z
started: 2026-09-29T22:08:36Z
closed: 2026-09-29T22:21:36Z
---

## Description

GIT-US-0168 bounds impact tier 1 so a `decl` reason whose declaration has more than `DefaultMaxDeclUsers` users is dropped (and counted as `dropped`). Coverage drift detection in `internal/trace/reqcoverage.go` also calls `TraceTouching`, but it still counts every `decl` hit whatever its `users` count, so a change to a widely used type can still mark many requirements as drifted.

## Acceptance Criteria

- [x] Coverage drift applies the same users bound as impact tier 1 (shared constant or option, not a copy).
- [x] Drift that the bound suppresses stays visible (a count or reason), never silently lost.
- [x] Table-driven tests cover a widely used and a narrowly used declaration.
- [x] docs/03 §21 describes the rule for coverage as well as impact.

## Notes

Found while implementing GIT-US-0168; kept out of that story's scope. Done in PR #104: `trace.DefaultMaxDeclUsers`/`trace.WideDecl` are shared with impact, and a suppressed wide `decl` hit shows as reason `bounded:<n>` instead of drift.
