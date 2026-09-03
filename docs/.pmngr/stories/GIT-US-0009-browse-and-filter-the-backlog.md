---
id: GIT-US-0009
type: story
title: Browse and filter the backlog
status: in_progress
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T21:47:06Z
started: 2026-09-03T21:47:06Z
author: team
priority: high
parent: GIT-EP-0002
milestone: GIT-M-0002
estimate: 5
labels: [web]
links:
  - kind: blocked_by
    target: GIT-US-0007
---

## Description

As a project member, I want to see all epics, stories, tasks and milestones of a project
and narrow them down quickly, so that I can find what I need without opening files by hand.

The backlog view lists items with their key fields, supports filtering by type, status,
label, assignee, milestone and priority, sorting, grouping by epic or milestone, and
full-text search across titles and bodies served by the core's query engine. Selecting an
item opens a detail view with the parsed front matter, the rendered body, its children and
its typed links.

Filters are reflected in the URL so a view can be shared or bookmarked, and the empty state
explains how to create the first item.

## Acceptance Criteria

- [ ] All item types are listed with id, title, status, priority, assignees and estimate.
- [ ] Filters for type, status, label, assignee, milestone and priority combine correctly.
- [ ] Full-text search returns ranked results in under 100 ms on the fixture vault.
- [ ] Grouping by epic and by milestone both work, with counts and point totals.
- [ ] Filter and search state is encoded in the URL and restored on reload.
- [ ] The detail view shows front matter, rendered body, children and typed links.
- [ ] Navigating between parent and child items works in both directions.
- [ ] Empty and error states are designed, not default.
