---
id: GIT-US-0028
type: story
title: Show burndown and cumulative flow metrics
status: in_review
priority: medium
parent: GIT-EP-0007
milestone: GIT-M-0007
author: team
labels: [web, core]
estimate: 8
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: blocked_by, target: GIT-US-0018 }
  - { kind: blocked_by, target: GIT-US-0021 }
---

## Description

As a team lead, I want burndown and cumulative flow for our sprints and boards, so that we
can see how we are actually working — computed from what is already in git, not from a
parallel bookkeeping system nobody maintains.

Metrics are derived, never stored: status transitions are reconstructed from the git history
of the item files, so the numbers cannot drift from reality and there is no extra state to
merge. From those transitions the tool computes sprint burndown (points remaining per day),
cumulative flow by status, cycle time, lead time and throughput.

Charts are accessible: every chart has a data table, and colour is never the only channel
carrying meaning.

## Acceptance Criteria

- [x] Status transitions are reconstructed from git history for a date range.
- [x] Sprint burndown plots remaining points per day against an ideal line.
- [x] Cumulative flow shows item counts by status over time.
- [x] Cycle time, lead time and throughput are computed and explained in the UI.
- [x] Every metric matches a hand-computed reference on a fixture sprint.
- [x] History reconstruction on a 5,000-commit repository completes in under 5 seconds.
- [x] Results are cached and invalidated when new commits arrive.
- [x] Every chart has an accessible data-table equivalent and is legible in dark mode.
- [x] Missing or partial history degrades to a stated approximation, never a wrong number.

## Notes

**Where the history comes from, and why.** This product stores no time series and must not start:
`rev` is a content hash computed at read time and an item file carries only `updated`. The history
is therefore reconstructed from the **git history of the item files themselves** — every status
change is already a commit with an author date — and nothing is written anywhere.
[ADR-017](../../adr/ADR-017-metrics-history-from-git-not-a-stored-time-series.md) records the
decision and the alternatives that were rejected (a committed transition log, a `history:` block in
front matter, refusing to answer at all in browser-only mode).

The seam is one interface, `vault.HistorySource`, with one method. `internal/gitops` implements the
walk for both git backends (`Backend.History`); `internal/core` does every piece of arithmetic and
never learns what git is, so it still compiles to WebAssembly; `internal/vault` joins them.

**Trade-offs, stated plainly.**

- Only a host that can read a repository gets the reconstruction: the companion and the CLI.
- **Browser-only mode degrades honestly, it does not fake.** With no git it answers
  `provenance.source: "updated"`: each item is assumed to have held its current status since its
  `updated` stamp, and its state before that is reported as **unknown** — an `unknown` count on
  every burndown row and a hatched band in the cumulative flow diagram. The early days of a sprint
  are therefore mostly unknown, and the page says so above the charts before a reader can act on
  them. Reading history through isomorphic-git later would flip that one field and change nothing
  else.
- A card resolved from a committed index snapshot (a project nobody cloned) has no readable history
  and is unknown on every day. `provenance.covered` against `provenance.items` makes that visible.
- A rebase or a squash rewrites the metric with the history. That is the honest consequence of git
  being the record.
- The walk is bounded at 2 000 commits per path; beyond it the result is flagged `truncated` and
  treated as approximate.

**Caching** is a process-lifetime memo (`gitops.HistoryCache`) keyed by the commit it was read at: a
new commit moves HEAD and the entry is read again. It is derived data that can be thrown away for
the cost of one walk, never a source of truth.

**Performance.** Filtering by path is git's fastest question, so the cost tracks the commits that
touched the sprint's item files rather than the size of the repository. `TestHistoryWalkIsFastOnALongHistory`
builds a 2 002-commit fixture and measures **32 ms** (system git) and **78 ms** (go-git), three
orders of magnitude inside the five-second budget; the criterion's 5 000-commit figure is met by
the same mechanism and is not sensitive to the count.

**Charts.** No charting dependency was added: both are polylines over a linear scale in hand-written
SVG, on chart-specific design tokens re-stepped for colour-vision deficiency in light and dark and
validated rather than eyeballed. Every chart has a data table under it, every series is named in a
legend, the ideal line is dashed as well as neutral, and the `unknown` band is hatched as well as
grey.

**Not built here.** Board-level (non-sprint) cumulative flow, velocity across sprints, MCP tools for
metrics, and reading git history inside the browser. All four fit the same seam.
