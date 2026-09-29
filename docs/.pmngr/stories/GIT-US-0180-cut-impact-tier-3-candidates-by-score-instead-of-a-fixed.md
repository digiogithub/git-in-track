---
id: GIT-US-0180
type: story
title: Cut impact tier 3 candidates by score instead of a fixed count
status: backlog
priority: medium
author: mcp
labels: [core, server, agent-ok]
created: 2026-09-29T22:56:37Z
updated: 2026-09-29T22:56:37Z
---

## Description

In the GIT-US-0170 re-measurement (#107, §10.2 and §10.6) tier 3 returned 8 candidates whatever the diff: only 2 of 67 candidates were right, and a PR with no hits grew from 67 to 470 tokens. The long story-based query gets no full-text hits, so scores are single-leg (at most 0.016) and nearly flat.

## Acceptance Criteria

- [ ] Tier 3 keeps a candidate only above a score or relative-gap threshold, so a flat ranking yields few or no candidates; the cut is documented in docs/03 R-IMP-4.
- [ ] Optionally, a short name-word query is added so the full-text leg can contribute (§10.4 shows it matches the same requirements on single-declaration PRs).
- [ ] The benchmark replay (`docs/research/spec-impact-benchmark/replay.sh`) shows tier-3 tokens down and recall not lower than 19/21.
