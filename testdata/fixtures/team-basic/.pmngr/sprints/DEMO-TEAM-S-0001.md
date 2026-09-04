---
id: DEMO-TEAM-S-0001
type: sprint
title: Sprint 1 — Checkout end to end
board: demo-scrum
state: active
start: 2026-08-24
end: 2026-09-06
goal: A guest can pay without an account, and the hero section is ready to ship with it.
capacity_hours: 180
velocity_target: 13
participants: [jose, marta]
committed:
  - DEMO/DEMO-US-0001
  - WEB/WEB-US-0031
items:
  - DEMO/DEMO-US-0001
  - DEMO/DEMO-T-0001
  - WEB/WEB-US-0031
retro: DEMO-TEAM-R-0001
created: 2026-08-21T15:10:00Z
updated: 2026-09-02T09:12:00Z
author: jose
---

## Goal

Prove the whole guest checkout path against the sandbox payment provider, and
ship the rewritten hero section with it.

## Scope Notes

- `DEMO/DEMO-T-0001` was added on day 4, after address validation turned out to
  block the checkout; it is not part of the commitment.
- `WEB/WEB-US-0031` lives in a repository nobody here cloned: the card is read
  from the committed index snapshot and cannot change status from this board.
