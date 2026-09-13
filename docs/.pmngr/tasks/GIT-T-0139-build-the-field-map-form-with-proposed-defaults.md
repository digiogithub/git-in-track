---
id: GIT-T-0139
type: task
title: Build the field map form with proposed defaults
status: todo
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:18:42Z
updated: 2026-09-13T13:18:42Z
---

## Description

Extend the YouTrack settings card with a field-map section: one row per YouTrack State, Priority and Type value with a native `<select>` (`web/src/components/ui/select.tsx`) of local targets, plus pickers for the field names carrying estimation and assignee. On first open, propose defaults by case-insensitive name match and by the `isResolved` flag of state values; leave anything unmatched in a visible "unmapped" state with a warning rather than a silent default.

## Acceptance Criteria

- [ ] Rows render for State, Priority and Type values plus the estimation and assignee field pickers.
- [ ] Defaults are proposed on first open and unmapped values are shown as warnings.
- [ ] Saving patches the settings endpoint and reflects the `persisted` flag.
- [ ] Vitest covers default proposal, the unmapped warning and save; `npm run tokens:check` passes.
