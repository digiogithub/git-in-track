---
id: GIT-US-0075
type: story
title: Sprint dates become optional and status becomes derived from them
status: in_progress
priority: high
parent: GIT-EP-0017
milestone: GIT-M-0012
author: mcp
labels: [core, docs]
estimate: 8
created: 2026-09-13T13:13:49Z
updated: 2026-09-13T14:02:24Z
started: 2026-09-13T14:02:24Z
---

## Description

As a team planning ahead, I want to create a sprint without dates and have the product call it a draft, and I want a dated sprint's status to follow the calendar by itself, so that nobody has to remember to flip `state: planned` to `active` and the board can never disagree with the date on the wall.

Today `core.Sprint` (`internal/core/sprint.go:67-98`) stores `state: planned|active|closed` and `Validate` (`:300`) makes `start` and `end` mandatory (`E-SPRINT-DATES`). This story adds a derived status alongside the stored one: `SprintStatus` = `draft|upcoming|current|completed`, computed by a pure `func (s *Sprint) DerivedStatus(now time.Time) SprintStatus` — `draft` when either date is absent, `current` when `start <= today <= end`, `upcoming` when `start > today`, `completed` when `end < today` or `state == closed`. It is exposed on `SprintSummary` (`internal/core/sprintview.go:38-59`) and never written to the file. The stored `state` keeps its existing meaning — it is the record of the explicit `sprint.start` and `sprint.close` acts (R-SPR-5, R-SPR-3) — and `closed` always wins over the calendar.

Validation changes with it. `start` and `end` become "both or neither": one alone is `E-SPRINT-DATES`, neither is a draft and is legal. The no-overlap rule, today a write-time check in `checkSprintDates` (`internal/vault/sprint.go:748`) plus the `W-SPRINT-OVERLAP` warning (docs/04 §8.4), is extended so a draft is exempt — removing the dates is the documented escape hatch when a range collides — and the refusal message names the other sprint and its dates, as R-SPR-6 already requires. Listing gains ordering and filtering by derived status: `SprintListParams` (`internal/vault/sprint.go`) takes `status []SprintStatus` and the default order becomes current → upcoming → draft → completed.

## Acceptance Criteria

- [ ] `core.SprintStatus` with `draft|upcoming|current|completed` and a pure `DerivedStatus(now)` that never reads a clock itself; `state: closed` always derives `completed`.
- [ ] `start` and `end` are optional but must be given together; a sprint with neither is a valid draft and `E-SPRINT-DATES` fires only for exactly one of the two, or for `end < start`.
- [ ] A draft is exempt from the overlap rule; a dated create or date change that would overlap another sprint of the same board is refused with `sprint_overlap` and a message naming the other sprint and its range.
- [ ] `SprintSummary` and every REST and MCP sprint payload carry the derived status; nothing writes it to the file.
- [ ] Sprint listings can filter by derived status and default to the order current → upcoming → draft → completed.
- [ ] `docs/04-team-repository.md` §8.2 and §8.4 are updated (optional dates, the draft exemption, the derived status table) and `docs/03-data-model.md` cross-references it.
- [ ] `docs/adr/ADR-034-sprint-status-is-derived-from-dates.md` is written, extends ADR-017's "nothing derived is ever stored" line, and lists its negative consequences (two notions of state, timezone sensitivity).
- [ ] `go test -race ./internal/core/... ./internal/vault/...` covers each derived status at boundary days, both-or-neither dates and the draft overlap exemption; `make wasm` still builds.

## Notes

Existing code: `internal/core/sprint.go:24-37` (`SprintState`), `:67-98` (`Sprint`), `:165-198` (`TotalDays`/`RemainingDays`), `:189` (`Overlaps`), `:300+` (`Validate`), `internal/core/sprintview.go:38-59` (`SprintSummary`), `internal/vault/sprint.go:424` (`UpdateSprint`), `:748` (`checkSprintDates`), `:765` (`checkNoActiveSprint`). Rules R-SPR-5 and R-SPR-6 are at `docs/04-team-repository.md` §8.2.

Dates are day-granular in the team timezone (`team.yaml timezone`, docs/04 §3); `DerivedStatus` must take the already-localised `now` from the caller — `internal/core` is WASM-clean and must not resolve a timezone database.

Plane's equivalent: `apps/api/plane/app/views/cycle/base.py` (the `Case` annotation) and `packages/utils/src/cycle.ts` L21-42 — derived everywhere, stored nowhere.

Do NOT remove or repurpose the stored `state` field; existing sprint files must keep parsing unchanged. Do NOT introduce a separate Cycle entity — decision 9 of the planning brief is that cycles extend the sprint.
