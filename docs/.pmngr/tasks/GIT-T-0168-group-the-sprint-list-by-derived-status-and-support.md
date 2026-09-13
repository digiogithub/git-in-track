---
id: GIT-T-0168
type: task
title: Group the sprint list by derived status and support dateless drafts
status: todo
priority: medium
parent: GIT-US-0089
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:19:23Z
updated: 2026-09-13T13:19:23Z
---

## Description

Regroup `web/src/features/boards/SprintList.tsx` by derived status — Current, Upcoming, Draft, Completed — with a "Draft" badge and a hint that adding dates schedules the sprint. Make the dates optional in `NewSprintDialog.tsx` and render a `sprint_overlap` refusal with the other sprint's name and range plus the "remove the dates to keep it a draft" escape hatch. The error code already exists in `web/src/api/provider.ts:824`.

## Acceptance Criteria

- [ ] The list groups in the order current → upcoming → draft → completed and marks drafts with a badge.
- [ ] A sprint can be created with no dates, and one created with overlapping dates shows the named refusal and the escape hatch.
- [ ] Vitest covers the grouping, the badge and the overlap error path.
