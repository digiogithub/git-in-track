---
id: GIT-T-0166
type: task
title: Build the live queue table with retry and cancel
status: done
priority: medium
parent: GIT-US-0081
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 5
created: 2026-09-13T13:19:20Z
updated: 2026-09-13T15:49:23Z
started: 2026-09-13T15:49:09Z
closed: 2026-09-13T15:49:23Z
---

## Description

Add the queue table to the card: kind, key, state, attempts, next attempt and last error per row, with counts by state in the header, fed by TanStack Query and kept fresh by `useSyncJobEvents` rather than polling. Add per-row Retry and Cancel buttons, disabled in states where the action does not apply. Error text arriving from YouTrack is untrusted and must be rendered as plain text, never as raw HTML or unsanitised Markdown.

## Acceptance Criteria

- [x] The table renders every job state and updates live from `sync.job.*` with no timer-based polling.
- [x] Retry and Cancel act per row, are disabled where inapplicable and show success and failure toasts.
- [x] Error text is rendered as plain text and cannot inject markup.
- [x] Vitest covers rendering from the fake provider, the live update path and both actions.

## Notes

Retry is enabled for `failed` and `cancelled` only, Cancel for `queued` and `running` only — the same two rows the companion's own state machine allows, so a refusal is a race rather than a mis-click. A retried **cancelled** job comes back with a new id and the toast says so, because the engine has no edge out of `cancelled`.

`lastError.message` goes through JSX text interpolation and nothing else; a test asserts that a message containing an `<img onerror=…>` produces no element. Nothing on this card touches the Markdown pipeline, so `markdown/sanitize.ts` is not on any path here.

No `refetchInterval` anywhere: `useSyncJobEvents` invalidates `['syncJobs']` on every frame, including the synthetic `resync` the provider raises after a reconnect, a `stream.overflow` or a `resume.gap`.
