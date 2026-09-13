---
id: GIT-T-0054
type: task
title: Extract a generic Combobox from ItemPicker into components/ui
status: todo
priority: medium
parent: GIT-US-0055
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:16:39Z
updated: 2026-09-13T13:16:39Z
---

## Description

Move the generic typeahead mechanics of `web/src/components/editor/ItemPicker.tsx:29-163` into `web/src/components/ui/combobox.tsx`: the `role="combobox"` input with `aria-expanded`, `aria-controls` and `aria-autocomplete="list"`, the `role="listbox"` popup, the 200 ms debounce, keyboard navigation and the 150 ms blur grace. The new component takes items, a render function, a query callback and selection handlers. Refactor `ItemPicker` to use it with no visible behaviour change.

## Acceptance Criteria

- [ ] `Combobox` is generic over the item type and carries no item- or YouTrack-specific logic.
- [ ] `ItemPicker` behaves identically, with the same ARIA attributes and debounce.
- [ ] Vitest covers keyboard navigation, selection, blur close and the debounce for the extracted component.
