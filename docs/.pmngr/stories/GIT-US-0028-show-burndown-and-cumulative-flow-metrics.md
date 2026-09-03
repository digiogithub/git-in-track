---
id: GIT-US-0028
type: story
title: Show burndown and cumulative flow metrics
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
author: team
priority: medium
parent: GIT-EP-0007
milestone: GIT-M-0007
estimate: 8
labels: [web, core]
links:
  - kind: blocked_by
    target: GIT-US-0018
  - kind: blocked_by
    target: GIT-US-0021
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

- [ ] Status transitions are reconstructed from git history for a date range.
- [ ] Sprint burndown plots remaining points per day against an ideal line.
- [ ] Cumulative flow shows item counts by status over time.
- [ ] Cycle time, lead time and throughput are computed and explained in the UI.
- [ ] Every metric matches a hand-computed reference on a fixture sprint.
- [ ] History reconstruction on a 5,000-commit repository completes in under 5 seconds.
- [ ] Results are cached and invalidated when new commits arrive.
- [ ] Every chart has an accessible data-table equivalent and is legible in dark mode.
- [ ] Missing or partial history degrades to a stated approximation, never a wrong number.
