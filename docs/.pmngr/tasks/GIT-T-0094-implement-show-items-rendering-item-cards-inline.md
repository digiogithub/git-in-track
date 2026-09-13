---
id: GIT-T-0094
type: task
title: Implement show_items rendering item cards inline
status: todo
priority: medium
parent: GIT-US-0064
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:17:36Z
updated: 2026-09-13T13:17:36Z
---

## Description

Implement `show_items`, which takes a list of item ids and renders cards inline in the transcript rather than navigating. Resolve the ids through the existing backlog query hooks (`web/src/features/backlog/queries.ts`) so the cards reuse the TanStack Query cache, and reuse the badge and metadata components from `web/src/features/backlog/Badges.tsx` and `item-meta.ts`. The executor returns the resolved and unresolved id lists to the agent so it can correct itself; the cards themselves are stored on the message.

## Acceptance Criteria

- [ ] Cards render inline in the transcript with title, type, status and priority badges.
- [ ] Resolution uses the existing query cache and issues no per-card request.
- [ ] Unresolved ids are reported back to the agent and shown as a muted note in the card block.
- [ ] Vitest covers rendering, cache reuse and partial resolution.
