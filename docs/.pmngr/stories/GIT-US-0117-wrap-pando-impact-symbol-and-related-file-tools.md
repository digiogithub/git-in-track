---
id: GIT-US-0117
type: story
title: Wrap Pando impact, symbol and related-file tools
status: done
priority: high
parent: GIT-EP-0025
milestone: GIT-M-0015
author: claude
labels: [server, agent-ok]
estimate: 3
created: 2026-09-24T12:10:32Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
---

## Description

As the impact resolver, I need typed access to Pando's `code_impact_analysis`, `code_find_symbol` and `code_related_files`. `internal/pando/search.go` wraps only `kb_search_documents`, `code_hybrid_search`, `code_list_projects` and `code_index_project` today.

## Acceptance Criteria

- [x] `internal/pando` gains `ImpactAnalysis(project, symbols)`, `FindSymbol(project, name)` and `RelatedFiles(project, path)` with typed results (symbol, path, line range, depth/score).
- [x] Errors map to the existing `unavailable` classification when Pando is not configured or unreachable.
- [x] Tests against a fake MCP transport cover success, empty results and unavailability.
- [x] The search/Pando section of the docs lists the new wrappers.

## Notes

No dependency on the spec data model; can start immediately. Pando code stays out of `internal/core` and `internal/vault`.
