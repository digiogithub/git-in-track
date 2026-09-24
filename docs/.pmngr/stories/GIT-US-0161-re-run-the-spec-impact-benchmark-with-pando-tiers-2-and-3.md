---
id: GIT-US-0161
type: story
title: Re-run the spec impact benchmark with Pando tiers 2 and 3
status: backlog
priority: low
parent: GIT-EP-0029
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-24T23:18:51Z
updated: 2026-09-24T23:18:51Z
---

## Description

The GIT-US-0137 benchmark (PR #76) ran without Pando, so only tier 1 was measured. Repeat it with Pando configured to measure tiers 2 (transitive) and 3 (semantic candidates): their extra hits, precision, and token cost.

## Acceptance Criteria

- [ ] The same PR set is run with Pando configured and the repository already indexed. Indexing jobs are not polled during the run.
- [ ] The benchmark doc gains tier 2 and tier 3 columns: hits, precision and tokens.
- [ ] A follow-up is filed if tier 2 or tier 3 hurts the budget or precision.
