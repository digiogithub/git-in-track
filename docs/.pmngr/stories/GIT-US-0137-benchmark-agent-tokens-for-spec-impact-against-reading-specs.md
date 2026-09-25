---
id: GIT-US-0137
type: story
title: Benchmark agent tokens for spec impact against reading specs
status: backlog
priority: medium
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [docs, agent-ok]
estimate: 3
created: 2026-09-24T12:12:00Z
updated: 2026-09-24T12:12:00Z
links:
  - { kind: blocked_by, target: GIT-US-0124 }
  - { kind: blocked_by, target: GIT-US-0135 }
  - { kind: blocked_by, target: GIT-US-0136 }
---

## Description

As the product owner, I want evidence that the impact query delivers the promised saving: the tokens an agent spends answering "what does this PR affect?" with `spec_impact` versus reading the relevant spec folder.

## Acceptance Criteria

- [ ] At least five real or replayed PRs touching the dogfooded capabilities are measured both ways with the same tokenizer approximation.
- [ ] Results (tokens, hits, precision of tiers 1–2, candidates) are recorded in `docs/research/<date>-spec-impact-benchmark.md`.
- [ ] The typical PR's report is ≤ 1.5k tokens and ≥ 10× cheaper; if not, follow-up stories are created in backlog.
- [ ] Determinism check: running tiers 1–2 twice on each PR gives identical output.

## Notes

Closes the milestone's success criteria.
