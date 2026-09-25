---
id: GIT-US-0168
type: story
title: Bound the declaration reach of impact tier 1 through widely used types
status: todo
priority: low
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [core, agent-ok]
estimate: 3
created: 2026-09-25T12:27:40Z
updated: 2026-09-25T12:27:40Z
links:
  - { kind: relates_to, target: GIT-US-0161 }
  - { kind: relates_to, target: GIT-US-0158 }
---

## Description

GIT-US-0158 reaches traced code through changed package-level declarations. When the changed type is used everywhere, this floods tier 1. In the GIT-US-0161 benchmark, `decl:… uses Item` added 14 hits to P4, and `uses ItemResult` added 12 `test-only` hits to P5. With those hits, tier 1 alone reaches the worst-case report of 1,421 tokens (docs/research/2026-09-25-spec-impact-benchmark.md §9.4).

## Acceptance Criteria

- [ ] The P4 and P5 `decl:` hits are judged by hand, and the verdicts are recorded in the benchmark doc.
- [ ] The `decl:` reach is capped or ranked, for example by the number of users of the declaration, so that a widely used type does not fill the budget. Hits it drops are counted in the report, never silently lost.
- [ ] Golden tests cover a widely used type and a narrowly used one.
- [ ] docs/03 §21 describes the rule. `make test` and `make lint` pass.
