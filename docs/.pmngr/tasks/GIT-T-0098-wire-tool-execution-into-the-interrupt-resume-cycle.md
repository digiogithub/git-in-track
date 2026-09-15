---
id: GIT-T-0098
type: task
title: Wire tool execution into the interrupt-resume cycle
status: in_review
priority: medium
parent: GIT-US-0064
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 3
created: 2026-09-13T13:17:41Z
updated: 2026-09-15T16:30:45Z
started: 2026-09-15T16:13:44Z
---

## Description

Connect the registry to the store: when a run parks on an interrupt whose pending tool is a registered frontend tool, validate the arguments, execute the matching executor, and resume with a trailing `tool` message carrying the result. Unknown tool names and failed validation resume with a structured error result rather than throwing or leaving the run parked. Execution must survive the user navigating away, since a tool's own job is often to navigate.

## Acceptance Criteria

- [ ] A tool interrupt executes and resumes automatically, with the step visible in the transcript.
- [ ] An unknown tool name or invalid arguments resume with an error result and no side effect.
- [ ] Navigating during execution does not lose the thread; returning to `/agent` shows the resumed run.
- [ ] Vitest covers the full cycle, the unknown tool and the invalid arguments.
