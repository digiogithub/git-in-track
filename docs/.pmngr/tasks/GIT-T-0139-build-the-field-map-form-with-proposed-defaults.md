---
id: GIT-T-0139
type: task
title: Build the field map form with proposed defaults
status: done
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:18:42Z
updated: 2026-09-13T17:03:35Z
started: 2026-09-13T15:05:00Z
closed: 2026-09-13T17:03:35Z
---

## Description

Extend the YouTrack settings card with a field-map section: one row per YouTrack State, Priority and Type value with a native `<select>` (`web/src/components/ui/select.tsx`) of local targets, plus pickers for the field names carrying estimation and assignee. On first open, propose defaults by case-insensitive name match and by the `isResolved` flag of state values; leave anything unmatched in a visible "unmapped" state with a warning rather than a silent default.

## Acceptance Criteria

- [x] Rows render for State, Priority and Type values plus the estimation and assignee field pickers.
- [x] Defaults are proposed on first open and unmapped values are shown as warnings.
- [x] Saving patches the settings endpoint and reflects the `persisted` flag.
- [x] Vitest covers default proposal, the unmapped warning and save; `npm run tokens:check` passes.

## Notes

The first criterion was blocked on the API and is now unblocked, so it is ticked
rather than reinterpreted. `integrations.youtrack.field_map` became a nested
shape (`{field, values}` per key, `config.FieldMapping`), and
`GET /api/v1/youtrack/fields` now answers each bundle-backed field with its
allowed values and a top-level `valueMappableFields`.

What landed this pass, on top of the field-name table already there:

- `YouTrackSettings.fieldMap` and the patch are `Record<string,
  YouTrackFieldMapping>`. The companion always writes the object form; the
  scalar form is still read, so an older answer or a hand-written `project.yaml`
  still loads. **This was also a live bug**: the parser read every entry as a
  string, so after the wire shape changed the whole table came back empty.
- A value table per value-mappable field whose mapped YouTrack field has a
  bundle. Local targets are this project's own workflow statuses, its declared
  priorities and the four importable item types — read through `useProject` and
  `readProjectSchema`, never hard-coded.
- Value defaults are proposed by normalized name match in both directions
  ("In Progress" ↔ `in_progress`), with `isResolved === true` as the single
  fallback for a state. An **absent** flag proposes nothing: silence is not a
  claim that a value closes an issue.
- An archived value is listed with a badge rather than hidden — it is still on
  the old issues, which are exactly the ones an import reads — and a value
  mapped onto something this project no longer declares is kept and flagged,
  the same rule the field rows already followed.
- Pointing a field somewhere else drops the values read off the old bundle, and
  a field with no value mapping is written flat, with no empty `values` key.
- The instance's per-value colours are deliberately not carried into the
  provider type: a colour in a component is a bug (docs/13 §1).

Eight new Vitest cases; `docs/05-web-app.md` §3.1 documents the two questions
the table answers and the three rules that keep it honest.
