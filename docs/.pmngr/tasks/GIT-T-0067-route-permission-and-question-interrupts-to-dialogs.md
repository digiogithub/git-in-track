---
id: GIT-T-0067
type: task
title: Route permission and question interrupts to dialogs
status: todo
priority: medium
parent: GIT-US-0061
milestone: GIT-M-0013
author: mcp
labels: [web, security]
estimate: 3
created: 2026-09-13T13:16:58Z
updated: 2026-09-13T13:16:58Z
---

## Description

Extend the agent store's interrupt handling so a parked run whose pending tool is named `pando_permission_request` or the question tool is classified as a human-in-the-loop interrupt rather than a frontend-tool call, and exposes the parsed arguments. Add the answer actions that resume the run with the correctly shaped trailing `tool` message. Anything other than an explicit approval must serialise as a denial, because Pando denies by default and a dropped answer cancels the turn silently.

## Acceptance Criteria

- [ ] The two tool names are classified as HITL interrupts and their arguments are parsed and exposed.
- [ ] Approve, deny and answer each resume the run with the right message shape.
- [ ] A malformed argument payload produces a denial rather than an unhandled error.
- [ ] Vitest covers classification, all three answers and the malformed case.
