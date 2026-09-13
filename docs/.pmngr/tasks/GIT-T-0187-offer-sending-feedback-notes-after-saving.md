---
id: GIT-T-0187
type: task
title: Offer sending feedback notes after saving
status: done
priority: medium
parent: GIT-US-0076
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 2
created: 2026-09-13T13:19:46Z
updated: 2026-09-13T16:36:51Z
started: 2026-09-13T16:36:39Z
closed: 2026-09-13T16:36:51Z
---

## Description

Add a "send to YouTrack after saving" checkbox next to the save action in `web/src/features/feedback/FeedbackPanel.tsx`, remembered per project in the existing feedback store (`web/src/features/feedback/feedback-store.ts`). When checked, saving the item feedback — which is already an ordinary comment (`web/src/features/feedback/format.ts:9-22`) — also enqueues the push. The checkbox appears only for the `comment` destination, never for the KB page destination.

## Acceptance Criteria

- [x] The checkbox appears only for the item (comment) destination and its state is remembered per project.
- [x] Saving with it checked enqueues a push for the created comment.
- [x] The KB page destination is unaffected.
- [x] Vitest covers both destinations and the remembered preference.

## Notes

The preference lives under `gintrack:feedback:push-youtrack:<project>`,
alongside the existing draft keys.

The panel itself drops the control for the `page` destination rather than
relying on the caller to omit it, so a KB-page sink cannot grow one by
accident — KB-page feedback is a block in the page and belongs to GIT-EP-0014.
