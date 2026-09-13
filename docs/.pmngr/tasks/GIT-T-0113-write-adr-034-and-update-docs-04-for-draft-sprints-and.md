---
id: GIT-T-0113
type: task
title: Write ADR-034 and update docs/04 for draft sprints and derived status
status: todo
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:03Z
updated: 2026-09-13T13:18:03Z
---

## Description

Write `docs/adr/ADR-034-sprint-status-is-derived-from-dates.md` in the house format, referencing ADR-017's "nothing derived is ever stored" position and listing the negative consequences: two notions of state on one entity, the calendar changing a sprint's status without a write, and sensitivity to the team timezone. Update `docs/04-team-repository.md` §8.2 (optional dates, the draft rule) and §8.4 (the relaxed `E-SPRINT-DATES` and the draft overlap exemption), add the derived-status table, and link the ADR from `docs/adr/README.md` and from `docs/03-data-model.md`.

## Acceptance Criteria

- [ ] ADR-034 exists with status, phase, related ADRs and a negative-consequences section, and is linked from the ADR index.
- [ ] `docs/04` §8.2 and §8.4 describe optional dates, drafts, the derived-status table and the exemption.
- [ ] `make lint` passes.
