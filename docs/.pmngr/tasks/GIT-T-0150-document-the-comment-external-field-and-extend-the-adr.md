---
id: GIT-T-0150
type: task
title: Document the comment external field and extend the ADR
status: todo
priority: medium
parent: GIT-US-0068
milestone: GIT-M-0011
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:58Z
updated: 2026-09-13T13:18:58Z
---

## Description

Update `docs/03-data-model.md` §11 with the comment `external` field and the JSON schema in §18, and extend the `external`-reference ADR written in GIT-EP-0011 to cover comments rather than writing a second ADR. State explicitly that a local delete never deletes remotely. Add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] `docs/03-data-model.md` §11 and the JSON schema in §18 document the field.
- [ ] The existing `external` ADR is extended to comments, including the delete asymmetry.
- [ ] `CHANGELOG.md` has an entry and `make lint` passes.
