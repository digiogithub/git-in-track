---
id: GIT-T-0181
type: task
title: Widen SearchHit and the provider search method
status: todo
priority: medium
parent: GIT-US-0086
milestone: GIT-M-0013
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:39Z
updated: 2026-09-13T13:19:39Z
---

## Description

Widen the `SearchHit` type and the `search(query)` method on `DataProvider` (`web/src/api/provider.ts:940`) with an origin discriminator and an optional snippet string. Map the new fields in `companion-provider.ts`, return only exact hits from `browser-provider.ts`, and extend `fake-provider.ts` to produce both kinds so the UI tests have something to render.

## Acceptance Criteria

- [ ] The widened type compiles across all four providers with no `any`.
- [ ] The companion provider maps the origin and snippet from the API response.
- [ ] The fake provider yields both exact and semantic hits, covered by Vitest.
