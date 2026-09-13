---
id: GIT-T-0113
type: task
title: Write ADR-034 and update docs/04 for draft sprints and derived status
status: done
priority: medium
parent: GIT-US-0075
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:03Z
updated: 2026-09-13T14:31:22Z
started: 2026-09-13T14:31:15Z
closed: 2026-09-13T14:31:22Z
---

## Description

Write `docs/adr/ADR-034-sprint-status-is-derived-from-dates.md` in the house format, referencing ADR-017's "nothing derived is ever stored" position and listing the negative consequences: two notions of state on one entity, the calendar changing a sprint's status without a write, and sensitivity to the team timezone. Update `docs/04-team-repository.md` §8.2 (optional dates, the draft rule) and §8.4 (the relaxed `E-SPRINT-DATES` and the draft overlap exemption), add the derived-status table, and link the ADR from `docs/adr/README.md` and from `docs/03-data-model.md`.

## Acceptance Criteria

- [x] ADR-034 exists with status, phase, related ADRs and a negative-consequences section, and is linked from the ADR index.
- [x] `docs/04` §8.2 and §8.4 describe optional dates, drafts, the derived-status table and the exemption.
- [ ] `make lint` passes.

## Notes

Docs half closed in a later pass. `docs/04` §8.2 now marks `start` and `end` optional, carries the derived-status table with the timezone price and the purity of `(*Sprint).DerivedStatus(now)`, and adds R-SPR-9 (dates together or neither, drafts) and R-SPR-10 (the listing order and filter). R-SPR-6 names the draft exemption and the escape-hatch sentence `core.SprintOverlapMessage` produces. §8.4 relaxes `E-SPRINT-DATES` to "exactly one date, or `end` < `start`" and records that a draft raises `W-SPRINT-OVERLAP` against nothing.

Written against the code as committed in `cf1cad4`, which is narrower than the ADR in two places, both marked *As built* in the doc: the vault still refuses a dateless sprint on `sprint.create` / `sprint.update` (`checkSprintDates`), and `core.SortSprintsForListing` / `core.FilterSprintsByStatus` have no caller, so `sprint.list` does not order or filter by derived status yet.

`make lint` is not green in the shared working tree: `golangci-lint` reports 9 issues in files other agents are writing right now (`internal/vault/inbox.go`, `internal/youtrack/mapping/`). This pass touched no Go file; `make lint-web` and `make lint-ci` are clean.
