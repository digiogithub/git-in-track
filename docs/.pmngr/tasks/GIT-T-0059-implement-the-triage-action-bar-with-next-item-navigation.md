---
id: GIT-T-0059
type: task
title: Implement the triage action bar with next-item navigation
status: done
priority: medium
parent: GIT-US-0060
milestone: GIT-M-0012
author: mcp
labels: [web]
estimate: 4
created: 2026-09-13T13:16:46Z
updated: 2026-09-13T16:40:14Z
started: 2026-09-13T16:40:00Z
closed: 2026-09-13T16:40:14Z
---

## Description

Add `InboxActions.tsx` with accept, reject, snooze and duplicate. Accept routes to the existing editor (`web/src/features/editor/ItemEditorPage.tsx:43`) retitled and with a non-triage status preselected, whose save calls `triageInboxItem({action:'accept'})`. Reject and snooze are small dialogs (snooze with a date picker); duplicate uses `web/src/components/editor/ItemPicker.tsx` to pick the target. Compute the next queue item *before* mutating and navigate after the mutation resolves. Mutations are optimistic with rollback and a pending-count update, and a stale rev opens the existing `ConflictDialog`. Bind `j`, `k`, `a`, `r` and `s`.

## Acceptance Criteria

- [x] Each action works, reports success and failure with a toast, and advances to the next queued item without leaving an empty pane.
- [x] Accept commits the triage decision in the same save as the edit form.
- [x] An optimistic update rolls back on error and a stale rev opens the conflict dialog.
- [x] Vitest covers the next-item computation, the rollback and the keyboard shortcuts.

## Notes

The accept form is `InboxAcceptPage.tsx` rather than `ItemEditorPage` itself.
`ItemEditorPage` takes no props, reads its own route params and calls
`provider.updateItem` directly, and this wave did not own `features/editor/`.
The wrapper composes the *same* pieces — `FrontMatterForm`, `MarkdownEditor`,
`DiagnosticList`, `ConflictDialog`, `readProjectSchema`/`valuesFromItem`/
`buildPatch` — so it is not a bespoke form, but it repeats the page shell.
**Follow-up**: give `ItemEditorPage` a `mode: 'edit' | 'accept'` prop and delete
the wrapper.

The save is two provider calls in one button: `triageInboxItem` first, because
it owns status and parent and it is the write that commits the acceptance, then
`updateItem` for anything else the person changed, pinned to the rev the triage
returned.

`r` opens the same confirmation the button does: a keystroke should not skip a
confirmation the mouse needs.
