---
id: GIT-T-0086
type: task
title: Implement the navigation tools open_item, open_kb_page and focus_board_card
status: in_review
priority: medium
parent: GIT-US-0064
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:17:26Z
updated: 2026-09-15T16:30:42Z
started: 2026-09-15T16:13:39Z
---

## Description

Implement the three navigation executors in `web/src/features/agent/tools/`. `open_item` navigates to `/p/$project/items/$id`, `open_kb_page` to `/p/$project/kb/$` resolving the splat the way `web/src/features/kb/kb-links.ts` does, and `focus_board_card` navigates to `/boards/$slug` and scrolls the card into view. Each returns a structured result describing what happened. An id or path that does not resolve returns a not-found result to the agent and performs no navigation; a path that would leave the app's own routes is rejected outright.

## Acceptance Criteria

- [ ] Each tool navigates through TanStack Router to an existing route and returns a result describing the outcome.
- [ ] Unresolvable ids and paths return a not-found result with no navigation.
- [ ] A path argument cannot escape the app's routes.
- [ ] Vitest covers success, not-found and the escape attempt for each tool.
