---
id: GIT-T-0029
type: task
title: Document the inbox block and write ADR-033
status: todo
priority: medium
parent: GIT-US-0051
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:15:58Z
updated: 2026-09-13T13:15:58Z
---

## Description

Update `docs/03-data-model.md`: add the `inbox` block to the item front-matter field table, place it in the canonical key order at §3.2 (line 185-198), add the `triage` category to the workflow section §6 (line 522-525) and extend the JSON Schema at §18. Then write `docs/adr/ADR-033-inbox-is-a-reserved-triage-status-category.md` in the house format (`docs/adr/README.md`): context, decision, and mandatory negative consequences — a fifth category every consumer must now handle, the risk of a project that declares no triage status silently having no inbox, and snooze expiry being a query-time rule with no notification. Link it from `docs/adr/README.md`.

## Acceptance Criteria

- [ ] `docs/03-data-model.md` documents the block, the key order, the category and the schema, and the schema example validates.
- [ ] `docs/adr/ADR-033-*.md` exists with status Accepted, a phase, related ADRs and a negative-consequences section, and is linked from the ADR index.
- [ ] `make lint` passes (including the docs checks).
