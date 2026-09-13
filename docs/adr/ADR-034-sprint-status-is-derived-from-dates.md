# ADR-034 — A sprint's status is derived from its dates, and a closed sprint freezes one snapshot

- **Status:** Accepted
- **Date:** 2026-09-13
- **Phase:** 8 (Inbox and cycles)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-013](ADR-013-board-card-ordering.md), [ADR-017](ADR-017-metrics-history-from-git-not-a-stored-time-series.md)
- **Implements:** `GIT-US-0075` — Sprint dates become optional and status becomes derived from them; `GIT-US-0080` — Closing a sprint freezes a progress snapshot into its file

## Context

A sprint file carries `state: planned | active | closed`. It is written by hand,
or by the explicit `sprint.start` and `sprint.close` acts (R-SPR-5, R-SPR-3),
and it carries a `start` and an `end` that were, until now, mandatory.

Two things went wrong with that, and they are the same thing seen twice.

**The stored state and the calendar disagree.** A sprint whose `end` was
yesterday still says `active` until somebody remembers to write the file. A
sprint created for next month says `planned`, which is also what a sprint
created for last month says. The board, the listing and the header each had to
decide for themselves whether to believe the field or the dates, and they did
not always decide the same way. Nothing in the product reconciled them, because
there was nothing to reconcile them *to*.

**Planning ahead was impossible without lying.** `start` and `end` were required
(`E-SPRINT-DATES`), and two sprints of one board may not cover a common day.
Sketching the next three sprints therefore meant inventing three date ranges
that do not collide, before anybody knows when the work starts. The only way to
park a sprint was to give it dates far enough in the future that nothing else
reached them.

Plane solves the first problem the same way, and it is worth naming because the
shape is not obvious from the outside: a cycle's status there is a `Case`
annotation computed in the query (`apps/api/plane/app/views/cycle/base.py`) and
the same four-branch rule again in the front end
(`packages/utils/src/cycle.ts`). It is derived at every read and stored nowhere.

There is a third force, pulling the other way, and it belongs in the same
record. ADR-017 decided that **no time series is ever stored**: a burndown is a
function of the item files, and a stored copy would be a second truth that has
to be kept correct, merged and explained. That decision still holds for every
open sprint. It stops holding the moment a sprint closes, and the amendment
below says why.

## Decision

### A sprint's status is derived, never stored

`core.SprintStatus` is `draft | upcoming | current | completed`, and
`(*Sprint).DerivedStatus(now time.Time)` computes it:

| Derived status | When |
|---|---|
| `completed` | `state: closed`, whatever the dates say; or `end` is before today |
| `draft` | the sprint carries no dates |
| `upcoming` | `start` is after today |
| `current` | `start <= today <= end`, both ends inclusive |

The stored `state` keeps its existing meaning and its existing spelling. It is
the record of the two explicit acts, and **`closed` always wins over the
calendar** — an explicit close is a fact about the sprint, and a date cannot
argue with it. A closed sprint with no dates at all is `completed`, not a draft.

`DerivedStatus` takes `now` as a parameter. `internal/core` compiles to
WebAssembly (ADR-003) and resolves no timezone database, so the caller localises
the instant to the team timezone (doc 04 §3) and passes the result in. Nothing
in the model reads a clock.

The value travels on `SprintSummary` and therefore through every REST and MCP
payload. It is never written to a file: `SerializeSprint` has no `status` key
and will not grow one.

### Dates are optional, both or neither

`start` and `end` are given together or not at all. Exactly one of the two, or
an `end` before the `start`, stays `E-SPRINT-DATES`. A sprint with neither is a
**draft**: a valid, legal, listable sprint that has a goal and a scope and no
place on the calendar yet.

A draft is **exempt from the no-overlap rule**. Two dated sprints of one board
still may not share a day, and a create or a date change that would collide is
refused with `sprint_overlap` and a message naming the other sprint, its range
and the escape hatch — **remove the dates and it stays a draft**. That is the
whole point of the exemption: there is now a way to say "later" without inventing
a date. On disk the collision remains the `W-SPRINT-OVERLAP` warning (doc 04
§8.4): validation describes, a write decides (R-SPR-6).

Listings order by derived status — `current`, `upcoming`, `draft`, `completed` —
ties broken by start date and then by id, and can filter on it.

### Cycles are sprints

A cycle is a sprint with optional dates and a derived status. There is no new
item type, no new file kind and no parallel entity. Everything above is the
whole of it.

## Amendment — a closed sprint freezes exactly one snapshot

ADR-017's position is unchanged for every sprint that is still open, and this
section is the one exception to it, recorded here rather than in a separate ADR
because it is the same decision seen from the other end.

**Why the reasoning inverts at the close.** ADR-017 refused a stored time series
on the grounds that it would be a *second* truth: the numbers are recomputable
from the item files at any time, so storing them can only create something that
disagrees with the source. After a sprint closes, that premise fails. The items
leave the scope — carried to the next sprint, sent back to the backlog, or
simply worked on afterwards — and their files are rewritten. The sprint's
numbers stop being recomputable from the current state at all, and a walk of git
history is then the *only* way to answer a question about it, which requires a
clone of every project the sprint touched and gets slower every year.

So `sprint.close` writes a `snapshot` block into the sprint file, in the same
rev-checked write that sets `state: closed`: `version`, `closed_at`, `totals`,
`by_status`, `by_assignee`, `by_label`, a `burndown` series, and the
`MetricsProvenance` of the history it froze. `BuildSprintSnapshot` computes it,
purely, in `internal/core`, from the view and metrics the caller already holds.

Three rules keep it from becoming the thing ADR-017 rejected:

1. **Written exactly once.** Closing a sprint that already carries a snapshot
   leaves it alone. It is never recomputed, never repaired and never refreshed
   on a later read: it is a record of a moment, not a cache.
2. **Never for an open sprint.** Nothing stores a series before the close.
3. **Honest about itself.** The frozen provenance says where the numbers came
   from, and a snapshot taken where no git history could be read — browser-only
   mode — is marked `approximate` rather than passed off as a reconstruction.
   A reader shown the stored block is told it was frozen at the close.

Reads then prefer it: `sprint.metrics` and the burndown endpoint return the
stored block when one is present and only walk history when it is absent.

This is not the stored time series ADR-017 rejected. That one would have been
maintained — appended to as work happened, merged when two people wrote it,
repaired when it drifted. This one is written once at a moment when the truth is
about to become unreachable, and is thereafter immutable. It is closer to a
retro than to a cache.

## Consequences

**Positive**

- The status on the board can never disagree with the date on the wall, because
  there is no second copy to disagree with.
- Nobody has to remember to flip a field. A sprint becomes current on its start
  date and completed the day after its end, with no write.
- A team can plan five sprints ahead as drafts, order them, and give them dates
  when the dates are known.
- Removing the dates is a real escape hatch out of an overlap, so the no-overlap
  rule can stay strict without blocking planning.
- Velocity history for a closed sprint is readable from the Markdown alone,
  years later, without a clone of every project and without a history walk.
- Closed sprints stop getting slower to read as the repository grows.

**Negative**

- **Two notions of state on one entity.** `state` and the derived status both
  exist, they use different words, and a reader has to learn which is which.
  The refusal to rename or repurpose `state` is what keeps existing files
  parsing, and it is the price.
- **The calendar changes a sprint's status without a write.** Nothing in git
  records the transition from `upcoming` to `current`; the file is byte-identical
  before and after. A `git log` cannot tell you when a sprint started unless
  somebody ran `sprint.start`.
- **Timezone sensitivity.** "Today" is a day in the team timezone, resolved by
  the caller. Two clients that disagree about the team timezone — or a caller
  that passes a UTC instant where a local one was meant — will disagree about a
  sprint's status for a few hours around midnight.
- **A stored figure that can disagree with a later reading of history.** A
  snapshot frozen in browser-only mode, or frozen before a rebase rewrote the
  history, will not match what a git walk says today. The provenance explains
  it; it does not remove it.
- **The sprint file grows by one point per sprint day.** A two-week sprint
  freezes fourteen burndown rows. The series is capped at one point per sprint
  day and holds only the observed days, so the growth is bounded and small, but
  a closed sprint file is no longer a handful of lines.
- A snapshot cannot be corrected. A close run against a half-synced workspace
  freezes half-synced numbers, and the only remedy is to say so in the retro.

**Neutral**

- A draft sorts between `upcoming` and `completed` in the default order, which
  is a product judgement rather than a derivation: what is running comes first,
  then what is coming, then what is being planned, then what is over.
- Nothing forces a team to use drafts. A workflow that always dates its sprints
  up front sees no change at all.

## Alternatives considered

- **Storing the derived status as a fourth `state`.** Rejected: it is the
  problem restated. A stored `current` would go stale at midnight exactly as
  `active` does today, and the product would then have three fields to
  reconcile instead of two.
- **Replacing `state` with the derived status.** Rejected: the explicit start
  and close are real events with real consequences (`committed` is frozen at the
  start, the snapshot at the close), and no date range can express them. It
  would also break every existing sprint file.
- **Keeping the dates mandatory and adding a `draft: true` flag.** Rejected: a
  second way to say the same thing, and it leaves the invented dates in the file
  where they can collide and mislead.
- **Letting dated sprints overlap freely instead of exempting drafts.**
  Rejected: the no-overlap rule is what makes "the current sprint" a
  well-defined phrase. The exemption removes the reason teams wanted to break
  the rule without breaking it.
- **Recomputing the snapshot on every read of a closed sprint.** Rejected: it is
  exactly the drift ADR-017 warns about, and it would silently rewrite history
  every time an item that used to be in scope was edited.
- **A separate `snapshots/` file or a `.pmngr/metrics/` folder.** Rejected: a
  second file to keep beside the sprint, to merge, and to lose. The numbers
  belong to the sprint and live in it.
- **A new `cycle` item type.** Rejected: a cycle is a sprint with optional dates.
  A parallel entity would duplicate the board scoping, the carry rules, the
  retro link and the metrics, and force every surface to handle both.
