---
title: Research — Pando AG-UI, SDK and semantic search
type: page
tags: [research, pando, agent, search]
---

> ⚠️ **Partly superseded — annotation added 2026-09-16 (`GIT-US-0099`).** Kept as written; it
> was accurate on its date. The export-to-`.pando-kb` shape it recommends (and the
> `internal/server/pandosync.go` hub subscriber) was built and then **retired** by
> `GIT-EP-0020`: Pando now indexes the repository's own committed files, `KBPath` is the
> repository's documentation folder, and there is no exporter. See
> [docs/21](../21-semantic-search.md) §5 and
> [ADR-036](../adr/ADR-036-pando-indexes-the-repository-directly.md).

# Pando → git-in-track integration research

Analysed `/www/MCP/Pando/pando`. Target `/www/git-in-track` (Go+chi `internal/server`, React 18.3.1 + Vite + TanStack Router/Query + Zustand + shadcn in `web/`). All paths absolute; line numbers verified unless marked *(uncertain)*.

---

## 1. AG-UI support in Pando

**Package `internal/agui`** — an isolated side-car adapter, **off by default**. The full design contract is in `/www/MCP/Pando/pando/internal/agui/doc.go` (invariants I1–I7, phases P0–P7).

| File | Purpose |
|---|---|
| `internal/agui/events.go` | event types + constructors |
| `internal/agui/input.go` | `RunAgentInput`, `Message`, `Tool`, `Context` |
| `internal/agui/sse.go` | SSE writer |
| `internal/agui/server.go` | routes, auth/CORS, `/info`, `handleRun` |
| `internal/agui/runtime.go` | `Runtime`, agent resolution |
| `internal/agui/agentpool.go` | builds agent instances |
| `internal/agui/state.go` | `StateDoc` shared state |
| `internal/agui/frontend_tool.go` | frontend-tool proxies + suspension registry |
| `internal/agui/hitl.go` | permissions + questions in the browser |
| `internal/agui/subagents.go` | mesnada sub-agents → state |
| `internal/agui/threads.go` | durable `agui_threads` thread→session map |
| `internal/agui/translate.go` | Pando events → AG-UI events |

Wiring: `internal/api/server.go:146` (`setupAGUI`), `:175-231`; routes `internal/api/routes.go:261-266`; AG-UI paths bypass the main auth/CORS middleware at `internal/api/server.go:233-239`, `:426`, `:506-507`.

### Endpoints (`internal/agui/server.go:26-33`)

```
GET     {path}/info      → discovery
OPTIONS {path}/          → preflight
POST    {path}/{agent}   → run, SSE stream
POST    {path}           → same, default agent
```

Default `{path}` = `/api/v1/agui` (`internal/config/config.go:1140-1141`). Concretely `POST http://localhost:8090/api/v1/agui/coder`.

`{agent}` must be one of the **seven built-in agent names** (`internal/config/config.go:56-81`): `coder`, `summarizer`, `task`, `title`, `cli-assist`, `persona-selector`, `context-enricher`. Resolution/allow-list: `internal/agui/runtime.go:103-115`. **Pando has no user-defined agents** — customisation is persona + skills + model.

### Event types (`internal/agui/events.go:9-42`)

`TEXT_MESSAGE_START|CONTENT|END|CHUNK`, `TOOL_CALL_START|ARGS|END|RESULT`, `STATE_SNAPSHOT|STATE_DELTA|MESSAGES_SNAPSHOT`, `ACTIVITY_SNAPSHOT|DELTA`, `RUN_STARTED|RUN_FINISHED|RUN_ERROR`, `STEP_STARTED|FINISHED`, `REASONING_START|MESSAGE_START|MESSAGE_CONTENT|MESSAGE_END|END`, `RAW`, `CUSTOM` (Pando signals namespaced `pando.*`, `:341-352`). Outcomes `"success"` / `"interrupt"` (`:45-52`). Struct shapes: `RunStartedEvent` :75, `RunFinishedEvent` :86, `ToolCallStartEvent` :173, `ToolCallResultEvent` :208, `StateSnapshotEvent` :236, `StateDeltaEvent` :245.

### Request format (`internal/agui/input.go:38-47`)

```jsonc
{ "threadId":"…",          // required, input.go:186
  "runId":"…",             // required, input.go:189
  "parentRunId":"…", "state":{…},
  "messages":[…],          // full transcript, resent every turn
  "tools":[{"name","description","parameters"}],   // :51-55
  "context":[{"description","value"}],             // :58-61
  "forwardedProps": any }
```

- **Only the trailing user message is forwarded to the agent** (`input.go:200-211`) — Pando keeps its own history, so replaying the array would defeat compaction/caching.
- `context[]` is rendered into a `<context>…</context>` block prefixed to the prompt (`input.go:235-253`, used `server.go:283-285`).
- **`forwardedProps` is decoded but has no consumer** — grep finds it only at `input.go:46`. Use `context[]` to pass per-request state.
- Trailing `tool` messages after the last user message = frontend-tool results resuming an interrupt (`input.go:216-231`; dispatch `server.go:263-271`).
- Multimodal parsed, text-only forwarded (`input.go:112-140`). Body cap 8 MiB (`sse.go:13`).

### SSE, not HTTP chunked JSON

`internal/agui/sse.go:33-47`: `text/event-stream`, `no-cache`, `keep-alive`, `X-Accel-Buffering: no`. Every frame is a bare `data: {json}\n\n` — the discriminator is the JSON `type`, not the SSE `event:` field (`:19-23`). Heartbeats are SSE comments (`:79`).

### Auth (`internal/agui/server.go:47-103`)

- **Origin allow-list mandatory** for browsers: empty ⇒ 403 on every cross-origin request; never `Access-Control-Allow-Origin: *` (`:81-92`).
- **Bearer token** when `RequireToken` (default true): `Authorization: Bearer …` or `?token=` (`:73-79`), constant-time compare (`:66`), fails closed if unprovisioned (`:59-64`).

### Frontend tools (P3)

`RunAgentInput.tools` become blocking `tools.BaseTool` proxies (`agentpool.go:110-116` → `frontend_tool.go:241`). A call emits `TOOL_CALL_START/ARGS/END` then `RUN_FINISHED{outcome:"interrupt"}` (`server.go:470-507`); the agent goroutine stays alive parented to the adapter's base context, not the HTTP request (`server.go:85-86`). The next POST on the same `threadId` with a trailing `tool` message **re-attaches** without a new run (`resumeRun` :356, `deliverToolResults` :388). The pool is keyed by agent name + hash of the declared toolset (`agentpool.go:54-77`).

### Human in the loop (`internal/agui/hitl.go`)

- Permission prompts arrive as a synthetic tool call **`pando_permission_request`** (`:40`); args at `:117`; anything that is not explicit approval **denies** (`approvalFromMessage` :145).
- `AskUserQuestion` is swapped for `hitlQuestionTool` (`:188-231`) that waits on the client; no answer ⇒ `questionCancelled` (`:225`).
- The adapter owns its own `permission.Service` / `userinput.Service`, so a browser run never raises a TUI prompt (invariant I3).

### Shared state (`internal/agui/state.go:80-95`)

```jsonc
{ "thread","session","agent",
  "model":{"id","name","provider","contextWindow"},        // :31-34
  "todos":[…],
  "tokenUsage":{"promptTokens","completionTokens","contextWindow","estimated",
                "cacheReadTokens","cacheWriteTokens","reasoningTokens","cost"}, // :40-47
  "files":[{"path","name","action"}],                      // :52-54
  "subAgents":[…],                                         // from mesnada traffic
  "client": any }
```

`STATE_SNAPSHOT` after every `RUN_STARTED`, then RFC-6902 `STATE_DELTA`. **The doc belongs to the thread, not the run.**

### CopilotKit

**Pando deliberately does NOT implement CopilotKit's runtime (GraphQL)** — `internal/agui/doc.go`, "Deliberately not implemented": AG-UI is the universal protocol, CopilotKit's runtime already translates it, and embedding it would move the API token into the browser. So **a Node hop is required if you use `@copilotkit/react-core`**.

Client glue in the TS SDK, `sdk/typescript/src/agui/copilotkit.ts`: `createPandoAgent` :73 (one `HttpAgent` at `{base}{path}/{agent}` with bearer header, :81-86), `discoverPandoAgents` :95 (reads `/info` → `Record<name, HttpAgent>`), `registerPandoCopilotKit`. Also `PandoAguiClient` / `parseSSE` :254 / `PERMISSION_TOOL_NAME` in `sdk/typescript/src/agui/client.ts`.

Runnable example `/www/MCP/Pando/pando/examples/copilotkit/`:
- `app/api/copilotkit/route.ts` — `registerPandoCopilotKit` + `CopilotRuntime`, `ExperimentalEmptyAdapter`, `copilotRuntimeNextJSAppRouterEndpoint`, `HttpAgent`
- `app/page.tsx:11-14` `CopilotKit` / `useCoAgent` / `useCopilotAction` / `CopilotSidebar`; `:34` `useCoAgent<PandoState>({name:AGENT})`; `:103-117` frontend tool; `:125-145` `useCopilotAction({name:"pando_permission_request", available:"remote", renderAndWaitForResponse})`
- deps: `@ag-ui/client ^0.0.39`, `@copilotkit/react-core|react-ui|runtime ^1.10.0`, next 16, **React 19**

**Pando's own web-ui uses neither `@copilotkit/*` nor `@ag-ui/*`** (verified in `web-ui/package.json`); those are optional *peerDependencies of the TS SDK only* (`sdk/typescript/package.json:38-46`).

### Example request / stream

```http
POST /api/v1/agui/coder
Authorization: Bearer <token>
Origin: http://localhost:5173
Content-Type: application/json

{"threadId":"thr_01","runId":"run_01",
 "messages":[{"id":"m1","role":"user","content":"Which stories mention the KB watcher?"}],
 "tools":[{"name":"open_item","description":"Open a backlog item",
           "parameters":{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}}],
 "context":[{"description":"current project","value":"GIT"}],"state":{}}
```

```
data: {"type":"RUN_STARTED","threadId":"thr_01","runId":"run_01","timestamp":1770000000000}

data: {"type":"STATE_SNAPSHOT","snapshot":{"thread":"thr_01","session":"ses_…","agent":"coder","model":{…},"todos":[],"tokenUsage":{…},"files":[],"subAgents":[]}}

data: {"type":"TEXT_MESSAGE_START","messageId":"msg_1","role":"assistant"}
data: {"type":"TEXT_MESSAGE_CONTENT","messageId":"msg_1","delta":"Let me search"}
data: {"type":"TOOL_CALL_START","toolCallId":"tc_1","toolCallName":"kb_search_documents","parentMessageId":"msg_1"}
data: {"type":"TOOL_CALL_ARGS","toolCallId":"tc_1","delta":"{\"query\":\"KB watcher\"}"}
data: {"type":"TOOL_CALL_END","toolCallId":"tc_1"}
data: {"type":"TOOL_CALL_RESULT","messageId":"msg_2","toolCallId":"tc_1","content":"…","role":"tool"}
data: {"type":"STATE_DELTA","delta":[{"op":"replace","path":"/tokenUsage/promptTokens","value":12345}]}
data: {"type":"TEXT_MESSAGE_END","messageId":"msg_1"}
data: {"type":"RUN_FINISHED","threadId":"thr_01","runId":"run_01","outcome":"success"}
```

Interrupt variant ends `…TOOL_CALL_END` then `RUN_FINISHED{outcome:"interrupt"}`; the client resumes by re-POSTing the same `threadId` with a trailing `{"role":"tool","toolCallId":"tc_9","content":"opened"}`.

`GET /api/v1/agui/info` (`server.go:150-193`):

```json
{"protocol":"ag-ui","version":"…","path":"/api/v1/agui",
 "agents":[{"name":"coder","description":"Pando coder agent",
            "url":"http://localhost:8090/api/v1/agui/coder",
            "model":{"id":"…","name":"…","provider":"…","contextWindow":200000}}],
 "capabilities":{"frontendTools":true,"humanInTheLoop":true,"sharedState":true,"interrupts":true}}
```

---

## 2. Pando SDK (`sdk/`)

| Dir | Package |
|---|---|
| `sdk/typescript` | **`@pando-ai/sdk`** v0.1.0 + subpath `@pando-ai/sdk/agui` (ESM+CJS, Node≥18/Bun/Deno) |
| `sdk/python` | **`pando-sdk`** v0.1.0 (import `pando`, `pando.agui`), Py≥3.10 |
| `sdk/java` | `io.pando:pando-sdk` (Maven, Jackson) |
| `sdk/dotnet` | `Pando.Sdk` v0.1.0 (`Pando.Sdk.Agui.*`) |

**No Go client SDK.** `pkg/` holds only `pkg/extension` (in-binary extension contract) and `pkg/mesnada/models`. A Go host hand-rolls the HTTP/SSE client or speaks MCP.

Four modes (`sdk/typescript/README.md`):
1. **Subprocess one-shot** — `PandoClient.run()/stream()` (`pando -p "…"`).
2. **ACP stdio** — `PandoAgent`: `connect()`, `createSession()`, `session.send()` yielding `content_delta | thinking_delta | tool_call | tool_result | response | error | summarize`; `onToolPermission` callback; `listSessions/loadSession/setPersona/cancel`; `Symbol.asyncDispose`.
3. **HTTP REST** — `PandoHttpClient({baseUrl, apiToken, rejectUnauthorized, timeout})`: `sessions.create/list/rename/sendMessage(SSE)/streamSession`, `models.list/setActive`, `personas.list/setActive`, `health()`.
4. **AG-UI** — `PandoAguiClient.run(options)` (`sdk/typescript/src/agui/client.ts:77-95`: `prompt|messages|threadId|runId|tools|context|state|agent|signal`; body :204; POST+`parseSSE` :167-184).

### Exposing host tools to a Pando agent

**There is no "register a tool handler" API in any SDK.** Three mechanisms:

1. **MCP client config (recommended).** Pando dials *out*. `internal/config/config.go:31-54`:
   ```toml
   [MCPServers.gintrack]
   Type = 'streamable-http'   # or 'stdio' | 'sse'
   URL  = 'http://127.0.0.1:7777/mcp'
   Timeout = '60s'
   [MCPServers.gintrack.Headers]
   Authorization = 'Bearer <gintrack token>'
   ```
   Optional `[...Auth]` sub-table (mTLS) + OAuth 2.1 (`docs/mcp-authentication.md`, `POST /api/v1/mcp/{name}/login|logout`, `GET .../status`). **These tools reach AG-UI runs automatically**: `internal/agui/agentpool.go:82-90` passes `deps.Gateway` into `agent.CoderAgentToolsWithMesnada(orchestrator, remembrances, gateway, perms, history, lsp, userInput, sessions)`.
2. **AG-UI frontend tools** — browser-executed only; good for UI actions, bad for data.
3. *(Heavy)* **Compile-time extension** — `pkg/extension` (`docs/extension-authoring.md`): tools, `MemorySink`, and a `RemembranceSearchWrapper` that decorates search results (`pkg/extension/memory.go:1-40`). The docs themselves recommend MCP/Lua/skills instead.

### Mesnada

Tool constants `internal/llm/tools/mesnada.go:19-28`: `mesnada_spawn_agent` (Info :107-190), `mesnada_get_task` (:444-457), `mesnada_list_tasks` (:480-506), `mesnada_wait_task` (:547-564, **blocking**), `mesnada_cancel_task` (:605-618), `mesnada_get_task_output` (:641-659), `mesnada_note` (:706), `mesnada_swarm` (:791). Plus `mesnada_await` (`internal/llm/tools/mesnada_await.go`) — **non-blocking, preferred**: `task_ids[]`, `policy` (`all|any|quorum`), `quorum`, `timeout` (default `1h`); it ends the turn and auto-resumes the agent.

`mesnada_spawn_agent` params (required `prompt`): `task_id`, `prompt`, `work_dir`, `project`, `engine`, `model`, `background`, `timeout`, `dependencies[]`, `include_dependency_logs`, `dependency_log_lines`, `tags[]`, `persona` (enum dynamically bound to `orchestrator.ListPersonas()`, :123-135), `force`. The MCP-server-side schema (`internal/mesnada/server/tools.go:214-300+`) additionally exposes `mcp_config`, `extra_args[]`, `acp_mode` (`code|ask|architect`), `acp_agent`, `acp_config_options`, `acp_mcp_servers[]`.

**A mesnada "agent" is NOT `config.AgentName`.** It is engine + persona + model:
- **Engine** — `models.Engine`, `pkg/mesnada/models/task.go:21-55`: `pando` (default, `DefaultEngine()` :65-67), `copilot`, `claude`, `gemini`, `opencode`, `mistral`, `acp`, `acp-claude`, `acp-codex`, `ollama-claude`, `ollama-opencode`, `warm-acp`, plus runtime custom template engines.
- **Persona** — markdown role files, `internal/mesnada/persona/persona.go:42`; built-ins embedded (`assistant.md`, `qa.md`, `software-engineer.md`, `system-engineer.md`) overlaid from `Mesnada.Orchestrator.PersonaPath`; a persona may carry its own `mcp-config.json`. Text is prepended to the prompt.

Storage: JSON `FileStore` (`internal/mesnada/store/store.go:17-35`, `:48-92`) at `Mesnada.Orchestrator.StorePath` (`./.pando/mesnada`, `tasks.json`), with `ClaimForDispatch` CAS against double-dispatch. Execution: `orchestrator.startTask` (`internal/mesnada/orchestrator/orchestrator.go:874-925`) → warm path (`tryStartWarm`, reuse a per-project Pando ACP instance) or cold path (`agent/manager.go` → per-engine `exec.CommandContext` spawners).

HTTP (`internal/api/routes.go:162-167`, handlers in `internal/api/handlers_orchestrator.go`):
```
GET/POST   /api/v1/orchestrator/tasks                (:70 / :132)
GET/DELETE /api/v1/orchestrator/tasks/{id}           (:174 / :201)
POST       /api/v1/orchestrator/tasks/{id}/cancel    (:231)
GET        /api/v1/orchestrator/delegation/metrics   (:99)
```
`CreateTaskRequest` (:121-129) accepts only `prompt, work_dir, model, engine, tags, dependencies, background` — **no `persona`, no `project`, no `timeout`** — and forces `Background=true` (:160). A second Mesnada HTTP server (default port **5013**) serves `/mcp`, `/mcp/sse`, `/api/tasks/*`, ACP routes and an embedded UI (`internal/mesnada/server/gin.go:71-86`, `api.go:29-37`, `api_acp.go:17-24`).

**Mesnada is essentially not in the SDKs** — only the read-only `PandoSubAgentState` type (`sdk/typescript/src/agui/types.ts:276-277`, surfaced as `subAgents` :304, mirrored in Python/.NET). No `spawnAgent()/listTasks()` client method anywhere.

### Agent definitions / schema

- `/www/MCP/Pando/pando/mimocode.json` is **not a Pando file** — it's an 8-line Xiaomi MimoCode config pointing at deepwiki MCP.
- `/www/MCP/Pando/pando/pando-schema.json` (630 lines) is **stale/partial**: it covers `acp, agents, contextPaths, data, debug, debugLSP, lsp, mcpServers, mesnada, permissions, providers, skills, tui, wd` and **omits `AGUI`, `MCPServer`, `Remembrances`, `Server`, `Projects`, `Design`**. Generator `cmd/schema/main.go`; the authority is the Go `Config` struct (`internal/config/config.go:1051-1090`).
- The real config is `.pando.toml` (520 lines). `[Agents.<name>]` only carries `Model`, `MaxTokens`, `AutoCompact`, `AutoCompactThreshold`, `ContextWindowOverride`, `ReasoningEffort`, `ThinkingMode` (`.pando.toml:21-93`; note the malformed legacy `[Agents.cliassist]` at :31 which `config.go:68-72` prunes). **Prompts are not in TOML** — they come from `internal/llm/prompt/*` plus personas/skills (`[Skills] Paths = ['./agents/skills']`, `.pando.toml:415-420`).
- `[AGUI]` full struct (`internal/config/config.go:1137-1186`): `Enabled`, `Path` (`/api/v1/agui`), `Port` (0 = mounted), `Host` (`localhost`), `Agents` (default `["coder"]`, seeded `config.go:2166`), `AllowedOrigins`, `RequireToken` (true), `FrontendTools` (true), `AgentPoolSize` (4), `AgentPoolTTL` (`30m`), `AutoApprove`, `Persona`.

---

## 3. Indexing + semantic search

### Hybrid = BM25/FTS5 + embeddings, fused by RRF (k=60)

Three independent implementations, both legs concurrent with `subLimit = limit*3`:

| Corpus | Entry | Vector | Lexical | Fusion |
|---|---|---|---|---|
| KB | `internal/rag/kb/kb.go:663` → `:678` | `searchVector` :833 — loads **all** embedded chunks, cosine in Go (`cosine` :1180) | `searchFTS` :945 — `-bm25(kb_fts)` :954, normalised :1013-1018 | `rrfFuse` :1093 |
| Code | `internal/rag/code/indexer.go:1248` | `vectorSearch` :1309 | FTS5 `code_symbols_fts` | `rrfFuseCode` :1735 + weighted re-score `boostHybridResults` :1067 / `lexicalBoost` :1016 |
| Legacy | `internal/rag/search.go:180` | :18 | :105 | :226 — **unused in production** |

`hybrid_search_remembrances` (`internal/rag/hybrid.go:35`) does **not** RRF: it concatenates KB (:48) + session events (:67) + per-project code (:89) and sorts by raw score (:119-121). KB RRF scores (~0.016) and boosted code scores (~1.0) are incommensurable, so **code results dominate**.

### Storage

**Plain SQLite, no sqlite-vec, no `vec0`** — embeddings are little-endian float32 BLOBs, similarity in Go (`internal/rag/store.go:11-18`); driver `github.com/ncruces/go-sqlite3` (WASM, `internal/db/connect.go:9-10`). ⚠ `internal/rag/types.go:1-14` claims sqlite-vec/vec0 — that comment is **stale and wrong**.

DB file `<Data.Directory>/pando.db`, `Data.Directory` defaults to `.pando` (`internal/config/config.go:1566`, :2135) ⇒ `<project>/.pando/pando.db`, WAL, ≤8 conns.

Tables (`internal/db/migrations/`): `kb_documents` (+ memory columns `memory_key`, `memory_scope`, `outdated`, `expires_at`, `hits`, `importance`, `source`), `kb_chunks`(embedding BLOB), `kb_fts` FTS5 external-content `porter unicode61`, `kb_links`, `code_projects`, `code_files`, `code_symbols`, `code_symbols_fts` (trigger-maintained), `code_edges`, legacy `rag_chunks/rag_fts/rag_meta`. **Memories are not a separate table** — `kb_documents` rows with `memory_key`/`memory_scope` + `memory` tag. Secondary instances proxy writes to the primary (`kb.go:190-226`).

### Embeddings

`internal/rag/embeddings/factory.go` — `openai`, `openai-compatible`, `google`/`gemini`, `ollama`, `anthropic`/`voyage`. Defaults: `text-embedding-3-small`(1536), `text-embedding-004`(768), `nomic-embed-text`(768), `voyage-3`(1024). Config `RemembrancesConfig` `internal/config/config.go:459-495`, TOML `[Remembrances]`. Live values (`.pando.toml:352-401`): document `ollama`/`nomic-embed-text:latest`, code `ollama`/`hf.co/limcheekin/CodeRankEmbed-GGUF:Q4_K_M`, `ChunkSize=800`, `ChunkOverlap=100`, `IndexWorkers=4`, `KBPath='./.kb'`, `KBWatch=true`, `KBAutoImport=true`, `KBWikiLinks=true`, `KBConvertDocuments=true`.

⚠ **No dimension validation**: chunks whose vector length ≠ the query's are silently skipped (`kb.go:894-896`). Switching embedding models silently degrades recall until a full reindex.

### KB documents API (`internal/llm/tools/remembrances_kb.go`)

| Tool | Info | Params |
|---|---|---|
| `kb_add_document` | :86 | `file_path`*, `content`*, `tags[]`, `metadata{}`, `key` |
| `kb_search_documents` | :226 | `query`*, `limit`(≤20, def 5), `tags[]`(fuzzy), `sort_by_date`, `exclude_outdated`(def true), `scope` |
| `kb_get_document` | :369 | `file_path`* |
| `kb_delete_document` | :420 | `file_path`* |
| `kb_mark_outdated` | :457 | `file_path`* |
| `kb_related_documents` | `remembrances_kb_links.go` | wiki-link neighbours |

Result shape (:304-315): `file_path`, `chunk_content`, `score`, `rank`, `tags[]`, `created_at`, `updated_at`, `metadata{}`, `links`/`backlinks`, `related_to_top_result`.

Model `kb.Document` `internal/rag/kb/types.go:10-26`. **Tags live inside the JSON `metadata` column** under `"tags"` (`frontmatter.go:217-237`); filtering is post-fusion fuzzy substring (`kb.go:820-830`). **There is no collection/namespace concept** — only the `file_path` string and, for memories, the `memory_scope` prefix (`user/`, `project/`, `session/`). Chunking `embeddings/chunking.go:23` (char-based, sentence-aware, 800/100). YAML front matter is **stripped before storing**; regenerated for the disk mirror (`frontmatter.go:16-31`, :90, :207). `kb_add_document` also mirrors to `<KBPath>/<file_path>` — a no-op unless `KBPath` is set (`kb/filesystem.go:44-48`, :85-103; configured `internal/app/remembrances.go:89`). With tag `memory` + `key` it routes to the memory upsert instead (:137-172).

### Remembrances / memory

`internal/llm/tools/remembrances_memory.go` over `internal/rag/kb/memory.go`. `remember` (:60): default scope `user/`, path `memory/<scope><key>.md`, importance 0.5, tag `memory` forced; keyed upsert is one atomic `INSERT … ON CONFLICT(memory_key) DO UPDATE SET content, metadata, updated_at, hits=hits+1, expires_at=MAX(…), outdated=0` (`memory.go:245-260`), TTL default 180 days. `recall` (:157) → `GetMemoriesForInjection` (`memory.go:429`), re-scored `0.6*relevance + 0.3*(1/(ageDays+1)) + 0.1*min(hits/100,1)` (:447), merged with pinned scopes, char-budget trimmed. `forget` (:251) hard-deletes; `kb_mark_outdated` is the soft variant. GC `memory_gc.go:11` on `MemoryGCInterval` (default `1h`).

### Code index (`internal/llm/tools/remembrances_code.go`)

| Tool | Info | Params |
|---|---|---|
| `code_index_project` | :182 | `project_path`*, `project_name`, `languages[]` |
| `code_index_status` | :248 | `job_id` |
| `code_hybrid_search` | :300 | `project_id`*, `query`*, `limit`(≤50,def 20), `offset`, `languages[]`, `symbol_types[]`, `min_score`, `include_docs` (**def false — markdown excluded**), `debug`, `group_by_file` |
| `code_find_symbol` | :784 | `project_id`, `name_path_pattern`, `relative_path`, `symbol_types[]`, `languages[]`, `include_body`, `depth`, `substring_matching`, `limit`, `offset`, `group_by_file` |
| `code_get_symbols_overview` | :955 | `project_id`, `relative_path`, `max_results` |
| `code_get_project_stats` | :1028 | `project_id` |
| `code_delete_project` | :1063 | `project_id` |
| `code_reindex_file` | :1098 | `project_id`*, `file_path`* (relative) |
| `code_list_projects` | :1142 | — |
| `code_search_pattern` | :1168 | pattern/regex |

`project_id = sanitizeProjectID(project_path|project_name)` (:216-220, fn :1523-1543: keeps `[A-Za-z0-9_-]`, maps `/ \ space .` → `_`, trims). So **`/www/git-in-track` → `www_git-in-track`**. Startup auto-path uses `Remembrances.ContextEnrichmentCodeProject` else the sanitised cwd (`internal/app/remembrances_code.go:52-63`).

Incremental: `ReindexFile` `internal/rag/code/indexer.go:659` (content-hash skip, else delete+reinsert symbols/edges/embeddings). **Code watcher exists**: `internal/app/remembrances_watch.go:20` — fsnotify recursive, 250 ms debounce, skips dot-dirs/`node_modules`/`vendor`/`dist`/`build`/`__pycache__` (:189-194), filtered by `treesitter.IsSupportedFile` (:100), → `ReindexFile` (:157) / `DeleteFile` (:127). Languages (`internal/rag/treesitter/languages.go:48-203`): Go, TS, TSX, JS, PHP, Lua, **Markdown**, Svelte, TOML, Vue, Rust, Java, Kotlin, Swift, ObjC, C, C++, Python, Ruby, C#, Scala, Bash, YAML, HTML, CSS.

### Indexing an arbitrary markdown corpus — **the key mechanism**

**`[Remembrances] KBPath` + `KBAutoImport` + `KBWatch`** is exactly the "index a markdown corpus and keep it in sync" path. `internal/app/remembrances.go:75-160`:
- :79-88 `KBPath` resolved (relative → joined to the working dir); empty ⇒ mirror, import and watch **all** disabled
- :89 `ConfigureFilesystemMirror(kbPath)`
- :96-100 document converter when `KBConvertDocuments` (docx/pdf/xlsx/pptx → Markdown)
- :102-141 `SyncDirectoryWithStats(kbPath, true)` in background when `KBAutoImport`
- :143-160 `WatchDirectory(kbPath)` when `KBWatch`

`SyncDirectoryWithStats` (`internal/rag/kb/sync.go:42`): worker-pool `filepath.WalkDir` (:125) with **no hidden-dir or `node_modules` exclusion** (:124-165), mtime skip via `source_mtime_unix` (:148-156), `deleteMissing` prunes vanished sources (:312-339), front matter parsed for tags/aliases. Indexable = `.md` + convertible docs (`isIndexableFile` :387).

`WatchDirectory` (`internal/rag/kb/watcher.go:20`): fsnotify, **recursive with no exclusions** (`addWatchRecursively` :194), 250 ms debounce (:47-58), newly created dirs auto-watched (:81-91), create→`AddDocument`, modify→`UpdateDocument`, remove/rename→`DeleteDocument` (:104-130), unchanged-mtime skip, origin `"watcher"`.

⚠ Do **not** point `KBPath` at a repo root containing `node_modules` — it will be walked and watched (inotify exhaustion).

**There is no `kb_import_path` MCP tool and no CLI to index a folder** — rationale at `internal/llm/tools/remembrances_kb.go:13-22` (path-rooted keys would duplicate documents). `cmd/kb.go:21-77` has exactly one subcommand: `pando kb relink [--force]`.

### HTTP REST endpoints for search — **they do not exist**

`internal/api/routes.go` exposes **no** REST route for `kb_search_documents`, `code_hybrid_search`, `hybrid_search_remembrances`, `remember`/`recall` or `kb_add_document`. The only RAG-adjacent routes are administrative (`routes.go:145-152`, handlers `handlers_remembrances.go` / `handlers_embedding_models.go`):

```
GET  /api/v1/remembrances/projects
POST /api/v1/remembrances/projects/index
POST /api/v1/remembrances/reindex
POST /api/v1/remembrances/test-connection
GET  /api/v1/remembrances/embedding-models
GET  /api/v1/remembrances/enrichment
PUT  /api/v1/remembrances/enrichment
```

**Search is reachable only through MCP tools or an agent run.** This is the single most important constraint.

### Pando as an MCP server

`pando mcp-server` (`cmd/mcp_server.go:26-65`): stdio + **streamable HTTP on `/mcp`**, default port **9777** (:77-78). Groups: fetch/web search, browser, design, **remembrances (KB, events, code intelligence, memory)**, mesnada, cache/pagination, `--file-tools[-write]`, `--system-exec`, `--gateway-expose`, `--self-improvement`, `--design-tools`. Config `[MCPServer]` (`.pando.toml:205-228`, all sub-groups `Enabled=false` by default; `HttpPort=9777`, `StdioEnabled=true`).

⚠ **The HTTP MCP transport has no authentication** — `internal/mesnada/server/server.go:97-127` has no token field, and grep for `Authorization|Bearer|token` in that file returns nothing. Bind loopback only.

---

## 4. Pando web-ui

`/www/MCP/Pando/pando/web-ui/package.json`: **React 19**, `react-router-dom ^7.1.1`, **Zustand 5**, **Tailwind 4** (`@tailwindcss/vite`, CSS-first), Vite 6, TS 5.7, built with **bun**. `react-markdown ^9` + `remark-gfm` + `rehype-highlight` + `highlight.js`, `@monaco-editor/react`, `@xterm/xterm`, FontAwesome 6.7, `i18next ^25` / `react-i18next ^17`, `vite-plugin-pwa`. Workspace dep `@pando/client` (`web-ui/packages/pando-client`). **No shadcn/ui, no Radix, no CVA, no tailwind-merge, no `@copilotkit/*`, no `@ag-ui/*`.**

Chat components — `web-ui/src/components/chat/`: `ChatView.tsx` (210), `SimpleChatView.tsx` (496), `MessageList.tsx` (194), **`MessageBubble.tsx` (870 — markdown, tool calls, diffs)**, `ChatInput.tsx` (292), `ChatInfoSidebar.tsx` (542), `DiffViewer.tsx`, `FileChangesBar.tsx`, `GoalStatus.tsx`, `PlanView.tsx`, `PermissionDialog.tsx`, `QuestionDialog.tsx`, `SlashCommandMenu.tsx`. Also `components/orchestrator/` (mesnada board), `agentvcs/`, `settings/AgentsSettings.tsx`, `shared/PersonaSelector.tsx`. Routes `web-ui/src/App.tsx:130-158`.

**Transport lives in the separately-factored `@pando/client`** (`web-ui/packages/pando-client/`, peers React≥19 + zustand≥5, subpath exports `./services/*`, `./stores/*`, `./hooks/*`, `./types`):
- send: `POST /api/v1/chat/stream` via `createSSEStream` (`src/hooks/useChat.ts:527-528`), body `{sessionId, prompt, model?}` (`internal/api/handlers_chat.go:32-36`)
- reattach: `GET /api/v1/sessions/{id}/stream` via `createGETSSEStream` (`useChat.ts:577-579`)
- steer: `POST /api/v1/sessions/{id}/steer` (`useChat.ts:475`)
- SSE parsing: manual `ReadableStream` + `TextDecoder` (`src/services/sse.ts:32-60`), **not** `EventSource`, so POST-with-body streaming works; event kinds validated at `sse.ts:10-30`
- auth: `X-Pando-Token` header or `?token=` for GET-SSE (`src/services/api.ts:81-83`); token from `POST /api/v1/token` (`src/services/auth.ts:8-38`), cached in `localStorage` key `pando_token`
- 30 zustand stores under `src/stores/`

**Reusability in git-in-track: moderate — high for the transport layer, low for the visuals.** `@pando/client` is a clean seam (copy the folder, repoint `getBaseURL()`, get streaming chat with zero UI coupling), but: React **19** vs git-in-track's 18.3.1; Tailwind **4** vs git-in-track's token/shadcn system (`web/scripts/check-design-tokens.mjs`); hard `react-i18next` dependency in every chat component; FontAwesome vs lucide-react; `@/` path alias; `ChatView.tsx` is an app shell pulling `useGoal`/`useLayoutStore`/`useFileChangesStore`/`useDesktopNotifications`. And decisively: **these components speak Pando's own session/event model, not AG-UI**. `SimpleChatView.tsx` (496 lines, single file) is the closest drop-in reference; `MessageBubble.tsx` is the most valuable self-contained piece.

---

## 5. Deployment

| Command | Default | Notes |
|---|---|---|
| `pando serve` | **:8765**, TLS auto | `--host --port --debug --tls-cert --tls-key --agui-port --agui-host --age-keys` (`cmd/serve.go:232-238`); port fallback `chooseAvailablePort` (:66-73); `--agui-port>0` flips `cfg.AGUI.Enabled` and moves the adapter to its own listener (:87-96) |
| `pando app` | **:8765** | same flags (`cmd/app.go:63-68`), auto-opens browser after 350 ms (:212-217) |
| `pando agui-serve` | **:8090**, TLS on unless `--no-tls` | `--host --port --cwd --debug --allow-origin(repeat) --agent(repeat) --persona --token --no-token --no-tls --tls-cert --tls-key --auto-approve` (`cmd/agui_serve.go:229-241`); token generated if absent (:99-103, :218-226) and **printed to stdout** (:174-175) |
| `pando mcp-server` | **:9777** `/mcp` + stdio | `cmd/mcp_server.go` |
| Mesnada web/MCP server | **:5013** | `.pando.toml:325-326` |
| Mesnada ACP server | **:8766**, host `0.0.0.0` | `.pando.toml:280-288` |

**API token**: generated per process — `generateToken()` 32 random bytes hex (`internal/api/server.go:413-419`, assigned :118/:126, `GetToken()` :409). **Not persisted to `~/.pando`.** Presented as `X-Pando-Token` or `?token=` (`hasValidToken` :448-454). Handed out at `TokenPath = /api/v1/token` (:459), gated by `tokenEndpointAuthenticated()` (:476-478): loopback bind **or** basic auth required, otherwise 401 with an explicit message (:494-496). CORS allows `Content-Type, X-Pando-Token, X-Pando-Client, Authorization` (:433-434); AG-UI paths bypass this and enforce their own pinned origin list (:420-427). The persistent credential is the age-encrypted `[[Server.BasicAuth.Users]]` password in `.pando.toml:411-414`, decrypted with top-level `AgeKeys`.

Config file `.pando.toml` in the project working dir (JSON `pando.json` also supported). Data dir `.pando/` with `pando.db`. Env vars: provider keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`, `GITHUB_TOKEN`, …, `README.md:119-141`), `PANDO_DEV_DEBUG`, `PANDO_PATH` (SDK binary lookup), `PANDO_TOKEN`, `PANDO_API_KEY`, documented-only `PANDO_AGUI_TOKEN` (`cmd/agui_serve.go:46`), plus a full `PANDO_DELEGATION_*` override set and `PANDO_TELEMETRY_*`.

**Pando is per-project**: `agui-serve --cwd` selects it, and only one instance may hold a project's `ipc.lock`.

---

## 6. Recommendations

### 6.0 Two hard constraints

1. **Pando exposes no HTTP search API.** KB/code/memory search is MCP-only (or via an agent run). Server-side semantic search from git-in-track must speak MCP to `pando mcp-server`.
2. **CopilotKit's React client speaks GraphQL to a Node `CopilotRuntime`**, which Pando explicitly refuses to implement (`internal/agui/doc.go`). git-in-track's backend is Go; its frontend is Vite, not Next.js.

### 6.1 (a) Chat UI — three options, ranked

**Option A — `@ag-ui/client` directly, custom chat UI (recommended).**
No Node process. `web/` adds `@ag-ui/client`; a new Go route in `internal/server` (e.g. `POST /api/agent/run`) reverse-proxies the SSE stream to `http://127.0.0.1:8090/api/v1/agui/coder`, injecting the Pando bearer token server-side and authenticating the caller with git-in-track's existing companion token (`web/src/api/token.ts`, `internal/server/cors.go`) — the same shape as the existing `internal/server/cors_proxy.go`. Render `TEXT_MESSAGE_*` / `TOOL_CALL_*` / `STATE_*` with shadcn components and reuse `web/src/markdown/`. HITL: render `pando_permission_request` as a shadcn dialog, answer by re-POSTing the thread with a trailing `tool` message. Frontend tools: declare `open_item`, `open_kb_page`, `focus_board_card` in `RunAgentInput.tools`, handle `RUN_FINISHED{outcome:"interrupt"}`, run, resume. ~400–600 lines; React-18-safe, design-system consistent, no extra runtime.

**Option B — CopilotKit with a Node sidecar.** A small Node/Hono service running `registerPandoCopilotKit`, proxied to `/api/copilotkit`; you get `CopilotSidebar`, `useCoAgent`, `useCopilotAction`, `renderAndWaitForResponse` free (see `examples/copilotkit/app/page.tsx`). Cost: a second runtime to ship and version-pin in a product whose selling point is "no server"; and the example is validated on **React 19** — **verify `@copilotkit/react-*` 1.10 against React 18 before committing** *(uncertain)*.

**Option C — implement CopilotKit's GraphQL runtime in Go.** Don't. Pando evaluated and rejected it for good reasons.

In all cases the feature must be **companion-mode only** — git-in-track's browser/WASM provider (`web/src/api/browser-provider.ts`, `detect.ts`) has no server to proxy through. Gate it in `provider-factory.ts`.

### 6.2 Exposing gintrack's tools to the agent

git-in-track **already** serves MCP over streamable HTTP: `gintrack serve --mcp-http [--mcp-allow-write]` mounts `POST /mcp` (`/www/git-in-track/internal/server/mcp.go:25`, `internal/mcp/transport.go` `HTTPHandler`, flags `cmd/gintrack/serve.go:87-92`). Add `[MCPServers.gintrack]` (§2) to the `.pando.toml` of the directory `agui-serve` runs in and `list_items`, `get_item`, `search_items`, `search_kb`, `get_kb_page`, `list_kb_pages` (+ writes) become callable in chat — **no Pando code change** (`agentpool.go:82-90`).

Caveat: the AG-UI agent is the **coder** agent with the full toolset (bash, edit, write, browser, mesnada…). For a backlog assistant, constrain it via `[AGUI] Persona`, a skill, and `[InternalTools]` / `[ToolDiscovery]`. There is **no per-AG-UI-agent tool allow-list** in `AGUIConfig`.

### 6.3 (b) Semantic-search sync

**Recommended: `[Remembrances] KBPath` pointed at a curated export directory with `KBAutoImport=true`, `KBWatch=true`.** It is Pando's only first-class corpus-sync path (`internal/app/remembrances.go:75-160`), incremental (mtime skip), prunes deletions (`sync.go:312-339`), and needs zero code beyond writing files. `kb_add_document` per item also works but is chattier and its mirror writes into `KBPath` anyway. `code_index_project` on the docs folder is **wrong**: `code_hybrid_search` excludes markdown by default and the index is symbol-oriented.

```
<workdir>/.pando-kb/            ← Remembrances.KBPath
  items/GIT-US-0024.md          ← front matter: id, type, status, labels, milestone, project
  kb/docs/08-mcp-server.md
```

- One markdown file per item with YAML front matter. `tags` and `aliases` are the only keys Pando reads specially (`kb/frontmatter.go:16-31`); everything else lands in `metadata` and is returned by `kb_search_documents`. Put the item id in `tags` so `kb_search_documents(tags:["GIT-US-0024"])` works.
- Drive the export from git-in-track's existing pipeline: `internal/watcher/watcher.go` emits debounced batches; `internal/server/events.go:178-217` already publishes `item.changed` / `file.changed` (with `IsKb`) / `index.updated`. A `internal/server/pandosync.go` hub subscriber rewriting affected files is ~150 lines; Pando's 250 ms-debounced watcher picks it up.
- **Do not** point `KBPath` at the repo root — no `node_modules`/dot-dir exclusions in `sync.go:124-131` or `watcher.go:194-225`.
- `[[GIT-US-0024]]` wiki links in bodies build a graph (`KBWikiLinks=true`) → `kb_related_documents` for free.

**Complement:** also `code_index_project` the git-in-track **source tree** (`project_id = www_git-in-track`); `internal/app/remembrances_watch.go` keeps it fresh.

**Available sync APIs:** `code_reindex_file` (MCP); `POST /api/v1/remembrances/reindex` and `.../projects/index` (REST, code only). **No REST or MCP trigger exists for KB reindex** — the filesystem watcher is the only path.

### 6.4 (c) Agentic search in the chat

| Question shape | Tool |
|---|---|
| "which stories/pages talk about X" (semantic) | `kb_search_documents(query, tags?, limit)` |
| "find GIT-US-0024", "todo stories in M6" (exact/structured) | gintrack MCP `get_item`, `list_items`, `search_items` |
| "where is this implemented" | `code_hybrid_search(project_id:"www_git-in-track", query)` |
| "everything about X" | `hybrid_search_remembrances(query, project_ids, include_kb, include_sessions, include_code)` |

- git-in-track's own search is a **weighted substring matcher**, not semantic: `/www/git-in-track/internal/core/query.go:544` `Index.Search`, `scoreID=100 / scoreTitle=3 / scoreLabel=2 / scoreBody=1`, every term must match. That is exactly the gap Pando fills — keep both.
- Prefer calling `kb_search_documents` and `code_hybrid_search` separately over `hybrid_search_remembrances`, whose merge sorts incomparable scores (`internal/rag/hybrid.go:119-121`).
- Ship a **Pando skill** encoding this routing table rather than relying on tool descriptions.
- `remember`/`recall` with scope `project/` gives durable cross-thread facts; `StateDoc.todos`/`subAgents` give a live side-panel through `STATE_SNAPSHOT`/`STATE_DELTA` for free.

### 6.5 Missing in Pando (would need building)

1. **No REST API for KB/code/memory search.** ~100 lines in `internal/api` would add it (the services are already on `App.Remembrances`); otherwise git-in-track must speak MCP.
2. **No auth on Pando's MCP HTTP transport** (`internal/mesnada/server/server.go:97-127`). Localhost-only today.
3. **No KB reindex trigger over API** — fsnotify only; a `POST /api/v1/remembrances/kb/reindex?path=` would make sync deterministic.
4. **One `KBPath` per instance, no KB collections/namespaces.** Multi-repo workspaces collide in a flat `file_path` space — namespace by path prefix yourself.
5. **No user-defined agents** and no per-AG-UI-agent tool allow-list — a restricted "backlog assistant" needs a persona+skill convention or a small Pando change (`[AGUI] Tools`).
6. **KB vector search is a full scan** of every embedded chunk per query, in Go, no ANN (`kb.go:833-896`; same per code project, `indexer.go:1309`). Fine for thousands of chunks; plan for tens of thousands.
7. **No embedding-dimension guard** (`kb.go:894-896`) — pin the model, reindex deliberately.
8. **`forwardedProps` is inert** — pass per-request context via `context[]`, which *is* injected into the prompt.
9. **No Go client SDK** — hand-roll the AG-UI POST+SSE client, or use the MCP Go SDK git-in-track already depends on (`github.com/modelcontextprotocol/go-sdk/mcp`) against `pando mcp-server`.
10. **Operational coupling** — Pando is per-project with an `ipc.lock`; `agui-serve` defaults to self-signed TLS, so the Go proxy must trust the cert or start Pando with `--no-tls` on loopback.
11. **`pando-schema.json` is stale** (no `AGUI`/`Remembrances`/`MCPServer`/`Server`) — treat `internal/config/config.go` as authoritative. `mimocode.json` is unrelated (a Xiaomi MimoCode file).

### 6.6 Minimal first slice

1. `pando agui-serve --cwd <repo> --port 8090 --no-tls --allow-origin <gintrack origin>` on loopback; the companion captures the printed token at startup.
2. `.pando.toml`: `[AGUI] Enabled=true, Agents=['coder'], AllowedOrigins=[…], Persona=<backlog-assistant>`; `[MCPServers.gintrack]` streamable-http → gintrack `/mcp`; `[Remembrances] KBPath='<workdir>/.pando-kb', KBAutoImport=true, KBWatch=true`.
3. gintrack `internal/server/agent.go` — chi group `/api/agent` proxying `POST /run` (token injection) and `GET /info`, behind the existing bearer token + origin checks, feature-flagged off by default.
4. gintrack `internal/server/pandosync.go` — hub subscriber exporting changed items/KB pages to `.pando-kb/`.
5. `web/src/features/agent/` — `@ag-ui/client` chat panel, companion-mode only, with the `pando_permission_request` dialog and two frontend tools.