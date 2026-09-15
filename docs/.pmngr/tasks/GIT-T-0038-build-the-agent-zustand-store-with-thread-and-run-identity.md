---
id: GIT-T-0038
type: task
title: Build the agent Zustand store with thread and run identity
status: done
priority: medium
parent: GIT-US-0053
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:16:14Z
updated: 2026-09-15T16:43:52Z
started: 2026-09-15T15:19:25Z
closed: 2026-09-15T16:43:52Z
---

## Description

Create `web/src/features/agent/agent-store.ts`, a Zustand store holding `threads`, `activeThreadId`, `messages`, `toolCalls`, `stateDoc`, `runStatus` and `interrupt`, with actions `send`, `cancel`, `newThread` and `selectThread`. Each send allocates a fresh run id, reuses the thread id, and posts the full message array — Pando forwards only the trailing user message but keeps its own history keyed by thread. Events from the provider are folded through the reducer. Follow the documented rule at `web/src/app/store.ts:168-174`: no server state and no navigational state in this store.

## Acceptance Criteria

- [ ] A send allocates a new run id and keeps the thread id stable across turns.
- [ ] Cancel aborts the in-flight request and settles `runStatus` without leaving an open message.
- [ ] Switching threads swaps the message list and state document cleanly.
- [ ] Vitest covers send, cancel, thread switching and error propagation against the fake provider.
