---
id: GIT-T-0133
type: task
title: Document the snapshot block and amend the metrics ADR
status: done
priority: medium
parent: GIT-US-0080
milestone: GIT-M-0012
author: mcp
labels: [docs, agent-ok]
estimate: 2
created: 2026-09-13T13:18:34Z
updated: 2026-09-13T14:31:44Z
started: 2026-09-13T14:31:34Z
closed: 2026-09-13T14:31:44Z
---

## Description

Document the `snapshot` block in `docs/04-team-repository.md` §8.2 (field table plus a worked example) and the snapshot-wins rule in §12, cross-reference it from `docs/03-data-model.md`, and add an amendment section to ADR-034 explaining why a closed sprint is the one place a derived number is stored — the items leave the scope, so the numbers stop being recomputable — and what that costs: a stored figure that can disagree with a later reading of history, and a file that grows by one point per sprint day.

## Acceptance Criteria

- [x] `docs/04` §8.2 documents every snapshot field and shows an example; §12 states the snapshot-wins rule.
- [x] ADR-034 carries the amendment with its negative consequences and cites ADR-017.
- [ ] `make lint` passes.

## Notes

Docs half closed in a later pass. `docs/04` §8.2 adds the `snapshot` row to the front-matter table, a field table covering `version`, `closed_at`, `totals`, `by_status`, `by_assignee`, `by_label`, `burndown` and `provenance`, the canonical emitted YAML, the key-order rule (between `retro` and `created`, unmodelled keys sorted after `provenance`) and R-SPR-11 (written exactly once, never for an open sprint). §12.1 gains R-MET-12, the snapshot-wins rule, with the reason the ADR-017 reasoning inverts at the close and the prices it carries; §12.2 gains the `snapshot` provenance row.

Written against `cf1cad4`, and the doc says so where the code is narrower than the design: `core.BuildSprintSnapshot` has no caller — `vault.CloseSprint` does not build a snapshot — and `sprint.metrics` does not read one back, so today a block is only parsed, preserved on rewrite and reported on the sprint summary.

`make lint` is not green in the shared working tree: `golangci-lint` reports 9 issues in files other agents are writing right now (`internal/vault/inbox.go`, `internal/youtrack/mapping/`). This pass touched no Go file; `make lint-web` and `make lint-ci` are clean.
