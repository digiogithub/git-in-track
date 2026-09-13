---
id: GIT-T-0108
type: task
title: Wire the dialog, run progress and result summary
status: todo
priority: medium
parent: GIT-US-0059
milestone: GIT-M-0011
author: mcp
labels: [web, agent-ok]
estimate: 3
created: 2026-09-13T13:17:57Z
updated: 2026-09-13T13:17:57Z
---

## Description

Assemble `web/src/features/youtrack/ImportDialog.tsx` from the query bar, options and preview, and add the backlog toolbar entry that opens it, rendered only when `capabilities.features.youtrack` is true and the project is linked. Running enqueues the job, switches to a progress strip fed by the `sync.job.*` WebSocket events bridged the way `web/src/features/backlog/queries.ts:130-148` already bridges write events, and ends in a summary listing created, updated and failed issues with a link to each new item and the error text for each failure.

## Acceptance Criteria

- [ ] The toolbar entry appears only when the feature is available and the project is linked, and is absent in browser-only mode.
- [ ] Running shows live progress from `sync.job.progress` and transitions to a result summary.
- [ ] The summary lists created, updated and failed issues with links and error text.
- [ ] Vitest covers the full flow from search to summary with a mocked provider and mocked events.
