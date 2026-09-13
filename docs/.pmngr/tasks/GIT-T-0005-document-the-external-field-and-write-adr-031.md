---
id: GIT-T-0005
type: task
title: Document the external field and write ADR-031
status: done
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 2
created: 2026-09-13T13:15:00Z
updated: 2026-09-13T14:03:13Z
started: 2026-09-13T14:03:06Z
closed: 2026-09-13T14:03:13Z
---

## Description

Update `docs/03-data-model.md`: add `external` to the front-matter field table, place it in the §3.2 canonical key order and extend the §18 JSON Schema. Write `docs/adr/ADR-031-external-references.md` recording why this is a first-class field rather than a custom field or a link kind, what the accepted shape is, and its negative consequences. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] The field table, key order and JSON Schema all describe the same shape the code implements.
- [x] ADR-031 follows the template in `docs/adr/README.md`, including a filled-in negative consequences section.
- [ ] `make lint` passes and `CHANGELOG.md` records the data-model change.

## Notes

Docs landed: `docs/03-data-model.md` §3.2 (key order, items and comments), §7.1/§10.1/§11.2 field
tables, a new §12.5 "External references" with rules R-EXT-1..7, and §18 (`external` and `inbox`
`$defs` plus the story schema properties). `docs/adr/ADR-031-external-references.md` is Accepted,
phase 7, with six negative consequences and six rejected alternatives; it is linked from
`docs/adr/README.md`.

Last criterion is **not** ticked: `CHANGELOG.md` is outside the file set this agent was allowed to
touch in this wave (other agents are editing it concurrently), so the changelog entry is left for
the coordinator. Text to add under Unreleased → Added:
"core: `external` references on items, comments and KB pages, indexed by `(system, id)` (ADR-031)".
