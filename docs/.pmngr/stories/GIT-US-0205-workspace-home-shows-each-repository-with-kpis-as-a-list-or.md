---
id: GIT-US-0205
type: story
title: Workspace home shows each repository with KPIs, as a list or as cards
status: todo
priority: medium
parent: GIT-EP-0033
author: mcp
labels: [web, agent-ok]
estimate: 5
created: 2026-11-05T14:58:06Z
updated: 2026-11-05T14:58:06Z
---

## Description

The workspace home lists the configured repositories. Add small KPIs per repository so the maintainer sees the state of every project at a glance, and offer a card layout next to the current list.

## Acceptance Criteria

- [ ] Each repository row/card shows:
  - number of knowledge-base docs — click opens that project's KB section;
  - milestones (total and open) — click opens the project's milestones;
  - epics (total and open) — click opens the backlog filtered to epics;
  - user stories (total and remaining, i.e. not in a `done`/`cancelled` category) — click opens the backlog filtered to stories;
  - specs (total and specs with requirements not yet verified/satisfied) — click opens the project's specs section;
  - a progress bar with the estimated percentage of the project closed (done stories over non-cancelled stories, weighted by `estimate` when stories carry one; document the formula in a tooltip).
- [ ] A list/cards switch on the workspace home; the choice persists in `localStorage` via `web/src/app/ui-prefs.ts`. The current list stays the default.
- [ ] KPIs come from the existing index/query data (CoreApi / TanStack Query); no new domain logic in `web/`; if a count needs a new core query, add it in `internal/core`/`internal/vault` so browser-only (WASM) and companion modes agree.
- [ ] Loading and error states per repository; one failing repository does not blank the page.
- [ ] Works in browser-only mode and companion mode.
- [ ] Vitest coverage for KPI computation, links and the layout switch.
- [ ] `docs/05-web-app.md` documents the workspace KPIs.
