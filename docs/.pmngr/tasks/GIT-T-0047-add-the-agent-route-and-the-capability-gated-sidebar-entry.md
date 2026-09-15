---
id: GIT-T-0047
type: task
title: Add the /agent route and the capability-gated sidebar entry
status: in_review
priority: medium
parent: GIT-US-0057
milestone: GIT-M-0013
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:16:27Z
updated: 2026-09-15T16:00:42Z
started: 2026-09-15T15:45:45Z
---

## Description

Add one `createRoute` for `/agent` to `web/src/app/router.tsx`, register it in the route tree array, and add the matching nav entry to `web/src/app/layout/AppShell.tsx:38-53`. The entry and the route component render only when `capabilities.agent` is true, following the existing pattern of branching on capabilities rather than on provider kind (`AppShell.tsx:442-445`). Lazy-load the page component the way `/boards/$slug` and `/sprints` do, so the chat bundle does not load for users who never open it.

## Acceptance Criteria

- [ ] `/agent` resolves in companion mode with the feature enabled and shows a not-available state otherwise.
- [ ] The sidebar entry is absent when `capabilities.agent` is false.
- [ ] The page is code-split; Vitest covers both capability states.
