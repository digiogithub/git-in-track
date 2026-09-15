---
id: GIT-T-0090
type: task
title: Implement apply_backlog_filter over the URL search schema
status: done
priority: medium
parent: GIT-US-0064
milestone: GIT-M-0013
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:17:31Z
updated: 2026-09-15T16:44:40Z
started: 2026-09-15T16:13:41Z
closed: 2026-09-15T16:44:40Z
---

## Description

Implement `apply_backlog_filter` by validating the tool arguments against the existing zod search schema (`web/src/features/backlog/search.ts:58-73`, `validateItemSearch` :109-111) and writing them into the URL through the setter hook in `use-search.ts`, then navigating to `/p/$project/items`. The URL is the filter in this app, so no parallel filter state may be introduced. Invalid arguments return a validation error result naming the offending field.

## Acceptance Criteria

- [ ] Valid arguments produce URL search params the router accepts and the item table re-queries.
- [ ] Invalid arguments return a validation error naming the field and change nothing.
- [ ] No filter state is stored outside the URL.
- [ ] Vitest covers a valid filter, an invalid field and an unknown field.
