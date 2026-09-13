---
id: GIT-T-0065
type: task
title: Add the field-mapping section to YouTrackCard
status: todo
priority: medium
parent: GIT-US-0055
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:16:54Z
updated: 2026-09-13T13:16:54Z
---

## Description

Add a field-mapping section to the card that loads the YouTrack custom fields for the selected project via `listYouTrackFields` and lets the user map gintrack `status`, `priority`, `type`, `assignee` and `estimate` onto real YouTrack field names, saving into `integrations.youtrack.field_map`. Validate client side in the spirit of `features/editor/front-matter.ts:273`, flagging a mapping to a field that no longer exists rather than silently dropping it.

## Acceptance Criteria

- [ ] The mapping UI offers the project's real field names and saves into `field_map`.
- [ ] A mapping pointing at a missing field is shown as a warning and is not silently discarded.
- [ ] Changing the project reloads the field list and clears stale mappings only after confirmation.
- [ ] Vitest covers the load, map, warn and save paths against the fake provider.
