---
id: GIT-US-0076
type: story
title: Send to YouTrack action on comments and feedback notes
status: backlog
priority: medium
parent: GIT-EP-0013
milestone: GIT-M-0011
author: mcp
labels: [web]
estimate: 5
created: 2026-09-13T13:13:53Z
updated: 2026-09-13T13:13:53Z
---

## Description

As a reviewer, I want a "Send to YouTrack" action on a comment and on the feedback note I just wrote, so that I can choose which of my remarks reach the issue tracker and see whether they arrived.

Extend `CommentsPanel` (`web/src/features/backlog/ItemDetail.tsx:175-265`) with a per-comment action that is rendered only when `capabilities.features.youtrack` is true, the project is linked and the item carries a YouTrack `external` reference. The action enqueues the push and the comment then shows a small state badge: pending while the job is queued or running, sent with the remote comment id and a link to the YouTrack issue once `external` is filled in, failed with the error and a retry affordance. State is derived from the comment's `external` field plus the `sync.job.*` events for that comment path, not from local component state, so a reload shows the truth.

Feedback on an item is already just a comment (ADR-030, `web/src/features/feedback/format.ts:9-22`), so the feedback panel needs only one extra control: a "send to YouTrack after saving" checkbox on the save button in `web/src/features/feedback/FeedbackPanel.tsx`, remembered per project in the existing feedback store. A project-level toggle for `push_comments: manual | auto` lives in the YouTrack settings card and, when set to auto, the per-comment action collapses into the state badge alone because everything is pushed.

## Acceptance Criteria

- [ ] The action appears only when the YouTrack feature is available, the project is linked and the item has a YouTrack `external` reference.
- [ ] Comment state renders as pending, sent (with a link to the YouTrack comment) or failed with a retry.
- [ ] State is derived from the comment's `external` field and `sync.job.*` events and survives a page reload.
- [ ] The feedback panel offers "send to YouTrack after saving" and honours it when the note is saved as a comment.
- [ ] The YouTrack settings card exposes `push_comments: manual | auto` and the comment UI adapts to auto.
- [ ] Only design tokens are used; `npm run tokens:check` passes and the controls are keyboard reachable.
- [ ] Vitest covers gating, the three states, the retry path and the feedback checkbox with a mocked provider.

## Notes

Depends on the push job kind and the auto-push seam of this epic, and on GIT-EP-0011 for the capability flag and the settings card.

Existing code: comments are fetched through `useComments` (`web/src/features/backlog/queries.ts:102-109`) and created with `useAddComment` (`:292-304`); feedback drafts live in `web/src/features/feedback/feedback-store.ts` with `localStorage` keys `gintrack:feedback:<item|kb>:<project>:<ref>`; event bridging is `queries.ts:130-148`.

Do NOT build a second feedback sink — KB-page feedback is a block in the page and belongs to GIT-EP-0014, not here. Do NOT offer a delete-remotely action; deletion is deliberately local only.
