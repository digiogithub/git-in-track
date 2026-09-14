---
title: Research — Pando as an agent backend, gap analysis (AG-UI server)
type: page
tags: [research, pando, agent, agui]
---

# Pando as an agent backend server for a web solution — AG-UI / Go side

Scope: `internal/agui/**`, `internal/api/**`, `internal/config/config.go`, `cmd/agui_serve.go`,
`cmd/serve.go`, `cmd/mcp_server.go`, `internal/mesnada/server/**`, `internal/ipc/**`.
Repo: `/www/MCP/Pando/pando` (working tree has uncommitted changes, noted in §7).
Consumer assumed: git-in-track — Go/chi companion proxying a React app, several repos, one Pando,
tools reached over MCP streamable HTTP at gintrack's `/mcp`.

All line references are `path:line` in that repo. Verdict legend: **C** = config only,
**W** = workaround on git-in-track's side, **P(S/M/L)** = Pando code change.

---

## Summary table

| # | Question | Today | Verdict |
|---|---|---|---|
| 1 | Named agent profile with tool allow-list | Impossible. Agent names are a closed 7-name enum (`config.go:73-90`); every AG-UI agent gets the identical full coder toolset (`agentpool.go:82-91`); no per-agent/per-request tool filter exists on any path. Persona and AutoApprove are adapter-wide, not per agent. | **P(M)** — `internal/agui` + `internal/config` |
| 2 | Thread list / read / delete / reattach | AG-UI exposes only `GET /info` and `POST {path}[/{agent}]` (`server.go:26-33`). No thread listing, no transcript read, no delete, no reattach. Client disconnect **cancels the run** (`server.go:412-417`). | **W** for list/read/delete (REST, if co-mounted); **P(M)** for reattach |
| 3 | Identity and multi-tenancy | One shared bearer token; no per-request identity anywhere. `forwardedProps` is decoded and never read (`input.go:46`, only occurrence in the tree). Permissions are scoped per *session*, which is per thread — that part is fine. One Pando process = one cwd = one SQLite. | **P(M)** for identity; **W** (one Pando per repo) or **P(L)** for multi-cwd |
| 4 | Auth and network | Absent `Origin` ⇒ allowed (server-to-server works). Bearer **or** `?token=`. TLS on by default in `agui-serve`. Token printed to stdout / `--token`; no env var. **MCP HTTP server (:9777) has no authentication at all** and answers `Access-Control-Allow-Origin: *`. Pando→gintrack MCP auth via `[MCPServers.x.Headers]` works. | mostly **C**; MCP-server auth is **P(M)** |
| 5 | Operability | `/info` is the only AG-UI endpoint (and it requires the token, so it is a poor probe). No metrics anywhere. No graceful drain. No concurrency cap, no queue, no 503. 8 MiB body cap, 15 s heartbeat. | **P(S)** health/limits, **P(M)** metrics/drain |
| 6 | Mesnada from AG-UI | Always on when the orchestrator exists (`tools.go:320-346`); sub-agents surface in shared state (`subagents.go`). Not switchable per agent. | **P(S)** as part of #1 |
| 7 | Working tree | IPC-only: port window moved out of the ephemeral range, bind retry, refusal to start a second primary. Improves the "several Pando instances" story; changes nothing in AG-UI. | informational |

---

## 1. Agent definition and a restricted "backlog-assistant"

### What exists

- **Names are a closed enum.** `config.go:56-90`: `AgentName` constants `coder`, `summarizer`,
  `task`, `title`, `cli-assist`, `persona-selector`, `context-enricher`; `KnownAgentNames` is
  "the canonical, ordered set", and `IsKnownAgent` gates everything. Config load prunes unknown
  agent keys (`config.go:3114`, `config.go:4358-4400`). The AG-UI adapter rejects anything else
  twice: `deps.go:117-122` (unknown names silently dropped from `Agents`) and
  `runtime.go:109-115` (`unknown agent %q` → 404).
- **`config.Agent` carries no tool fields.** `config.go:110-121` — only `Model`, `MaxTokens`,
  `ReasoningEffort`, `ThinkingMode`, `AutoCompact*`, `ContextWindowOverride`. No `Tools`, no
  `Prompt`, no `Persona`, no `Skills`.
- **Every AG-UI agent is built with the same toolset.** `agentpool.go:82-91` calls
  `agent.CoderAgentToolsWithMesnada(...)` unconditionally and ignores `name` except as the
  argument to `agent.NewAgent` (`agentpool.go:118-124`). The name only selects model/token
  config downstream. So `POST /agui/task` and `POST /agui/coder` get *identical* tools —
  bash, edit, write, patch, browser, desktop, mesnada, KB, code index, MCP gateway
  (`tools.go:228-386`).
- **`Persona` is adapter-wide, not per agent.** `deps.go:76-79`, applied per session in
  `runtime.go:163-171` via `agent.SetSessionLLMOverrides` with `PersonaScoped=true`. One
  `pando agui-serve` process ⇒ exactly one persona for all its agents.
- **`AutoApprove` is adapter-wide** (`deps.go:71`, `hitl.go:48-62`) — it is a *behaviour* switch
  (approve vs. ask vs. deny), never a capability list. Denying `bash` still leaves `bash` in the
  model's tool schema; the model will call it and get a denial, burning turns.
- **`InternalTools` is global and coarse.** `config.go:689-785`: `BrowserEnabled`,
  `DesktopEnabled`, `FetchEnabled`, `AskUserQuestionDisabled`, … — process-wide booleans
  consulted inside `CoderAgentTools*`. Turning browser off here also turns it off for the TUI,
  the Web UI and ACP sharing that config. There is no way to say "off for this agent".
- **`ToolDiscovery` is visibility, not authorization.** `tool_discovery.go:42-113`: deferred tools
  are removed from the *visible* list but stay **executable** through the single `tool_search`
  tool, which carries a remote executor for the whole MCP catalog (`:103-106`). Using
  `DeferredSources`/`NonDeferredTools` as an allow-list is therefore unsound — the agent can
  still reach `bash` by searching for it. `NonDeferredTools` only pins things *visible*.
- **`filterToolsByNames` exists but is unreachable from AG-UI.** `tools.go:85-101` — a real
  name-based filter, but it is only used by the ContextTrimmer heuristic, and it force-includes
  `alwaysIncludedTools` = bash/edit/view/glob/grep/write/patch/ls (`tools.go:45-61`). Even if it
  were wired up, it could not exclude bash.
- **`Permissions`** (`config.go:198-200`) is a single `AutoApproveTools bool` for local sessions.
  Not a policy engine, no tool patterns.
- **Skills**: `SkillsConfig` (`config.go:436-441`) is global enable/paths; `deps.Skills` is one
  `*skills.SkillManager` handed to every agent (`agentpool.go:118-124`). No per-agent skill set.
- **Model per agent**: `[Agents.<name>] Model` works, and `modelDescriptor` (`server.go:198-217`)
  reports it — but only for the 7 built-in names.

### Is there any per-request or per-agent tool filter?

**No.** The only per-request tool input is `RunAgentInput.tools`, which *adds* frontend tools
(`server.go:293`, `agentpool.go:110-116`); it never subtracts. The pool key hashes them
(`agentpool.go:173-192`) so pages with different declared tools do not share an instance — the
machinery for per-request tool variation is already there, it just has no subtractive path.

### Verdict — **P(M)**

Not achievable by config or by any git-in-track-side workaround. A gintrack-side prompt
("never use bash") is not a control: `AutoApprove=false` + `HumanInTheLoop=true` would at least
force a browser approval for every dangerous call (`hitl.go:48-62`) — the only mitigation
available today, and it is per-call UX friction, not a boundary.

**Smallest Pando change.** Add a *profile* layer inside `internal/agui` only, leaving
`config.KnownAgentNames` untouched:

```toml
[AGUI.Profiles.backlog-assistant]
Base    = "coder"          # must be a KnownAgent; supplies model/token config
Model   = "claude-..."     # optional, per-session override (mechanism already exists)
Persona = "backlog"        # optional, per-profile instead of per-adapter
Prompt  = "..."            # optional extra system text
Tools   = ["gintrack__*", "kb_search_documents", "hybrid_search_remembrances", "view", "grep"]
Mesnada = false
```

Touch points, all small:
1. `internal/config/config.go` — new `AGUIProfile` struct + `AGUIConfig.Profiles map[string]AGUIProfile`
   (next to `AGUIConfig` at `:1137`). No change to `Agent`/`KnownAgentNames`.
2. `internal/agui/deps.go` — carry profiles through `ConfigFromApp` (`:93-127`) and have
   `allowsAgent` (`:130-137`) accept profile names.
3. `internal/agui/runtime.go:104-117` — `resolveAgent` returns `(baseAgentName, *profile)` instead
   of failing on a non-built-in name.
4. `internal/agui/agentpool.go:58-129` — key the pool by profile name, and apply a **subtractive**
   allow-list filter (glob on `t.Info().Name`) to the slice returned by
   `CoderAgentToolsWithMesnada` before `agent.NewAgent`. This is the load-bearing ~20 lines; it
   must run *after* `ApplyToolDiscovery`, or discovery must be bypassed for a profile with an
   explicit `Tools` list, otherwise `tool_search` reintroduces everything.
5. `internal/agui/server.go:177-184` — `/info` lists profiles.
6. Per-profile persona/model: reuse `runtime.go:163-171` (`SetSessionLLMOverrides` already carries
   `Model`, `Persona`, `PersonaScoped` — `session_overrides.go:23-38`), so that half is nearly free.

Size **M** (~300-400 LOC + tests), contained in `internal/agui` and one config struct.
An **S** variant that gets 80% of the value: a single adapter-wide
`[AGUI] Tools = [...]` / `DenyTools = [...]` allow-list applied at `agentpool.go:91`, plus running
one `pando agui-serve` per profile. That is genuinely small and would already give the restricted
backlog assistant, at the cost of one process per agent profile.

---

## 2. Threads and history

### Surface

`server.go:26-33` registers exactly four patterns: `GET {path}/info`, `OPTIONS {path}/`,
`POST {path}/{agent}`, `POST {path}`. There is **no** `GET /threads`, no
`GET /threads/{id}/messages`, no `DELETE /threads/{id}`, no reattach endpoint.

`threads.go` is a `threadId → sessionId` map, write-through to an `agui_threads` table
(`threads.go:26-35`, `:110-128`) with `thread_id`, `session_id`, `agent`, `updated_at`. It has
`get`/`put`/`forget` (`:48`, `:67`, `:75`) but **no `list`** — and no handler reaches any of them.
The table is the natural backing store for a listing API; it just has no reader.

### What can be done today

If AG-UI is **co-mounted** on `pando serve` (`AGUI.Port = 0`, `server.go:230`), the REST API is on
the same listener and does have the pieces:

| Need | REST today |
|---|---|
| list threads | `GET /api/v1/sessions` (`handlers_sessions.go:40-95`) — paginated, newest-first, `is_running`. AG-UI sessions are recognizable: titles are prefixed `agui: ` (`runtime.go:186-198`). |
| read a thread | `GET /api/v1/sessions/{id}` (`handlers_sessions.go:116-134`) — session + full `messages`. |
| delete | `DELETE /api/v1/sessions/{id}` (`handlers_sessions.go:136-149`) — deletes messages then session. Leaves the `agui_threads` row orphaned; harmless, `sessionForThread` (`runtime.go:126-137`) detects the dangling binding, warns, and rebinds. |
| live stream | `GET /api/v1/sessions/{id}/stream` (`routes.go:30`) — the Web-UI SSE stream, a *different* event vocabulary from AG-UI. |

But co-mounting contradicts the isolation the adapter is designed around (`listener.go:64-67`,
`server.go:208-228`: "a browser origin that can reach the AG-UI endpoint then cannot reach the
Web-UI API at all"). Co-mounted, the API token that unlocks AG-UI also unlocks `/api/v1/files/`,
`/api/v1/terminal/exec`, `/api/v1/config/*` — unacceptable for a web product unless gintrack's
proxy is the only holder of the token and whitelists paths.

**Verdict: W** — gintrack proxies a small whitelist (`/api/v1/sessions`, `…/{id}`, `DELETE`) from
its own chi router, never exposing the token to the browser, and maps `threadId → sessionId`
itself. Note `runtime.go:138-144` explicitly honours a **Pando session id used as the threadId** —
so gintrack can pre-create a session over REST and reuse its id as the AG-UI `threadId`, which
makes the mapping free. That is the cleanest interim path.

**Verdict for a clean product: P(S)** — add `GET {path}/threads`, `GET {path}/threads/{id}`,
`DELETE {path}/threads/{id}` to `internal/agui/server.go` backed by a new `threadStore.list` and
`deps.Messages.List`. ~150 LOC, no new dependencies, and it keeps the isolated-listener shape.

### Client disconnect mid-run

`server.go:407-418`: `stream` selects on the **request** context; `ctx.Done()` ⇒ `finishRun(run)`
⇒ `run.stop()` ⇒ `cancel()` on the run context (`run.go:86-99`, `:146-152`). **The agent run is
cancelled and its partial work is lost.** A dropped mobile connection, a proxy idle timeout, or a
tab close mid-tool-sequence kills the turn. There is no "detach and keep running" and no way to
re-attach: `runs` is keyed by threadId (`run.go:102-122`) but the only entry point that consults it
is `handleRun`, and it only resumes a run **suspended on a frontend tool**
(`server.go:263-271` → `resumeRun`). A run that is merely *streaming* cannot be re-joined; a second
POST while it streams abandons it (`server.go:270`, `run.go:157-174`).

This is the single biggest gap for a web product. The *intent* is already there — runs are parented
to `r.baseCtx`, not the request (`runtime.go:40-43`, `server.go:330`) — so the fix is to stop
treating request-context cancellation as run termination and to let a later POST (or a new
`GET {path}/threads/{id}/stream`) re-attach to `run.events`.

**Verdict: P(M)** — `internal/agui/server.go` (`stream`, `handleRun`), `internal/agui/run.go`
(park on disconnect with a grace timer; the parking machinery at `run.go:63-83` already exists),
plus an event buffer so a reconnecting client does not miss what was emitted while away.
~300 LOC; the trickiest part is replay semantics.

### Idle/TTL vs. thread continuity

Two different lifetimes, correctly independent:
- **Pooled agent instances**: `AgentPoolTTL`, default 30 min (`deps.go:85`, `:111-116`), LRU-evicted
  above `AgentPoolSize` (default 4, `deps.go:84`, `agentpool.go:133-155`). Eviction destroys only
  the in-memory `agent.Service`; the *session* (history, summaries, compaction state) lives in
  SQLite, so the next POST rebuilds an agent and continues the same session. **Thread continuity
  survives eviction and a process restart** (`threads.go:13-25` is explicit about this).
- Wart: `evictLocked` (`agentpool.go:140-154`) evicts LRU entries *before* inserting, using
  `len(entries) >= AgentPoolSize`, and can evict an entry currently serving a run. Nothing breaks
  (the running `agent.Service` is still referenced by its `activeRun`), but the next request on
  that thread pays a rebuild.

### Concurrency: two POSTs on the same threadId at once

Three layers; outcome is "one wins, the other errors", not corruption:
1. `handleRun` (`server.go:263-271`) — if a run exists and the new payload is not a tool-result
   resumption, `abandonRun` cancels it and waits up to 2 s for the goroutine to release the session
   (`run.go:157-174`). The comment is candid that this exists to dodge `ErrSessionBusy`.
2. Both requests then call `svc.Run(ctx, sessionID, prompt)` on the **same pooled
   `agent.Service`** (one instance serves all sessions; busy-ness is per session —
   `agent.go:932-934`, `IsSessionBusy`). The loser gets `ErrSessionBusy`, surfaced as a
   `RUN_ERROR` with code `session_busy` (`server.go:333-338`, `:542-552`).
3. `runStore.put` (`run.go:118-122`) is last-write-wins on the threadId; `remove` is
   identity-checked (`run.go:126-132`) so the loser cannot evict the winner.

Genuine TOCTOU window: both requests can pass step 1 and race into step 2, yielding a 200 SSE
stream that immediately carries `RUN_ERROR{session_busy}` — not a crash, but the client must handle
it. **git-in-track should serialize per thread on its own side** (a per-threadId mutex in the chi
proxy). **Verdict: W.**

---

## 3. Identity and multi-tenancy

### Per-request user identity: none

- One shared bearer token for the whole adapter (`deps.go:43-46`, `server.go:56-70`). Every caller
  is the same principal.
- `RunAgentInput.ForwardedProps` is declared at `input.go:46` and **never read** — a repo-wide grep
  for `ForwardedProps` returns exactly that one line. Same for `ParentRunID` (`input.go:41`).
- `Context []Context` *is* consumed, but only flattened into a `<context>` text block prepended to
  the prompt (`input.go:235-253`, `server.go:283-285`). Model-visible text, not a trust boundary:
  a browser can put anything there, so it cannot carry an identity claim.
- No header is inspected beyond `Origin` and `Authorization` (`server.go:47-79`).
- The session created for a thread carries no owner: `sessionForThread` (`runtime.go:125-155`) only
  sets a title.

Multi-user is entirely gintrack's problem today: authenticate the user, mint/keep the `threadId`,
hold Pando's token server-side, enforce "user X may only resume thread T".
**Verdict: W** for a trusted single-tenant deployment; **P(M)** for anything where Pando should
enforce it (`Deps.Token` → a token *verifier* interface, a `User`/`Tenant` on the run context, an
owner column on `agui_threads`, an ownership check in `sessionForThread`).

### Permission / HITL scoping

Sound. `installPermissionPolicy` (`hitl.go:48-62`) registers a **per-session** handler via
`perms.RegisterSessionHandler`, consulted first in `permissionService.Request`
(`permission.go:135-142`) ahead of any global auto-approve. The adapter constructs its *own*
`permission.Service` and `userinput.Service` (`runtime.go:57-58`, invariant I3 at
`runtime.go:26-27`), so an approval raised by a browser run can never appear in a TUI dialog, and
vice versa. Since threadId ↔ sessionId is 1:1, **decisions are effectively scoped per thread.**
`watchQuestions` (`hitl.go:285-304`) defensively cancels any question raised outside the AG-UI
question tool.

Caveat: `removePermissionPolicy` (`hitl.go:65-67`) is defined and, in the AG-UI code, never called;
handlers accumulate for the life of the process — one closure per session leaked on a long-lived
server. Minor but real (**P(S)**).

### One Pando per project directory?

**Yes, today.** The binding is the working directory, in three independent places:
- `config.Load(cwd, …)` (`agui_serve.go:72`) resolves per-directory config; `Data.Directory`
  defaults to the **relative** `".pando"` (`config.go:1566`, `:2135`), so `db.Connect` opens
  `<cwd>/.pando/pando.db` (`db/connect.go:89-97`). Sessions, messages, `agui_threads`, the KB and
  the code index all live in that per-cwd database.
- `agui-serve --cwd` does a process-wide `os.Chdir` (`agui_serve.go:207-216`) — a start-time
  decision, not a per-request one.
- `ipc.lockFilePath` = `<workdir>/.pando/ipc.lock` (`ipc/lock_common.go:31-33`).

Notably **`agui-serve` does not bootstrap IPC at all** — no `ipcruntime.Bootstrap` in
`cmd/agui_serve.go`; it goes straight to `db.Connect()` (`:115`), the read-write connection.
`cmd/serve.go:105` *does* bootstrap. So `pando agui-serve` and `pando serve` in the same directory
give **two unsynchronised read-write SQLite writers on the same file**, with none of the
primary/secondary proxying `serve` relies on. Deploy one or the other per repo, never both.
(The working-tree change in §7 hardens `serve`'s side of this, not `agui-serve`'s.)

`Projects` (`config.go:919-927`) and `handlers_projects.go` are an in-process multi-project feature
for the desktop UI: `ProjectManager.Activate` (`project/manager.go:90-140`) keeps warm per-project
instances inside one process and switches an `activeID` pointer. Not reachable from AG-UI —
`resolveAgent`/`sessionForThread` have no project parameter — and `pando serve`-only.

**Recommendation: W** — one `pando agui-serve` per repository; gintrack routes by repo to the right
upstream port. A handful of systemd units, and it matches the isolation the adapter was designed
for. Making one Pando serve several cwds is **P(L)**: cwd is threaded through config (a
package-global `config.Get()`), the DB, the tools and the code index — not a localized change.

### What "instances" gives you

`handlers_instances.go` + `internal/ipc` are **observation and remote control across separately
running Pando processes**, not multi-tenancy. `GET /api/v1/instances` lists live processes from
`instanceregistry` (`handlers_instances.go:45-64`) with `path`, `pid`, `pub_port`, `rpc_port`,
`mode`, `is_primary`. The `/instances/{id}/sessions…` routes (`routes.go:222-230`) proxy over ZMQ
to another instance: list sessions, read messages, subscribe to its event stream, send a message,
cancel. So a single `pando serve` **can** act as a control plane over N per-repo Pando processes —
but it speaks the Web-UI event vocabulary over ZMQ, not AG-UI, and those routes sit behind the full
API token. Useful for a gintrack *admin* view; not a substitute for an AG-UI multi-project endpoint.

---

## 4. Auth and network

### Origin allow-list behind a reverse proxy

`authorize` (`server.go:47-53`):

```go
origin := req.Header.Get("Origin")
if origin != "" && !r.originAllowed(origin) { …403… }
```

**An absent `Origin` is allowed.** Server-to-server calls (gintrack's Go proxy, curl, any non-browser
client) pass the CORS gate unconditionally, and `setCORSHeaders` (`server.go:94-103`) then emits
nothing. Exactly the shape git-in-track wants: React → gintrack → Pando with no `Origin`, and
`AllowedOrigins` can stay empty. `agui-serve` warns about the empty list (`agui_serve.go:108-110`) —
accurate, but for a proxied deployment empty is the *correct* setting.

`originAllowed` (`server.go:85-92`) is exact case-insensitive match or `"*"`; no wildcard
subdomains, no port patterns. `handlePreflight` (`server.go:105-113`) 403s a missing Origin, which
is fine — preflights always carry one. Forwarded headers are deliberately ignored for the host in
`requestBaseURL` (`server.go:226-238`), with a good comment about `X-Forwarded-Host` poisoning
`/info` URLs; only `X-Forwarded-Proto` is honoured, and only to upgrade the scheme. That means
`/info`'s `agents[].url` is built from `req.Host` — behind gintrack's proxy it will report Pando's
internal host unless the proxy rewrites `Host`. **git-in-track should either set `Host` upstream or
ignore `/info` URLs and construct its own.**

### Bearer vs `?token=`

`bearerToken` (`server.go:73-79`) accepts `Authorization: Bearer <t>` **or**, failing that,
`?token=<t>`. Comparison is constant-time (`server.go:66`). The query form exists for `EventSource`,
which cannot set headers — but it puts the token in proxy access logs, `Referer` headers and browser
history. **git-in-track should never use `?token=`**; its Go proxy can always set the header, and
should strip an inbound `?token=` before proxying.

Fail-closed is correct: `RequireToken` with an empty `deps.Token` returns 500, not open access
(`server.go:59-64`).

### TLS

`agui-serve` defaults to **TLS on**, auto-generating a self-signed cert into `Data.Directory` via
`tlsutil.EnsureCert` when `--tls-cert/--tls-key` are absent (`agui_serve.go:127-140`); `--no-tls`
opts out, and the help text says it is "only sane behind a reverse proxy that terminates TLS"
(`agui_serve.go:48-49`). For git-in-track behind a local proxy, `--no-tls` on loopback is the
pragmatic choice; otherwise gintrack's HTTP client needs `InsecureSkipVerify` or the cert pinned.
The listener binds `localhost` unless `--host` says otherwise (`listener.go:16-19`, `:75-79`) — a
deliberate, documented default. `ReadTimeout: 30s`, `WriteTimeout: 0` (`listener.go:88-92`) —
correct for SSE.

### Token provisioning

- `agui-serve`: `--token`, else a random 32-byte hex minted at startup (`agui_serve.go:99-104`,
  `:218-224`) and **printed to stdout** (`agui_serve.go:174-176`). `--no-token` disables auth with a
  warning (`:105-107`).
- **There is no `PANDO_AGUI_TOKEN` env var.** The only occurrence in the tree is inside the
  `Example:` string at `agui_serve.go:46`, where it is *shell* expansion — the binary never reads
  it. Worth flagging: a reader of `--help` will reasonably assume the env var works.
- Co-mounted mode: `Token: s.token` (`server.go:199`), a per-process random token
  (`server.go:118-121`), retrievable at `GET /api/v1/token` (`routes.go:26`,
  `handlers_base.go:14-18`) when the server is on loopback or basic auth is satisfied
  (`server.go:476-478`).

**For git-in-track: P(S)** — read `PANDO_AGUI_TOKEN` (or `--token-file`) in `cmd/agui_serve.go` so
a systemd unit does not have to scrape stdout. ~20 LOC, and it also stops the token landing in the
journal.

### `basicauth.go`

`basicAuthMiddleware` protects the Web UI on the main server. It does **not** apply to the AG-UI
adapter on its own listener (`listener.go:88` — handler is `r.Handler()` only). Co-mounted it runs
*before* `authMiddleware`, so a basic-auth-enabled `pando serve` would challenge AG-UI requests too:
the AG-UI path is excluded from `authMiddleware` at `server.go:504-510`, but I found **no equivalent
exclusion in the basic-auth layer**. Check this before co-mounting.

### MCP HTTP transport (`pando mcp-server`, :9777) — unauthenticated

`cmd/mcp_server.go:152-177` starts `mesnadaServer.New{Addr: host:port}` and mounts `/mcp`,
`/mcp/sse`, `/health` plus a gin engine (`mesnada/server/server.go:129-141`). The only middleware
is `corsMiddleware`, which sets `Access-Control-Allow-Origin: *` (`mesnada/server/server.go:150-152`).
Grepping that file for `Authorization`, `Bearer`, `401`, `Unauthorized`, `apiKey` returns
**nothing**. And `cmd/mcp_server.go:124` does `pandoApp.Permissions.SetGlobalAutoApprove(true)`.

So: **anyone who can reach port 9777 gets unauthenticated, auto-approved tool execution**, which —
with `--system-exec` or `--file-tools-write` (`cmd/mcp_server.go:61-62`) — is remote code execution.
It defaults to the configured host (`cfg.MCPServer.HttpHost`); as long as that is loopback the blast
radius is local, but there is no defence in depth. **P(M)** to add a bearer token to
`internal/mesnada/server` (and it should refuse to bind a non-loopback host without one). Not on
git-in-track's critical path — gintrack is the MCP *server*, Pando the client — but it matters the
moment anyone enables Pando's own MCP HTTP transport on a shared host.

### Pando as MCP client → gintrack

Fully supported by config. `config.MCPServer` (`config.go:42-54`) has `Type` (`streamable-http`,
`config.go:36`), `URL`, `Headers map[string]string`, `Timeout`, and an `Auth *MCPAuth` for
static/OAuth (`config.go:50-53`, `internal/api/handlers_mcp_auth.go` +
`POST /api/v1/mcp/{name}/login|logout`, `GET …/status` — `routes.go:82-84`). So:

```toml
[MCPServers.gintrack]
Type = "streamable-http"
URL  = "http://127.0.0.1:PORT/mcp"
[MCPServers.gintrack.Headers]
Authorization = "Bearer <gintrack token>"
```

**Verdict: C.** Caveat: gintrack's tools then land in the same undifferentiated pile as bash and the
browser (§1) — the allow-list gap is what makes this awkward, not the transport.

---

## 5. Operability as an embedded backend

| Concern | State | Evidence |
|---|---|---|
| Health | **No unauthenticated health endpoint on the AG-UI listener.** `/info` is the closest thing and it goes through `authorize` (`server.go:160`), so a probe needs the token. The main API's `/health` (`routes.go:25`, bypassed at `server.go:499`) is **not** registered on the dedicated listener — `Handler()` mounts only `Register`'s four routes. | `server.go:26-33`, `:159-161`, `listener.go:88` |
| Metrics | None. No prometheus import anywhere under `internal/`. OpenLit/telemetry config exists (`routes.go:96`, `app.go:340`) but that is LLM tracing, not server metrics. No run counters, no latency, no pool-occupancy gauge. | — |
| Structured logs | Yes — `internal/logging` with key/value pairs throughout (`runtime.go:77-85` logs the whole resolved config at startup; `server.go:50`, `:380`, `:496`, `run.go:148`, `:158`). Good enough to ship to a collector. No request id, no correlation with `runId`. | |
| Graceful shutdown | **No drain.** `Listener.Shutdown` (`listener.go:57-59`) is `http.Server.Shutdown`, which waits for handlers — but `Runtime.Close` (`runtime.go:91-101`) hard-cancels **every** active run and only *logs* `"AG-UI adapter closing with runs still in flight"` (`:97-99`). `agui-serve` gives the listener a 5 s ctx (`agui_serve.go:188-192`) and its `defer runtime.Close()` fires after. Any in-flight agent turn is lost on SIGTERM. | |
| Concurrency limit | **None.** `AgentPoolSize` (default 4, `deps.go:84`) caps *cached instances*, not concurrent runs — one `agent.Service` serves unboundedly many sessions concurrently, busy-ness being per session (`agent.go:932-934`; `IsBusy` at `:748-760` is only used for shutdown diagnostics, `agentpool.go:159-168`). N simultaneous threads ⇒ N simultaneous LLM calls and N tool executions. No queue, no 503, no `Retry-After`. The only backstop is the provider's rate limit. | |
| Body limit | 8 MiB, enforced by `io.LimitReader` in `DecodeRunAgentInput` (`sse.go:13`, `input.go:169-182`). A truncated-at-limit body surfaces as a generic 400 `invalid RunAgentInput`, not 413. | |
| Heartbeat | 15 s SSE comment (`deps.go:86`, `server.go:408`, `:419-423`); a failed write tears the run down. Verify gintrack's proxy read timeout is > 15 s. | |
| Backpressure | Agent event channel is buffered at 512 (`agent.go:931`); a slow client eventually blocks the agent goroutine rather than dropping events. `drainQueued` (`server.go:525-539`) is a non-blocking flush used only around suspension. No explicit slow-consumer policy. | |
| Frontend-tool timeout | 10 min (`frontend_tool.go:41`), parked-run grace = timeout + 1 min (`run.go:28`). A suspended run pins its session for up to 11 minutes if the browser never answers. | |

**Verdict: P(S)** for an unauthenticated `GET {path}/healthz` on the dedicated listener and a
`MaxConcurrentRuns` with 503 + `Retry-After` (both small, localized in `internal/agui/server.go` +
`deps.go`). **P(M)** for metrics and a real drain (`runtime.go:91-101` + `cmd/agui_serve.go`).

For a chat panel in a React app, the missing concurrency cap is the one that bites first: a dozen
users hitting "send" produces a dozen concurrent model calls with no queue.

---

## 6. Mesnada from AG-UI

- **Spawning is always available** when the orchestrator is non-nil. `agui-serve` passes
  `pandoApp.MesnadaOrchestrator` unconditionally (`agui_serve.go:148`), and
  `CoderAgentToolsWithMesnada` then registers `mesnada_spawn_agent`, `_get_task`, `_list_tasks`,
  `_wait_task`, `_cancel_task`, `_get_task_output`, `_note` (`tools.go:320-329`), plus
  `mesnada_await` and `mesnada_swarm` when `Mesnada.Delegation` is enabled (`tools.go:335-345`).
- **Not controllable from the web.** The frontend can only *add* tools (`server.go:293`,
  `agentpool.go:110-116`); nothing in `RunAgentInput` removes them. `subagents.go` is purely
  observational — it derives the sub-agent list by parsing tool traffic it already sees
  (`subagents.go:38-51`; the file is explicit: "Nothing is read from the orchestrator"), surfacing
  it in the AG-UI shared-state document. The page can *watch* fan-out and render cards; it cannot
  start, stop or forbid it. Cancelling is only possible by the agent calling `mesnada_cancel_task`.
- **The only off switch is the process**: build the adapter with a nil `Orchestrator`. No config
  flag — `Deps.Orchestrator` (`deps.go:33`) comes straight from the app, and `Deps.validate`
  (`deps.go:49-57`) only requires Sessions and Messages, so nil is legal and would work. But
  `cmd/agui_serve.go` offers no flag for it.

**Should it be on for a backlog assistant? No.** It multiplies model spend, produces work nobody
asked for, runs each sub-agent with the same unrestricted toolset (including bash), and the web user
cannot stop it. The sub-agent state feed is a nice affordance for a *coding* surface, dead weight
here.

**Verdict: P(S)** — fold `Mesnada = false` into the profile work of §1 (one guard around
`tools.go:320-346`'s output, or simply exclude `mesnada_*` via the allow-list). If §1 lands, this
comes free.

---

## 7. Uncommitted working tree

`git status --porcelain` / `git diff --stat`: 16 files, +491/-23, confined to `cmd/`
(`app.go`, `desktop.go`, `ipc.go`, `root.go`, `serve.go`), `internal/app/app.go`, `internal/ipc/**`,
plus two new `.kb/pando/fixes/*.md` notes. **Nothing under `internal/agui/`, `internal/api/` or
`internal/config/`.**

Substance, all IPC robustness:

1. **`internal/ipc/ports.go`** — the deterministic per-path port window moves from `40000-60000` to
   `20000-26000`, with a comment explaining that the old window overlapped every OS ephemeral range
   (Linux `32768-60999`, macOS/Windows `49152-65535`) and caused intermittent "continuing without
   IPC" startups. Secondaries still read the primary's ports from the lock file, so a new binary
   interoperates with a primary started by an old one.
2. **`internal/ipc/runtime/runtime.go`** — on `ErrPrimaryLockHeld` with an unreadable lock file,
   retry once after 250 ms, then **refuse to start** rather than falling through to the primary
   branch: "that would open a second read-write connection to a database another process already
   owns". Also fixes the failover watcher to publish the primary's actual ports.
3. **`internal/ipc/lock_common.go`** — `waitForLockInfoRetry`, 10 × 20 ms, for the window where the
   primary has truncated the lock file before rewriting it.
4. **`internal/ipc/bind.go`** (new, 97 lines) + **`cmd/serve.go`** — `ipc.StartBusWithRetry` replaces
   a bare `bus.Start`, and a bind failure is now `logging.Error` ("this instance is primary but
   unreachable over IPC") instead of a `Warn`.

**Relevance:** it makes the "one Pando process per repository" deployment (§3's recommendation)
materially more reliable — fewer silent IPC failures, and a hard refusal to run two primaries
against one SQLite file. It does **not** touch the `agui-serve` path, which still bypasses IPC
entirely (`cmd/agui_serve.go:115` → `db.Connect()`), so the "don't run `agui-serve` and `serve` in
the same cwd" hazard stands unchanged. If git-in-track standardises on `agui-serve`, none of this
code executes; if it standardises on co-mounted `pando serve`, all of it does.

---

## Proposed backlog items (PANDO project)

### EPIC — AG-UI agent profiles (named, restricted agent definitions)
Let a deployment declare named agent profiles over AG-UI with their own model, persona, prompt and
an explicit tool allow-list, without widening `config.KnownAgentNames`. Unlocks embedding Pando as a
product-specific assistant (backlog assistant, docs assistant) rather than a general coder.

- **Story: `[AGUI.Profiles.<name>]` config schema** — new
  `AGUIProfile{Base, Model, Persona, Prompt, Tools, DenyTools, Mesnada}` and `AGUIConfig.Profiles`.
  `Base` must be a known agent.
  - AC: a profile with an unknown `Base` fails config validation with a named error.
  - AC: `KnownAgentNames` and the `Agent` struct are unchanged; existing configs load identically.
  - AC: round-trips through the config API without loss.
- **Story: profile resolution in the adapter** — `resolveAgent` accepts a profile name and returns
  base + profile; pool keyed by profile; `/info` lists profiles with their model.
  - AC: `POST {path}/backlog-assistant` runs; an undeclared name still 404s.
  - AC: two profiles over the same `Base` get distinct pooled instances.
  - AC: `GET /info` reports each profile's `model` without instantiating an agent.
- **Story: subtractive tool allow-list** — filter the `CoderAgentToolsWithMesnada` slice by glob
  before `agent.NewAgent`, and bypass/suppress `tool_search` for profiles with an explicit `Tools`
  list.
  - AC: a profile listing only `gintrack__*` + KB search exposes no
    `bash`/`edit`/`write`/`browser`/`mesnada_*` in the model's tool schema.
  - AC: the allow-list is not defeatable through `tool_search` (regression test asserts a denied
    tool is unreachable).
  - AC: frontend tools declared in `RunAgentInput.tools` are still added on top and cannot shadow a
    denied name.
- **Story: per-profile persona and model override** — reuse `SetSessionLLMOverrides`.
  - AC: two threads on two profiles in one process resolve different personas and models
    concurrently.

### EPIC — AG-UI thread lifecycle and run durability
Make AG-UI usable from a browser on a flaky network: threads become listable and deletable, and a
dropped stream no longer kills the agent's work.

- **Story: thread API** — `GET {path}/threads`, `GET {path}/threads/{id}`,
  `DELETE {path}/threads/{id}` backed by `agui_threads` + `Messages.List`.
  - AC: works on the dedicated listener with no REST API co-mounted.
  - AC: listing is paginated and scoped to this adapter's threads only.
  - AC: delete removes messages, session and the `agui_threads` row.
- **Story: survive client disconnect** — park the run on request-context cancellation instead of
  `finishRun`, with a grace timer.
  - AC: closing the stream mid-run leaves the agent running; the turn completes and is visible in
    the thread's messages afterwards.
  - AC: a parked run with no reconnect within the grace period is torn down and leaks no goroutine.
- **Story: re-attach to a running turn** — `GET {path}/threads/{id}/stream`, or a POST with no new
  user message, re-joins `run.events` and replays what was missed.
  - AC: reconnecting mid-run yields a consistent event sequence (no duplicate `TOOL_CALL_END`, no
    orphaned `TOOL_CALL_ARGS`).
  - AC: two concurrent POSTs on one threadId produce exactly one run; the loser gets a documented
    error rather than a race.

### EPIC — AG-UI operability for embedded deployments
The things an SRE needs before putting this behind a product.

- **Story: unauthenticated `GET {path}/healthz`** on the dedicated listener.
  - AC: returns 200 + version with no token; leaks no config, agent list or session data.
- **Story: concurrency cap and backpressure** — `[AGUI] MaxConcurrentRuns`.
  - AC: over the cap, a run request gets 503 + `Retry-After` before any session is created or agent
    built.
  - AC: the cap is observable (log + health payload).
- **Story: graceful drain on shutdown** — finish or checkpoint in-flight runs instead of
  hard-cancelling.
  - AC: SIGTERM with a run in flight waits up to a configurable deadline; the run's messages are
    persisted.
- **Story: token provisioning without stdout scraping** — read `PANDO_AGUI_TOKEN` / `--token-file`
  in `agui-serve`.
  - AC: env var and file both work; precedence `--token` > `--token-file` > env > generated; the
    token is never logged.
  - AC: the `--help` example no longer implies an env var that is not read.

### EPIC — Authenticate the MCP HTTP transport
`pando mcp-server`'s HTTP transport serves `/mcp` with `Access-Control-Allow-Origin: *`, no
authentication, and global auto-approve. With `--system-exec` that is unauthenticated RCE for
anyone who can reach the port.

- **Story: bearer token on `internal/mesnada/server`**
  - AC: `/mcp` and `/mcp/sse` require a token when one is configured; `/health` does not.
  - AC: binding a non-loopback host without a token is refused at startup.
  - AC: CORS stops being `*` by default; an explicit allow-list replaces it.

### EPIC (stretch) — Request identity for AG-UI
Only if Pando should enforce multi-user rules rather than trusting the proxy.

- **Story: per-request principal** — verify a caller-supplied identity (JWT or a verifier interface
  replacing the fixed `Deps.Token`), record an owner on `agui_threads`, reject cross-owner thread
  access.
  - AC: a token for user A cannot resume a thread owned by user B.
  - AC: single-token deployments keep working unchanged.

---

## Decisions for the user

1. **Deployment shape: one `pando agui-serve` per repository (recommended) vs. one co-mounted
   `pando serve`.** Per-repo `agui-serve` matches the isolation the adapter was designed for
   (`listener.go:64-67`) and avoids exposing `/api/v1/files`, `/terminal/exec` and `/config/*` on the
   same listener. Co-mounting buys the session REST API for free (§2) at a real security cost.
   **Recommend: per-repo `agui-serve`,** plus gintrack proxying a whitelisted subset of a separate
   `pando serve` only if the thread-list workaround is needed before the thread API lands. Never both
   in the same cwd (§3, two RW SQLite writers).

2. **Restricted agent: wait for profiles, or ship the S-variant now?** The **S** variant — an
   adapter-wide `[AGUI] Tools` allow-list plus one `agui-serve` process per profile — is a few dozen
   lines and unblocks the backlog assistant immediately; **M** (named profiles) is the right end
   state. **Recommend: implement the allow-list first as the load-bearing half,** with the config key
   placed where the profile work will later absorb it. Do not ship the assistant with the full coder
   toolset and only `AutoApprove=false` as protection.

3. **Interim tool restriction, before any Pando change.** Options: (a) `HumanInTheLoop=true` +
   `AutoApprove=false` so every dangerous call needs a browser approval; (b) run `agui-serve` as a
   low-privilege user in a container with a read-only bind mount. **Recommend both**, and treat (a)'s
   approval card as a product surface gintrack must render anyway (`permissionToolName` =
   `pando_permission_request`, `hitl.go:40`).

4. **Disconnect durability: accept or fund it?** Today a dropped stream cancels the turn (§2). For a
   browser chat panel this will happen daily. **Recommend funding the "survive disconnect" story
   before launch**; re-attach can follow. A gintrack-side mitigation — the proxy holds the upstream
   stream open and buffers for the browser — is a genuine, cheaper option, at the cost of gintrack
   owning replay state.

5. **Concurrency cap.** No limit exists today (§5). **Recommend gintrack impose a per-user and global
   in-flight cap in its proxy now** (cheap, immediate) and file the Pando story for a proper 503 +
   `Retry-After`.

6. **Identity.** For a single-tenant internal deployment, leave it to gintrack (**W**) — it already
   authenticates the user and can own the thread↔user table. Only fund the identity epic if Pando must
   be reachable by more than one trusted proxy. **Recommend: workaround,** plus a note in the gintrack
   design that the Pando token is a service credential that must never reach the browser (and that
   `?token=` is not to be used).

7. **Thread ids.** `sessionForThread` honours a Pando session id used as a `threadId`
   (`runtime.go:138-144`). **Recommend gintrack create the session over REST and reuse its id as the
   AG-UI threadId** — it makes the thread API gap far less painful today and stays correct after the
   thread API lands.

8. **Mesnada.** **Recommend off for the backlog assistant** (§6) — fold `Mesnada = false` into the
   profile/allow-list work rather than treating it as a separate feature.

9. **Pando's own MCP HTTP transport.** If nothing needs it, **recommend leaving
   `[MCPServer] HttpEnabled` off** — it is unauthenticated with global auto-approve
   (`cmd/mcp_server.go:124`, `internal/mesnada/server/server.go:129-152`). File the auth epic
   regardless; it is a latent foot-gun independent of git-in-track.
