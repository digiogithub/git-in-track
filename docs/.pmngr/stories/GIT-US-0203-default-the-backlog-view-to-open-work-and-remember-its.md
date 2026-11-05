---
id: GIT-US-0203
type: story
title: Default the backlog view to open work and remember its filter per project
status: todo
priority: medium
parent: GIT-EP-0033
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-11-05T14:58:06Z
updated: 2026-11-05T14:58:06Z
---

## Description

Opening a project's backlog today shows every item, closed work included. The maintainer wants the view to open on pending work and to come back to the filter they last applied when they reload the page.

## Acceptance Criteria

- [ ] With no stored filter and no filter in the URL, the backlog view applies a default filter showing pending work only: every status whose workflow category is not `done` or `cancelled` (the project's own workflow decides, not hard-coded ids).
- [ ] Any filter change (status, type, label, assignee, milestone, quick view, search text — whatever `FilterBar`/`QuickViews` already manage) is saved in `localStorage` keyed by repository/project, so two projects keep independent filters.
- [ ] Reloading the page or navigating back to the project's backlog restores that project's stored filter.
- [ ] Filters in the URL (shared links) win over the stored filter, and are then stored as the new last filter.
- [ ] A visible way to reset to the default filter (and clearing the filter to "all" is stored too, so the user can opt out of the default).
- [ ] Corrupt or stale stored values (unknown statuses, bad JSON) fall back to the default without crashing.
- [ ] Vitest coverage for default, persistence per project, URL precedence and corrupt storage.
- [ ] `docs/05-web-app.md` documents the default filter and the per-browser persistence.

## Notes

Follow the pattern of `web/src/app/ui-prefs.ts` (per-browser, never in the repository).
