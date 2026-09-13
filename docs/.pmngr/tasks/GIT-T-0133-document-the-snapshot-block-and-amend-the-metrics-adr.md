---
id: GIT-T-0133
type: task
title: Document the snapshot block and amend the metrics ADR
status: todo
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:34Z
updated: 2026-09-13T13:18:34Z
---

## Description

Document the `snapshot` block in `docs/04-team-repository.md` §8.2 (field table plus a worked example) and the snapshot-wins rule in §12, cross-reference it from `docs/03-data-model.md`, and add an amendment section to ADR-034 explaining why a closed sprint is the one place a derived number is stored — the items leave the scope, so the numbers stop being recomputable — and what that costs: a stored figure that can disagree with a later reading of history, and a file that grows by one point per sprint day.

## Acceptance Criteria

- [ ] `docs/04` §8.2 documents every snapshot field and shows an example; §12 states the snapshot-wins rule.
- [ ] ADR-034 carries the amendment with its negative consequences and cites ADR-017.
- [ ] `make lint` passes.
