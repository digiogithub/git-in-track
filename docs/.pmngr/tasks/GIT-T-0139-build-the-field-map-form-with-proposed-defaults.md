---
id: GIT-T-0139
type: task
title: Build the field map form with proposed defaults
status: in_review
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:18:42Z
updated: 2026-09-13T15:05:11Z
started: 2026-09-13T15:05:00Z
---

## Description

Extend the YouTrack settings card with a field-map section: one row per YouTrack State, Priority and Type value with a native `<select>` (`web/src/components/ui/select.tsx`) of local targets, plus pickers for the field names carrying estimation and assignee. On first open, propose defaults by case-insensitive name match and by the `isResolved` flag of state values; leave anything unmatched in a visible "unmapped" state with a warning rather than a silent default.

## Acceptance Criteria

- [ ] Rows render for State, Priority and Type values plus the estimation and assignee field pickers.
- [x] Defaults are proposed on first open and unmapped values are shown as warnings.
- [x] Saving patches the settings endpoint and reflects the `persisted` flag.
- [x] Vitest covers default proposal, the unmapped warning and save; `npm run tokens:check` passes.

## Notes

Blocked on the API for the first criterion, so it is left unticked rather than quietly reinterpreted. The committed surface maps **field names**, not field **values**: `integrations.youtrack.field_map` is a `map[string]string` of git-in-track field → YouTrack custom field name (`internal/config/projectlink.go`), and `GET /api/v1/youtrack/fields` returns `{id, name, type, bundleId, bundleType, canBeEmpty}` with no bundle values. Per-value rows (a YouTrack `State` value mapped onto a workflow status id) need both a value-carrying discovery response and a nested `field_map` shape; neither exists.

What landed instead is `web/src/features/settings/YouTrackFieldMap.tsx`: one native `<select>` row per git-in-track field the companion declares in `gintrackFields`, with case-insensitive name defaults proposed on first open (marked `proposed`, never saved silently), a visible `unmapped` badge, a `missing in YouTrack` warning for a mapping the instance no longer offers, and a save that patches `PATCH /api/v1/youtrack/settings` and reports `persisted`.
