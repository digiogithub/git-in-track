---
id: GIT-T-0005
type: task
title: Document the external field and write ADR-031
status: todo
priority: medium
parent: GIT-US-0044
milestone: GIT-M-0011
author: mcp
labels: [docs]
estimate: 2
created: 2026-09-13T13:15:00Z
updated: 2026-09-13T13:15:00Z
---

## Description

Update `docs/03-data-model.md`: add `external` to the front-matter field table, place it in the §3.2 canonical key order and extend the §18 JSON Schema. Write `docs/adr/ADR-031-external-references.md` recording why this is a first-class field rather than a custom field or a link kind, what the accepted shape is, and its negative consequences. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The field table, key order and JSON Schema all describe the same shape the code implements.
- [ ] ADR-031 follows the template in `docs/adr/README.md`, including a filled-in negative consequences section.
- [ ] `make lint` passes and `CHANGELOG.md` records the data-model change.
