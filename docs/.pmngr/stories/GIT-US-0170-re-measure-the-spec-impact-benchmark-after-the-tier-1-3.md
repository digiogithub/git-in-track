---
id: GIT-US-0170
type: story
title: Re-measure the spec impact benchmark after the tier 1–3 precision work
status: done
priority: low
parent: GIT-EP-0029
milestone: GIT-M-0015
assignees: [claude]
author: mcp
labels: [docs, agent-ok]
created: 2026-09-29T17:49:23Z
updated: 2026-09-29T22:56:16Z
started: 2026-09-29T22:20:15Z
closed: 2026-09-29T22:56:16Z
---

## Description

GIT-US-0165 (tier 3 story-based query, spec-only search), GIT-US-0166 (tier 2 shared names and test callers), GIT-US-0167 (tier 2 unavailable without a code graph) and GIT-US-0168 (tier 1 declaration-reach bound) all change precision and token cost, but none re-ran the GIT-US-0161 benchmark against a live Pando: the replay patches and hand edits of that benchmark were kept outside the repository. GIT-US-0165 acceptance criterion 4 (re-measure and record) is therefore open.

## Acceptance Criteria

- [x] The benchmark PR set (P1–P5) and its hand edits are committed under `docs/research/` (or a testdata folder) so a re-run is reproducible without private files.
- [x] The benchmark is re-run with Pando tiers 2 and 3 on a code graph built with `BuildCodeGraph = true`, and precision, recall and tokens per tier are recorded next to the 2026-09-25 numbers.
- [x] The doc notes that the long tier-3 query gets no full-text hits (Pando's full-text search requires every word) and whether that hurts recall.

## Notes

Follow-up from GIT-US-0165 (AC4 not met there). Done in PR #107 using gintrack's managed Pando mode (ADR-039), which worked end to end. The replay kit is in `docs/research/spec-impact-benchmark/`. P1–P7 and C1 were rebuilt from git history; S1–S5 and C2 were rewritten from their descriptions, because the originals were never saved. Follow-ups filed from benchmark §10.6.
