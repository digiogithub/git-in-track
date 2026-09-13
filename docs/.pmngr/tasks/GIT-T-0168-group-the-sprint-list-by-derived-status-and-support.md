---
id: GIT-T-0168
type: task
title: Group the sprint list by derived status and support dateless drafts
status: done
priority: medium
parent: GIT-US-0089
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:19:23Z
updated: 2026-09-13T16:39:22Z
started: 2026-09-13T16:39:11Z
closed: 2026-09-13T16:39:22Z
---

## Description

Regroup `web/src/features/boards/SprintList.tsx` by derived status — Current, Upcoming, Draft, Completed — with a "Draft" badge and a hint that adding dates schedules the sprint. Make the dates optional in `NewSprintDialog.tsx` and render a `sprint_overlap` refusal with the other sprint's name and range plus the "remove the dates to keep it a draft" escape hatch. The error code already exists in `web/src/api/provider.ts:824`.

## Acceptance Criteria

- [x] The list groups in the order current → upcoming → draft → completed and marks drafts with a badge.
- [x] A sprint can be created with no dates, and one created with overlapping dates shows the named refusal and the escape hatch.
- [x] Vitest covers the grouping, the badge and the overlap error path.

## Notes

The grouping rule lives in `sprint-grouping.ts` rather than inside the
component, so it is testable on its own and `SprintList` stays a component.

`SprintPanel` keeps two badges, because the lifecycle `state` (planned, active,
closed) and the derived `status` (draft, upcoming, current, completed) are
different facts about the same sprint: an active sprint whose end date has
passed is `completed` to a reader and still `active` in the file.

A half-dated input — one date without the other — is refused in the form: it is
neither a schedule nor a draft.
