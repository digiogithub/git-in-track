---
id: GIT-US-0129
type: story
title: Requirement detail with trace panel
status: backlog
priority: medium
parent: GIT-EP-0027
milestone: GIT-M-0015
author: claude
labels: [web, agent-ok]
estimate: 5
created: 2026-09-24T12:11:34Z
updated: 2026-09-24T12:11:34Z
links:
  - { kind: blocked_by, target: GIT-US-0114 }
  - { kind: blocked_by, target: GIT-US-0127 }
---

## Description

As a reviewer, I want to open one requirement and see its statement, scenarios, status, verification stamp and trace — code symbols, tests with last result, and the stories that implement or modify it.

## Acceptance Criteria

- [ ] Requirement detail view (panel or route `/p/$project/specs/$spec/$req`) renders the block, status control, `verified` stamp and suspect reasons.
- [ ] Trace panel lists code and tests grouped by origin (marker vs `trace:`), with links to stories via `implements`/`modifies`.
- [ ] Editing the block saves only that block with its block `rev`; a conflict shows the per-field diff, as for items.
- [ ] Vitest tests for rendering, save and conflict; docs/05 updated.

## Notes

Browser-only shows the block and edit, trace shows `unavailable`.
