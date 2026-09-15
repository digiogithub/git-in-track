---
id: GIT-T-0041
type: task
title: Implement interrupt parking and resume with trailing tool messages
status: done
priority: medium
parent: GIT-US-0053
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:16:19Z
updated: 2026-09-15T16:43:53Z
started: 2026-09-15T15:19:26Z
closed: 2026-09-15T16:43:53Z
---

## Description

Handle `RUN_FINISHED{outcome:"interrupt"}` by parking the run: record the pending tool calls, set `runStatus: 'interrupted'` and expose a `resume(results)` action that re-POSTs the same `threadId` with a new `runId` and the message array extended with one `{role:'tool', toolCallId, content}` entry per result. The reducer must continue the existing message list rather than starting a new one, because Pando re-attaches to the live agent instead of starting a fresh run.

## Acceptance Criteria

- [ ] An interrupt leaves the pending tool calls readable and the transcript intact.
- [ ] Resume posts the same thread id, a new run id and correctly shaped trailing `tool` messages.
- [ ] Events after resume append to the same message list.
- [ ] Vitest covers a full interrupt-resume-finish cycle from a fixture.
