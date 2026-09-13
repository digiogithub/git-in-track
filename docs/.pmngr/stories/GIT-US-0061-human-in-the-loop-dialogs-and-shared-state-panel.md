---
id: GIT-US-0061
type: story
title: Human in the loop dialogs and shared-state panel
status: backlog
priority: medium
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [web, security]
estimate: 5
created: 2026-09-13T13:12:43Z
updated: 2026-09-13T13:12:43Z
---

## Description

As a user, I want to approve or refuse what the agent is about to do and answer its questions in the browser, so that an agent run can touch my backlog only with my explicit consent.

Pando surfaces permission prompts as a synthetic tool call named `pando_permission_request` and swaps `AskUserQuestion` for a tool that blocks on the client (`internal/agui/hitl.go:40`, `:188-231`). In `web/src/features/agent/` add `PermissionDialog.tsx` and `QuestionDialog.tsx` over `web/src/components/ui/dialog.tsx`, opened by the store when an interrupt carries one of those tool names. The dialogs answer by resuming the run with a trailing `tool` message; anything that is not an explicit approval must serialise as a denial, because Pando's `approvalFromMessage` (`hitl.go:145`) denies by default and a dropped answer silently cancels the turn (`questionCancelled`, `:225`). Offer approve / deny / approve-always, where always is remembered per tool name for the current thread only, in memory, never persisted.

Add `StatePanel.tsx`, a right-hand column rendering the shared state document: todo list, sub-agents, token usage with a context-window bar, and the touched-files list. The document belongs to the thread, not the run, so it survives between turns and must render from the last snapshot plus applied deltas rather than being cleared on `RUN_STARTED`.

## Acceptance Criteria

- [ ] A `pando_permission_request` interrupt opens a dialog naming the tool and showing its arguments verbatim, escaped as text.
- [ ] Approve, deny and approve-always each resume the run with a correctly shaped trailing `tool` message.
- [ ] Dismissing the dialog (Escape, backdrop) sends an explicit denial rather than leaving the run parked.
- [ ] Approve-always applies to later calls of the same tool in the same thread and is forgotten on reload.
- [ ] An `AskUserQuestion` interrupt opens the question dialog and the typed answer reaches the agent; cancelling produces a cancelled outcome the UI shows.
- [ ] The state panel renders todos, sub-agents, token usage and files, updating live from `STATE_DELTA` and persisting across turns of the same thread.
- [ ] Vitest covers approve, deny, dismiss-as-deny, question answer/cancel, and delta application in the panel.

## Notes

Interaction model reference: Pando `examples/copilotkit/app/page.tsx:125-145` shows the permission tool rendered with `renderAndWaitForResponse`; the shape of the arguments is at `internal/agui/hitl.go:117`. State document fields at `internal/agui/state.go:31-54`, `:80-95`.

Argument payloads are agent output and therefore untrusted content — render them as text, never as Markdown-with-HTML, and never auto-approve anything. Do not persist an always-approval to `localStorage` or the config file; a standing grant across sessions is a security decision that would need its own ADR.

`web/src/components/ui/dialog.tsx` is one of only two Radix primitives in the app — use it rather than adding another dependency.
