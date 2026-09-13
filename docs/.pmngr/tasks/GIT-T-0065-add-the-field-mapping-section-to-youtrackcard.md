---
id: GIT-T-0065
type: task
title: Add the field-mapping section to YouTrackCard
status: done
priority: medium
parent: GIT-US-0055
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:16:54Z
updated: 2026-09-13T15:04:58Z
started: 2026-09-13T15:04:45Z
closed: 2026-09-13T15:04:58Z
---

## Description

Add a field-mapping section to the card that loads the YouTrack custom fields for the selected project via `listYouTrackFields` and lets the user map gintrack `status`, `priority`, `type`, `assignee` and `estimate` onto real YouTrack field names, saving into `integrations.youtrack.field_map`. Validate client side in the spirit of `features/editor/front-matter.ts:273`, flagging a mapping to a field that no longer exists rather than silently dropping it.

## Acceptance Criteria

- [x] The mapping UI offers the project's real field names and saves into `field_map`.
- [x] A mapping pointing at a missing field is shown as a warning and is not silently discarded.
- [x] Changing the project reloads the field list and clears stale mappings only after confirmation.
- [x] Vitest covers the load, map, warn and save paths against the fake provider.

## Notes

`YouTrackFieldMap.tsx` renders one row per key of `gintrackFields` as the companion declares them (`config.FieldMapKeys`), so the git-in-track half of the vocabulary is never hard-coded in the frontend. The field list is a TanStack Query keyed on the project, so changing the project refetches; a stale mapping stays selected as a `(missing)` option behind a warning and is cleared only by the explicit button.
