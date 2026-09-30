---
id: GIT-US-0180
type: story
title: Cut impact tier 3 candidates by score instead of a fixed count
status: done
priority: medium
assignees: [claude]
author: mcp
labels: [core, server, agent-ok]
created: 2026-09-29T22:56:37Z
updated: 2026-09-30T15:11:17Z
started: 2026-09-30T12:43:06Z
closed: 2026-09-30T15:11:17Z
---

## Description

In the GIT-US-0170 re-measurement (#107, §10.2 and §10.6) tier 3 returned 8 candidates whatever the diff: only 2 of 67 candidates were right, and a PR with no hits grew from 67 to 470 tokens. The long story-based query gets no full-text hits, so scores are single-leg (at most 0.016) and nearly flat.

## Acceptance Criteria

- [x] Tier 3 keeps a candidate only above a score or relative-gap threshold, so a flat ranking yields few or no candidates; the cut is documented in docs/03 R-IMP-4.
- [x] Optionally, a short name-word query is added so the full-text leg can contribute (§10.4 shows it matches the same requirements on single-declaration PRs).
- [ ] The benchmark replay (`docs/research/spec-impact-benchmark/replay.sh`) shows tier-3 tokens down and recall not lower than 19/21.

## Notes

Done in PR #117. A candidate is kept only with score ≥ 0.017 (both legs matched), ≥ 80% of the best score, and at most 5. There is also one short name-word query per changed declaration.

Replay: tier-3 candidates fell from 83 to 16, an empty report from about 470 to about 70 tokens, and the median from 707 to 405 tokens. Recall fell from 19/21 to 18/21, because the two correct tier-3 finds sat inside a flat single-leg ranking. The maintainer accepted this trade-off on 2026-09-30; it is written up in benchmark §11.
