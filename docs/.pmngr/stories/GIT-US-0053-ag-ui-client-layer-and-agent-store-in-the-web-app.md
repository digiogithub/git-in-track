---
id: GIT-US-0053
type: story
title: AG-UI client layer and agent store in the web app
status: backlog
priority: high
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 8
created: 2026-09-13T13:12:06Z
updated: 2026-09-13T13:12:06Z
---

## Description

As a web-app developer, I want a transport and state layer that turns the AG-UI event stream into a renderable conversation, so that the chat UI is a pure view over a tested reducer instead of hand-rolled stream parsing.

Add `@ag-ui/client` to `web/package.json` (justified in the PR per AGENTS.md) and create `web/src/features/agent/` with `client.ts` building an `HttpAgent` whose URL is the companion's `/api/v1/agent/run` and whose headers carry the companion bearer token from `web/src/api/token.ts:133-136`. The feature must not call `fetch('/api')` directly: expose `getAgentInfo()` and `runAgent()` as new `DataProvider` methods (`web/src/api/provider.ts:866-1193`) implemented in `companion-provider.ts`, stubbed as `not_supported` in `browser-provider.ts`, and faked in `fake-provider.ts`.

Own thread and run identity in the client: a thread id per conversation, a fresh run id per POST, and the full message array resent each turn (Pando forwards only the trailing user message, `input.go:200-211`). Add `agent-store.ts`, a Zustand store holding `messages`, `toolCalls`, `stateDoc`, `runStatus` and `interrupt`, fed by a pure reducer `events.ts` over `TEXT_MESSAGE_*`, `TOOL_CALL_*`, `STATE_SNAPSHOT`/`STATE_DELTA` (RFC 6902), `REASONING_*`, `RUN_STARTED`/`RUN_FINISHED`/`RUN_ERROR`. `RUN_FINISHED{outcome:"interrupt"}` parks the run; resuming re-POSTs the same `threadId` with trailing `tool` messages carrying the results. Per `web/src/app/store.ts:168-174`, this store holds conversation state only — nothing that belongs in TanStack Query or the URL.

## Acceptance Criteria

- [ ] `@ag-ui/client` is added, pinned, and the bundle-size delta is reported in the PR.
- [ ] `getAgentInfo` and `runAgent` exist on `DataProvider` and are implemented in all four providers.
- [ ] The reducer builds an ordered message list from a recorded SSE fixture, interleaving text, reasoning and tool calls correctly.
- [ ] `STATE_SNAPSHOT` followed by `STATE_DELTA` patches produces the expected state document; an unapplicable patch is ignored rather than throwing.
- [ ] `RUN_FINISHED{outcome:"interrupt"}` leaves `runStatus: 'interrupted'` and exposes the pending tool calls.
- [ ] Resuming posts the same `threadId`, a new `runId`, and trailing `tool` messages; the reducer continues the same message list.
- [ ] Aborting a run cancels the underlying request and ends in `runStatus: 'cancelled'` without a dangling open message.
- [ ] `RUN_ERROR` and a mid-stream transport failure both surface a terminal error state.
- [ ] Vitest covers the reducer, thread/run id management and resume, using a checked-in SSE fixture.

## Notes

Event catalogue and struct shapes: Pando `internal/agui/events.go:9-42`, request shape `internal/agui/input.go:38-47`, state document `internal/agui/state.go:80-95`. A reference TS client is `sdk/typescript/src/agui/client.ts:77-95` (options) and `:254` (`parseSSE`) — read it, do not vendor it.

`forwardedProps` is decoded by Pando but has no consumer (`input.go:46`); pass per-request context through `context[]`, which *is* injected into the prompt (`input.go:235-253`).

Do not use `EventSource` — the run is a POST with a body. Do not put conversation state in TanStack Query, and do not import the WASM bridge or `isomorphic-git` (`web/src/api/provider.ts:6`).
