---
id: GIT-T-0099
type: task
title: Build the query bar with presets and multi-select
status: done
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:17:47Z
updated: 2026-09-13T15:47:57Z
started: 2026-09-13T15:47:45Z
closed: 2026-09-13T15:47:57Z
---

## Description

Add `web/src/features/youtrack/ImportQueryBar.tsx`: a debounced ARIA combobox over the search hook, copying the pattern of `web/src/components/editor/ItemPicker.tsx:29-163` (200 ms debounce, `role="combobox"`, `role="listbox"`, blur grace), with preset chips for epics, stories, tasks, versions and unresolved, multi-select with a running count, and an "already imported" marker showing the gintrack id for results whose `linked` is set.

## Acceptance Criteria

- [x] Typing is debounced and results render in an accessible listbox navigable by keyboard.
- [x] Preset chips switch the preset and reset paging; multi-select keeps a visible count.
- [x] Results with `linked` show the gintrack id and stay selectable for an update.
- [x] Vitest covers debounce, keyboard navigation, preset switching and selection; `npm run tokens:check` passes.

## Notes

Built on the generic `web/src/components/ui/combobox.tsx` that GIT-EP-0011 extracted from `ItemPicker`, rather than on a second copy of its mechanics.

The 200 ms debounce lives in `useYouTrackIssueSearch` and the picker is given `debounceMs={0}`: a preset chip and a typed word feed the same query, only one of them comes from a keyboard, and two timers would have made one keystroke cost 400 ms.
