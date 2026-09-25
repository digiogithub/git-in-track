---
id: GIT-US-0167
type: story
title: Report a missing Pando code graph as unavailable in impact tier 2
status: todo
priority: medium
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [server, docs, agent-ok]
estimate: 2
created: 2026-09-25T12:27:40Z
updated: 2026-09-25T12:27:40Z
links:
  - { kind: relates_to, target: GIT-US-0161 }
---

## Description

When a project is indexed with `[TokenOptimization] BuildCodeGraph = false`, Pando answers "No callers found … or the project lacks call edges". Tier 2 reports that as `ok 0`, so the agent is told that nothing is affected instead of being told that the tier could not answer (docs/research/2026-09-25-spec-impact-benchmark.md §9.6, §9.8). The maintainer's own `~/.pando.toml` had the graph off.

## Acceptance Criteria

- [ ] A missing code graph (no call edges for the project) makes tier 2 answer `unavailable` with a fixed reason that names `BuildCodeGraph`, never `ok 0`.
- [ ] Tests with the fake Pando server cover a project without call edges and a real "no callers" answer.
- [ ] docs/21 §6.1 documents that tier 2 needs `[TokenOptimization] BuildCodeGraph = true`, and that the code project id is derived from the repository root.
- [ ] `make test` and `make lint` pass.
