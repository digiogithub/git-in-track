---
title: Research — Pando TypeScript SDK and AG-UI client from a browser
type: page
tags: [research, pando, agent, sdk]
---

# Pando TypeScript SDK / AG-UI client — evaluation for a git-in-track browser panel

Scope: `@pando-ai/sdk` (`/www/MCP/Pando/pando/sdk/typescript`, version 0.1.0) and its `agui` subpath,
cross-checked against the Go server contract in `/www/MCP/Pando/pando/internal/agui/*.go`.
Target consumer: git-in-track `web/` (React 18.3.1 + Vite 6.4.3, `/www/git-in-track/web/package.json`),
browser -> Go companion proxy -> Pando.

Legend for the "Class" column: **(a)** works today with config only - **(b)** works with a
git-in-track-side workaround - **(c)** needs a Pando code change (S/M/L).

## Summary table

| # | Question | Verdict | Class |
|---|---|---|---|
| 1 | Browser-usable modes | Only the AG-UI subpath. `PandoClient`/`PandoAgent` spawn processes; `PandoHttpClient` statically imports `node:https` (`src/http.ts:20`) -> breaks a Vite browser build. `PandoAguiClient` is genuinely isomorphic (fetch + ReadableStream + AbortSignal, no node imports). | (a) for agui; (c)-S to make `http.ts` browser-safe |
| 2 | Package build for browsers | `tsup --format esm,cjs --dts` (`package.json:30`); **no** `sideEffects`, no `browser` condition, no bundling of the `node:https` import out of the main entry. `./agui` is a separate export so the node code is not pulled in — but `agui/index.ts:389` statically re-exports `copilotkit.ts`, which does `await import(variable)` (`copilotkit.ts:281`) and makes Vite emit a dynamic-import warning. | (c)-S |
| 3 | Recommendation | **Copy/port `PandoAguiClient` + `agui/types.ts` into the git-in-track web app (or depend on the subpath) and add a small local reducer.** Do not adopt `@ag-ui/client` HttpAgent as primary. See §1.4. | (b) |
| 4 | Event coverage | Server emits 19 distinct types; SDK types 24 names but gives typed interfaces to only 14 and parses/reduces none. No helper for interrupts, permissions, questions, JSON-Patch, or transcript assembly. | (c)-M |
| 5 | RunAgentInput coverage | `forwardedProps` and `parentRunId` are unreachable from `RunOptions` (`agui/client.ts:77-95`, `205-221`); multimodal `content` parts are untypeable. `forwardedProps` is also **dead on the server** (decoded at `input.go:46`, referenced nowhere else). | (c)-S client, (c)-M server |
| 6 | Thread lifecycle / reload | **No AG-UI endpoint lists threads, fetches a thread's messages, or reattaches to a run.** `MESSAGES_SNAPSHOT` is defined (`events.go:429`) but never emitted (grep: zero call sites for `NewMessagesSnapshot`). Recovery is only possible by reusing a Pando session id as the threadId + the REST API. | (b) now, (c)-M for a proper fix |
| 7 | Cancellation | `signal.abort()` closes the stream -> request ctx cancels -> `stream()` calls `finishRun` -> agent context cancelled (`server.go:412-417`, `run.go:146-152`). So abort **does** kill the agent. No cancel endpoint, and a *suspended* run cannot be aborted (only the 11-min reaper, `run.go:28`). | (a) |
| 8 | Two tabs on one thread | Hostile. A second POST on a live thread calls `abandonRun` and kills the first tab's run (`server.go:263-271`). | (c)-M |
| 9 | Auth from a browser | Bearer header **or** `?token=` (`server.go:73-79`). `Origin` is only checked when the header is present (`server.go:48-53`); a Go proxy that strips Origin bypasses CORS entirely and never needs an allow-list entry. | (a) |
| 10 | Per-user identity | None. No user concept anywhere in `internal/agui`; `context[]` is flattened into the prompt text (`input.go:235-253`), `state` is echoed read-only into `StateDoc.Client` (`state.go:92-95`). | (c)-M |
| 11 | Multi-project / cwd | One cwd per **process**: `agui-serve --cwd` chdirs once (`cmd/agui_serve.go:208-216`), config+DB are resolved from it, and `.pando/ipc.lock` is per workdir (`internal/ipc/lock_common.go:32`). No per-request repo selection. | (c)-L, or (b) process-per-repo |
| 12 | SDK hygiene | `npx tsc --noEmit` exits 0. 7 agui tests for the client + 5 for CopilotKit glue (`tests/agui.test.ts`), all fetch-mocked; **no test covers interrupt/resume, permissions, STATE_DELTA or reconnect**. | (c)-S |

---

## 1. Which modes are usable from a browser

### 1.1 The four modes

| Mode | Entry | Browser? | Evidence |
|---|---|---|---|
| Subprocess | `PandoClient` (`src/client.ts`) | No | spawns `pando -p` |
| ACP stdio | `PandoAgent`/`PandoSession` (`src/agent.ts`, `src/transport.ts`) | No | child process + JSON-RPC over stdio |
| HTTP REST | `PandoHttpClient` (`src/http.ts`) | **No, as shipped** | `import * as https from "node:https";` at `src/http.ts:20`, used at `src/http.ts:88-91` to build an `agent` for self-signed certs. A static node builtin import in the *main* entry point; Vite will fail to resolve it. |
| AG-UI | `PandoAguiClient` (`src/agui/client.ts`) | **Yes** | `globalThis.fetch` (`client.ts:124`), `response.body.getReader()` + `TextDecoder` (`client.ts:260-288`), `AbortController`/`AbortSignal` (`client.ts:225-233`), `globalThis.crypto.randomUUID` with a fallback (`client.ts:319-323`). No node imports anywhere in `src/agui/`. |

Note the REST client is only *incidentally* node-only: everything else in `http.ts` is plain `fetch`.
Moving the `https.Agent` branch behind a dynamic import, or dropping it, makes it isomorphic — **(c)-S**,
one file.

### 1.2 Package/bundler shape

`package.json:5-20`: `"type": "module"`, dual ESM/CJS, two exports (`.` and `./agui`) with
`types`/`import`/`require` conditions. What is missing for a browser consumer:

- no `"sideEffects": false` -> no tree-shaking guarantee;
- no `"browser"` export condition -> nothing steers a bundler away from `node:https`;
- `src/agui/index.ts:389-401` statically re-exports `createPandoAgent`, `discoverPandoAgents`,
  `registerPandoCopilotKit` from `copilotkit.ts`. Those only *dynamically* import the peers
  (`copilotkit.ts:279-288`), so nothing node-only is pulled in, but `import(/* @vite-ignore */ specifier)`
  with a variable specifier produces a Vite warning and dead weight in the bundle. A
  `@pando-ai/sdk/agui/client` deep export would remove the problem — **(c)-S**.
- `tsconfig.json` targets ES2022 with `"lib": ["ES2022"]` and no `DOM` lib; the agui client still
  typechecks because `@types/node` supplies `fetch`/`ReadableStream`. The published `.d.ts` has not
  been validated against a DOM-only lib set.

`engines` declares node/bun/deno only (`package.json:21-25`) — the claim that the agui subpath
"works in ... a browser" (`src/agui/client.ts:5`) is true for the code but is backed by no browser
test or build target.

### 1.3 Alternative: raw `@ag-ui/client` `HttpAgent`

Inspected the version the Pando example pins, `@ag-ui/client@0.0.39`
(`examples/copilotkit/package.json:13`, `node_modules/@ag-ui/client/dist/index.mjs`).

What it *adds* over `PandoAguiClient`:

- an `AbstractAgent` that owns `threadId`, `messages` and `state`, with `onMessagesChanged` /
  `onStateChanged` subscribers;
- **applies RFC-6902 `STATE_DELTA` for you** via `fast-json-patch` (`applyPatch` imported at the top of
  `dist/index.mjs`) — exactly the piece the Pando SDK leaves to the app;
- `verifyEvents`, a protocol-ordering validator;
- an `AbortController` per agent with `abortRun()`; requests are `POST` + `Accept: text/event-stream`.

What it *costs / breaks* against Pando's actual stream:

- **`RUN_FINISHED.outcome` is not in `RunFinishedEventSchema`** (only `threadId`, `runId`, `result`).
  Events are not zod-parsed on the streaming path so the extra field survives as raw JSON, but no
  built-in API exposes it: the interrupt signal Pando's whole HITL/frontend-tool design depends on
  (`events.go:220-227`) is invisible to the agent abstraction.
- **`EventType` in `@ag-ui/core@0.0.39` has no `REASONING_*` and no `ACTIVITY_*`** — it has
  `THINKING_TEXT_MESSAGE_*` instead. Pando emits `REASONING_START / REASONING_MESSAGE_START /
  REASONING_MESSAGE_CONTENT / REASONING_MESSAGE_END / REASONING_END` (`events.go:212-216`,
  `translate.go:164-176`). They pass `verifyEvents`' default branch but are never reduced into
  `messages`: **thinking output silently disappears** in an HttpAgent-driven UI.
- rxjs 7.8 + zod + uuid + `@ag-ui/proto` + `@ag-ui/encoder` in the browser bundle.
- `dist/index.mjs` reads `process.env.NODE_ENV` / `process.env.JEST_WORKER_ID` in a subscriber-error
  path — under Vite dev this throws `process is not defined` unless you add a `define`.

### 1.4 Recommendation for git-in-track

**Hand-roll on top of `PandoAguiClient`'s transport, not on top of `HttpAgent`.**

Concretely: depend on `@pando-ai/sdk/agui` for `PandoAguiClient` + `parseSSE` + the `types.ts`
declarations + `PERMISSION_TOOL_NAME`, and write ~150 lines of git-in-track-side reducer that the SDK
does not provide (transcript assembly, JSON-Patch apply, interrupt resume, permission cards). Rationale:

1. the transport half is already correct and dependency-free (chunk-boundary reassembly is tested,
   `tests/agui.test.ts:71`);
2. the reducer half must be written either way, because neither the SDK nor `HttpAgent` handles
   Pando's `outcome:"interrupt"`, `pando_permission_request`, `REASONING_*` or `pando.*` CUSTOM signals;
3. `HttpAgent` would add ~6 runtime deps and *still* need the same custom code, plus a `process` shim.

Fallback if the SDK is not published to a registry git-in-track can consume: vendor
`src/agui/client.ts` + `src/agui/types.ts` (~470 lines, zero deps) into `web/src/lib/agui/`. That is
also the cheapest way to fix the gaps below without waiting on Pando releases.

CopilotKit is a non-starter for this app — see §5.

---

## 2. Completeness of the AG-UI client vs the server

### 2.1 `RunAgentInput` field coverage

Server struct: `internal/agui/input.go:38-47`. SDK request type: `src/agui/types.ts:229-238`.
SDK *builder*: `src/agui/client.ts:205-221`.

| Field | Server | SDK type | Reachable from `RunOptions`? |
|---|---|---|---|
| `threadId` | required (`input.go:186`) | yes | yes (auto-generated when omitted) |
| `runId` | required (`input.go:189`) | yes | yes (auto-generated) |
| `parentRunId` | accepted, **unused** | yes | **no** — `buildInput` never sets it |
| `state` | echoed into `StateDoc.Client` (`state.go:92-95`) | yes | yes |
| `messages` | consumed: last user message + trailing tool messages only (`input.go:200-231`) | yes | yes |
| `tools` | proxied when `FrontendTools` is on (`server.go:293`, `319-326`) | yes | yes |
| `context` | flattened into a `<context>` prompt block (`input.go:235-253`) | yes | yes |
| `forwardedProps` | **decoded and dropped** — only occurrence is the struct field, `input.go:46` | yes | **no** |

Message shape gaps: server `Message` has `activityType` (`input.go:164`) — absent from `AguiMessage`
(`types.ts:204-212`); server `content` accepts a string *or* an array of multimodal parts
(`input.go:76-102`), while `AguiMessage.content` is `string | undefined`, so image/document parts
cannot be typed (they are also discarded server-side, `input.go:114-129`).

### 2.2 Event coverage

"Emitted?" = a Go call site outside `events.go` exists. "Typed?" = a dedicated interface in
`src/agui/types.ts`. "Parsed/helper?" = the SDK does anything beyond `JSON.parse`.

| Event | Emitted by server | Typed in SDK | Parsed | Helper |
|---|---|---|---|---|
| `RUN_STARTED` | yes (`translate.go` Start(), `server.go:312`) | yes (`types.ts:57`) | passthrough | no |
| `RUN_FINISHED` (+`outcome`, `result`) | yes (`server.go:432,440,491`) | yes, incl. `RunOutcome` (`types.ts:55,64`) | passthrough | **no** — no interrupt detection |
| `RUN_ERROR` (+`code`) | yes (`server.go:336,449`); codes `session_busy`, `cancelled` (`server.go:542-552`) | yes (`types.ts:72`) | `runText` throws on it (`client.ts:197`) | partial |
| `STEP_STARTED` / `STEP_FINISHED` | **never** (no call sites) | name only, no interface | — | — |
| `TEXT_MESSAGE_START/CONTENT/END` | yes (`translate.go:160-162`, `304`) | yes | `runText` concatenates CONTENT (`client.ts:192-202`) | minimal |
| `TEXT_MESSAGE_CHUNK` | **never** | name only | — | — |
| `TOOL_CALL_START/ARGS/END` | yes (`translate.go:254-279`) | yes | no | no |
| `TOOL_CALL_RESULT` | yes (`translate.go:200`) | yes | no | no |
| `STATE_SNAPSHOT` | yes, after every RUN_STARTED (`server.go:316`; resume `server.go:377`) | yes, full `PandoState` (`types.ts:296-307`) | no | no |
| `STATE_DELTA` (RFC 6902) | yes: `/todos`, `/tokenUsage`, `/files/-`, `/files/{i}`, sub-agent paths (`state.go:252,274,328,334`) | yes, `JsonPatchOperation` (`types.ts:122`) | **no — no patch applier ships** | **no** |
| `MESSAGES_SNAPSHOT` | **never emitted** (zero `NewMessagesSnapshot` call sites) | name only | — | — |
| `ACTIVITY_SNAPSHOT` / `ACTIVITY_DELTA` | **never emitted** (zero call sites) | name only | — | — |
| `REASONING_START` / `REASONING_MESSAGE_START` / `_CONTENT` / `_END` / `REASONING_END` | yes (`translate.go:169-176`, `282-291`) | only `REASONING_MESSAGE_CONTENT` (`types.ts:139`); the other four fall into `OtherAguiEvent` | no | no |
| `RAW` | **never emitted** | name only | — | — |
| `CUSTOM` | yes, see below | yes (`types.ts:145`) | no | no |

**Every `pando.*` CUSTOM signal** (complete list, all call sites):

| name | payload | site |
|---|---|---|
| `pando.frontendToolsDisabled` | `{count, reason}` | `server.go:322` |
| `pando.todos` | todo array — **only when no state tracker**, i.e. never in the current wiring since `withState` is always set (`server.go:311`) | `translate.go:219` |
| `pando.tokenUsage` | usage — same dead-unless-stateless condition | `translate.go:225` |
| `pando.summarize` | `{progress, done}` | `translate.go:228` |
| `pando.<agentEventType>` | system-message string, for `AgentEventTypeSystemMessage`, `SteeringQueued`, `SteeringInjected`, `ConclusionQueued`, `ConclusionInjected`, `Resurrected` | `translate.go:233-242` |

A UI must therefore be prepared for `pando.summarize`, `pando.frontendToolsDisabled` and six
`pando.<eventType>` names whose exact spelling comes from `agent.AgentEventType` constants. **The SDK
types none of these** — no union of custom names, no payload types. (c)-S.

### 2.3 Interrupt / resume, permissions, questions

Server contract:
- a frontend tool call or a HITL prompt suspends the run and ends the response with
  `RUN_FINISHED{outcome:"interrupt"}` (`server.go:470-504`);
- the *next* POST on the same threadId must carry tool messages **after the last user message**
  (`input.go:216-231`) whose `toolCallId` matches; `deliverToolResults` resolves them and
  `resumeRun` re-attaches without starting a new agent run (`server.go:356-399`);
- permission prompts arrive as a synthetic tool call named `pando_permission_request` with args
  `{toolName, action, description, path, params}` (`hitl.go:38`, `115-137`); the answer is read by
  `approvalFromMessage` (`hitl.go:140-172`) which accepts `{"approved":true}`, `{"allow":true}`, or the
  literals `true/yes/approve/approved/allow/accept`. **Anything else, including no answer, is a deny.**
- questions arrive as a real `AskUserQuestion` tool call (`hitl.go:176-220`); the answer may be prose
  or `{cancelled, answers:[{questionId, header, selected[], otherText}]}` (`hitl.go:228-262`);
- the suspension window is 10 minutes (`frontend_tool.go:41`); the run is reaped at 11 (`run.go:28`).

SDK coverage: **constants and doc comments only.** `PERMISSION_TOOL_NAME` and
`PandoPermissionRequest` / `PandoPermissionAnswer` are exported (`client.ts:37-54`), and `run()`'s
docstring explains the resume protocol (`client.ts:160-166`) — but there is no
`resume(threadId, toolCallId, result)`, no transcript accumulator that appends the assistant message +
tool message in the right order, and no typed shape for the question tool's answer at all. Every
integration must re-derive the message-ordering rule from `input.go`. This is the single largest gap
for a chat panel: **(c)-M** (or (b), ~80 lines app-side).

### 2.4 STATE_DELTA: who applies the patch?

**The app does.** `src/agui/types.ts:121-137` declares `JsonPatchOperation` and nothing more; there is
no `applyPatch` anywhere in `src/`. Contrast `@ag-ui/client`, which bundles `fast-json-patch`. A
git-in-track panel that renders todos/token usage/files must add a patch applier. The paths Pando
emits are simple (`/todos`, `/tokenUsage`, `/files/-`, `/files/{index}`, plus `/subAgents/...`), so a
30-line hand-rolled `add`/`replace`/`remove` applier suffices and avoids a dependency.

---

## 3. Thread lifecycle for a web app

### 3.1 On page reload: no protocol answer

- There is **no** `GET {path}/threads`, no `GET {path}/threads/{id}/messages`, no reattach route.
  `Register` mounts exactly three things: `GET {path}/info`, `OPTIONS {path}/`, `POST {path}/{agent}`
  (`server.go:26-33`).
- `MESSAGES_SNAPSHOT` exists in the type system (`events.go:429-436`) but is **never emitted**, so
  even a fresh POST does not replay history.
- The thread->session binding *is* durable (`agui_threads` table, `threads.go:279-392`), and the
  `StateDoc` is per-thread but **memory-only, LRU-capped at 256 threads** (`state.go:179`). After a
  Pando restart the agent still has its history; the browser gets a fresh `STATE_SNAPSHOT` with empty
  todos/files.
- AG-UI's design assumption is that *the client owns the transcript* (`input.go:35-37`). With no
  server-side read API, a browser that reloads and has no local copy has lost the conversation.

**Workaround (b), and a good one:** `sessionForThread` honours a Pando **session id used as the
threadId** (`runtime.go:138-144`). So git-in-track can:
1. create/track a Pando session id (REST `POST /api/v1/sessions`, `internal/api/routes.go:29`), or
   simply record the `session` field of the first `STATE_SNAPSHOT` (`state.go:80-82`);
2. use that id as the AG-UI `threadId` from then on;
3. on reload, rebuild the transcript from REST `GET /api/v1/sessions/{id}` which returns
   `{session, messages, is_running}` (`internal/api/handlers_sessions.go:112-131`).

Caveat: that REST API is **not served by `pando agui-serve`** — that process mounts only
`runtime.Handler()` (`cmd/agui_serve.go:159`; the command docstring at `cmd/agui_serve.go:22-35` states
the REST API and Web UI are deliberately absent). You need `pando serve --agui-port`, which re-exposes
the whole Web-UI API surface; the companion proxy should then whitelist exactly `{path}/*` plus
`GET /api/v1/sessions*`.

Persisting the transcript in git-in-track's own store (IndexedDB or the Go companion) is the other
workaround and avoids the REST dependency entirely.

### 3.2 Reattaching to a run in progress

Not possible. There is no listener/replay buffer on the AG-UI side: `listener.go` is only the
dedicated `http.Server` (nothing to do with event listeners). Worse, a disconnect is destructive —
`stream()`'s `<-ctx.Done()` branch calls `finishRun(run)`, which cancels the agent
(`server.go:412-417`). A browser that loses the network mid-run kills the run.

Compare the Web-UI path, which *does* have this: `GET /api/v1/sessions/{id}/stream` "will replay
buffered events and resume live" (`internal/api/handlers_chat.go:112`, route at
`internal/api/routes.go:30`). The capability exists in Pando; it is simply not wired into the AG-UI
adapter. Porting the buffered-replay listener into `internal/agui` is **(c)-M**
(`internal/agui/{server,run}.go` + a replay buffer).

### 3.3 Cancellation

`signal.abort()` on the SDK's `run()` aborts the fetch (`client.ts:228-233`), closing the response
body; Go's request context fires, `stream()` takes the `ctx.Done()` branch and `finishRun` ->
`run.cancel()` -> the agent's context is cancelled (`run.go:86-99`, `146-152`). **Abort really cancels
the agent, it does not leak.** Two caveats:

- a **suspended** run has no open HTTP request, so it cannot be aborted at all; it dies after
  `suspendGrace` = 10 min + 1 min (`run.go:28`). A "Stop" button during a permission prompt does
  nothing server-side.
- there is no explicit cancel endpoint, so cancelling from *another* tab is impossible.

### 3.4 Two browser tabs on one thread

Broken by design in the current server. `runStore` is keyed by threadId (`run.go:118-122`), and a POST
on a thread that already has a live run either resumes it (if it carries matching tool results) or
**abandons it** (`server.go:263-271`, `abandonRun` at `run.go:157-175`). Two tabs on one thread cancel
each other; two threads mapping to the *same session* hit `agent.ErrSessionBusy` ->
`RUN_ERROR{code:"session_busy"}` (`server.go:546`). Recommendation: one thread per tab, or
leader-election in the app (BroadcastChannel) — **(b)**.

---

## 4. Auth from a browser, and what the Go proxy must forward

`authorize()` (`server.go:47-71`) runs for both `/info` and the run endpoint:

1. `origin := req.Header.Get("Origin")`. **If Origin is empty, the check is skipped entirely** — no
   allow-list consulted, no CORS headers set (`server.go:48-53`; `setCORSHeaders` early-returns at
   `server.go:95-97`).
2. If Origin is present and not in `AllowedOrigins` (case-insensitive, `*` allowed) -> **403 `origin
   not allowed`**, before auth.
3. Token: `Authorization: Bearer <t>` or `?token=<t>` (`bearerToken`, `server.go:73-79`), compared with
   `subtle.ConstantTimeCompare`. Missing/wrong -> 401. `RequireToken` defaults on for `agui-serve`
   unless `--no-token` (`cmd/agui_serve.go:97`).

Consequences for the companion Go proxy:

- **A server-to-server proxy that does not set `Origin` needs no allow-list at all** — `AllowedOrigins`
  can stay empty and Pando still serves it. That is the recommended shape: the browser talks
  same-origin to git-in-track's Go companion, which adds `Authorization: Bearer` and forwards without
  an Origin header. The Pando token never reaches the browser.
- If the proxy *forwards* the browser's Origin (e.g. `http://localhost:5173`), that exact string must
  be in `--allow-origin`; a rewritten/invented Origin that is not listed yields a 403 easy to
  misdiagnose as an auth failure. Comparison is exact-string (plus `*`), not suffix/wildcard
  (`server.go:85-92`).
- The proxy must **not** buffer: Pando sets `X-Accel-Buffering: no` and flushes per event
  (`sse.go:38-45`), and sends a `: keep-alive` comment every 15 s (`deps.go:86`, `server.go:419-423`).
  A Go `httputil.ReverseProxy` needs `FlushInterval: -1`.
- No write timeout is set on the dedicated listener precisely because the streams are long-lived
  (`listener.go:488-491`); the proxy must match that.
- `/info` returns URLs built from the `Host` header, honouring `X-Forwarded-Proto` for the scheme only
  (`server.go:226-238`). Behind a path-rewriting proxy those URLs are wrong; the SDK already works
  around it by keeping the configured origin and taking only the path (`copilotkit.ts:133-140`).
- TLS: `agui-serve` self-signs into the data dir unless `--no-tls` (`cmd/agui_serve.go:127-140`). The
  Go proxy needs `InsecureSkipVerify` or the generated cert — plan for `--no-tls` on loopback.

**Per-user identity: none.** No user concept in `internal/agui` at all. `forwardedProps` — the natural
carrier — is decoded and never read (`input.go:46`; exactly one occurrence in the repo). `context[]`
ends up inside the prompt text (`input.go:235-253`), so it is model-visible, not policy-visible.
`state` is echoed into `StateDoc.Client` for rendering only (`state.go:92-95`). A multi-user
git-in-track deployment must do identity/authorization in the Go companion; Pando sees one token and
one identity. Making `forwardedProps` reach permission policy / session metadata is **(c)-M**
(`internal/agui/{input,runtime,hitl}.go`).

---

## 5. `copilotkit.ts`: what it gives, what it costs

Gives (`src/agui/copilotkit.ts`):
- `createPandoAgent` — one `HttpAgent` with the bearer header pre-attached (`copilotkit.ts:73-86`);
- `discoverPandoAgents` — reads `/info` and builds one `HttpAgent` per advertised agent, keeping the
  caller's origin and taking only the path from discovery (`copilotkit.ts:95-140`) — a genuinely
  useful proxy workaround;
- `registerPandoCopilotKit` — a whole Next.js App Router route: `CopilotRuntime({agents})` +
  `ExperimentalEmptyAdapter` + `copilotRuntimeNextJSAppRouterEndpoint` (`copilotkit.ts:222-239`).

Costs:
- **A Node server hop is mandatory.** CopilotKit's client speaks GraphQL to its own runtime; Pando
  deliberately does not implement it (`internal/agui/doc.go`, "Deliberately not implemented"). So
  git-in-track would need a Node process in addition to its Go companion — the module's own docstring
  admits it removes "the boilerplate, not the hop" (`copilotkit.ts:216-220`).
- `@copilotkit/runtime@1.64.1` declares peers on `openai`, `langchain`, `@langchain/*`,
  `@anthropic-ai/sdk`, `groq-sdk` (verified in the example's `node_modules`). Heavy, and pointless
  here since the agent owns the model.
- The route helper is **Next.js-shaped**; git-in-track has no Next.js.
- React 19 is *not* actually required: `@copilotkit/react-core@1.64.1` peers are `"react": "^18 || ^19"`.
  The example pins React 19 + Next 16 (`examples/copilotkit/package.json`), but React 18.3.1 is
  acceptable to CopilotKit itself. The blocker is the Node runtime, not React.

Reusable without CopilotKit:
- the `/info`-discovery + origin-rewriting logic of `agentUrl` (`copilotkit.ts:133-140`) — worth
  copying into the Go proxy;
- `authHeaders` (trivial);
- `HttpAgentLike` / `HttpAgentConstructor` structural typings — only if you adopt `@ag-ui/client`.
- Nothing else. For a Vite SPA, `copilotkit.ts` is dead weight that should not be re-exported from
  the `agui` barrel (see §1.2).

**Verdict: do not use CopilotKit for git-in-track.** The example app under `examples/copilotkit/app/`
is still worth reading as a reference for *what the state document affords*: `app/page.tsx` renders
model, token budget, todos, files and sub-agents purely from `useCoAgent<PandoState>()`, and binds the
permission card to `PandoPermissionRequest`.

---

## 6. Multi-project / working directory

One repo per Pando process. Evidence:

- `pando agui-serve --cwd X` calls `os.Chdir(X)` once at startup and then `config.Load(cwd, ...)`
  (`cmd/agui_serve.go:56-74`, `resolveWorkingDir` at `cmd/agui_serve.go:208-216`);
- the SQLite DB is opened from `config.Get().Data.Directory` (`internal/db/connect.go:23-27`), i.e.
  the project's `.pando/`;
- single-instance arbitration is per workdir via `<workdir>/.pando/ipc.lock`
  (`internal/ipc/lock_common.go:32`, hint at `internal/ipc/runtime/runtime.go:109-111`);
- **nothing in `internal/agui` reads a cwd/workdir from the request.** `RunAgentInput` has no such
  field (`input.go:38-47`); `resolveAgent` only picks an agent *name* from the path
  (`runtime.go:104-117`). `context[]` could carry a path string but it would only be prose in the
  prompt, not a real working directory for the tools.

What a web solution serving several git-in-track repos needs, in increasing order of Pando work:

1. **(b) Process per repo, routed by the Go companion.** `pando agui-serve --cwd /repo/N --port 80XX`,
   one token each; the companion maps `projectId -> {baseUrl, token}` and proxies. Works today, costs
   one Pando process (+ agent pool, default 4, `deps.go:84`) per repo. Recommended shape.
2. **(c)-L Per-request project selection.** Requires a request-scoped working directory threaded
   through `agentPool`, `config`, the tool layer and the session store — Pando's config and DB are
   process-global (`config.Get()`), so this is a deep change, not an adapter change. Do not ask for it.
3. **(c)-M middle ground**: an `agui-serve` that accepts a repeated `--cwd` and exposes one route
   prefix or agent name per project, each with its own `app.App`/DB. Still substantial
   (`internal/app`, `internal/db`, `cmd/agui_serve.go`) but avoids per-request cwd.

Capacity note: `AgentPoolSize` defaults to 4 with a 30-min TTL (`deps.go:84-85`, `deps.go:111`), and
the per-thread state store is capped at 256 threads (`state.go:179`).

---

## 7. SDK hygiene

- **Version** `0.1.0` (`package.json:3`), name `@pando-ai/sdk`, MIT. Not verified as published to npm;
  the example consumes it as `file:../../sdk/typescript`.
- **Typecheck**: `cd sdk/typescript && npx tsc --noEmit` -> exit 0, no diagnostics. Strict mode with
  `exactOptionalPropertyTypes` and `noUncheckedIndexedAccess` (`tsconfig.json`), hence the
  `...(x ? {x} : {})` spreads throughout.
- **Tests** (`tests/agui.test.ts`, 318 lines): 7 for `PandoAguiClient`/`parseSSE` — bearer header +
  URL shape, chunk-boundary reassembly, `runText` concatenation, `RUN_ERROR`, error status mapping,
  no-token case, `/info`; plus 5 for the CopilotKit glue. All mock `fetch`. Bun and Deno suites exist
  (`tests/bun/`, `tests/deno/`) but **do not cover agui at all**.
- **Untested / absent behaviours**: interrupt->resume round-trip, `pando_permission_request` answer
  shape, `AskUserQuestion` answer shape, `STATE_DELTA` application, `CUSTOM pando.*` handling, abort
  mid-stream, non-SSE content-type response, 401/403 distinction, reconnect/backoff.
- **Typing gaps** (all in `src/agui/`):
  - `RunOptions` cannot set `forwardedProps` or `parentRunId` (`client.ts:77-95` vs `types.ts:229-238`);
  - no interfaces for `REASONING_START/MESSAGE_START/MESSAGE_END/REASONING_END`, `STEP_*`,
    `MESSAGES_SNAPSHOT`, `ACTIVITY_*`, `RAW` — they degrade to `OtherAguiEvent` with an `unknown`
    index signature (`types.ts:173-176`), so `event.messageId` is `unknown` and needs a cast;
  - `CustomEvent.value` is `unknown` and `name` is `string` — no union of the `pando.*` names;
  - `AguiMessage.content` is `string` only (no multimodal parts), and `activityType` is missing;
  - `PandoTodo` is `[key: string]: unknown` — weaker than the Go `tools.TodoItem`;
  - name collision: `RunOptions` is exported from both `./index` (subprocess mode, `client.ts`) and
    `./agui` — different shapes, same name.
- **Small correctness notes**: `PandoAguiError` is constructed with `status: 0` for a `RUN_ERROR`
  (`client.ts:198`), conflating a protocol error with an HTTP one; `parseSSE` silently drops malformed
  frames (`client.ts:299-303`) — deliberate, but a web app gets no telemetry; there is no check that
  the response `Content-Type` is `text/event-stream`, so a proxy returning HTML yields an empty,
  successful-looking run. The header timeout (default 60 s, `client.ts:123`) is correctly cleared once
  headers arrive so it does not truncate long streams (`client.ts:226-244`).
- **Docs**: `README.md:180-230` documents the agui subpath accurately (token + origin allow-list, off
  by default) but says nothing about interrupts, permissions or state patches.
- **Index docstring is stale**: `src/index.ts:4-9` still says "Pando operates in three modes" and does
  not mention AG-UI.

---

## Proposed backlog items (PANDO project)

### Epic: PANDO — AG-UI thread & run lifecycle for browser clients
Give browser clients a way to recover a conversation after a reload and to survive a dropped
connection. Today the AG-UI surface has exactly one route (POST run) and a disconnect cancels the
agent, which makes a production web panel impossible without leaning on the Web-UI REST API.

- **Story: `GET {path}/threads` and `GET {path}/threads/{id}/messages`**
  Expose the adapter-owned `agui_threads` bindings and the session's message history as AG-UI
  `Message[]`, so a reloaded page can rebuild its transcript without the Web-UI REST API.
  *AC:* both routes go through `authorize()`; messages come back in AG-UI shape (role, toolCalls,
  toolCallId); thread list is scoped and paginated; `agui-serve` serves them; tests in
  `internal/agui/threads_test.go`.
- **Story: emit `MESSAGES_SNAPSHOT` on the first run of a known thread**
  `NewMessagesSnapshot` exists but has zero call sites; emit it right after `STATE_SNAPSHOT` when the
  thread already has a session, so a client that lost its transcript is resynchronised in-band.
  *AC:* emitted only when the thread pre-existed; ordering RUN_STARTED -> STATE_SNAPSHOT ->
  MESSAGES_SNAPSHOT; size-capped; covered by `server_test.go`.
- **Story: replay buffer + reattach for in-flight runs**
  Port the Web-UI's buffered-replay behaviour (`internal/api/handlers_chat.go:112`) into the adapter so
  a reconnect resumes a live run instead of killing it.
  *AC:* a disconnect no longer calls `finishRun` immediately (grace period); a re-POST or a
  `GET {path}/runs/{threadId}/stream` replays buffered events then continues live; two tabs can follow
  one run read-only.
- **Story: explicit run cancellation endpoint**
  `POST {path}/runs/{threadId}/cancel` that cancels both live and *suspended* runs.
  *AC:* cancels a run parked on a permission prompt (today only the 11-minute reaper does); emits
  `RUN_ERROR{code:"cancelled"}` to any attached stream; idempotent.

### Epic: PANDO — AG-UI request identity and multi-project routing
AG-UI currently carries no user identity and one Pando process serves exactly one repository.
Both block a multi-user, multi-repo git-in-track deployment.

- **Story: make `forwardedProps` load-bearing**
  It is decoded (`input.go:46`) and never read. Surface it to the runtime as request metadata (user id,
  project id, correlation id) available to permission policy, session titles and logs.
  *AC:* a typed `ForwardedProps` struct with an escape-hatch map; recorded on the session; visible in
  `logging`; documented in the SDK.
- **Story: per-user permission policy over AG-UI**
  Map an identity from `forwardedProps` to an approval policy instead of the single process-wide
  `AutoApprove`/`HumanInTheLoop` pair.
  *AC:* policy resolved per request; a denied identity gets a clear `RUN_ERROR`; fails closed.
- **Story: multi-project `agui-serve`**
  Accept repeated `--cwd`, exposing one project per route prefix or per agent id, each with its own
  app/DB, so one process serves several git-in-track repositories.
  *AC:* `--cwd` repeatable; `/info` advertises the projects; ipc.lock acquired per workdir; a run is
  pinned to one project for its whole thread.

### Epic: PANDO — TypeScript SDK, browser-first
The SDK's AG-UI client is a transport plus type declarations; every consumer must re-implement
transcript assembly, interrupt/resume and state patching from the Go source.

- **Story: browser-safe package build**
  Remove the static `node:https` import from `src/http.ts`, add `"sideEffects": false`, a `browser`
  export condition, and a `./agui/client` deep export that does not drag `copilotkit.ts` in.
  *AC:* a Vite React 18 app imports the agui client with no warnings and no node polyfills; a smoke
  test builds the SDK under Vite in CI.
- **Story: a stateful `PandoThread` helper**
  A small class over `PandoAguiClient` owning `threadId`, the transcript and the state document:
  accumulates TEXT/REASONING/TOOL events into messages, applies `STATE_DELTA` (RFC 6902), detects
  `RUN_FINISHED{outcome:"interrupt"}` and exposes `resume(toolCallId, result)`.
  *AC:* resume builds the trailing-tool-message shape `input.go:216-231` requires; patch application
  covers add/replace/remove including `/files/-`; unit tests replay a recorded Pando stream.
- **Story: typed HITL helpers**
  First-class helpers for `pando_permission_request` (`approve()` / `deny()` producing
  `{"approved":boolean}`) and for `AskUserQuestion` (typed `{cancelled, answers[]}`), matching
  `hitl.go:140-262`.
  *AC:* answers are accepted by the Go side in a round-trip test; deny-by-default semantics documented.
- **Story: complete the event and input typings**
  Interfaces for `REASONING_*`, `STEP_*`, `MESSAGES_SNAPSHOT`, `ACTIVITY_*`, `RAW`; a union of the
  `pando.*` CUSTOM names with payload types; `forwardedProps` and `parentRunId` on `RunOptions`;
  multimodal `content` parts and `activityType` on `AguiMessage`; rename the colliding `RunOptions`.
  *AC:* generated from or diffed against `internal/agui/{events,input}.go` in CI so drift is caught.
- **Story: agui test coverage for the protocol's hard half**
  Tests for interrupt->resume, permission and question round-trips, STATE_DELTA application, aborted
  streams and non-SSE responses.
  *AC:* recorded-stream fixtures come from a real `agui-serve` run; bun/deno suites include agui.

### Epic: PANDO — documentation for non-CopilotKit web clients
The only worked example is a Next.js + CopilotKit app that needs a Node runtime; a plain SPA has no
reference.

- **Story: a Vite/React 18 example without CopilotKit**
  A minimal SPA + reverse-proxy recipe showing streaming, reasoning, tool cards, permission prompts
  and state rendering against `PandoAguiClient`.
  *AC:* runs against `agui-serve --no-tls` on loopback; no Node server beyond the proxy.
- **Story: document the proxy contract**
  Origin-header semantics (absent Origin bypasses the allow-list), `FlushInterval: -1`, no write
  timeout, keep-alive comments, `?token=` vs bearer, `/info` URL rewriting.
  *AC:* a section in `internal/agui/doc.go` and the SDK README; a copy-pasteable Go
  `httputil.ReverseProxy` snippet.

---

## Decisions for the user

1. **Client library.** Options: (a) depend on `@pando-ai/sdk/agui`, (b) vendor `client.ts` + `types.ts`
   into `web/src/lib/agui/`, (c) `@ag-ui/client` `HttpAgent`, (d) fully hand-rolled fetch+SSE.
   **Recommend (b) now, (a) once the browser-build story lands.** The SDK's transport is good and
   dependency-free; vendoring avoids a publishing/versioning dependency on Pando while the reducer half
   has to be written locally anyway. (c) loses `outcome`, `REASONING_*` and `ACTIVITY_*` and adds
   rxjs/zod/proto to the bundle.
2. **CopilotKit: in or out?** **Out.** It mandates a Node process speaking GraphQL that git-in-track
   does not otherwise need, and Pando refuses to implement that protocol on purpose
   (`internal/agui/doc.go`). React 19 is *not* the blocker — CopilotKit peers allow React 18.
3. **Deployment shape.** `pando agui-serve` (AG-UI only) vs `pando serve --agui-port` (AG-UI + full
   Web-UI REST API). **Recommend `agui-serve`** for the security posture, and solve history by
   persisting the transcript in git-in-track (IndexedDB or the Go companion) rather than by exposing
   the Web-UI API. If you would rather not store transcripts client-side, use `pando serve
   --agui-port` with the companion whitelisting only `{path}/*` and `GET /api/v1/sessions*`.
4. **Thread id convention.** **Recommend using the Pando session id as the AG-UI threadId** — the
   server explicitly honours it (`runtime.go:138-144`) and it makes every future history/reattach route
   trivially reachable. Cost: one extra REST call (or reading `STATE_SNAPSHOT.session` from the first
   run) before the thread id is stable.
5. **Origin handling in the Go proxy.** **Recommend stripping `Origin`** and keeping Pando's allow-list
   empty: the proxy is server-to-server, the token stays server-side, and `server.go:48-53` skips the
   CORS check when the header is absent. Forwarding the browser Origin only buys a second,
   easy-to-misconfigure defence.
6. **Multi-repo.** **Recommend one `agui-serve` process per repo**, routed by the companion
   (`projectId -> {baseUrl, token}`). Per-request cwd is a deep Pando change (process-global config and
   DB) and should not be requested.
7. **State document rendering.** Decide whether the panel renders `PandoState` (todos, token budget,
   touched files, sub-agents) at all. If yes, budget for a JSON-Patch applier — **recommend a 30-line
   hand-rolled applier** over `fast-json-patch`, since Pando only emits `add`/`replace` on four simple
   pointer shapes (`state.go:252,274,328,334`).
8. **Concurrency policy.** One live run per thread is a hard server constraint (`server.go:263-271`).
   **Recommend one thread per browser tab** plus a BroadcastChannel guard, until a reattach/replay
   story exists in Pando.
