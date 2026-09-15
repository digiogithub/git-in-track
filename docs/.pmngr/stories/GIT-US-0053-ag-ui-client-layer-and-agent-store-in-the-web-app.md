---
id: GIT-US-0053
type: story
title: AG-UI client layer and agent store in the web app
status: in_review
priority: high
parent: GIT-EP-0018
milestone: GIT-M-0013
author: mcp
labels: [web]
estimate: 8
created: 2026-09-13T13:12:06Z
updated: 2026-09-15T15:35:47Z
started: 2026-09-15T15:19:30Z
---

## Description

As a web-app developer, I want a transport and state layer that turns the AG-UI event stream into a renderable conversation, so that the chat UI is a pure view over a tested reducer instead of hand-rolled stream parsing.

Add `@pando-ai/sdk/agui` to `web/package.json` (justified in the PR per AGENTS.md) and create `web/src/features/agent/` with `client.ts` building the SDK's AG-UI client against the companion's `/api/v1/agent/run`, with headers carrying the companion bearer token from `web/src/api/token.ts:133-136`. The feature must not call `fetch('/api')` directly: expose `getAgentInfo()` and `runAgent()` as new `DataProvider` methods (`web/src/api/provider.ts`) implemented in `companion-provider.ts`, stubbed as `not_supported` in `browser-provider.ts`, and faked in `fake-provider.ts`.

**Use the SDK's own building blocks wherever they exist** — its `PandoThread` abstraction, its SSE parser, its state/JSON-Patch applier and its HITL helpers — rather than a home-grown reducer. Write local code only for what the SDK does not already give us. `@ag-ui/client` is not an option (it loses `outcome`, `REASONING_*` and `ACTIVITY_*` and adds rxjs/zod/proto), and the SDK is not to be vendored.

Own thread and run identity in the client: a thread id per conversation, a fresh run id per POST, and the full message array resent each turn (Pando forwards only the trailing user message, `input.go:200-211`). Add `agent-store.ts`, a Zustand store holding `messages`, `toolCalls`, `stateDoc`, `runStatus` and `interrupt`, fed from the SDK's thread/event surface over `TEXT_MESSAGE_*`, `TOOL_CALL_*`, `STATE_SNAPSHOT`/`STATE_DELTA` (RFC 6902), `REASONING_*`, `RUN_STARTED`/`RUN_FINISHED`/`RUN_ERROR`. `RUN_FINISHED{outcome:"interrupt"}` parks the run; resuming re-POSTs the same `threadId` with trailing `tool` messages carrying the results. Per `web/src/app/store.ts:168-174`, this store holds conversation state only — nothing that belongs in TanStack Query or the URL.

**Concurrency, until reattach exists.** One live run per thread is a hard Pando constraint (`server.go:263-271`) and a dropped stream cancels the turn. So: one thread per browser tab, plus a `BroadcastChannel` guard that stops a second tab from opening a run on a thread another tab already owns. This is interim and goes away when PANDO-EP-0003 ships park-on-disconnect and reattach.

## Acceptance Criteria

- [ ] `@pando-ai/sdk/agui` is added, pinned, and the bundle-size delta is reported in the PR. No `@ag-ui/client`, no vendored copy of the SDK transport.
- [ ] `getAgentInfo` and `runAgent` exist on `DataProvider` and are implemented in all four providers.
- [ ] The SDK's thread/HITL/state-patch helpers are used where they exist; any local reducer code is limited to what the SDK does not cover and the PR says which is which.
- [ ] The conversation builds an ordered message list from a recorded SSE fixture, interleaving text, reasoning and tool calls correctly.
- [ ] `STATE_SNAPSHOT` followed by `STATE_DELTA` patches produces the expected state document; an unapplicable patch is ignored rather than throwing.
- [ ] `RUN_FINISHED{outcome:"interrupt"}` leaves `runStatus: 'interrupted'` and exposes the pending tool calls.
- [ ] Resuming posts the same `threadId`, a new `runId`, and trailing `tool` messages; the message list continues rather than restarting.
- [ ] The thread id is persisted by git-in-track per conversation (a Pando session id is an acceptable value), so a conversation keeps its identity across a reload — even though replaying its history waits on PANDO-EP-0003.
- [ ] One thread per tab is enforced: a second tab opening the same thread is refused by a `BroadcastChannel` guard rather than racing the first.
- [ ] Aborting a run cancels the underlying request and ends in `runStatus: 'cancelled'` without a dangling open message.
- [ ] `RUN_ERROR` and a mid-stream transport failure both surface a terminal error state.
- [ ] Vitest covers the event handling, thread/run id management, resume and the tab guard, using a checked-in SSE fixture.

## Notes

**Blocked on PANDO-EP-0001**: `@pando-ai/sdk/agui` must ship a browser-safe build (no Node-only imports, an ESM entry the Vite build can tree-shake) before this story can start. The reducer, JSON-Patch applier and HITL helpers this story consumes are part of that same epic.

**PANDO-EP-0003** owns thread history (`MESSAGES_SNAPSHOT`, thread list/messages/delete), park-on-disconnect, reattach and cancel. Until it lands, a reload cannot restore the transcript from Pando and the one-thread-per-tab rule above stands. Do not design the store so that reattach would require rewriting it.

Event catalogue and struct shapes: Pando `internal/agui/events.go:9-42`, request shape `internal/agui/input.go:38-47`, state document `internal/agui/state.go:80-95`.

`forwardedProps` is decoded by Pando but has no consumer (`input.go:46`); pass per-request context through `context[]`, which *is* injected into the prompt (`input.go:235-253`).

Do not use `EventSource` — the run is a POST with a body. Do not put conversation state in TanStack Query, and do not import the WASM bridge or `isomorphic-git` (`web/src/api/provider.ts:6`).
