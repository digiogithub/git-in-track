---
id: GIT-EP-0033
type: epic
title: Workspace dashboard and per-browser view preferences
status: done
priority: medium
author: mcp
labels: [web]
created: 2026-11-05T14:57:45Z
updated: 2026-11-05T19:37:20Z
started: 2026-11-05T19:37:17Z
closed: 2026-11-05T19:37:20Z
---

## Description

Web UI ergonomics requested by the maintainer on 2026-11-05:

- the backlog view opens with a sensible default filter (open work) and remembers the last filter per project in the browser;
- a settings switch hides the team surface (team panel, boards, retros, metrics) even when a team repository is connected;
- the workspace home shows each configured repository with small KPIs (KB docs, milestones, epics, stories, specs, progress), as a list or as cards, each KPI linking to its project section.

All preferences are per-browser conveniences kept in `localStorage`, never in the repository.

## Acceptance Criteria

- [x] Default + persisted backlog filter per project.
- [x] Teams visibility toggle in settings hides team navigation and routes.
- [x] Workspace home KPIs with list and card layouts.
