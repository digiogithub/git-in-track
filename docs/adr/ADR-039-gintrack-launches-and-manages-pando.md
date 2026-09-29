# ADR-039 — gintrack launches and manages Pando over the workspace

- **Status:** Proposed — 2026-09-29. Nothing in this ADR is implemented. The maintainer has
  decided four points of it (see *Maintainer decisions*). The open questions at the end are
  still undecided and must be answered before it is accepted.
- **Date:** 2026-09-29
- **Phase:** Semantic search (Phase 9 surface), for the impact tiers 2 and 3 of Phase 11
- **Related:** [ADR-002](ADR-002-git-as-only-sync.md), [ADR-005](ADR-005-companion-cli-go-embed.md),
  [ADR-032](ADR-032-local-integration-credential-storage.md),
  [ADR-035](ADR-035-agent-interface-over-ag-ui.md),
  [ADR-036](ADR-036-pando-indexes-the-repository-directly.md),
  [ADR-037](ADR-037-specs-with-requirement-blocks.md)
- **Extends:** ADR-036. Pando still indexes the repository's own files in place. This ADR
  changes only who starts Pando and who writes its configuration.
- **Does not change:** the data model (docs/03), item paths, IDs or front matter. The roadmap
  (docs/11) is not changed either.
- **Evidence:** [the spec impact benchmark](../research/2026-09-25-spec-impact-benchmark.md) §9.1
  and §9.8. The Pando source at `/www/MCP/Pando/pando`, commit `b1ed27a4b` (2026-09-29). The
  installed binary reports `v1.1.1`.

## Context

### How Pando is used today

Semantic search (docs/21) and impact tiers 2 and 3 (docs/21 §6.1) need a Pando instance. The
companion reaches it through `internal/pando`, a streamable-HTTP MCP client. It is configured
by hand under `search.pando` (docs/07 §3.3):

- `mcpUrl`: a loopback URL. Non-loopback URLs are refused unless `allowRemote` is set.
- `mcpToken`: the bearer token.
- `projectId`: defaults to `pando.SanitizeProjectID(<repo root>)`.
- `restUrl` and `restToken`: optional, and used only by the KB reindex.

Setting these values up is the user's job:

1. `gintrack agent init` writes a `.pando.toml` into the repository (`[Remembrances] KBPath =
   <repo>/docs`, `KBWatch = true`). That file is meant for the AG-UI agent panel.
2. The user starts `pando mcp-server` by hand, on some port, in some directory.
3. The user copies the URL and token into the gintrack configuration.
4. `gintrack serve` registers each ready mount as a code project with `code_index_project`
   (`internal/server/search_register.go`).

The workspace list has an **Enable semantic search** button (GIT-US-0101). It does not store
an opt-in. It posts `POST /api/v1/search/reindex {"repo":…}` for one repository. Today every
ready mount is registered at startup, so opting in is really "everyone, as long as a Pando is
configured".

### What goes wrong, measured

The 2026-09-25 benchmark (§9.8) checked the maintainer's own machine. Tiers 2 and 3 showed
nothing, and none of the reasons was a gintrack bug:

- `search.pando.projectId` was set but `mcpUrl` was not, so both tiers answered `unavailable`.
- Every running `pando mcp-server` had been started with `--no-http`, so there was no endpoint
  to point `mcpUrl` at. Five such processes are running as this ADR is written, each using
  170–420 MB of RSS.
- The global `~/.pando.toml` sets `[TokenOptimization] BuildCodeGraph = false`. A project
  indexed under that setting has no call edges, and tier 2 then answers `ok 0`. That looks
  like "nothing is affected", which is a false negative.
- The repository's code project was registered as `figma-linux`, not under the id the impact
  seam derives from the repository root.

The benchmark got working results only after it built exactly what this ADR proposes:

- a private `pando mcp-server --no-stdio` on a loopback port with a bearer token;
- its own data directory;
- a local `.pando.toml` with `KBPath`, `BuildCodeGraph = true`, and Mesnada and the API server
  turned off;
- the code project id derived from the repository root.

It took one person an afternoon to put together. Every user who wants tiers 2 and 3 would have
to do the same.

### What Pando offers (verified at `b1ed27a4b`)

| Question | Answer, with source |
|---|---|
| How is it configured? | Configuration is merged from a global file (`$HOME/.pando.{toml,json}`, `$XDG_CONFIG_HOME/pando/`, `~/.config/pando/`) and a **local** `.pando.toml`. Pando looks for the local file in the working directory and then walks parent directories up to `$HOME`. `PANDO_CONFIG_PARENT_SEARCH=false` limits the search to the working directory (`internal/config/local_discovery.go`). Settings in the local file override the global file, which is how the benchmark's `BuildCodeGraph = true` beat the global `false`. **There is no `--config` flag.** The working directory, set with `--cwd`, decides which local file applies. |
| Where are the index and runtime files? | The SQLite database is `<data.directory>/pando.db` (`internal/db/connect.go`). The default `data.directory` is `.pando`, relative to the working directory. The primary-instance lock is `<workdir>/.pando/ipc.lock` (`internal/ipc/lock_unix.go`). **So a Pando started inside a repository writes `.pando/` into that repository.** It is git-ignored there (`.pando*`), but it is still state inside the repository. |
| Transports | `pando mcp-server` enables stdio and streamable HTTP by default. `--no-stdio` and `--no-http` turn each one off. The default port is `9777`. `MCPServer.HttpHost` defaults to `localhost` (`cmd/mcp_server.go`). |
| Port | If the preferred port is taken, Pando **quietly picks another one** (`chooseAvailablePort`) and only logs a warning. A supervisor cannot rely on the port it asked for. |
| Token | `MCPServer.HttpToken` is used when it is set. Otherwise, on a loopback bind, Pando generates a token once, stores it in a **per-user** token file shared by every instance, and prints it to stderr. A non-loopback bind without a configured token is refused (`ensureMCPHTTPToken`). |
| CORS | `MCPServer.HttpAllowedOrigins`. When it is empty, the default, no `Access-Control-*` headers are sent and preflight requests are refused (`internal/mesnada/server/cors_test.go`). docs/07 §3.3 and `internal/pando/doc.go` still say CORS is `*`. That text describes an older Pando. |
| Tool exposure | The `[MCPServer]` sections `FileTools`, `SystemExecution`, `GatewayExpose`, `Design` and `SelfImprovement` are separate switches and are off unless enabled. The remembrances tools, which include `kb_add_document`, and the Mesnada tools are always exposed. The server runs with global auto-approve. |
| Knowledge base | **One `KBPath` per instance** (`RemembrancesConfig.KBPath`, `internal/app/remembrances.go`). The walk is `filepath.WalkDir`. It does not follow symlinked directories, so a symlink farm that pulls several repositories' `docs/` under one root indexes nothing. A document's identity is its path relative to `KBPath` (`sync.go`, `filepath.Rel(baseDir, path)`). `deleteMissing` only affects documents under the base (`isPathWithinBase`). |
| Code index | One instance holds many code projects. `code_index_project` takes `project_path`, plus optional `project_name` and `languages`. The project id is `sanitizeProjectID` of the name or path. `BuildCodeGraph` defaults to `true` (`viper.SetDefault("tokenOptimization.buildCodeGraph", true)`). A global file can still turn it off, and on this machine it does. |
| Several processes, one data dir | If two Pando processes share a working directory, the one holding the lock is primary. The others open the database read-only and forward writes to the primary over ZMQ RPC (`internal/db/connect.go`, `internal/ipc`). |

## Decision drivers

1. **No new source of truth.** Pando's configuration, index, logs and lock files are derived
   data. They go into gintrack's cache directory and never into a repository. The rule in
   AGENTS.md "no state outside Markdown/YAML" allows caches only if they can be rebuilt from
   the files, so deleting the cache directory must be enough to start over.
2. **Correct by construction for the impact tiers.** The code graph is on, the project id is
   `SanitizeProjectID(<repo root>)`, and `KBPath` is the docs folder. None of these depends on
   what a user's global `~/.pando.toml` says.
3. **Opt-in per repository.** Only repositories the user turned on get indexed. An index costs
   disk space, embedding calls and about a minute of CPU per 1,000 files.
4. **Nothing on the network by default.** Pando's MCP surface includes tools that write, and it
   runs with auto-approve (ADR-036, docs/20 §6.2).
5. **Fail soft.** A missing, crashing or slow Pando must never block `gintrack serve` or
   `gintrack mcp`. It shows up as `unavailable`, with a reason the user can read.
6. **Little code, no changes to `internal/core`.** Supervision uses `os/exec`, so it is
   native-only. `internal/core` still compiles to WASM.
7. **gintrack is not a Pando proxy.** gintrack uses Pando for its own features: semantic search
   and impact tiers 2 and 3. An agent that wants Pando's own tools connects to Pando itself.

## Considered options

### A. Keep Pando external and document the setup better

gintrack does not start anything. We would publish a recipe for the benchmark setup, and
possibly add a `gintrack pando config` command that prints a `.pando.toml` and the matching
`search.pando` block.

- **Pros:** no process management and no new security surface. A power user keeps full control
  of their Pando.
- **Cons:** the §9.8 failure modes remain, because each one is a step a person can get wrong,
  and one of them (the global `BuildCodeGraph = false`) fails without any error. Every new clone
  and every new machine repeats the setup. Pando started in a repository still writes `.pando/`
  there.
- **Verdict:** kept as the **external mode** for users who run their own Pando. Rejected as the
  only mode.

### B. One managed Pando per workspace, one KB root, one code project per opted-in repository

gintrack starts a single `pando mcp-server` with a generated configuration. Every opted-in
repository becomes a code project in it. `KBPath` names one directory.

- **Pros:** one process, one port, one token, one database. The client stays a single
  `pando.Client`. Code projects already work with many projects per instance, and tier 2 needs
  nothing more.
- **Cons:** **only one repository's knowledge base can be indexed.**
  - The code indexer skips dot-directories, so it cannot see any repository's backlog under
    `docs/.pmngr/`. With two opted-in repositories, the second one's items, comments and specs
    are not searchable, and tier 3 returns nothing for its requirements.
  - Pointing `KBPath` at a common parent walks everything with no exclusions (docs/21 §4).
  - A symlink farm does not work, because the walk does not follow symlinked directories.
  - Picking "the first repository" as the KB root is an arbitrary rule the user cannot see.
- **Verdict:** rejected for multi-repository workspaces. It is the same as option C when only
  one repository is opted in.

### C. One managed Pando per opted-in repository

gintrack starts one `pando mcp-server` per opted-in repository. Each instance has its own
generated configuration, its own data directory, `KBPath = <repo>/<docs folder>`, and a single
code project for the repository root.

- **Pros:**
  - Every opted-in repository gets full coverage: KB, code and code graph.
  - It matches the model the rest of the system already assumes. docs/20 describes "one
    `agui-serve` per repository", `agent.pando.repos` is a per-repository routing table, and
    `codeProjectsOf` says "there is one Pando per repository".
  - Opting out is simple: stop one process and delete one directory.
  - A crash affects one repository.
- **Cons:**
  - Memory grows with the number of repositories. The measured cost is 170–420 MB RSS per
    process, so we need a cap.
  - There is one port and one token per instance.
  - The search client has to route by repository. `search.semantic` and the workspace search
    run the query against every instance in parallel, inside the existing 300 ms
    `pandoBudget`, and merge the results. docs/21 §0.2 already normalises each leg by its own
    top score, and that normalisation carries over to per-instance legs.
  - Each instance does its own embedding calls.
- **Verdict:** **chosen**, with no plan to consolidate (maintainer decision 1).

### D. Require upstream `KBPaths` (plural), then run a single instance

Add `[Remembrances] KBPaths = [...]` to Pando, then run option B with every opted-in
repository's docs folder as a KB root.

- **Pros:** one process per workspace, with full coverage. This is the best end state.
- **Cons:**
  - It waits on another project's release.
  - The upstream change is bigger than adding a list. Document identity is the path relative
    to `KBPath` (verified), so two repositories that both have `.pmngr/stories/…` would
    collide. `KBPaths` needs identity namespaced per root, for example the absolute
    `source_path` or a `<root id>/` prefix.
  - It also needs one watcher per root, and hits that report which root they came from.
    `deleteMissing` is already limited to the base, so that part works.
- **Verdict:** **rejected** by the maintainer on 2026-09-29 (decision 1). One instance per
  repository keeps each repository's index, crashes and lifecycle separate from the others.
  Opting a repository out stays "stop one process, delete one directory". It also does not
  make gintrack depend on a change to another project. No `KBPaths` follow-up will be filed.

## Maintainer decisions (2026-09-29)

The maintainer decided the following on 2026-09-29. The rest of this ADR follows them.

1. **Topology: option C only.** There is one managed Pando per opted-in repository, and no plan
   to merge them into one instance. Option D is rejected, and no upstream `KBPaths` work is
   requested.
2. **The opt-in is machine-local.** It is stored in `repos[].semanticSearch` in the user's
   gintrack configuration file. The data model does not change.
3. **Managed mode is the default when a `pando` binary is found** (`auto`). An explicit
   `search.pando.mode: external` or `off` overrides it. *Mode resolution* below gives the
   precedence rules. A missing binary makes the tiers `unavailable`. It is not an error.
4. **`gintrack mcp` does not start Pando, and gintrack does not proxy Pando's tools.**
   - Only `gintrack serve` supervises Pando.
   - `gintrack mcp` and `gintrack spec` connect to an already-running managed instance, found
     through the supervisor's state file. They use it for `search_semantic` and impact tiers 2
     and 3, and answer `unavailable` when no instance is running.
   - An agent that wants Pando's KB or semantic tools connects to Pando's MCP server itself,
     using the endpoint that `gintrack pando status --json` reports (see *Agents connecting to
     the managed Pando*).

## Decision (proposed)

**`gintrack serve` supervises one Pando per repository that opts in to semantic search (option
C). The hand-run setup stays available as the external mode (option A).**

- Managed mode is picked automatically when a `pando` binary is available and the
  configuration does not say otherwise.
- Only `gintrack serve` starts and stops instances.
- `gintrack mcp` and `gintrack spec` only ever connect to them.
- The browser-only mode never has one.

### Configuration keys (gintrack configuration file, machine-local)

These keys live in the gintrack configuration file (docs/07 §3.3). They are not stored in any
repository and do not touch docs/03.

```yaml
search:
  pando:
    mode: auto              # auto (default) | managed | external | off
    managed:
      binary: pando         # name on PATH, or an absolute path
      maxInstances: 4       # refuse to start more; the extra repositories report unavailable
      minVersion: ""        # empty means the version floor built into gintrack
      logLevel: info
    # external-only keys, unchanged: mcpUrl, mcpToken, restUrl, restToken, projectId, allowRemote
repos:
  - id: acme-api
    path: /home/dana/src/acme-api
    semanticSearch: true    # the opt-in. Absent or false means not indexed in managed mode.
```

- `mode: managed` together with any external-only key (`mcpUrl`, `mcpToken`, `restUrl`,
  `restToken`, `projectId`) is **refused by name** at load time. This follows the pattern used
  for `search.pando.corpusDir`. Two sources of truth for "where Pando is" are worse than an
  error.
- The opt-in is machine-local (maintainer decision 2). A clone does not carry it, and nothing
  in `project.yaml` or docs/03 changes.
- `repos[].semanticSearch` is the persisted opt-in that GIT-US-0101 does not have today.
  - In managed mode, the workspace list's **Enable semantic search** button sets it, saves the
    configuration and starts the instance. A new **Disable** action clears it and stops the
    instance.
  - In external mode the button keeps its current meaning, a scoped reindex.
- There is no `mcpToken` in managed mode. The token is generated, lives in the instance
  directory, and never appears in the gintrack configuration file or any API response
  (ADR-032).

### Mode resolution

The effective mode is decided once, when the configuration is loaded, and again when it
changes. The first matching rule wins:

| # | Configuration | Effective mode |
|---|---|---|
| 1 | `mode: off` | **off.** No Pando is started or contacted. Semantic search and tiers 2 and 3 answer `unavailable` with "Pando is turned off". |
| 2 | `mode: external` | **external**, exactly as today. With no `mcpUrl` set, the tiers answer `unavailable` with "Pando is not configured". |
| 3 | `mode: managed` | **managed.** If the binary is missing or older than the version floor, the effective mode is managed-but-unavailable (see below). |
| 4 | `mode: auto` (or no `mode` key) **and** `mcpUrl` set | **external.** An explicit endpoint always beats a binary found on `PATH`, so an existing hand-run setup keeps working unchanged after an upgrade. |
| 5 | `mode: auto`, no `mcpUrl`, and a `pando` binary at or above the floor (`managed.binary`, else `PATH`) | **managed.** Nothing is indexed until a repository opts in. |
| 6 | `mode: auto`, no `mcpUrl`, and no usable binary | **off**, and it does not count as an error. The tiers answer `unavailable` with "no Pando binary was found" (or "Pando is older than <floor>"). |

- **A missing binary is never an error.** `gintrack serve` starts normally, the settings card
  and `gintrack pando status` report `binary: not found`, and every Pando-backed answer is
  `unavailable`, never empty.
- An explicit `mode: managed` with no binary works the same way. The status reads
  `managed, unavailable: no pando binary`. The configuration still loads.
- **The binary is looked up again on each supervisor start**, not only once at load. A user who
  installs Pando after `gintrack serve` started gets it at the next "Enable semantic search"
  or `gintrack pando start`, with no restart needed.
- `gintrack doctor` reports the resolved mode and the rule number that produced it.

### The generated Pando configuration

The supervisor writes `<instance dir>/.pando.toml` with mode `0600` and starts Pando with its
working directory set to `<instance dir>` and `PANDO_CONFIG_PARENT_SEARCH=false`. The generated
file is therefore the only local configuration Pando reads, and the user's global file only
provides what gintrack does not set: model providers, embedding provider and model, and API
keys. The supervisor sets these values:

| Key | Value | Why |
|---|---|---|
| `[Data] Directory` | `<instance dir>/data` (absolute) | Keeps the index out of the repository. |
| `[Remembrances] Enabled`, `KBPath`, `KBAutoImport`, `KBWatch` | `true`, `<repo>/<docsFolder>`, `true`, `true` | Same as ADR-036: one root, this repository's docs folder. |
| `[TokenOptimization] BuildCodeGraph` | `true` | Tier 2 depends on it. The global `false` found in §9.8 is overridden. |
| `[MCPServer] HttpHost` | `127.0.0.1` | A literal loopback address, not `localhost`. |
| `[MCPServer] HttpPort` | a free port chosen by the supervisor | See *Lifecycle*. |
| `[MCPServer] HttpToken` | 32 random bytes per instance | The token is per instance instead of Pando's per-user file. |
| `[MCPServer] HttpAllowedOrigins` | `[]` | No CORS, so web pages cannot reach it. |
| `[MCPServer] StdioEnabled` | `false`, plus `--no-stdio` | The child's stdin is not an MCP client. |
| `[MCPServer] FileTools`, `SystemExecution`, `GatewayExpose`, `Design`, `SelfImprovement` | all off | Exposes as little as possible. |
| Mesnada, the API server, telemetry | off | Same as the benchmark setup. |

The code project is registered by the existing pass (`search_register.go`), pointed at the
managed instance's endpoint, with `project_path = <repo root>`. The id is
`SanitizeProjectID(<repo root>)`, as it is today. Each instance holds exactly one code project.

### Where things live

All of it is under `<cacheDir>/pando/<instance key>/`. `cacheDir` is `index.cacheDir`, or the
directory of the gintrack configuration file. The instance key is
`SanitizeProjectID(<repo root>)` plus a short hash of the absolute path, so two clones cannot
collide.

```
<cacheDir>/pando/<key>/            0700
  .pando.toml                      0600  generated, rewritten on every start
  token                            0600  the instance's bearer token, the only copy on disk besides .pando.toml
  data/pando.db (+ -wal, -shm)           the index, which is derived and safe to delete
  .pando/ipc.lock                        Pando's own lock, now outside the repository
  state.json                             pid, port, pando version, started, last error
  supervisor.lock                        flock held by the supervising gintrack process
  pando.log                              stderr, rotated at 10 MB, 2 kept
```

Deleting this directory resets the instance, and the next start rebuilds it from the
repository. For this repository that took 56–62 s for the code index, and the KB import runs
in the background. Nothing under any repository is written by gintrack or by the managed
Pando. The one known exception is the ADR-036 hazard, and this design narrows it (see
*Security*).

### Lifecycle

- **Who supervises.** Only `gintrack serve` supervises (maintainer decision 4).
  - It takes `supervisor.lock` with a non-blocking flock for each instance. The flock is
    released automatically if the process dies.
  - A second `gintrack serve` on the same configuration finds the lock held and connects to
    the running instance instead of starting its own.
  - Instances live as long as the `gintrack serve` process that started them. Whether they
    should outlive it is open question 1.
- **Clients that only connect.** `gintrack mcp` and the `gintrack spec` commands never start
  Pando.
  - They read `state.json` and `token` for the repository they need, check that the recorded
    pid is alive and that `Client.Health` passes, and then use the instance.
  - If there is no state file, the pid is dead, or Health fails, the answer is `unavailable`
    with "managed Pando is not running — start `gintrack serve`".
  - They use the instance only for gintrack's own features: `search_semantic` and impact tiers
    2 and 3. `gintrack mcp` does not re-export any Pando tool.
- **Start.** Startup never blocks the listener. The supervisor:
  1. resolves `managed.binary`, and checks `pando --version` against the version floor;
  2. picks a port by binding `127.0.0.1:0` and releasing it;
  3. writes the configuration;
  4. runs `pando mcp-server --no-stdio --cwd <instance dir>` in its own process group. On
     Linux it also sets `Pdeathsig = SIGTERM`, so the child dies with its supervisor.
- **Ready and healthy.**
  - The instance counts as ready when `Client.Health` (MCP initialize plus `tools/list`)
    succeeds with **our** token on **our** port, within 30 s.
  - Pando may switch to another port without saying so. If that happens, Health fails with an
    unreachable error, and the supervisor kills the child and retries on a new port. A `401`
    means some other process owns the port.
  - Once ready, Health runs every 30 s. Three failures in a row count as a crash.
- **Restart.** Restarts use exponential backoff from 1 s to 60 s. After 5 crashes within 10
  minutes the instance is marked `failed` and is not retried until the user asks, from the
  settings card or with `gintrack pando restart`. The last 20 stderr lines go into `state.json`
  after being passed through the same redaction as the logs.
- **Stop.** On exit the supervisor sends SIGTERM to the process group and waits 10 s for
  Pando's own shutdown before sending SIGKILL. Opting a repository out also stops its instance.
  The data directory is kept unless the user chooses "Disable and delete index".
- **Reindex.** Managed mode has no Pando REST process, so there is no `restUrl`.
  - For a managed instance, `POST /api/v1/search/reindex` runs the code phase as it does today.
  - The KB phase **restarts the instance**, and `KBAutoImport` performs a full sync. `kbNote`
    reports that as what happened.
- **Status.** `GET /api/v1/search/settings` gains `mode` and, per `indexed[]` row,
  `managed: {state, pid, port, version, since, error}`. `state` is one of `stopped`,
  `starting`, `ready`, `restarting` or `failed`. The token is never included. A `gintrack pando
  status|start|stop|restart|reset` command gives the same view and controls from the CLI.
  `start`, `stop`, `restart` and `reset` act through the running `gintrack serve`, which is the
  only supervisor. `status` reads the state files directly and works without a running server.

### Agents connecting to the managed Pando

gintrack does not proxy Pando. An agent that wants `kb_search_documents`, `code_hybrid_search`
or any other Pando tool configures Pando's MCP server as a separate MCP server in its client. It
finds the endpoint here:

```console
$ gintrack pando status --json
{"mode":"managed","modeRule":5,"binary":"/home/dana/bin/pando","version":"1.1.1",
 "instances":[{"repo":"acme-api","root":"/home/dana/src/acme-api","state":"ready",
   "mcpUrl":"http://127.0.0.1:41873/mcp","tokenFile":"/home/dana/.config/gintrack/pando/home_dana_src_acme-api-3f2a/token",
   "project":"home_dana_src_acme-api","pid":48120,"since":"2026-09-29T10:02:11Z"}]}
```

- The output includes `tokenFile`, never the token. The agent's MCP configuration reads the
  file and sends its contents as `Authorization: Bearer …`. A `--repo <id>` filter narrows the
  output to one instance.
- **The port changes on every start**, so an agent configuration that hard-codes it breaks when
  `serve` restarts. A client should resolve the endpoint through `gintrack pando status --json`
  when it launches. Whether to offer a stable port is part of open question 2.
- **The agent then sees Pando's full MCP tool surface**, including `kb_add_document` and the
  memory tools, which write, under Pando's auto-approve. This is the ADR-036 hazard, and it is
  outside gintrack's control: the token confines the endpoint to local users who can read the
  file, and nothing restricts which tools such a user calls. docs/20 §6.2 and docs/08 §10 must
  say so where the endpoint is documented.

### Security

- **Loopback only, and no override.** Managed mode ignores `allowRemote`. The bind address is
  written as the literal `127.0.0.1`.
- **A token for each instance.** Only processes that can read the `0600` token file can call
  the instance: gintrack itself, and any agent the user deliberately points at it. The token is not the user's shared Pando listener token, so a
  managed instance cannot be reached with a token issued for the user's other Pando processes,
  and the reverse holds too.
- **No CORS.** `HttpAllowedOrigins = []`. This closes the "a web page can reach
  `127.0.0.1:9777`" concern in docs/07 §3.3, **provided the installed Pando is new enough**.
  The version floor exists for this reason.
- **Tool surface.** The optional tool groups are off. `kb_add_document`, the memory tools and
  the Mesnada tools are still exposed, and the server auto-approves. What limits them in
  practice is that only gintrack holds the token, and `internal/pando` has no wrapper for any
  tool that writes. An agent that connects directly (see *Agents connecting to the managed
  Pando*) is not limited this way. This is still narrower than today, where the user's hand-run Pando exposes
  whatever their configuration enables to whoever holds the shared token. It is still a
  configuration boundary, not a code boundary (ADR-036). Only a tool allow-list in
  Pando's `mcp-server` would close it, and this ADR does not depend on one.
- **Nothing in the repository.** The working directory is the instance directory, so Pando's
  `.pando/` lock and database cannot land in a repository. The KB watcher reads only
  (ADR-036).
- **Secrets.** A generated token is written only to the instance's `.pando.toml` and is held in
  gintrack's memory. It is never logged and never sent over the API. The provider API keys
  stay in the user's global Pando configuration and are never copied.

### Browser-only mode

There is no process to start and no route to a local Pando. `features.search` stays `"core"`.
The web app hides the managed controls, just as it hides the GIT-US-0101 button today.
`search_semantic` and impact tiers 2 and 3 answer `unavailable`, never an empty result
(docs/21 §6). Nothing reaches `internal/core` or `wasm/`: the supervisor is a native-only
package next to `internal/pando`, and it imports neither.

## Consequences

### Easier

- Tiers 2 and 3 work on a new machine with `pando` on `PATH` and one click per
  repository. The three §9.8 mistakes can no longer happen: a missing endpoint, a code graph
  that is off, and a mismatched project id.
- Pando no longer writes `.pando/` into repositories that gintrack manages.
- "Is my Pando up?" has an answer in the settings card and in `gintrack pando status`.
- Opting out cleans up: stop one process and, if the user wants, delete one directory.

### Harder, and what we accept

- **Memory, permanently.** Each opted-in repository costs one Pando process, 170–420 MB
  measured, and there is no plan to consolidate. `maxInstances` caps it, and the repositories
  over the cap say why they are `unavailable`.
- **Search fans out to every instance.** Queries go to each instance in parallel within the
  300 ms budget, and a slow instance only loses its own results. The fan-out is permanent.
- **Managed Pando needs `serve`.** An agent session with only `gintrack mcp` gets semantic search
  and tiers 2 and 3 only while a `gintrack serve` is running on the same machine. Otherwise
  they answer `unavailable`.
- **Auto mode can surprise.** Installing Pando turns on managed mode at the next start. Nothing
  is indexed until a repository opts in, but the settings card changes from "not configured"
  to "managed". Rule 4 of *Mode resolution* keeps existing external setups as they are.
- **Double indexing.** A user who also runs their own Pando on the same repository indexes it
  twice, and pays for the embeddings twice. We accept this, because managed and external modes
  are mutually exclusive in gintrack's configuration but gintrack cannot see the user's other
  processes.
- **Version coupling.** The design relies on Pando behaviour pinned in this ADR: configuration
  merge order, `PANDO_CONFIG_PARENT_SEARCH`, `--no-stdio`, `HttpAllowedOrigins`, and the
  location of `data.directory`. If a Pando release changes any of these, managed mode breaks.
  The version floor and a smoke test (start, Health, `code_list_projects`, stop) are what
  catch it.
- **Process supervision is new to gintrack.** The only external process gintrack starts today
  is system `git`, and those runs are short. cloudflared is embedded as a library (ADR-027).
  A long-running child needs care on Windows too: no `Pdeathsig`, so a Job Object is needed.
  See open questions.
- **The first index is not instant.** About a minute per 1,000 files for code, plus the KB
  embedding pass. Until the index is ready, rows show `indexing` and the tiers may answer from
  a partial index.

## Rollout

1. **Accept this ADR** and set the Pando version floor, which needs to be at least the release
   that contains `HttpAllowedOrigins`.
2. **Supervisor package** (native, `internal/pando/managed` or similar):
   - configuration writer, port pick, spawn, Health, backoff, lock, `state.json` and `token`;
   - tested against a fake `pando` binary in `testdata`, plus an optional integration test
     gated on a real `pando` on `PATH`.
3. **Configuration keys and mode resolution**: `search.pando.mode` (default `auto`),
   `search.pando.managed.*`, `repos[].semanticSearch`, the rule table, the refusal of mixed
   modes, and `gintrack doctor`. Update docs/07 §3.3.
4. **`gintrack serve` integration**:
   - start instances for the opted-in repositories;
   - route the search client per repository;
   - extend `/api/v1/search/settings` and the reindex.
5. **Clients that connect**:
   - `gintrack mcp` and `gintrack spec` discover instances through the state file;
   - `gintrack pando status [--json] [--repo]` works without a server, and
     `start|stop|restart|reset` go through `serve`.
6. **Web and docs**:
   - in the workspace list, Enable and Disable persist the opt-in; the settings card shows the
     managed state and the restart control;
   - docs/21 §1 and §7, docs/02 §8.1, docs/07 §3.3, docs/20 (the managed instance serves search
     only), and docs/08 §10 (connecting an agent to the managed Pando directly).

## Open questions for the maintainer

Maintainer decisions 1–4 settled the earlier questions on topology, where the opt-in lives,
the default mode and who may start Pando. These remain open:

1. **Lifetime.** Should an instance die with the `gintrack serve` that started it, as proposed?
   Or should it be a detached daemon that keeps running between runs, so that `gintrack mcp`
   sessions still have it after `serve` stops? A daemon would need an idle timeout that Pando
   does not have.
2. **Transport.** Loopback HTTP is proposed, because `gintrack mcp`, `gintrack spec` and
   directly-connected agents all need to reach the instance. Stdio would remove the port and
   the token, but only `serve` could use it. Related: should the port stay the same across
   restarts, stored in `state.json` and reused when it is free, so agent configurations do not
   break?
3. **Embedding model.** Should gintrack pin the embedding provider and model in the generated
   configuration, since a model change silently degrades recall (docs/07)? Or should it
   inherit them from the user's global Pando configuration, as proposed?
4. **AG-UI.** Should the same supervisor also manage `pando agui-serve` for the agent panel
   (ADR-035)? This ADR limits itself to search and impact.
5. **Windows and macOS.** Is managed mode Linux and macOS only at first? Windows needs Job
   Objects to tie the child's lifetime to gintrack.

## Alternatives considered

Options A, B and D are discussed under *Considered options* above. In short:

- **A** is kept as the external mode, but is not enough on its own, because every §9.8 mistake
  comes back.
- **B** cannot index more than one repository's knowledge base with a single `KBPath`, and
  neither a common parent root nor a symlink farm gets around that.
- **D** was rejected by the maintainer. Separate instances keep repositories isolated, and
  gintrack does not take a dependency on a Pando change.

Two more alternatives follow.

**Let `gintrack mcp` supervise too, or proxy Pando's tools through it.** Rejected by the
maintainer (decision 4).
- Letting `gintrack mcp` supervise would tie an index's lifetime to whichever agent session
  started first.
- Proxying would put Pando's writing tools behind gintrack's MCP surface, where they would
  look like backlog tools but skip gintrack's validation and `rev` protocol.

Two more were considered:

- **Run the managed Pando in the repository, as `agent init` does.** Rejected. Pando would write
  `.pando/` (its database and lock file) into the working tree, and the generated configuration
  would sit next to the one the user edits by hand for the agent panel.
- **Share the user's own Pando data directory**, using Pando's primary/secondary mode.
  Rejected. The user's global settings, including `BuildCodeGraph = false`, would still apply,
  gintrack's instance would depend on whichever process happens to be primary, and deleting
  the cache would no longer reset gintrack's index without touching the user's.
