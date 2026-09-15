---
id: GIT-T-0030
type: task
title: Add getAgentInfo and runAgent to the DataProvider seam
status: in_review
priority: medium
parent: GIT-US-0053
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:16:02Z
updated: 2026-09-15T15:35:42Z
started: 2026-09-15T15:19:22Z
---

## Description

Declare `getAgentInfo()` and `runAgent(input, handlers)` on the `DataProvider` interface (`web/src/api/provider.ts:866-1193`) with the AG-UI input and event types. Implement them in `companion-provider.ts` over the transport factory, reusing its `#send` conventions for headers and error mapping (`:194-252`). Implement them in `browser-provider.ts` as a `ProviderError` with a `not_supported` code, and in `fake-provider.ts` as a scripted event stream the tests can drive. Feature code must call these methods, never `fetch` directly.

## Acceptance Criteria

- [ ] All four providers compile and export the two methods.
- [ ] The browser provider rejects with a typed `ProviderError`, not a thrown string.
- [ ] The fake provider can replay a scripted sequence of AG-UI events, and Vitest asserts it.
