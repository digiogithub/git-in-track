---
id: GIT-T-0187
type: task
title: Offer sending feedback notes after saving
status: todo
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:46Z
updated: 2026-09-13T13:19:46Z
---

## Description

Add a "send to YouTrack after saving" checkbox next to the save action in `web/src/features/feedback/FeedbackPanel.tsx`, remembered per project in the existing feedback store (`web/src/features/feedback/feedback-store.ts`). When checked, saving the item feedback — which is already an ordinary comment (`web/src/features/feedback/format.ts:9-22`) — also enqueues the push. The checkbox appears only for the `comment` destination, never for the KB page destination.

## Acceptance Criteria

- [ ] The checkbox appears only for the item (comment) destination and its state is remembered per project.
- [ ] Saving with it checked enqueues a push for the created comment.
- [ ] The KB page destination is unaffected.
- [ ] Vitest covers both destinations and the remembered preference.
