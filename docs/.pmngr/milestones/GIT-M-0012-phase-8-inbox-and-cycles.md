---
id: GIT-M-0012
type: milestone
title: Phase 8 — Inbox and cycles
status: in_progress
author: mcp
labels: [core, web, server]
created: 2026-09-13T13:06:52Z
updated: 2026-09-13T19:53:36Z
started: 2026-09-13T19:53:36Z
due: 2026-12-15
---

## Description

Borrow two proven concepts from Plane. An **inbox** (Plane calls it Intake) where incoming work — from people, from agents, from YouTrack imports, from the public API — waits in a triage state until someone accepts, rejects, snoozes or marks it duplicate. And **cycles**: sprints that derive their state from dates, refuse overlapping ranges, may be drafts without dates, freeze a progress snapshot when they close and transfer unfinished work to the next one.

Cycles extend the existing sprint entity; no new item type is introduced.

## Acceptance Criteria

- [ ] Items can be created into the inbox; they stay out of the backlog and boards until accepted.
- [ ] Accept, reject, snooze (until a date) and duplicate-of actions exist in the UI, REST and MCP.
- [ ] Sprints gain derived status (draft / upcoming / current / completed), the no-overlap rule and a closing snapshot with transfer of incomplete items.
- [ ] Data model docs and ADRs updated.

## Notes

Due date is a planning estimate. Analysis source: `/www/Github/plane` (`apps/api` intake + cycle models, `web/` intake and cycles UI).
