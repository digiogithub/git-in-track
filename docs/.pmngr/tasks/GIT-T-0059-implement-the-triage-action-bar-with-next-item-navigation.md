---
id: GIT-T-0059
type: task
title: Implement the triage action bar with next-item navigation
status: todo
priority: medium
parent: GIT-US-0060
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 4
created: 2026-09-13T13:16:46Z
updated: 2026-09-13T13:16:46Z
---

## Description

Add `InboxActions.tsx` with accept, reject, snooze and duplicate. Accept routes to the existing editor (`web/src/features/editor/ItemEditorPage.tsx:43`) retitled and with a non-triage status preselected, whose save calls `triageInboxItem({action:'accept'})`. Reject and snooze are small dialogs (snooze with a date picker); duplicate uses `web/src/components/editor/ItemPicker.tsx` to pick the target. Compute the next queue item *before* mutating and navigate after the mutation resolves. Mutations are optimistic with rollback and a pending-count update, and a stale rev opens the existing `ConflictDialog`. Bind `j`, `k`, `a`, `r` and `s`.

## Acceptance Criteria

- [ ] Each action works, reports success and failure with a toast, and advances to the next queued item without leaving an empty pane.
- [ ] Accept commits the triage decision in the same save as the edit form.
- [ ] An optimistic update rolls back on error and a stale rev opens the conflict dialog.
- [ ] Vitest covers the next-item computation, the rollback and the keyboard shortcuts.
