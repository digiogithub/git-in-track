---
id: GIT-US-0158
type: story
title: Reach traced code through changed package-level declarations
status: backlog
priority: medium
parent: GIT-EP-0025
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-24T23:18:51Z
updated: 2026-09-24T23:18:51Z
---

## Description

The GIT-US-0137 benchmark (PR #76) found two tier-1 misses: a change to a package-level constant (`maxPageSize`), and a removed call (`registerSpecTools`). Tier 1 maps changed lines to enclosing symbols only, so neither reached a traced function.

## Acceptance Criteria

- [ ] In Go, a changed package-level const, var or type is mapped to the traced symbols in the same package that reference it. This uses `go/parser` identifier resolution, deterministically and without Pando.
- [ ] A removed line inside a traced function counts as touching that function.
- [ ] Both benchmark misses become hits in a regression fixture, and tiers 1–2 stay deterministic.
- [ ] docs/03 §21.11 is updated.
