---
id: GIT-US-0165
type: story
title: Aim impact tier 3 at requirement blocks with a story-based query
status: todo
priority: medium
parent: GIT-EP-0029
milestone: GIT-M-0015
author: claude
labels: [server, core, agent-ok]
estimate: 5
created: 2026-09-25T12:27:40Z
updated: 2026-09-25T12:27:40Z
links:
  - { kind: relates_to, target: GIT-US-0161 }
---

## Description

Even with Pando's cache paged by hand, tier 3 added 3 candidates on the GIT-US-0161 PR set, and all 3 were unrelated (docs/research/2026-09-25-spec-impact-benchmark.md §9.3, §9.5 item 3). `kb_search_documents` has no path filter, and the requirement kind filter runs after it. Only 9 of the 224 top-16 chunks came from spec files. The query is bare symbol names (`nextNumber`, `maxPageSize`), which match prose about the code better than they match the EARS statements of a requirement.

## Acceptance Criteria

- [ ] Tier 3 searches only spec content: the knowledge-base search is restricted to the specs folder, or requirement blocks are indexed as documents of their own. Pando still never writes into `docs/.pmngr/specs/` (ADR-036).
- [ ] The tier-3 query is built from the story (title, and its `## Spec Delta` when present) and the changed declarations, not from bare symbol names alone.
- [ ] Tests with the fake Pando server cover the filter and the query construction.
- [ ] The GIT-US-0161 PR set is re-measured and the benchmark doc records tier-3 hits, precision and tokens.
- [ ] docs/21 is updated. `make test` and `make lint` pass.
