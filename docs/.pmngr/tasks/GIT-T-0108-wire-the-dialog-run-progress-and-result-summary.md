---
id: GIT-T-0108
type: task
title: Wire the dialog, run progress and result summary
status: done
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:17:57Z
updated: 2026-09-13T15:48:33Z
started: 2026-09-13T15:48:18Z
closed: 2026-09-13T15:48:33Z
---

## Description

Assemble `web/src/features/youtrack/ImportDialog.tsx` from the query bar, options and preview, and add the backlog toolbar entry that opens it, rendered only when `capabilities.features.youtrack` is true and the project is linked. Running enqueues the job, switches to a progress strip fed by the `sync.job.*` WebSocket events bridged the way `web/src/features/backlog/queries.ts:130-148` already bridges write events, and ends in a summary listing created, updated and failed issues with a link to each new item and the error text for each failure.

## Acceptance Criteria

- [x] The toolbar entry appears only when the feature is available and the project is linked, and is absent in browser-only mode.
- [x] Running shows live progress from `sync.job.progress` and transitions to a result summary.
- [x] The summary lists created, updated and failed issues with links and error text.
- [x] Vitest covers the full flow from search to summary with a mocked provider and mocked events.

## Notes

The toolbar entry (`ImportButton.tsx`, rendered in the `ItemTable` header) needs **both** capabilities: `youtrackSupported` (this runtime can reach YouTrack at all) and `youtrack` (a project declares a link). The second is workspace-wide — `capabilities` reports that *at least one* mounted project is linked, not which — so a workspace holding a linked project and an unlinked one shows the entry on both. Making it exact needs a per-project capability or a cheap `GET /youtrack/settings?key=` probe; raised for the next wave rather than guessed at here.

Progress is bound to the job id the run answers and frames for any other job are ignored. No throttling is added on top of the engine's: progress is already coalesced to one frame per 500 ms per group, terminal frames are never throttled, and a jump in the counts is rendered as-is. Per-issue detail comes from the inline result when the runtime ran the import inline; when it enqueued a job, the terminal frame ends the strip and the failure text is read back from `getSyncJob(jobId).lastError`.
