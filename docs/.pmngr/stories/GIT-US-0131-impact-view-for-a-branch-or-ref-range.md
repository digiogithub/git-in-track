---
id: GIT-US-0131
type: story
title: Impact view for a branch or ref range
status: in_review
priority: medium
parent: GIT-EP-0027
milestone: GIT-M-0015
author: claude
labels: [web, agent-ok]
estimate: 5
created: 2026-09-24T12:11:34Z
updated: 2026-09-24T21:54:40Z
links:
  - { kind: blocked_by, target: GIT-US-0120 }
  - { kind: blocked_by, target: GIT-US-0127 }
---

## Description

As a reviewer of a PR, I want to pick a base and head (or the worktree) and see which requirements the change affects, grouped by tier, with reasons, status and suspect flags.

## Acceptance Criteria

- [ ] Impact view (`/p/$project/specs/impact`) with base/head pickers (branches, recent commits, worktree) in companion mode.
- [x] Hits grouped by tier (direct, transitive, candidate with score), each with reason, coverage status and suspect; candidates visually distinct from certain hits.
- [x] `unavailable` states: whole view in browser-only mode, tiers 2–3 when Pando is missing.
- [x] Vitest tests for grouping and unavailable states; docs/05 updated.

## Notes

Uses the same report as `spec_impact`.
