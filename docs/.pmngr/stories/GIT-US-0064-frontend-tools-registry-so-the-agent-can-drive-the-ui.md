---
id: GIT-US-0064
type: story
title: Frontend tools registry so the agent can drive the UI
status: done
priority: medium
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 5
created: 2026-09-13T13:12:59Z
updated: 2026-09-15T17:00:44Z
started: 2026-09-15T16:13:54Z
closed: 2026-09-15T17:00:44Z
---

## Description

As a user, I want the agent to open the item it is talking about, jump to a KB page or narrow my backlog filter, so that the conversation and the app stay in the same place instead of me copying ids by hand.

Add `web/src/features/agent/tools/` with a small registry: each entry declares a JSON-schema parameter shape and an executor, and the registry is serialised into `RunAgentInput.tools` on every POST. Ship five: `open_item` (navigate to `/p/$project/items/$id`), `open_kb_page` (`/p/$project/kb/$`), `focus_board_card` (navigate to the board and scroll the card into view), `apply_backlog_filter` (write the zod-validated search params of `web/src/features/backlog/search.ts:58-73` into the URL) and `show_items` (render item cards inline in the transcript rather than navigating).

Execution rides the interrupt protocol: Pando emits `TOOL_CALL_START/ARGS/END` then `RUN_FINISHED{outcome:"interrupt"}` and keeps the agent alive; the client runs the executor and re-POSTs the same `threadId` with a trailing `tool` message carrying the result (`internal/agui/server.go:470-507`, `:356`, `:388`). Validate every argument object against the declared schema before executing — arguments are model output, so an unknown tool name, a bad id or an out-of-app path must return a structured error result to the agent instead of navigating anywhere. `show_items` resolves ids through the existing backlog queries (`web/src/features/backlog/queries.ts`) so cards reuse the current cache and permissions.

## Acceptance Criteria

- [ ] The five tools are declared in `RunAgentInput.tools` with valid JSON-schema parameters and human-readable descriptions.
- [ ] A tool interrupt executes the matching executor and resumes the run with a trailing `tool` message; the transcript shows what happened.
- [ ] Arguments that fail schema validation, or a tool name that is not registered, resume the run with an error result and no navigation.
- [ ] `open_item` and `open_kb_page` route through TanStack Router and land on an existing route; an unknown id yields a not-found result to the agent, not a broken page.
- [ ] `apply_backlog_filter` produces URL search params that `validateItemSearch` accepts, and invalid filters are rejected.
- [ ] `show_items` renders item cards inline from the backlog query cache, with no extra request per card.
- [ ] Navigating away mid-run does not lose the thread; returning to `/agent` shows the resumed conversation.
- [ ] Vitest covers a successful interrupt-execute-resume cycle, a rejected argument set and an unknown tool.

## Notes

The agent pool is keyed by agent name plus a hash of the declared toolset (`internal/agui/agentpool.go:54-77`), so the tool list must be stable across turns of a thread — build it once from the registry rather than conditionally per message.

Frontend tools run in the browser and are good for UI actions, bad for data: everything that reads or writes backlog content must go through the gintrack MCP server instead (`GIT-EP-0018` Pando configuration story), where the rev protocol and the write gate apply.

Do not let a tool perform a write. Do not let a path or id argument escape the app's own routes.
