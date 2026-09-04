---
id: DEMO-TEAM-R-0001
type: retro
title: Sprint 1 Retrospective
sprint: DEMO-TEAM-S-0001
board: demo-scrum
date: 2026-09-07
facilitator: marta
participants: [jose, marta]
state: closed
votes_per_person: 3
themes:
  - id: t1
    title: Pairing on the payment sandbox paid off
    category: went_well
    notes: [n1]
  - id: t2
    title: Address validation keeps blocking the checkout
    category: to_improve
    notes: [n2, n3]
  - id: t3
    title: Is the stale snapshot badge on website cards useful or noise?
    category: puzzle
    notes: [n4]
votes:
  t1: [jose]
  t2: [jose, marta]
  t3: [marta]
actions:
  - id: a1
    title: Finish address validation before the checkout work resumes
    owner: jose
    due: 2026-09-11
    theme: t2
    task: DEMO/DEMO-T-0001
    status: promoted
  - id: a2
    title: Agree a 30-minute cap on the daily sync
    owner: marta
    due: 2026-09-07
    theme: t2
    status: done
    note: Team process change; nothing to build.
  - id: a3
    title: Decide whether the hero copy needs a second review pass
    owner: marta
    due: 2026-09-18
    theme: t3
    task: WEB/WEB-T-0007
    status: promoted
created: 2026-09-07T15:00:00Z
updated: 2026-09-07T16:20:00Z
author: marta
---

## Went well

- (n1) Pairing on the sandbox payment provider unblocked guest checkout in an afternoon. — jose

## To improve

- (n2) Address validation was found on day 4 and pulled into the sprint unplanned. — marta
- (n3) Nobody noticed the website card was read from a snapshot until review. — jose

## Puzzles

- (n4) The stale snapshot badge is honest, but is it actionable for us? — marta

## Discussion

t2 (2 votes) dominated: the sprint scope changed on day 4 because a dependency
was discovered late. One action fixes the immediate blockage (a1) and one caps
the meeting that was supposed to surface it (a2).

## Actions

- [ ] a1 — Finish address validation before the checkout work resumes (jose, 2026-09-11) → `DEMO/DEMO-T-0001`
- [x] a2 — Agree a 30-minute cap on the daily sync (marta, 2026-09-07)
- [ ] a3 — Decide whether the hero copy needs a second review pass (marta, 2026-09-18) → `WEB/WEB-T-0007`
