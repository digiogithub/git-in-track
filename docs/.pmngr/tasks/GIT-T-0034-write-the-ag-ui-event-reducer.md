---
id: GIT-T-0034
type: task
title: Write the AG-UI event reducer
status: in_review
priority: medium
parent: GIT-US-0053
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 5
created: 2026-09-13T13:16:08Z
updated: 2026-09-15T15:35:43Z
started: 2026-09-15T15:19:24Z
---

## Description

Create `web/src/features/agent/events.ts`, a pure reducer from `(state, event)` to state covering `TEXT_MESSAGE_START|CONTENT|END|CHUNK`, `TOOL_CALL_START|ARGS|END|RESULT`, `REASONING_*`, `STATE_SNAPSHOT`, `STATE_DELTA` (RFC 6902 patches), `MESSAGES_SNAPSHOT`, `STEP_*`, `RUN_STARTED`, `RUN_FINISHED` and `RUN_ERROR`. Tool-call argument deltas accumulate as a string and are parsed only at `TOOL_CALL_END`. An unknown event type is ignored rather than throwing, and an unapplicable state patch is dropped with a warning. Keep it free of React and of the transport so it is testable in isolation.

## Acceptance Criteria

- [ ] A recorded SSE fixture replays into the expected ordered message list, including interleaved tool calls and reasoning.
- [ ] `STATE_SNAPSHOT` followed by deltas produces the expected document; a bad patch is ignored without corrupting state.
- [ ] Unknown event types and out-of-order message ends are tolerated.
- [ ] Vitest covers all of the above with the fixture checked in under the feature folder.
