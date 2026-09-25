---
id: GIT-US-0128
type: story
title: Specs page with one row per requirement
status: done
priority: high
parent: GIT-EP-0027
milestone: GIT-M-0015
author: claude
labels: [web, agent-ok]
estimate: 5
created: 2026-09-24T12:11:34Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0111 }
  - { kind: blocked_by, target: GIT-US-0127 }
---

## Description

As a user, I want a `/p/$project/specs` page listing every spec as a collapsible group and every requirement as its own row, so requirements read as separate units with their own status and coverage.

## Acceptance Criteria

- [x] Route `/p/$project/specs` (TanStack Router) with a nav entry; specs grouped, requirement rows showing ref, title, workflow status and a coverage badge (`untested`/`passing`/`failing`/`suspect`, or `unavailable`).
- [x] Filters by status and coverage; a requirement row deep-links to its block anchor.
- [x] Create spec / add requirement actions use the templates.
- [x] Vitest + Testing Library tests for grouping, filters and the unavailable state; docs/05 updated.

## Notes

Decision 1: visually every requirement is a separate unit.
