---
id: GIT-T-0238
type: task
title: Review the spec-driven development overview before planning Phase 11
status: done
priority: high
assignees: [team]
author: mcp
labels: [docs, core, mcp]
created: 2026-09-24T11:49:18Z
updated: 2026-09-25T11:15:00Z
started: 2026-09-24T12:13:51Z
closed: 2026-09-25T11:15:00Z
---

## Description

Human review gate. An agent analysed the spec-driven development landscape (Spec Kit, Kiro, OpenSpec, Tessl, BMAD, Agent OS, Traycer, Doorstop, StrictDoc). It proposed adding a spec layer to git-in-track with these parts:

- `spec` and `requirement` items
- traceability from requirements to code and tests
- test-result ingest
- Doorstop-style suspect detection
- a three-tier impact query (direct trace, then Pando `code_impact_analysis`, then semantic fallback), exposed as token-budgeted MCP tools

The overview is in [[research/2026-09-24-spec-driven-development-overview]] (`docs/research/2026-09-24-spec-driven-development-overview.md`).

No milestone, epic or story exists yet. They are created only after this task is resolved.

## Acceptance Criteria

- [x] The reviewer answers the open questions in §7 of the overview (granularity, code anchors, requirement lifecycle, grammar strictness, first-cut scope, who implements E1, naming), as a comment on this task or as edits to the overview.
- [x] The reviewer approves, trims or reshapes the epic list in §4.
- [x] The agent turns the validated overview into the Phase 11 milestone (GIT-M-0015) with its epics and stories through the MCP tools, and links them back here.
- [x] ADR-037 is drafted as the first story of E1, because the data-model change is a human-only area.

## Notes

- The data-model part (new item types, link kinds, the `trace` and `verified` fields) needs an ADR and an update to docs/03, per AGENTS.md.
- Prerequisite found during analysis: the stdio `gintrack mcp` never installs the semantic searcher, so `search_semantic` over stdio always answers `unavailable`. Only `gintrack serve` wires it up.
- The next free IDs at analysis time were M-0015, EP-0022 and US-0104. The task counter was at 237, and IDs 0228–0237 are burned.
- 2026-09-24: decisions recorded in the overview §7 (status: validated) and in the comment thread. Backlog created: GIT-M-0015, epics GIT-EP-0022..0029 (milestone) and GIT-EP-0030 (deferred importers, no milestone), stories GIT-US-0104..0140. ADR-037 is story GIT-US-0104.
