# 07 — Companion CLI (`gintrack`) and Local API

Status: planning specification
Phase: **Phase 2 — Companion CLI** (with hooks into Phase 3 boards, Phase 4 sync, Phase 5 MCP)
Audience: contributors implementing `cmd/gintrack`, `internal/server`, `internal/watcher`, `internal/gitops`, `internal/core`

---

## 1. Purpose and scope

`gintrack` is the optional Go companion binary for **git-in-track**. The web app works
standalone in *browser-only mode* (File System Access API + the WASM build of the shared
core). When `gintrack` is running, the web app auto-detects it and upgrades to
*companion mode*, which adds:

- Native file-system access without per-session folder pickers.
- Real-time change detection (fsnotify) pushed over WebSocket.
- A much faster native indexer (the same `internal/core` code compiled natively).
- Git operations via `go-git` or the system `git` binary.
- A local REST API that non-browser clients (scripts, CI, editors) can call.
- The MCP server for AI agents (`gintrack mcp`, see `08-mcp-server.md`).

The CLI is **local-first**: it binds to loopback by default, has no cloud component, and
stores nothing outside the user's repositories and its own config file.

Non-goals for the CLI: acting as a multi-tenant server, hosting repositories, providing
authentication beyond a single local bearer token, or replacing git as the sync mechanism.

---

## 2. Installation

### 2.1 Release binaries (Phase 2, primary channel)

Releases are produced by GoReleaser from GitHub Actions on tags matching `v*`
(see the build/release documentation). The matrix is:

| OS      | Architectures  | Archive                                        |
| ------- | -------------- | ---------------------------------------------- |
| linux   | amd64, arm64   | `gintrack_<version>_linux_amd64.tar.gz`         |
| darwin  | amd64, arm64   | `gintrack_<version>_darwin_arm64.tar.gz`        |
| windows | amd64, arm64   | `gintrack_<version>_windows_amd64.zip`          |

Every archive contains the single static `gintrack` binary (with the web app embedded via
`go:embed`), `LICENSE`, and `README.md`. A `checksums.txt` file with SHA-256 sums is
attached to the GitHub Release. Binaries are **unsigned** in 1.0; macOS users may need
`xattr -d com.apple.quarantine ./gintrack` and Windows users may see a SmartScreen prompt.
This is documented in the release notes.

Linux/macOS:

```bash
VERSION=1.0.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux | darwin
ARCH=$(uname -m); [ "$ARCH" = "x86_64" ] && ARCH=amd64; [ "$ARCH" = "aarch64" ] && ARCH=arm64
curl -fsSLO "https://github.com/digiogithub/git-in-track/releases/download/v${VERSION}/gintrack_${VERSION}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "https://github.com/digiogithub/git-in-track/releases/download/v${VERSION}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt
tar -xzf "gintrack_${VERSION}_${OS}_${ARCH}.tar.gz" gintrack
install -m 0755 gintrack ~/.local/bin/gintrack
gintrack version
```

Windows (PowerShell):

```powershell
$Version = "1.0.0"
Invoke-WebRequest "https://github.com/digiogithub/git-in-track/releases/download/v$Version/gintrack_${Version}_windows_amd64.zip" -OutFile gintrack.zip
Expand-Archive gintrack.zip -DestinationPath "$env:LOCALAPPDATA\Programs\gintrack"
$env:Path += ";$env:LOCALAPPDATA\Programs\gintrack"
gintrack version
```

### 2.2 `go install` (developers)

Requires Go 1.26+. `go install` builds **without** the embedded web app, because a fresh
module download holds nothing under `web/dist` but its `.gitkeep`. **There is no build
tag**: `web/embed.go` embeds `web/dist` unconditionally and `web.Built()` reports `false`
when the directory is empty, so `gintrack serve` says there is no embedded UI while
`gintrack mcp` and every file command work normally.

```bash
# Full build from a checkout (recommended for contributors)
git clone https://github.com/digiogithub/git-in-track
cd git-in-track && make web && make build   # `make web` runs `make wasm` first

# API/MCP only, no embedded UI
go install github.com/digiogithub/git-in-track/cmd/gintrack@latest
```

`gintrack version` reports whether the UI is embedded (`ui: embedded` vs `ui: none`).

### 2.3 Package managers

Delivered in Phase 6 (`GIT-US-0029`). Every channel is written by the **same** GoReleaser
run from the same tag — there is no second workflow and no manual publishing step. The
authoritative reference is [09-ci-cd-and-releases.md](./09-ci-cd-and-releases.md) §10.

- **Homebrew tap** `digiogithub/homebrew-tap`, a **cask** at `Casks/gintrack.rb`, written by
  GoReleaser's `homebrew_casks:` block on each stable tag:
  ```bash
  brew install digiogithub/tap/gintrack
  ```
  A cask, not a formula: GoReleaser deprecated `brews:` in v2.10 and `goreleaser check`
  now rejects it. The consequence is that **`brew install` is macOS-only** — Homebrew on
  Linux cannot install a cask ([ADR-016](./adr/ADR-016-homebrew-cask-instead-of-formula.md)).
  The cask's `postflight` hook clears the quarantine attribute, which is why it is the
  recommended macOS route for an unsigned binary.
- **Scoop bucket** `digiogithub/scoop-bucket`, manifest `bucket/gintrack.json` generated by
  GoReleaser's `scoops:` block with both Windows architectures and their SHA-256 hashes:
  ```powershell
  scoop bucket add digiogithub https://github.com/digiogithub/scoop-bucket
  scoop install gintrack
  ```
- **Container image** on GHCR, `ghcr.io/digiogithub/git-in-track`, tagged `:X.Y.Z-amd64`,
  `:X.Y.Z-arm64`, `:X.Y.Z` (a manifest list) and `:latest` for stable tags only. It serves
  the working tree you bind-mount at `/work`; there is no state inside the container.
  Binding, the published port and the bearer token are explained in
  [09](./09-ci-cd-and-releases.md) §10.
- A pre-release tag (`vX.Y.Z-rc.N`) publishes the GitHub Release and the version-tagged
  images only: `skip_upload: auto` keeps it out of the tap and the bucket, and
  `skip_push: auto` leaves `:latest` where it is.
- Still out of scope: apt/rpm repositories, AUR, nixpkgs, winget, Snap and Flatpak.
  GoReleaser can produce `.deb`/`.rpm` with `nfpms:` if demand appears.

### 2.4 Shell completion

```bash
gintrack completion bash   > /etc/bash_completion.d/gintrack
gintrack completion zsh    > "${fpath[1]}/_gintrack"
gintrack completion fish   > ~/.config/fish/completions/gintrack.fish
gintrack completion powershell | Out-String | Invoke-Expression
```

---

## 3. Configuration

### 3.1 Location

Resolved by `os.UserConfigDir()` with an explicit override chain:

1. `--config <path>` flag (global, highest precedence).
2. `GINTRACK_CONFIG` environment variable.
3. `$XDG_CONFIG_HOME/gintrack/config.yaml` when `XDG_CONFIG_HOME` is set.
4. Platform default:

| Platform | Path                                                      |
| -------- | --------------------------------------------------------- |
| Linux    | `~/.config/gintrack/config.yaml`                          |
| macOS    | `~/Library/Application Support/gintrack/config.yaml`      |
| Windows  | `%APPDATA%\gintrack\config.yaml`                          |

Sibling files in the same directory: `state.db` (BoltDB or a JSON snapshot of the index
cache), `logs/gintrack.log`, `mcp-audit.log`.

The file is created on first run with mode `0600` (the auth token lives in it). On Windows
the ACL is restricted to the current user. `gintrack doctor` warns when permissions are
looser.

### 3.2 Schema

```yaml
# ~/.config/gintrack/config.yaml
version: 1

# Active workspace, used when a command omits --workspace.
defaultWorkspace: work

# Workspaces group registered repositories by id. A workspace that lists no
# repos stands for every registered repository, which is what a configuration
# with a single workspace wants.
workspaces:
  - name: work
    repos: [acme-api, acme-web, acme-team]
  - name: oss
    repos: [git-in-track]

# The repository registry. `gintrack add <path>` appends an entry here,
# `gintrack rm <id>` removes one, and every other command addresses a
# repository by its id.
repos:
  - id: acme-api                    # slug of the folder name, deduplicated
    path: /home/jose/code/acme-api  # absolute path of the git working tree
    role: project                   # project | team
    docsFolder: docs                # folder holding .pmngr/, "." for the root
    # Every documentation folder this registration declares, docsFolder first.
    # Discovery probes the repository root and its first-level directories on
    # its own; a folder deeper than that — a monorepo's apps/api/docs — is
    # found only because it is listed here (doc 03 R-DISC-1, ADR-018).
    docsFolders: [docs]
    enabled: true                   # false keeps the entry but skips indexing
  - id: mono
    path: /home/jose/code/mono
    role: project
    docsFolder: apps/api/docs
    docsFolders: [apps/api/docs, apps/web/docs]
    enabled: true
  - id: acme-team
    path: /home/jose/code/acme-team
    role: team
    docsFolder: knowledge
    enabled: true

server:
  bind: 127.0.0.1        # never bind 0.0.0.0 without reading the security notes
  port: 7317
  # Auth token for the REST/WS API. Generated on first run (32 random bytes,
  # base64url). Regenerate with `gintrack serve --token new`.
  token: "s7Q1e...redacted...9Zk"
  idleTimeout: 0s        # Go duration; 0 never shuts down
  openBrowser: true
  # Publishing this server through a Cloudflare quick tunnel (ADR-027, §4.1,
  # §5.1.1). Read only at startup, and refused outright when the server has no
  # token. A tunnel opened from the web UI is NOT written back here: it lasts
  # for the life of the process, so a workspace is never republished on the next
  # `serve` with nobody watching.
  tunnel:
    enabled: false       # open a tunnel on start, exactly as `serve --tunnel` does
    provider: cloudflare # the only one implemented; any other value reports the
                         # feature as unsupported rather than quietly using
                         # Cloudflare anyway

git:
  backend: auto          # auto | go-git | system
  commitOnSave: false    # commit every save (06-git-sync.md §3.3)
  commitDebounce: 2s     # Go duration; coalesce rapid saves of the same item
  # The shipped default is the `pmngr:` form of 06-git-sync.md; teams whose docs
  # folder sits next to code often prefer the conventional-commits variant
  # "docs({{.ProjectKey}}): update {{.ItemID}} — {{.Title}}". Both the field
  # form ({{.ItemID}}) and the short form ({{action}} {{id}}: {{title}}) work.
  messageTemplate: 'pmngr: update {{.ItemID}} "{{.Title}}"'
  authorName: ""         # empty -> read from the repo/global git config
  authorEmail: ""
  signCommits: false     # gpg/ssh signing; the system backend only
  # The browser-git CORS proxy the companion serves at /cors-proxy/
  # (06-git-sync.md §6.3, ADR-025). It forwards only the three git smart-HTTP
  # endpoints, only to the hosts below, and only for an authenticated caller on
  # a trusted origin.
  corsProxy:
    enabled: true          # false removes the endpoint; it answers 501 with the reason
    allowRepoRemotes: true # allow the registered repositories' own remote hosts
    allowedHosts: []       # extra hosts, `host` or `host:port`; no schemes, no wildcards

index:
  cacheDir: ""           # empty -> the directory the configuration file is in
  watch: true            # let `gintrack serve` run the file watcher
  debounce: 250ms        # Go duration

# The background job engine: the queue an import, a comment push or a
# knowledge-base publish runs on, off the request that asked for it (§4.1).
sync:
  engine:
    workers: 2           # size of the worker pool, 1-64
    batchSize: 20        # jobs handed to one handler call, 1-500
    rate: 5              # shared outbound requests per second; negative removes the limit
    maxAttempts: 5       # attempts before a job is dead-lettered, 1-20
    retention: 168h      # how long a finished job is remembered; Go duration

mcp:
  enabled: false         # mount POST /mcp on `gintrack serve` (same as --mcp-http)
  allowWrite: false      # write tools stay off until this is true, for `gintrack mcp`
                         # (the Settings page writes this field; section 5.5)
                         # over stdio as well as for POST /mcp

# The agent surface: the proxy at /api/v1/agent that relays a chat turn to a
# local `pando agui-serve` process (§5.5). It is off by default, and the token
# below never leaves the companion — it is injected as an Authorization header
# on the server-to-server hop and appears in no response, log line or URL.
agent:
  enabled: false                 # same switch as `gintrack serve --agent`
  pando:
    url: http://127.0.0.1:8090   # agui-serve base; loopback unless allowRemote
    path: /api/v1/agui           # Pando's AG-UI mount point
    token: ""                    # GINTRACK_PANDO_TOKEN overrides it
    tokenFile: ""                # read the token from a file instead
    agent: backlog-assistant     # AG-UI agent or profile: POST {path}/{agent}
    insecureTls: false           # for an agui-serve left on its self-signed cert
    allowRemote: false           # true to dial a host that is not loopback
    maxRuns: 8                   # runs in flight before a 503 + Retry-After, 0-256
    repos:                       # per-repository upstreams, matched on the mounted id
      - repo: git-in-track
        url: http://127.0.0.1:8091
        token: ""
        tokenFile: ""
        agent: ""                # empty inherits the section's

# Semantic search over Pando's knowledge base and code index. An empty `mcpUrl`
# leaves search on the built-in core index alone; the section is consumed by the
# search endpoints, which return candidates and then re-read every field from
# this companion's own index.
search:
  pando:
    mcpUrl: ""                   # e.g. http://127.0.0.1:9777/mcp; empty = off
    mcpToken: ""                 # GINTRACK_PANDO_MCP_TOKEN overrides it
    restUrl: ""                  # `pando serve` base; optional, enables REST reindex
    restToken: ""                # GINTRACK_PANDO_REST_TOKEN overrides it
    projectId: ""                # code project id; empty = Pando's sanitised repo path
    allowRemote: false

# The credentials of the external trackers this machine is linked to. It is the
# only place git-in-track stores a secret it did not generate itself, which is
# why the whole file is 0600 (ADR-032). The committed half of the connection —
# instance URL, remote project, field map, sync modes — lives in the project's
# project.yaml instead (doc 03 §6.1), so a clone knows where its items came from
# without holding a credential.
integrations:
  youtrack:
    DEMO:                        # the git-in-track project key
      token: perm:…              # YouTrack permanent token, prefix included

log:
  level: info            # debug | info | warn | error
  format: text           # text | json
```

The file is written with `gintrack config init` or by the first `gintrack add`,
always with mode `0600`. Keys this build does not know are ignored rather than
rejected, so a file written by a newer binary still opens: the later phases add
`server.extraOrigins`, `git.pushOnSync`, `git.pullStrategy`, `index.ignore`,
`index.maxFileSizeKB` and `log.file` to the same sections.

`PATCH /api/v1/git/settings` writes the `git:` section back to this file, so a
change made in the web UI survives a restart. `PATCH /api/v1/youtrack/settings`
writes the `integrations.youtrack.<projectKey>.token` key the same way, and
`PATCH /api/v1/sync/settings` writes `sync.engine`. Those three are the only
routes that edit the configuration; everything else remains a `gintrack config`
operation.

The YouTrack token is never rendered back. It is excluded from the JSON encoding
of the configuration, so `gintrack config show --json` omits it and
`gintrack config show` prints it as `[redacted]`; no API response, problem
document, log line or WebSocket payload carries it (ADR-032).

### 3.3 Precedence and environment variables

Effective value = flag > environment variable > config file > built-in default.

| Env var                  | Overrides           |
| ------------------------ | ------------------- |
| `GINTRACK_CONFIG`        | config file path    |
| `GINTRACK_WORKSPACE`     | `defaultWorkspace`  |
| `GINTRACK_PORT`          | `server.port`       |
| `GINTRACK_BIND`          | `server.bind`       |
| `GINTRACK_TOKEN`         | `server.token`      |
| `GINTRACK_GIT_BACKEND`   | `git.backend`       |
| `GINTRACK_SYNC_WORKERS`  | `sync.engine.workers` |
| `GINTRACK_SYNC_BATCH`    | `sync.engine.batchSize` |
| `GINTRACK_SYNC_RATE`     | `sync.engine.rate`  |
| `GINTRACK_SYNC_MAX_ATTEMPTS` | `sync.engine.maxAttempts` |
| `GINTRACK_GIT_COMMIT_ON_SAVE` | `git.commitOnSave` |
| `GINTRACK_YOUTRACK_TOKEN` | `integrations.youtrack.<key>.token`, for every project |
| `GINTRACK_PANDO_TOKEN`   | `agent.pando.token`, for every upstream |
| `GINTRACK_PANDO_MCP_TOKEN` | `search.pando.mcpToken` |
| `GINTRACK_PANDO_REST_TOKEN` | `search.pando.restToken` |
| `GINTRACK_LOG_LEVEL`     | `log.level`         |
| `GINTRACK_LOG_FORMAT`    | `log.format`        |
| `NO_COLOR`               | disables ANSI color |

Global flags available on every command: `--config`, `--workspace/-w`,
`--quiet/-q`, `--verbose/-v`, `--log-level`, `--no-color`, `--help/-h`. Naming a
workspace that does not exist creates it. `--json` is declared by every command
that has machine-readable output rather than globally, so that `gintrack --help`
never offers it where it would mean nothing.

`GINTRACK_YOUTRACK_TOKEN` is deliberately a name of its own: `GINTRACK_TOKEN`
already means both the companion's bearer token and the HTTP password go-git
authenticates a remote with, and a third meaning would make one leaked variable
hand out three credentials. It overrides the stored token of *every* project,
which is what a CI checkout with a single linked project wants; a machine
serving two linked projects should use the file instead. The provenance of the
effective token is reported — never its value — as `env`, `file`, `flag` or
`none` by `gintrack youtrack status` and by `GET /api/v1/youtrack/settings`.

The three `GINTRACK_PANDO_*` variables are separate names for the same reason,
and each one overrides exactly one key. Within the file half, an inline `token`
beats a `tokenFile`, and a row of `agent.pando.repos` beats the section-wide
value for the repository it names. All three are read through accessors
(`config.ResolvedPandoToken(repoID)` and its two siblings) rather than copied
into a struct, so no `Save` can write an environment secret back into the file
and no `gintrack config show --json` can print one.

#### `search.pando`

Semantic search is served by a local [Pando](https://github.com/digiogithub/pando) instance.
`mcpUrl` is Pando's streamable-HTTP MCP endpoint (typically `http://127.0.0.1:9777/mcp`) and
`mcpToken` the bearer token it requires (`MCPServer.HttpToken` in Pando's own configuration).
`projectId` names the indexed code project and defaults to the repository path sanitised the
way Pando does it — `/www/git-in-track` becomes `www_git-in-track`. `restUrl` and `restToken`
are optional and only enable the knowledge-base reindex (`POST /api/v1/remembrances/kb/reindex`,
authenticated with `X-Pando-Token`); they need `pando serve`, not `pando mcp-server`, so with
them unset the reindex reports "not configured" rather than failing. Leaving `mcpUrl` empty
switches semantic search off.

There is **no `corpusDir` key**. The exported corpus was retired in `GIT-EP-0020` (ADR-036):
Pando indexes the repository's own files, so there is nothing to write. A configuration that
still sets `search.pando.corpusDir` is **refused by name** with a message citing the epic,
rather than ignored — "my corpus stopped being written" is answered by the parser instead of by
silence. A corpus directory left behind by an older version is not read any more and is safe to
delete by hand.

**A URL whose host is not a loopback address is refused, and there is no override but
`allowRemote`.** Pando's MCP transport exposes far more than search — file writes, shell
execution, agent spawning — so a companion that could be pointed at a remote Pando would be a
remote-code-execution gadget wearing a search feature's clothes. `allowRemote` exists for a
future authenticated deployment and should stay off.

**Loopback is not a boundary against your own browser.** Until Pando's MCP CORS policy stops
being `*`, any web page you visit can reach `127.0.0.1:9777` from the browser with no companion
involved. Binding to loopback keeps the network out; it does not keep a hostile page out. That
is Pando's to fix (its backlog item PANDO-EP-0006), not the companion's.

### 3.4 Git backend selection

`git.backend: auto` resolves at startup: if a `git` executable ≥ 2.20 is on `PATH`, use
`system` (better credential-helper, SSH agent, LFS, hook and signing support; every
invocation is pinned non-interactive so a missing credential fails with
`git_auth_required` instead of waiting on a prompt — doc 06 §8.1); otherwise
fall back to `go-git` (pure Go, no external dependency, no hooks, limited credential
handling). The resolved backend is reported by `gintrack doctor` and by
`GET /api/v1/capabilities` (`features.gitBackend`, `features.gitVersion`), and
per repository by `GET /api/v1/git/status`.

`system` is not merely faster: hooks, gpg/ssh signing, credential helpers and a
commit limited to a pathspec exist only there. `Capabilities()` reports each of
them, and asking for one the resolved backend lacks fails with
`git_unsupported` rather than silently doing something else.

---

## 4. Command reference

All commands accept the global flags above. Exit codes:

| Code | Meaning                                                        |
| ---- | -------------------------------------------------------------- |
| 0    | success                                                        |
| 1    | generic error                                                  |
| 2    | usage error (bad flags/arguments)                              |
| 3    | validation error (front matter, schema, workflow transition)   |
| 4    | not found (item, repo, workspace)                              |
| 5    | conflict (stale `rev`, git conflict)                           |
| 6    | git error (auth, network, dirty tree)                          |

With `--json`, machine-readable output goes to stdout and human logs to stderr, so
`gintrack item list --json | jq` is always safe.

### 4.1 `gintrack serve`

Starts the local server: embedded web app + REST API + WebSocket event stream + watcher +
native indexer.

```
gintrack serve [flags]

  --port int          Port to listen on (default from config, 7317)
  --bind string       Bind address (default 127.0.0.1)
  --no-open           Do not open the browser
  --token string      Use this token; "new" regenerates and persists a fresh one;
                      "none" disables auth (loopback only, refuses non-loopback binds)
  --watch             Enable the file watcher (default true; --watch=false to disable)
  --idle-timeout dur  Exit after this idle duration (e.g. 30m); 0 disables
  --dev               Development mode: allow http://localhost:5173, verbose CORS logs,
                      do not serve embedded assets (proxy to Vite instead)
  --repo path         Serve this repository without registering it; repeatable
  --mcp-http          Serve the Model Context Protocol at POST /mcp (doc 08 §2.2)
  --mcp-allow-write   Advertise the MCP write tools; without it /mcp is read-only
  --mcp-agent name    Agent name recorded as the author of comments written through /mcp
  --agent             Serve the agent proxy at /api/v1/agent, relaying to the
                      configured Pando AG-UI adapter (§5.5, off by default)
  --tunnel            Publish this server through a Cloudflare quick tunnel and print
                      the temporary public https URL (off by default; refused with
                      --token none). See the warning below before using it.
  --sync-workers int  Background job workers (default 2)
  --sync-batch int    Jobs handed to one handler call (default 20)
  --sync-rate float   Shared outbound limit for background jobs, req/s (default 5)
  --sync-max-attempts int
                      Attempts a background job takes before it is dead-lettered
                      (default 5)
```

#### The background job engine

The last four flags configure the companion's background job engine
(`internal/syncengine`): the queue that an import, a comment push or a
knowledge-base publish runs on, off the request that asked for it. It starts
with the server and stops with it, and with no integration configured it starts
**idle** — an empty queue, no goroutine doing anything, no event published.

| Flag | Environment variable | Default | Range |
| --- | --- | --- | --- |
| `--sync-workers` | `GINTRACK_SYNC_WORKERS` | `2` | 1–64 |
| `--sync-batch` | `GINTRACK_SYNC_BATCH` | `20` | 1–500 |
| `--sync-rate` | `GINTRACK_SYNC_RATE` | `5` | > 0, at most 1000; a negative value removes the limit |
| `--sync-max-attempts` | `GINTRACK_SYNC_MAX_ATTEMPTS` | `5` | 1–20 |

Precedence is the full chain of §3.3 — flag, then environment variable, then the
configuration file, then the default. A value outside its range fails the
command **before the listener is opened**, naming the setting. The journal is
written under the configured `index.cacheDir`; with no cache directory the queue
lives in memory only and a restart starts empty.

The file layer is the `sync.engine` section:

```yaml
sync:
  engine:
    workers: 2          # 1-64
    batchSize: 20       # 1-500
    rate: 5             # requests per second; a negative value removes the limit
    maxAttempts: 5      # 1-20
    retention: 168h     # how long a finished job is remembered
```

`retention` has no flag and no environment variable: it is a housekeeping
setting, not something an operator tunes per run. An out-of-range value is
refused by configuration validation with the dotted key that carries it, for
example `sync.engine.workers: 0 is outside the range 1-64`.

Because the section exists, a change made in the web app through
`PATCH /api/v1/sync/settings` is written back to it and the response reports
`persisted: true` — unless the companion was started without a configuration
file (`serve --repo`, or a test), where the change lasts for the process only
and `persisted` is `false`.

The engine is a background component of the companion beside the watcher, the
committer and the tunnel, and it is brought up and taken down on the same path
as those (doc 02 §background components).

Four job kinds are registered by `internal/server` before the journal is
replayed, which is what lets a job queued by a previous run be dispatched after
a restart:

| Kind | What it does | Coalescing key |
| --- | --- | --- |
| `youtrack.import` | Imports a query result or an id list into a project backlog, in batches | the project key |
| `youtrack.comment.push` | Posts one comment file to the issue its item mirrors, or edits the comment a previous push created | the comment path |
| `youtrack.kb.publish` | Publishes a page or a folder as YouTrack articles | the selection |
| `youtrack.kb.pull` | Writes a page or a folder back from its articles | the selection |

The three kinds whose key is a **path** — the comment push and the two
knowledge-base directions — also enqueue under a stable job id derived from that
path, so a burst of writes to one comment or one page is one job rather than one
batch of several. Without it the handler would be delivered the same comment
three times and would create it, then edit it twice for nothing. `youtrack.import`
is deliberately excluded: its key is a query, and two imports of the same query a
minute apart are two things a person asked for.

**Shutdown** is a bounded drain: `SIGINT`/`SIGTERM` gives the queue a grace
period (5 s) to finish what is in flight, on a context detached from the
shutdown itself so that a job halfway through a call to a tracker is not
cancelled by the very stop that is waiting for it. Whatever has not finished by
then is written to the journal and picked up on the next start.

##### The journal (GIT-US-0070)

The journal is what makes "queued" mean something across a restart: without it a
job accepted by an HTTP request and not yet run would be lost the moment the
companion stopped.

**Where it is.** One file, `jobs.json`, directly inside the configured
`index.cacheDir`. With no cache directory the queue lives in memory only, the
journal is never opened, and a restart starts empty — which is exactly what a
test or a `serve --repo` run wants. It is written atomically (a temporary file
in the same directory, renamed over the target, the discipline `internal/core`
uses for items) and **rarely**: state transitions are coalesced behind a 500 ms
timer, so a thousand jobs finishing in a second produce one write and no worker
ever blocks on disk.

**What is in it.** A single JSON object:

```json
{
  "version": 1,
  "updatedAt": "2026-09-13T09:12:04Z",
  "jobs": [
    {"id":"job_000021","kind":"youtrack.import","key":"ACME","state":"queued",
     "payload":{"repo":"acme-api","project":"ACME","params":{"query":"project: ACME #Unresolved"}},
     "attempts":1,"createdAt":"2026-09-13T09:11:58Z","updatedAt":"2026-09-13T09:12:04Z",
     "nextAttempt":"2026-09-13T09:12:20Z",
     "lastError":{"attempt":1,"class":"retryable","message":"502 from the instance",
                  "at":"2026-09-13T09:12:04Z","retryAfter":"16s"}}
  ],
  "deadLetter": ["job_000018"]
}
```

`jobs` is the queue in enqueue order and `deadLetter` names, oldest first, the
ids inside it that gave up. `version` is bumped whenever the on-disk shape
changes; a file carrying an unknown version is treated exactly like a corrupt
one — **moved aside**, logged, and the engine starts empty. A damaged journal is
never fatal to `serve`.

A job entry carries its bookkeeping — id, kind, coalescing key, state, attempt
count, timestamps, the next attempt time and the last error with its class — and
the **payload the request carried**, because replaying a job means running it
with the arguments it was given. That payload is the request's own parameters: a
repository id, a project key, a YouTrack query, a comment path. **No item
content and no credential is ever written here.** A token lives in the `0600`
companion configuration and is read at dispatch time from there (ADR-032), and
error messages are redacted before they are recorded, so the journal cannot leak
one either.

**Retention and deletion.** A finished job is remembered for `sync.engine.retention`
(default 168h, one week) and dropped on the way back in when it is older than
that, which is what keeps the file from growing without bound in a companion
that runs for months. The whole file is **derived data and safe to delete at any
time**, with the companion running or stopped: the Markdown files remain the
source of truth, and the only thing lost is work that was queued and had not
run — no user data, no history, nothing that cannot be asked for again.

**What replay demands of a handler.** A job that was running when the process
died is re-queued with its attempt count intact, because the engine cannot know
whether the handler finished. **Every handler must therefore be idempotent**:
running it twice with the same payload must produce the same result as running
it once. It is the same contract a retry and a dead-letter retry impose, stated
in `internal/syncengine`'s package documentation, and it is why the four
YouTrack handlers match on the `external` block rather than creating blindly
(doc 03 §6.5).

`--tunnel` opens a free Cloudflare **quick tunnel** (`*.trycloudflare.com`,
ADR-027): no Cloudflare account, no DNS record and no inbound port. The same
switch lives in the web app's settings (doc 05 §3.1), and either one can be used
without the other — the flag simply opens the tunnel at startup.

**Read this before you open one.** A tunnel publishes *this server*, which has
read **and write** access to every mounted repository, to the public internet.
The bearer token is the only thing guarding it, which is why `--tunnel` together
with `--token none` does not start at all: `serve` exits before anything listens,
naming the flag that has to go. §5.1.1 states the whole posture; the short
version is that anyone with the URL loads the web app, and anyone with the token
can rewrite the user's backlog. Cloudflare gives quick tunnels **no uptime
guarantee** and reserves the right to investigate their use: this is a way to
show someone a board for ten minutes, never a way to host anything. A **new
hostname is minted on every enable**, so nothing survives a toggle and turning
the tunnel off invalidates every link already shared — which is the only
revocation the service offers.

The tunnel is opened after the listener has an address, so the banner announces
it in two steps and prints the public URL when it exists:

```
tunnel:     opening a public cloudflare tunnel…
listening on http://127.0.0.1:7317
…
tunnel:     https://gentle-pine-mist-42.trycloudflare.com   (public; the token is still required)
open:       https://gentle-pine-mist-42.trycloudflare.com/?token=s7Q1e...9Zk   (public link; anyone holding it has write access)
```

The bare hostname comes first and the `?token=` link below it, so that "share
the tunnel" and "share write access to my repositories" are two visibly
different lines to copy. The link exists because the web app takes the token
from `?token=` or from Settings and from nowhere else: a browser handed only the
bare hostname loads the app and then sits behind a "needs an access token"
banner with an empty workspace. The link is printed to the terminal only — never
to a log line, an event payload or an API response (§5.1.1). A tunnel that fails to open is a warning on stderr and nothing more: the
run degrades to loopback only rather than exiting, because a companion that
serves locally is more useful than one that refuses to serve at all. Shutdown
closes the tunnel, so a process that exits never leaves a published workspace
behind.

On start it prints:

```
$ gintrack serve --mcp-http
git-in-track 0.4.0 (commit 9f2c1ab, ui: embedded, git: system 2.45.2)
workspace: work — 3 repositories (2 project, 1 team)
indexing… 1,284 files, 431 items in 340ms
mcp:        http://127.0.0.1:7317/mcp (read-only)
listening on http://127.0.0.1:7317
token:      s7Q1e...9Zk   (also stored in ~/.config/gintrack/config.yaml)
open:       http://127.0.0.1:7317/?token=s7Q1e...9Zk
watching for changes — press Ctrl+C to stop
```

Notes:

- The `?token=` query parameter is consumed once by the SPA, exchanged for a
  `SameSite=Strict`, `HttpOnly=false` session value in `sessionStorage`, and stripped from
  the URL via `history.replaceState` so it does not linger in browser history.
- Only one instance may hold the port. A lock file next to the config (`serve.lock`,
  containing pid + port + token fingerprint) lets a second invocation say
  `already running at http://127.0.0.1:7317 (pid 4711)` and, unless `--no-open`, just open
  the browser and exit 0.
- `SIGINT`/`SIGTERM` triggers graceful shutdown: stop accepting connections, close WS
  clients with code 1001, flush the index cache, release the lock.

### 4.2 `gintrack add <path> [--team]`

Registers a repository in the current workspace.

```
gintrack add <path> [flags]

  --team              Register as a team repository (role: team). The folder
                      must hold a team.yaml, or --key creates one
  --docs strings      Documentation/KB folder relative to the repo root.
                      Repeatable: every occurrence is declared, the first one
                      is the primary (default: auto-detected)
  --key string        Create a project — or, with --team, a team repository —
                      with this key when the folder has none (see 4.14)
  --name string       Human name of what is created; defaults to the key
  --no-git            Register a folder that is not a git working tree
  --workspace string  Target workspace (created if it does not exist)
  --json              Machine-readable output
```

Behavior: resolves the path (`~` included) to an absolute one, checks it is a git working
tree (a `.git` directory or file) unless `--no-git` says otherwise, detects `role` (a
`team.yaml` at the repository root is the team-repo discovery marker), detects the
documentation folder by looking for `<folder>/.pmngr/project.yaml` at most four levels
deep and preferring `docs/`, assigns the id (the slug of the folder name, deduplicated
with `-2`, `-3`), refuses a duplicate by canonical path, appends to `repos` and joins the
active workspace. Nothing inside the repository is written unless `--key` asks for it.

**Only the primary folder is declared automatically.** Detection looks four levels down but
discovery reaches the repository root and its first-level directories only (doc 03 §2.1,
[ADR-018](./adr/ADR-018-bounded-project-discovery.md)), so a nested backlog it found is
*reported*, not imported — a `testdata/` fixture is not a project of the user. Declare the
ones that are yours by repeating `--docs`:

```bash
$ gintrack add ~/code/mono --docs apps/api/docs --docs apps/web/docs
added project repository mono  /home/jose/code/mono  (docs: apps/api/docs, 2 projects, 0 items)
  API  API  apps/api/docs/.pmngr
  WEB  WEB  apps/web/docs/.pmngr
```

```bash
$ gintrack add ~/code/acme-api
added project repository acme-api  /home/jose/code/acme-api  (docs: docs, 214 items)
  ACME  Acme API  docs/.pmngr
configuration: /home/jose/.config/gintrack/config.yaml

$ gintrack add ~/code/acme-team --team
added team repository acme-team  /home/jose/code/acme-team  (docs: knowledge, 41 items)
```

**`--team` needs a `team.yaml`.** The role recorded in the configuration is *reported, never
enforced*: every team surface — boards, sprints, retros, the team knowledge base — keys off a
parsed root `team.yaml`. Registering a folder without one used to succeed silently and then
fail on every one of those calls, so it is refused instead, with the command that fixes it
(exit code 3), and `--key` creates the team repository in the same command
([ADR-020](./adr/ADR-020-creating-a-team-repository.md), GIT-US-0034):

```bash
$ gintrack add ~/code/acme-team --team
gintrack: /home/jose/code/acme-team holds no team.yaml, so it is not a team repository: …
create it while registering with `gintrack add /home/jose/code/acme-team --team --key <KEY>`, …

$ gintrack add ~/code/acme-team --team --key ACME-TEAM --name "ACME Delivery Team"
added team repository acme-team  /home/jose/code/acme-team  (docs: knowledge, 0 items)
created team repository ACME-TEAM (ACME Delivery Team) in team.yaml
declare the projects this team owns in team.yaml (docs/04 section 3.3)
```

Nothing is registered when the refusal fires: the configuration file is written only after
the whole command succeeds.

`gintrack rm <id>` removes a registration (never touching files on disk).

A repository with no backlog at all is registered with a warning and a way forward. Pass
`--key` to create the project in the same command:

```bash
$ gintrack add ~/code/greenfield --key ACME --name "ACME Platform"
added project repository greenfield  /home/jose/code/greenfield  (docs: docs, 0 items)
created project ACME (ACME Platform) in docs/.pmngr/project.yaml
  ACME  ACME Platform  docs/.pmngr
```

`--key` writes into `--docs` (the first one), defaulting to `docs/`. It never uses the
detected folder: `--key` means "there is nothing here yet", and detection may have found a
backlog that is a fixture. A folder that already holds a `project.yaml` is refused with exit
code 5.

### 4.3 `gintrack ls`

Lists registered repositories in the active workspace.

```
gintrack ls [flags]
  --all               All workspaces
  --json              Machine-readable output
```

Every listed repository is indexed to count what it holds, so the command is as fast as an
index build. The git columns (`branch`, `clean`, `ahead`, `behind`) arrive with the git
backend in Phase 4; until then `git` only reports whether the folder is a working tree.

The `VCS` column names what manages the folder: `git`, `jj (colocated)`, `jj` or `none`
(GIT-US-0038, doc 06 §14). A `jj` repository is read and indexed like any other. Since
GIT-US-0040 its reads go through the `jj` binary — the real bookmark as the line of work,
jj's own dirty set, ahead/behind against the tracked remote bookmark, and conflicts read
out of the commit that records them — in both layouts. Every write is still refused;
writing through `jj` is GIT-US-0041.

```
$ gintrack ls
ID         ROLE     VCS             PATH                       DOCS           KEYS  ITEMS
acme-api   project  git             /home/jose/code/acme-api   docs           ACME    214
acme-web   project  git             /home/jose/code/acme-web   documentation  AWEB    176
acme-team  team     jj (colocated)  /home/jose/code/acme-team  knowledge      —        41
```

```json
$ gintrack ls --json
{
  "workspace": "work",
  "repos": [
    {
      "id": "acme-api",
      "path": "/home/jose/code/acme-api",
      "role": "project",
      "docs": "docs",
      "enabled": true,
      "workspace": "work",
      "git": true,
      "vcs": { "kind": "git", "gitDir": true },
      "projects": ["ACME"],
      "items": 214,
      "pages": 37,
      "errors": 0,
      "warnings": 2
    }
  ]
}
```

### 4.4 `gintrack index [id]`

Builds (or rebuilds) the index for the workspace.

```
gintrack index [id] [flags]
  --full              Ignore the cache and re-parse every file
  --json              Machine-readable output, with the diagnostics per repository
```

```
$ gintrack index --full
acme-api  214 items  1102 files  parsed in 212ms  (12 epics, 58 stories, 138 tasks, 6 milestones)
acme-web  176 items   884 files  parsed in 158ms  (9 epics, 41 stories, 120 tasks, 6 milestones)
0 errors, 2 warnings
run `gintrack doctor` for the details
```

The positional argument limits the run to one registered repository. `--watch` (stay
running and re-index on file changes) belongs to `gintrack serve`, which drives the same
indexer behind the file watcher. Publishing a project's index to the team repository is
`gintrack snapshot` (§4.13); `--out` and the persisted index cache arrive with the cache.

### 4.5 `gintrack item …`

Scriptable CRUD over backlog items. Every subcommand supports `--json`, and every write
subcommand supports `--dry-run`, which runs the real write path over an in-memory overlay
and prints the files it would have produced.

An item is addressed by its id alone: the project key in the id says which repository of
the workspace holds it. `--project` is only needed by `item new`, and only when the
workspace holds more than one project.

```
gintrack item list [flags]
  --project string        Project key (repeatable)
  --type string           epic|story|task|milestone (repeatable)
  --status string         Status name (repeatable)
  --assignee string       Assignee handle (repeatable)
  --label string          Label (repeatable, AND semantics)
  --parent string         Parent item ID
  --milestone string      Milestone ID
  --priority string       critical|high|medium|low
  --text string           Full-text query over title + body
  --updated-since string  RFC 3339 timestamp or duration (e.g. 7d)
  --sort string           updated|created|priority|id|title (prefix "-" to reverse)
  --limit int             Default 50
  --offset int
  --fields string         Comma-separated fields for --json output
  --all                   Include soft-deleted items
```

```
$ gintrack item list --project ACME --status in_progress --sort -updated
ID              TYPE   TITLE                                 STATUS       ASSIGNEE  PRIORITY  UPDATED
ACME-US-0042    story  Login with SSO                        in_progress  jose      high      2h ago
ACME-T-0311    task   Wire OIDC discovery endpoint          in_progress  marta     high      5h ago
```

```json
$ gintrack item list --project ACME --status todo --limit 2 --json
{
  "items": [
    {
      "id": "ACME-US-0042",
      "type": "story",
      "title": "Login with SSO",
      "status": "todo",
      "priority": "high",
      "assignees": ["jose"],
      "labels": ["auth", "q3"],
      "parent": "ACME-EP-0007",
      "milestone": "ACME-M-0002",
      "estimate": 5,
      "updated": "2026-09-03T07:41:11Z",
      "rev": "sha256:6f1c…a09",
      "path": "docs/.pmngr/stories/ACME-US-0042-login-with-sso.md"
    }
  ],
  "total": 37,
  "limit": 2,
  "offset": 0
}
```

```
gintrack item get <id> [flags]
  --body                  Include the Markdown body (default true; --body=false for front matter only)
  --comments              Include comments
```

`--render` (the body as HTML) waits for the renderer to move into the core: the CLI must
not grow a second Markdown pipeline.

```
gintrack item new [flags]
  --project string        Project key (required unless a single project is registered)
  --type string           epic|story|task|milestone (required)
  --title string          (required)
  --parent string         Epic for a story, story for a task
  --status string         Default: first status of the workflow (backlog)
  --assignee string       Repeatable
  --label string          Repeatable
  --priority string
  --estimate int          Story points
  --effort float          Hours
  --milestone string
  --due string            YYYY-MM-DD
  --body string           Body Markdown ("-" reads stdin)
  --dry-run               Print the file that would be written, write nothing
```

`--template` waits for body templates to exist in `project.yaml`.

```bash
$ gintrack item new --project ACME --type task --title "Wire OIDC discovery endpoint" \
    --parent ACME-US-0042 --assignee marta --label auth --priority high --effort 6
created ACME-T-0311  docs/.pmngr/tasks/ACME-T-0311-wire-oidc-discovery-endpoint.md

$ printf '## Description\nSee RFC 8414.\n' | \
  gintrack item new --project ACME --type task --title "Read RFC 8414" --body - --json
{"id":"ACME-T-0312","path":"docs/.pmngr/tasks/ACME-T-0312-read-rfc-8414.md","rev":"sha256:11c…"}
```

```
gintrack item edit <id> [flags]
  --title / --status / --assignee / --label / --priority / --estimate / --effort /
  --milestone / --due / --parent      Same semantics as `item new`
  --add-label / --remove-label        Incremental label edits
  --add-assignee / --remove-assignee  Incremental assignee edits
  --body string                       Replace the body ("-" reads stdin)
  --append string                     Append to the body
  --unset string                      Clear a front-matter field (repeatable)
  --rev string                        Optimistic lock; fails with exit 5 on mismatch
  --dry-run                           Print the file that would be written, write nothing
```

`--editor` (open `$EDITOR` on the file and validate on save) is deferred: it is the one
flag of this surface that cannot be scripted or tested headlessly.

```
gintrack item move <id> <status> [flags]
  --rev string        Optimistic lock
  --comment string    Add a comment describing the transition
  --force             Bypass workflow transition validation (recorded in the commit body)
  --dry-run           Print the files that would be written, write nothing
```

```bash
$ gintrack item move ACME-T-0311 in_review --comment "PR #218 open"
ACME-T-0311  in_progress -> in_review
```

```
gintrack item comment <id> [flags]
  --body string       Comment Markdown ("-" reads stdin, omit to open $EDITOR)
  --author string     Default: git user.name / config authorName
```

Comments are written to `.pmngr/comments/<ITEM-ID>/<YYYYMMDDTHHMMSSZ>-<author>.md` exactly
as specified in the data model, so the CLI and the web app never disagree about layout.

`gintrack item link <id> <relation> <target>` manages typed relations
(`blocks`, `blocked_by`, `relates_to`, `duplicates`, `duplicated_by`, and the spec kinds
`implements`, `modifies`, `supersedes`, `superseded_by` of data model §12.1), with `--remove`
to drop one. The target is an item ID or a requirement ref (`ACME-SP-0003.R2`), optionally
`<KEY>/`-qualified. The inverse relation is written on the counterpart item when both live in
the same workspace; `--inverse=false` writes only the side that was named. Spec links are
one-sided (R-LINK-8): `implements` and `modifies` are recorded on the story or task only and
never mirrored, since the index computes the spec's side, so linking a story does not rewrite
the spec. `implemented_by` and `modified_by` are refused with a usage error (exit 2) that
names the command to run instead, e.g. `gintrack item link ACME-US-0044 implements
ACME-SP-0003.R2`; `--remove` still drops one written by hand, which the validator reports as
`E-LINK-COMPUTED-ONLY`. The first spec kind or spec target raises the project to `schema: 2`
in the same write (§21.10).

### 4.6 `gintrack board …` and `gintrack retro …` — **not implemented**

**Neither command exists.** `gintrack --help` lists no `board` and no `retro`
subcommand, and typing one is an unknown-command error. This section is the
planned shape, kept here so the design is not re-invented, and marked so that
nobody writes a script against it. The sprint commands, which were once
specified alongside these, **are** built and have a section of their own
(§4.16).

Boards and retrospectives are fully reachable today by the other two routes:
over REST at `/api/v1/boards` and `/api/v1/retros` (§5.5), which is what the web
app uses, and as Markdown files in the team repository (doc 04 §5 and §9). What
is missing is only the terminal surface.

The planned tree, when it is written:

```
gintrack board list                       List boards in the team repo
gintrack board get <slug> [--column ...]  Show a board with its columns and cards
gintrack board move <ref> <column>        Move a card; updates status via column mapping
                                          and the `order:` list of the target column
      --position int                      Insert at index (default: end)
gintrack board new <slug> --kind kanban|scrum
gintrack retro list | get <id> | new --sprint <id>
```

The behaviour it has to reproduce is the one the REST layer already implements.
Cards are `ref: <projectKey>/<itemId>` references, never copies. When the
referenced project repo is not registered locally, the card is resolved from
`.pmngr/index/<projectKey>.json` and marked `remote: true`, carrying
`source: "snapshot"`, `snapshotAt`, `stale` and `remoteUrl`; a move of a remote
card updates the board order but refuses to change the item status, because the
file holding that status is not on this machine. Refresh those snapshots with
`gintrack snapshot` (§4.13), which **is** built.

### 4.7 `gintrack sync [--dry-run]`

Git synchronization for every registered repository (Phase 4).

```
gintrack sync [flags]
  --repo string        Limit to one repository (repeatable)
  --dry-run            Show what would happen; no fetch side effects beyond a read-only fetch
  --no-push            Fetch and integrate, do not push
  --strategy string    rebase | merge (default from config)
  --message string     Commit message for uncommitted working-tree changes
  --commit-all         Commit dirty .pmngr/docs changes before integrating
  --continue           Resume after resolving conflicts
  --abort              Abort an in-progress rebase/merge
```

```
$ gintrack sync --dry-run
ACME  ~/code/acme-api
  fetch origin              would fetch 2 new commits
  local changes             3 files modified (would be committed as
                            "docs(ACME): update ACME-T-0311 — Wire OIDC discovery endpoint")
  integrate                 rebase 1 local commit onto origin/main
  push                      would push 1 commit
AWEB  ~/code/acme-web       up to date
TEAM  ~/code/acme-team
  conflict risk             .pmngr/boards/platform-kanban.md modified locally and remotely
nothing was changed (--dry-run)
```

Conflicts are surfaced, not auto-resolved. For board `order:` lists and other list-shaped
YAML the sync engine offers a union merge assist in the web UI; the CLI reports the
conflicting paths and exits 5.

*As built (GIT-US-0021).* The flags above all work, plus `--no-snapshot` (skip the index
snapshot refresh a run that pulled work would do, rule R-SNAP-6(a) of doc 04 §6) and
`--json` (the machine-readable report). `--repo` is repeatable, `--continue` and `--abort`
are mutually exclusive, and so are `--dry-run` and `--commit-all`. Exit codes: 5 for a
conflict, 6 for any other git failure, 4 for an unregistered `--repo`, 2 for an unknown
`--strategy`. A repository that is registered but is not a git working tree is skipped with
its reason rather than failing the run. `--dry-run` fetches — which is read-only — and
changes nothing else.

### 4.8 `gintrack doctor [--fix] [--renumber]`

Health check across configuration, repositories, and content.

```
gintrack doctor [flags]
  --fix         Apply safe automatic fixes
  --renumber    Repair duplicate/colliding IDs (destructive, never implied by --fix)
  --yes         Do not ask for confirmation before a renumbering
  --repo string Limit to one repository (registration id)
  --strict      Treat warnings as errors (exit 3); useful in CI
  --json        Machine-readable output, with the diagnostics grouped by file
```

The command exits 3 when it found an error, and with `--strict` when it found a warning.
This build implements the checks that need no git backend: the configuration file and its
permissions, the readability of every registered path, the presence and validity of
`project.yaml`, every content diagnostic the indexer produces, and duplicate ids. The
`--fix` class is currently front matter rewritten in canonical key order and files whose
slug drifted from the title.

Checks performed:

1. **Environment** — Go binary metadata, git backend and version, embedded UI presence,
   config file permissions, writability of the config directory.
2. **Configuration** — unknown keys, unreadable repo paths, duplicate registrations,
   workspaces with zero repos, token strength.
3. **Repository** — is a git working tree or a Jujutsu workspace (GIT-US-0038: a jj
   repository is reported as such, with "reads and writes go through jj", and the
   `jj` scope reports the binary and warns when it is older than 0.41 or missing while a
   registered repository needs it — with GIT-US-0040 that warning has teeth, because a
   repository whose jj is older than 0.41 fails to open with `vcs_jujutsu_too_old` and one
   with no jj binary at all falls back to the read-only git guard, or to no backend when
   its store lives inside `.jj`), has a remote, docs folder exists, `.pmngr`
   scaffold present, `project.yaml`/`team.yaml` parse and validate. What is required
   depends on the role: a repository registered as a **team** repository is checked for a
   root `team.yaml` and never for a backlog — it holds none by the hard rule of doc 04 §1 —
   and a team registration without that file is an error naming
   `gintrack init <path> --team --key <KEY>` (GIT-US-0034).
4. **Content** — front matter parses; required fields present; `type` matches the folder;
   `status` is in the project workflow; `parent` exists and has the right type; `milestone`
   exists; `links[]` targets resolve; `assignees` are known team members (warning only);
   filename matches `<ID>-<slug>.md`; no duplicate IDs; no orphan comment folders; wikilinks
   `[[Page]]` resolve; Mermaid blocks parse.
5. **Index** — cache freshness, watcher descriptor budget (see cross-platform notes).

`--fix` handles only the safe class: normalize front matter key order and quoting, add
missing `updated`, rename files whose slug drifted from the title, create missing
`.pmngr` subfolders, remove index cache entries for deleted files, normalize line endings
per `.gitattributes`.

`--renumber` is separate and interactive by default because IDs are public identifiers
used by agents, commits and boards. It rewrites the colliding item, renames its file, its
comment folder, and every inbound `parent`/`links`/board `ref`, printing a full plan first.

```
$ gintrack doctor
✔ environment      gintrack 0.4.0, git 2.45.2 (system backend), ui embedded
✔ config           ~/.config/gintrack/config.yaml (0600), 1 workspace, 3 repos
✔ ACME             git ok, docs/, .pmngr ok, project.yaml valid
✖ ACME             duplicate id ACME-T-0207
                     docs/.pmngr/tasks/ACME-T-0207-cache-headers.md
                     docs/.pmngr/tasks/ACME-T-0207-cache-headers-2.md
                   fix: gintrack doctor --renumber --repo ACME
⚠ AWEB             12 items missing `updated`            fix: gintrack doctor --fix
⚠ AWEB             unresolved wikilink [[Auth Overview]] in docs/architecture.md
✔ TEAM             team.yaml valid, 3 boards, 2 sprints, 5 retros
2 errors, 2 warnings
```

### 4.9 `gintrack mcp`

Runs the Model Context Protocol server over stdin and stdout, so that an agent runtime can
spawn it as a tool server. Full specification in `08-mcp-server.md`.

```
gintrack mcp [flags]
  --allow-write          Advertise the write tools (default: mcp.allowWrite, false)
  --agent string         Agent name recorded as the author of comments it writes
  --repo path            Serve this repository without registering it; repeatable
  --list-tools           Print the tools this server would advertise, and exit
  -w, --workspace string Workspace to expose
```

```
$ gintrack mcp --list-tools --allow-write
add_comment
close_sprint
create_epic
create_inbox_item
create_milestone
create_requirement
create_spec
create_story
create_task
get_item
get_kb_page
import_youtrack_issues
list_inbox
list_items
list_kb_pages
list_requirements
move_on_board
publish_kb_page_to_youtrack
push_comment_to_youtrack
search_items
search_kb
search_semantic
spec_context
spec_coverage
spec_impact
sync_kb_page_from_youtrack
trace_requirement
transfer_sprint_items
triage_inbox_item
update_item
update_requirement
verify_requirement

$ gintrack mcp --agent claude-code
gintrack mcp 0.4.0: workspace work, 2 repositories, 13 tools (read-only)
```

Nothing but JSON-RPC frames is written to stdout; the startup line and every log go to
stderr. There are **thirty-two tools**: thirteen read-only — `list_items`, `search_items`,
`search_semantic`, `get_item`, `list_requirements`, `spec_context`, `spec_coverage`,
`spec_impact`, `trace_requirement`,
`list_inbox`, `list_kb_pages`, `get_kb_page` and `search_kb` — and nineteen writes. Without writes
enabled the nineteen write tools are absent
from `tools/list`, not merely refused.

Writes are enabled by `--allow-write` or by `mcp.allowWrite: true` in the configuration file
(section 3.2). The flag wins when it is typed — `--allow-write=false` turns the write tools
off for one run — and the configuration decides otherwise, so an agent runtime that spawns
`gintrack mcp` with no arguments gets the posture the user chose once. That setting is also
what the companion's **Settings › Agent tools (MCP)** switch writes
(`PATCH /api/v1/mcp/settings`, section 5.5), which is the way to enable writes without
editing a file or teaching every agent runtime a flag.

The **same thirty-two tools** are served over streamable HTTP at `POST /mcp` by
`gintrack serve --mcp-http` (section 4.1), which is what to use when the companion is already
running: one index and one watcher, shared with the web UI.

### 4.10 `gintrack version`

```
$ gintrack version
gintrack 0.4.0
commit:   9f2c1ab3
built:    2026-09-01T10:22:41Z
go:       go1.23.4 linux/amd64
ui:       embedded (web 0.4.0)
git:      system 2.45.2
core:     schema v1

$ gintrack version --json
{"version":"0.4.0","commit":"9f2c1ab3","date":"2026-09-01T10:22:41Z","go":"go1.23.4",
 "os":"linux","arch":"amd64","ui":"embedded","git":{"backend":"system","version":"2.45.2"},
 "schema":"v1"}
```

### 4.11 `gintrack completion`

Standard cobra completion generator (`bash`, `zsh`, `fish`, `powershell`). Custom
completions are registered for item IDs, project keys, status names (read from the active
project workflow), labels, board slugs and workspace names, so `gintrack item move ACME-T-<TAB>`
is useful.

### 4.12 `gintrack config`

```
gintrack config path [--json]   Print the resolved configuration file path
gintrack config show [--json]   Print the effective configuration
gintrack config init [--force]  Write a default configuration file
```

`config path` prints the file the precedence chain of section 3.1 selected, whether or not
it exists; with `--json` it also reports the state and cache directories and the active
workspace. `config show` prints the configuration after the flags, the environment and the
file have been layered on the defaults, which is the fastest way to see why a command is
looking at the wrong port or workspace. `config init` creates the file with mode `0600`,
and refuses to replace an existing one without `--force`.

```
$ gintrack config path
/home/jose/.config/gintrack/config.yaml

$ gintrack config show | head -4
# /home/jose/.config/gintrack/config.yaml
version: 1
defaultWorkspace: work
workspaces:
```

### 4.13 `gintrack snapshot [KEY...]`

Refreshes the committed index snapshots of the team repository (doc 04 §6), so that team
boards can render the cards of projects other people have not cloned.

```
gintrack snapshot [KEY...] [flags]
  --team string           Id of the registered team repository (default: the only one)
  --generated-by string   Handle recorded in the file (default: the configured author)
  --include-closed        Keep closed items regardless of their age
  --max-age-days int      How long a closed item stays in the snapshot (default 30)
  --dry-run               Report what would change; write nothing
  --json                  Machine-readable output
```

```
$ gintrack snapshot
DEMO     .pmngr/index/DEMO.json       written     (5 items)
WEB      —                            skipped     (not cloned in this workspace)
1 written, 0 unchanged, 1 skipped
commit them with `git -C ~/code/acme-team commit -m "chore(pmngr): refresh index snapshots"`

$ gintrack snapshot
DEMO     .pmngr/index/DEMO.json       unchanged   (5 items)
WEB      —                            skipped     (not cloned in this workspace)
0 written, 1 unchanged, 1 skipped
```

- The file carries front-matter-derived fields only — never a body, never a comment
  (R-SNAP-1) — with items sorted by id, two-space indentation and a trailing newline
  (R-SNAP-2).
- A project no registered repository serves is **skipped with a reason**, never guessed at.
- A regenerated file whose content matches the one on disk is **not written**: the run
  reports `unchanged` and the git history is left alone (ADR-014). This is what makes the
  command safe to run in CI on every push, or on a schedule.
- The command writes files; it does not commit or push them. Committing is `gintrack sync`
  (§4.7) or git itself, with the message prefix of R-SNAP-7.
- Exit codes: 4 when no team repository is registered or a named key is not declared, 2
  for a malformed key.

### 4.14 `gintrack init [path]`

Creates a project backlog in a repository that does not have one — or, with `--team`, a whole
team repository — so that a team can start from an empty repository without hand-writing a
`project.yaml` or a `team.yaml`.

```
gintrack init [path] [flags]           # path defaults to "."

  --key string          Project key, matching [A-Z][A-Z0-9]{1,9} (required)
  --name string         Human name; defaults to the key
  --description string  One paragraph shown in project pickers
  --timezone string     IANA timezone for date-only fields (default: UTC)
  --docs string         Documentation folder, "." for the repository root
                        (default: docs)
  --knowledge string    With --team, the knowledge-base folder
                        (default: knowledge)
  --register            Register the repository in the active workspace too
  --team                Create a team repository instead of a project
  --json                Machine-readable output
```

It writes exactly what doc 03 §2.2 prescribes — `project.yaml` with schema 1, the key, the
name and the default six-status workflow of doc 03 §6.2; a `.gitignore` holding
`index.json`; and the five item folders plus `attachments/` — and nothing else. No commit is
made: the new files are left for the user or for `gintrack sync`.

The command is **fully non-interactive**, because agents and scripts use it:

```bash
$ gintrack init . --key ACME --name "ACME Platform"
created project ACME (ACME Platform) in docs/.pmngr/project.yaml
register the repository with `gintrack add /home/jose/code/acme --docs docs`

$ gintrack init ~/code/mono --key API --docs apps/api/docs --register --json
{"key":"API","name":"API","docsFolder":"apps/api/docs", …,"repo":{…}}
```

`--register` also declares the folder, which matters for a nested one: discovery reaches the
repository root and its first-level directories on its own (doc 03 R-DISC-1,
[ADR-018](./adr/ADR-018-bounded-project-discovery.md)).

- Exit codes: 2 without `--key`, 3 for a key the grammar refuses, 5 for a folder that
  already holds a `project.yaml` — a backlog is never overwritten.
- The same scaffolder serves every surface: `core.CreateProject` in the shared core, reached
  by this command, by `gintrack add --key`, by `POST /repos/{id}/projects` (§5.5) and by the
  add-repository wizard of the web app.

#### `gintrack init --team` — a team repository

With `--team` the command writes what [doc 04 §2](./04-team-repository.md) prescribes instead:
`team.yaml` at the repository root (schema 1, the key, the name, the documented defaults, an
empty `members:` and an empty `projects:`), the `.pmngr/boards/`, `.pmngr/sprints/`,
`.pmngr/retros/` and `.pmngr/index/` folders, and a `knowledge/` base with an `index.md`
landing page. `--docs` plays no part: `team.yaml` sits at the root by R-TEAM-LOC-1.

A team key is `[A-Z][A-Z0-9-]{1,15}` — hyphens are allowed, unlike a project key.

```bash
$ gintrack init ~/code/acme-team --team --key ACME-TEAM --name "ACME Delivery Team" --register
created team repository ACME-TEAM (ACME Delivery Team) in team.yaml
  boards, sprints and retros go in .pmngr
  the team knowledge base is knowledge
registered team repository acme-team  /home/jose/code/acme-team
declare the projects this team owns in team.yaml (docs/04 section 3.3)
```

The `--json` payload is a different shape from the project one — a team repository has no
backlog and no documentation folder:

```json
{"key":"ACME-TEAM","name":"ACME Delivery Team","root":"/home/jose/code/acme-team",
 "configPath":"team.yaml","knowledgePath":"knowledge","teamDirPath":".pmngr","repo":{…}}
```

- Exit codes: 2 without `--key`, 3 for a key the grammar refuses, 5 for a folder that already
  holds a `team.yaml` — the routing table of a workspace is never overwritten.
- The projects list starts empty on purpose, and that is not an error: see
  [ADR-020](./adr/ADR-020-creating-a-team-repository.md).
- The same scaffolder serves every surface: `core.CreateTeam`, reached by this command, by
  `gintrack add --team --key`, by `POST /repos/{id}/team` (§5.5) and by the add-repository
  wizard of the web app.

### 4.15 `gintrack youtrack`

```
gintrack youtrack connect --url URL --project SHORTNAME [--project-key KEY]
                          [--token TOKEN] [--push-comments manual|auto]
                          [--kb-sync manual|on_write]
                          [--kb-sync-direction push|pull|both] [--json]
gintrack youtrack status  [--project-key KEY] [--offline] [--json]

gintrack youtrack import <query | ID...> [--project-key KEY] [--depth N]
                          [--comments] [--attachments] [--links]
                          [--dry-run] [--json]
gintrack youtrack push-comments <ITEM-ID> [--all | --comment PATH]
                          [--project-key KEY] [--wait] [--json]
gintrack youtrack kb push|pull <path> [--project-key KEY] [--recursive]
                          [--wait] [--json]
gintrack youtrack kb status [path] [--project-key KEY] [--recursive]
                          [--remote] [--json]
```

`connect` and `status` configure the connection; `import`, `push-comments` and
`kb` move content. The content commands hold no integration logic of their own:
each dispatches exactly one core method — `youtrack.import.preview` /
`youtrack.import.run`, `youtrack.comment.push`, `youtrack.kb.publish` /
`youtrack.kb.pull` / `youtrack.kb.status` — the same ones the REST API and the
MCP server call, so the idempotence rules and the git writes have a single
implementation. They open a companion with no listener rather than requiring
`gintrack serve` to be running.

`connect` links one git-in-track project to one YouTrack project. It resolves the
permanent token from `--token`, then `$GINTRACK_YOUTRACK_TOKEN`, then standard
input when it is piped — there is no interactive prompt, so the command stays
scriptable — validates it with `GET /api/users/me` and reads the remote project,
and only then writes anything. On success the instance URL, the project short
name and the sync modes are written into the project's `project.yaml`
(doc 03 §6.1) and the token into this machine's `0600` configuration file
(§3.2). `--project-key` names the git-in-track project when the workspace holds
more than one. An existing `field_map` is kept: connect sets the connection, it
does not reset the mapping.

`status` prints the instance, the project mapping, whether a token is present and
where it came from, and — unless `--offline` — the result of a live probe.

Neither command ever prints the token, in either form. `--json` reports the
provenance (`flag`, `env`, `stdin`, `file` or `none`) and never the value.

Prefer the environment or a pipe to `--token`: a token on a command line lands in
the shell history and in every process listing on the machine.

```
# From a provisioning script: the token comes from the environment.
$ export GINTRACK_YOUTRACK_TOKEN="$(vault read -field=token secret/youtrack)"
$ gintrack youtrack connect --url https://yt.example.com/youtrack --project ACME --json

# Or piped in, when the token is not already in the environment.
$ printf '%s' "$YT_TOKEN" | gintrack youtrack connect --url https://yt.example.com/youtrack --project ACME
Connected DEMO to ACME on https://yt.example.com/youtrack as jose.
  link:  demo:docs/.pmngr/project.yaml
  token: /home/jose/.config/gintrack/config.yaml (read from the stdin, stored file)

$ gintrack youtrack status
project:  DEMO
instance: https://yt.example.com/youtrack
mapping:  DEMO -> ACME
link:     demo:docs/.pmngr/project.yaml
token:    present (file)
probe:    ok as jose

$ gintrack youtrack status --json
{"projectKey":"DEMO","configured":true,"url":"https://yt.example.com/youtrack",
 "project":"ACME","hasToken":true,"tokenSource":"file",
 "projectPath":"demo:docs/.pmngr/project.yaml","probed":true,"ok":true,"login":"jose",
 "fullName":"Jose F. Rives"}
```

Exit codes (§4 conventions): `0` when the connection works; `2` for a missing
`--url`, `--project` or token; `3` when the connection would not be a valid
`integrations.youtrack` block; `4` when the project is unknown or declares no
connection; `1` when the probe fails — the message says whether YouTrack
answered 401 (bad token), 403 (no permission) or 404 (the URL is missing its
instance context path). A failed probe writes nothing at all.

#### `import`

One argument that is not a readable issue id is a YouTrack query; one or more
arguments that are issue ids import exactly those issues. Give one or the other,
never both — the vault refuses the combination with a field-level
`invalid_request`. `--depth` bounds the subtask recursion from `0` (the selected
issues only) to `5`.

An import is **idempotent**: the pair `(system, id)` of an issue decides whether
it becomes a new item or patches the one already mirroring it, so importing the
same issue twice can never produce a second item.

`--dry-run` calls the preview: what each issue would become, which item an
update would patch, and what could not be resolved. Nothing is written.

```
$ gintrack youtrack import "project: ACME #Unresolved" --depth 1 --dry-run
ISSUE    ACTION  ITEM          TYPE   TITLE
ACME-42  create  —             story  Guest checkout
ACME-58  update  DEMO-T-0031   task   Rate-limit the login endpoint
2 issues would be imported into DEMO; nothing was written.

$ gintrack youtrack import ACME-42 ACME-58 --comments
ISSUE    ACTION  ITEM          ERROR
ACME-42  create  DEMO-US-0044  —
ACME-58  update  DEMO-T-0031   —
2 created, 0 updated, 0 failed in DEMO.
```

A failure is recorded per issue and never aborts the batch: the other issues
still land, the row carries the reason and the command exits `1`. Per-issue
warnings — a field value that did not map, a parent outside the import set — go
to stderr, so `--json | jq` stays safe.

#### `push-comments`

Queues the comments of one item for the issue it mirrors. Give `--all` for the
whole thread or `--comment <path>` for one comment; one or the other, never
neither and never both. A comment that already carries a YouTrack reference is
**skipped** rather than posted twice.

`pushed` means *queued*. Without `--wait` the command prints the job id and the
table of what was selected; with `--wait` it follows the job and prints the
remote comment id of each one, exiting non-zero when any comment failed.
Interrupting a `--wait` stops the watching, not the job.

An item that mirrors no issue is refused before anything is queued, with exit
`3` and a message saying to import or link it first: it is not a failure another
attempt would fix.

**A local delete never deletes remotely.** There is no job for it and
deliberately none — a repository is not the authority on an issue's
conversation, and a mistaken `rm` must not erase a thread other people are
reading. The divergence is permanent and intended.

```
$ gintrack youtrack push-comments DEMO-US-0001 --all --wait
COMMENT                                                  REMOTE  RESULT   NOTE
docs/.pmngr/comments/DEMO-US-0001/20260901T1045Z-marta.md  4-118   pushed
docs/.pmngr/comments/DEMO-US-0001/20260902T0912Z-jose.md   4-103   skipped  the comment is already on the issue
```

When the project sets `integrations.youtrack.push_comments: auto`, this command
is unnecessary: every comment written on a linked item is queued by the same
seam, on every surface.

#### `kb push` and `kb pull`

`push` publishes knowledge-base pages as YouTrack articles; `pull` writes
articles back into pages. A path naming a page selects that page; a path naming
a folder selects its direct pages, or every page below it with `--recursive`.

Both are background jobs. Without `--wait` the command prints the job id and the
pages it selected; with `--wait` it follows the job and prints each page's
resulting state.

**The exit code is what a CI step relies on**: `0` when every page is in sync,
`5` when any page conflicted, `1` when the job itself failed. A conflict is not
an error — the page is left untouched and `<page>.conflict.md` holds the other
side — but nobody has reconciled the two, so it must not read as success.

```
$ gintrack youtrack kb push docs --recursive --wait
PAGE                            STATE     ARTICLE     NOTE
docs/index.md                   in_sync   DEMO-A-1    —
docs/architecture/overview.md   conflict  DEMO-A-2    —
2 pages published in DEMO.
1 page changed on both sides; <page>.conflict.md holds the other side.
$ echo $?
5
```

`kb status` reports the same states without changing anything:
`unlinked`, `in_sync`, `local_ahead`, `remote_ahead` or `conflict`. It reads only
the repository unless `--remote` is passed, which is **one article read per
page** and therefore never the default.

```
$ gintrack youtrack kb status docs --recursive --json
{"project":"DEMO","pages":[{"path":"docs/index.md","linked":true,"articleId":"DEMO-A-1",
 "url":"https://yt.example.com/youtrack/articles/DEMO-A-1","state":"in_sync",
 "syncedAt":"2026-09-13T12:00:00Z"}],"remote":false}
```

### 4.16 `gintrack sprint`

```
gintrack sprint list [--status draft|upcoming|current|completed]... [--board ID]
                     [--team ID] [--json]
gintrack sprint show <SPRINT-ID> [--team ID] [--json]
gintrack sprint start <SPRINT-ID> [--force] [--team ID] [--json]
gintrack sprint close <SPRINT-ID> [--transfer next|backlog|none] [--target SPRINT-ID]
                     [--dry-run] [--team ID] [--json]
gintrack sprint transfer <SPRINT-ID> --to SPRINT-ID [--dry-run] [--team ID] [--json]
```

Sprints live in the team repository and their items live in the project
repositories, which is the fact every one of these commands is shaped by. The
status a sprint is listed under is **derived** from its dates and today —
`draft`, `upcoming`, `current` or `completed` — computed on every read and never
written to the file, so a sprint becomes current because the calendar says so and
not because somebody remembered to edit it (doc 04 §8).

`close` and `transfer` never guess. They report, item by item, what they applied
and what they refused: an item in a project this machine has not cloned is a
`repo_not_cloned` refusal on its own line, not a silent skip and not a failure of
the run. The exit code is `0` when anything at all could be applied and `1` only
when nothing could — a partial run is a partial run, and a CI step that fails on
one uncloned repository would be useless.

`--dry-run` computes the whole report and writes nothing, which is what a job
asking "is this sprint clean?" wants.

```
$ gintrack sprint list
current
  DEMO-TEAM-S-0001  Sprint 12  2026-09-01 → 2026-09-14  18 items, 34 points
upcoming
  DEMO-TEAM-S-0002  Sprint 13  2026-09-15 → 2026-09-28   4 items,  8 points

$ gintrack sprint close DEMO-TEAM-S-0001 --transfer next --dry-run
completed    14 items, 27 points
incomplete    4 items,  7 points
carried      DEMO-US-0044 -> DEMO-TEAM-S-0002
refused      WEB/WEB-US-0031: the repository holding WEB is not cloned
nothing was written (--dry-run)
```

### 4.17 `gintrack inbox`

```
gintrack inbox list [--status pending|snoozed|rejected|accepted|duplicate|all]
                    [--project KEY] [--limit N] [--json]
gintrack inbox add --title T [--project KEY] [--type epic|story|task|milestone]
                   [--body B] [--source S] [--priority P] [--label L]... [--author A] [--json]
gintrack inbox accept <id> [--status S] [--type T] [--parent ID] [--json]
gintrack inbox reject <id> [--json]
gintrack inbox snooze <id> --until DATE [--json]
```

An inbox item is an ordinary item whose status belongs to the reserved triage
category; the `inbox:` block records how it arrived and what the triager decided
(doc 03 §12, ADR-033). Accepting one moves it into the ordinary workflow,
rejecting one cancels it, and snoozing one hides it until a date.

Every decision goes through `inbox.triage`, the same core method the web
application and the MCP tool `triage_inbox_item` call, quoting the `rev` the
command read — so a row somebody triaged first is a conflict (exit `5`) and never
an overwrite. `list` defaults to `--status pending`, which is the queue.

`add` files a submission through `item.create` with an `inbox` block, which is
what the MCP tool `create_inbox_item` calls too. It is deliberately thinner than
`gintrack item new`: there is no `--status` and no `--parent`, because a
submission has not been triaged yet and those are `accept`'s to choose. The
defaults are `--type story` and `--source cli`; `--project` is required only when
the workspace holds more than one project, and an omitted one is refused by the
core naming how many it found (exit `2`). `--body -` reads the body from standard
input, the same convention `item new` and `item comment` use. A project that
declares no status in the triage category has no inbox and the command refuses
with `no_triage_status`.

```
$ gintrack inbox list
ID            TYPE   TITLE                        TRIAGE   SOURCE    RECEIVED
DEMO-T-0090   task   Checkout times out on 3G     pending  web       2 days ago
DEMO-T-0091   task   Add a dark theme             snoozed  youtrack  6 days ago

$ gintrack inbox accept DEMO-T-0090 --status todo --parent DEMO-US-0001
accepted DEMO-T-0090 into todo under DEMO-US-0001; 1 pending

$ cat report.md | gintrack inbox add --title "Checkout hangs on Safari" --body -
filed DEMO-US-0092  docs/.pmngr/stories/DEMO-US-0092-checkout-hangs-on-safari.md
pending, source cli
```

The inbox REST surface is `GET /api/v1/inbox` and `POST /api/v1/items/{id}/triage`
(§5.5). `integrations.youtrack.land_in_inbox` (doc 03 §6, R-INT-7) is the fourth
entry point, and the importer honours it: an issue the import **creates** is
written with the status `core.InboxLandingStatus` decides plus an `inbox:` block
of `status: pending`, `source: youtrack`, while an issue it **updates** keeps
the status it has — landing is a decision about arrival, not about every later
sync. A project that sets the option and declares no triage status has no inbox,
so the whole import is refused with `no_triage_status` before anything is
written, preview included.

### 4.18 `gintrack agent init`

```
gintrack agent init [--repo <path>] [--companion-url <url>] [--agui-port <n>] [--kb-path <dir>]
                    [--force] [--json] [--pando <path>] [--age-keys <set>] [--plaintext-token]
```

Writes the Pando-side configuration for one repository so that `pando agui-serve` can act as
the agent behind the companion's `/api/v1/agent` proxy (docs/20, ADR-035). At the repository
root it creates `.pando.toml` (the `[AGUI]` adapter with its profile and tool allow-list,
`[MCPServers.gintrack]` pointing at this companion's `/mcp`, `[Remembrances]` pointing at the
repository's own documentation folder with `KBWatch = true`, `[MCPServer]` HTTP off),
`agents/personas/backlog-assistant.md` and `agents/skills/gintrack-search/SKILL.md`. Outside the repository it
creates, once, the AG-UI token file `<stateDir>/agui/<repo id>.token` (mode 0600). It never
prints a token.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--repo <path>` | `.` | The repository to configure; must be a mounted repository |
| `--companion-url <url>` | the configured bind address | Where Pando reaches this companion |
| `--agui-port <n>` | `8090` | Port written into `[AGUI] Port` |
| `--kb-path <dir>` | the repository's documentation folder | Directory written into `[Remembrances] KBPath` — the folder Pando indexes as its knowledge base |
| `--force` | off | Replace all three files wholesale instead of merging and skipping |
| `--json` | off | Machine-readable summary (`tokenEncrypted` reports which token form landed; `skipped`, `pandoConfigMerged`, `pandoConfigBackup` and `divergences` report the re-run) |
| `--pando <path>` | the `pando` on `PATH` | Binary used to encrypt the companion token with `pando secret` |
| `--age-keys <set>` | Pando's default set | Forwarded as `pando secret --age-keys <set>` (key sets live under `~/.config/pando/keys/`) |
| `--plaintext-token` | off | Write the token as a literal `Authorization` header instead of encrypting it |

**What Pando indexes is the repository itself** (GIT-EP-0020). `[Remembrances] KBPath` is the
repository's documentation folder — the one the registration declares, `docs` by default —
and not a directory outside the repository: Pando's KB walk applies no exclusions of any kind,
so a repository root would be indexed whole, while the documentation folder reaches the
backlog under `.pmngr/` and the knowledge-base pages and nothing else. That is also precisely
the half Pando's code indexer cannot see, because it skips dot-directories. `--kb-path` writes
another directory for a layout this cannot guess; the value is expanded (`~`, relative paths)
before it is written.

`KBWatch = true`, Pando's own default, so an edit is reindexed as it happens. The watcher
performs no file write of any kind; the tools that do mirror a document back to disk are
`kb_add_document`, `kb_delete_document` and the memory `remember`/`forget` path, which emit
Pando's typed front-matter keys alone and would strip an item's `id`, `status` and `parent`.
The generated `[AGUI] Tools` allow-list therefore admits the KB tools that read
(`kb_search_documents`, `kb_get_document`, `kb_related_documents`) and none that writes.

**Re-running is the normal way to pick up a change** — a new companion URL, a new token, a
newer template — and nothing already in place is an error. `.pando.toml` is merged into; the
persona and the skill are gintrack's own files that may have been edited by hand, so an
existing one is left untouched, named on stdout with the note that `--force` overwrites it,
and the run continues and exits 0.

**An existing `.pando.toml` is merged into, never replaced** (GIT-T-0227). A Pando
configuration is a Pando-wide file that long predates this feature, so the merge is a
line-level text edit: it finds each table by its header line, writes the keys gintrack owns,
and inserts the tables the file lacks verbatim from the template with their comments. Every
other table, key, comment, blank line and array-of-tables entry — including root-level keys
before the first table header — survives byte for byte. The file is never decoded and
re-encoded, because no TOML library available here preserves comments.

`[MCPServers.gintrack]` (with its `Auth`/`Headers` sub-tables) and `[AGUI.Profiles.<persona>]`
are gintrack's outright and are rewritten whole, which is also what keeps the three mutually
exclusive token branches — encrypted `Auth`, plaintext `Headers`, commented-out stub — from
accumulating across re-runs. In the tables gintrack shares with the user (`[AGUI]`,
`[ToolDiscovery]`, `[MCPGateway]`, `[PersonaAutoSelect]`, `[Skills]`, `[Remembrances]`,
`[MCPServer]`) a missing key is added and **a key that is already there with another value is
left alone** and reported on stdout with the recommended value and a one-line reason. That
conflict policy is deliberate: in a live configuration `[ToolDiscovery] Enabled` and
`[MCPGateway] Enabled` are load bearing, and imposing the template's values would break a
working setup.

`[AGUI]` is the one exception to it. Pando rewrites that section with every key at its zero
value the moment anything touches it, so a key there that is `''`, `0`, `false` or `[]` is
the absence of a choice rather than a choice: it counts as unset and the template's value is
written in place, with no divergence reported. The rule stops at that table — a `false` in
`[ToolDiscovery]`, `[MCPGateway]` or `[MCPServer]` is a setting somebody relies on and still
only produces a warning.

A copy of the previous version is written next to the file as
`.pando.toml.<timestamp>.bak` before the first edit — `.pando.toml` is git-ignored, so git is
not a safety net — and its path is printed. The merged file keeps mode 0600.

Exit code 5 when the merge cannot parse a line that opens a table (nothing is written, not even the backup, and the
message names the line number and the line), when `pando` cannot be found, or when
`pando secret` does not return an `age1:` ciphertext — again nothing is written, and the
message offers `--pando` and `--plaintext-token`.

**The generated `.pando.toml` carries the companion bearer token encrypted** in
`[MCPServers.gintrack.Auth]` (`Type = 'bearer'`, `Token = 'age1:…'`): Pando cannot expand
environment variables in an MCP server's configuration, but it decrypts an `age1:` value on
load and derives the `Authorization: Bearer …` header itself. `--plaintext-token` writes the
old `[MCPServers.gintrack.Headers]` form instead. The file is mode 0600 and must be
git-ignored; the command says so on stdout and never prints the token or the ciphertext.
`pando secret` takes its value as an argument (it reads no stdin), so the token is briefly
visible in the local process list. The two commands to run next are printed: `pando
agui-serve --cwd <repo> --port <n> --no-tls --token-file <f>` and `gintrack serve --agent
--mcp-http`.

### 4.19 `gintrack spec ingest <report>...`

> **Implemented** by `GIT-US-0115`. It is the first command of the `gintrack spec` family;
> `lint`, `impact`, `coverage`, `verify` and `trace` come with `GIT-US-0125`. Stamping
> `verified:` and the coverage state are `GIT-US-0116`; the per-requirement `verify.json` cache of
> doc 03 R-REQ-11 is `GIT-US-0141`. Native only: browser-only mode cannot run or ingest tests.

Record the last result of every test a run reported, so the requirements those tests verify can
be answered from real runs (ADR-037 §7, doc 03 §21.6). Nothing is written into a spec or anywhere
in the repository.

```bash
go test -json ./... > go.json;            gintrack spec ingest go.json
npx vitest run --reporter=json > vt.json; gintrack spec ingest --base web vt.json
gintrack spec ingest --format junit build/test-results/*.xml
go test -json ./... | gintrack spec ingest -
```

| Flag | Default | Meaning |
|---|---|---|
| `--format go\|junit\|vitest` | detect | report format; detection reads the first bytes: `<` is JUnit, a line with an `"Action"` key is `go test -json`, any other `{` is Vitest |
| `--repo <path>` | `.` | the repository the tests belong to; its working-tree root is the nearest ancestor holding `.git` or `.jj` |
| `--base <dir>` | root | repository-relative directory the report's **relative** paths start from, e.g. `web` for a Vitest run in `web/` |
| `--commit <sha>` | `HEAD` | commit the run was taken at; defaults to the working tree's current commit (`@` under jj), empty when there is none |
| `--cache <file>` | see below | the test-result cache file |
| `--json` | | print `{root, commit, cache, reports[], stored, requirements[]}` |

**Supported formats.**

| Format | Produced by | A test is | Outcome |
|---|---|---|---|
| `go` | `go test -json` (`cmd/test2json`) | `Package` + `Test` (`TestX/sub_case`) | terminal `pass` / `fail` / `skip` events; `run`, `pause`, `cont`, `output` and package-level events are ignored, so parallel tests may interleave freely; non-JSON lines (build output from `2>&1`) are skipped |
| `junit` | go-junit-report, Vitest/Jest JUnit, pytest, Surefire, … | `<testcase classname name file?>` in `<testsuites>`/`<testsuite>`, nested at any depth | `<failure>` or `<error>` fails, `<skipped>` skips, otherwise passes |
| `vitest` | `vitest run --reporter=json` (Jest `--json` has the same shape) | `testResults[].name` (the file) + `assertionResults[].ancestorTitles` and `title` | `passed` passes, `failed` fails, `skipped`/`pending`/`todo`/`disabled` skip |

All three parsers stream: a report is read one event, test case or test file at a time. A test
reported more than once in one report (`-count=2`, a rerun) collapses to one result — failing
beats passing beats skipped. Malformed input (a truncated JSON event, broken XML, an XML or JSON
document that is not a report) exits 3 and records nothing.

**Mapping a test to a trace ref.** Each test becomes `{id, format, path, symbol, result,
durationNs, commit, at}`, where `path#symbol` is spelled exactly as a `Verifies:` marker or a
`trace.tests` entry spells it (doc 03 §21.7):

- **go** — the import path is mapped to a directory through the module path of the root `go.mod`,
  else through the longest suffix of the import path that is a directory of the tree (nested
  modules, vendored copies); the test to the `_test.go` file of that directory declaring the
  top-level `TestX`. The symbol is the name as `go test` prints it, `TestX/sub_case`.
- **vitest** — the file the report names; the symbol is `describe > … > it`.
- **junit** — the `file` attribute (of the test case, else of the nearest suite) when present;
  else the `classname` read as a Go import path, then as a file path, then as a dotted Python
  module (`tests.test_mod.TestCase` → `tests/test_mod.py#TestCase.test_x`). The symbol is the
  test case name, qualified by the class part of the `classname` for a Python file.
- A relative path is tried under `--base`, then at the root; an absolute path under the root is
  made relative; any other path (the CI runner's checkout) maps to its **longest suffix** that
  exists in the tree.
- **Ambiguity.** When two `_test.go` files of one package declare the same test (build-tagged
  variants), the first in lexical order wins and the others are listed in `ambiguous`.
- A test that maps to no file keeps its report-level `id` (`<package or classname or
  file>#<name>`), counts as `unmapped`, and never matches a trace ref.

**Matching a requirement.** A requirement's linked tests are its `Verifies:` markers and
`trace.tests` entries (the trace graph, §6.7). A symbol ref takes the result of the same symbol
and of every symbol it encloses (`TestX` takes `TestX/sub`); with none it falls back to the nearest
enclosing result (`TestX/sub` takes `TestX`, for a JUnit report without sub-tests). A whole-file
ref takes every test of the file. Each linked test's outcome is failing over passing over skipped,
or `missing`; the requirement is `fail` if any linked test failed, `pass` if every one passed,
`partial` if some passed and the others are skipped or missing, and `untested` otherwise. These
are raw evidence for the coverage state of doc 03 R-REQ-12, not that state (there is no
`suspect` here). The command lists every requirement with at least one linked test.

**The test-result cache.** Results go to
`<index cache dir>/test-results/<hash of the repository root>.json` (§3.2 `index.cacheDir`,
default the configuration directory), one file per repository on this machine, outside the
repository and therefore never committed. Each test keeps its **last** result, whatever it was: a
new result replaces the cached one with the same `path#symbol` (so a JUnit run replaces the
`go test` run of the same test), or with the same format and `id` when unmapped. The file is
versioned; a missing, corrupt or other-version file reads as empty and is rebuilt by the next
ingest — never an error. Deleting it loses only evidence; ingesting the reports again rebuilds it.

---

## 5. Local REST API

Base URL: `http://127.0.0.1:7317/api/v1`. All requests and responses are JSON
(`application/json; charset=utf-8`) unless a raw Markdown representation is explicitly
requested.

### 5.1 Authentication

A bearer token is generated on first run (32 random bytes, base64url) and stored in
`server.token`. It is printed to the terminal on `serve` and shown in the config file.

```
Authorization: Bearer s7Q1e...9Zk
```

Rules:

- Every route except `GET /api/v1/health` requires the token.
- The WebSocket endpoint accepts the token via the `Authorization` header or, for browser
  clients that cannot set headers on `WebSocket`, via the `?token=` query parameter or the
  `Sec-WebSocket-Protocol: gintrack.v1, bearer.<token>` sub-protocol form.
- Token comparison uses `crypto/subtle.ConstantTimeCompare`.
- `--token none` disables auth and is refused unless the bind address is loopback.
- The token is *not* a security boundary against other local users; on shared machines,
  document that any local process can reach loopback ports.

#### 5.1.1 What the token is holding up once a tunnel is open

Everything above was written for a server nobody outside the machine can reach.
A public tunnel (§4.1, §5.5, ADR-027) removes that assumption, and it is worth
being blunt about what is left.

- **An open tunnel publishes a read-write server.** It is not a preview and not a
  read-only view: every `/api/v1` route is reachable, including the ones that
  create, edit and delete items, move cards, run a sync and commit to the user's
  repositories. Whoever gets through the token has the access the user has, over
  every mounted repository.
- **The bearer token is the only protection.** There is no second factor, no
  allow-list of visitors, no rate limit that would matter and no way to see who
  is connected. That is exactly why opening a tunnel on a server started with
  `--token none` is **refused**, with `409` and the `tunnel_requires_token`
  problem, rather than being permitted with a warning: an unauthenticated server
  on a public URL has no protection at all.
- **The web app is served without authentication.** The SPA bundle sits outside
  the auth group — it has to, because the tab needs the code that will ask for
  the token — so *anyone holding the URL loads the interface*. Only `/api/v1`
  demands the token. A stranger who finds the address therefore sees the login
  surface of a real workspace, and the address, while random, travels through
  Cloudflare's infrastructure and through whatever channel it was shared on.
- **`https://<host>/?token=<token>` is a full credential, not a convenience
  link.** The `?token=` form of §4.1 exists so the local browser opens ready to
  work; over a tunnel the same link hands a stranger read and write access with
  no password asked. Treat it exactly like a password: send it only to someone
  you would hand your working copy to, never over a channel you would not put a
  password on, and never paste it into an issue, a chat log or a screenshot. The
  UI keeps the two share actions apart for this reason (doc 05 §3.1): the
  prominent one copies the bare URL, and the link that carries the token is a
  second, quieter control with this warning next to it.
- **Turning the tunnel off is the revocation.** The hostname is regenerated on
  every enable, so switching the tunnel off invalidates every link that was
  shared. Rotating the token (`gintrack serve --token new`) is what revokes
  access that was already granted through a link.

### 5.2 CORS

Allowed origins:

- The embedded origin itself (`http://127.0.0.1:<port>` and `http://localhost:<port>`).
- `http://localhost:5173` and `http://127.0.0.1:5173` — the Vite dev server, **only when
  `--dev` is passed or `log.level=debug`**.
- Anything listed in `server.extraOrigins`.

Response headers on allowed origins:

```
Access-Control-Allow-Origin: http://localhost:5173
Access-Control-Allow-Credentials: false
Access-Control-Allow-Methods: GET, POST, PATCH, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Authorization, Content-Type, If-Match, X-Request-Id
Access-Control-Expose-Headers: ETag, X-Total-Count, X-Request-Id
Vary: Origin
```

Any other origin gets no CORS headers (the browser blocks the read). Combined with the
bearer token this prevents drive-by localhost attacks from arbitrary web pages.

### 5.3 Conventions

- **Versioning** — the path carries the major version (`/api/v1`). Additive changes only
  within a major version; clients must ignore unknown fields.
- **Optimistic concurrency** — every item, page and board response carries `rev`, a
  content hash (`sha256:` + first 16 hex chars of the canonical file bytes) and the same
  value in the `ETag` header. Mutations require `If-Match: <rev>`; a mismatch returns
  `412 Precondition Failed` with the `stale_revision` problem, which carries `currentRev`
  and `conflicts[]` — the fields the refused write would still have changed against the
  content on disk now — so that the client can merge without a second round trip. A mutation
  that omits the header on something that already exists returns `428 Precondition Required`
  (`precondition_required`), because a write with no revision is a lost update waiting to
  happen. `If-Match: *` bypasses the check (documented as unsafe). The contract itself is
  defined once, in `03-data-model.md` section 5; this section only says how HTTP spells it.
  Posting a comment creates a new file and therefore needs no `If-Match`; when one is sent it
  is honored against the *item's* revision, which is what the MCP surface requires of agents.
- **Pagination** — `?limit=` (default 50, max 500) and `?offset=`, plus `X-Total-Count`.
  Cursor pagination (`?cursor=`) is available on `/items` for large backlogs and is what
  the MCP layer uses.
- **Sorting** — `?sort=updated,-priority`; a leading `-` reverses. Ordering is total and
  deterministic: the final tiebreaker is always `id` ascending.
- **Filtering** — repeatable query params are OR within a field and AND across fields.
- **Time** — RFC 3339 UTC strings everywhere.
- **Request IDs** — clients may send `X-Request-Id`; the server echoes it and includes it
  in logs and problem details.
- **Idempotency** — `POST /items` accepts `Idempotency-Key`; a repeat within 10 minutes
  returns the original result instead of creating a duplicate.

### 5.4 Error format (RFC 7807)

Content type `application/problem+json`.

```json
{
  "type": "https://git-in-track.dev/problems/stale-revision",
  "title": "Stale revision",
  "status": 412,
  "detail": "Item ACME-US-0042 was modified on disk since revision sha256:6f1c…a09.",
  "instance": "/api/v1/items/ACME-US-0042",
  "code": "stale_revision",
  "requestId": "01J9Z6Q2K7",
  "currentRev": "sha256:9b21…7ce",
  "conflicts": [
    { "field": "status", "current": "in_progress", "proposed": "in_review" },
    { "field": "assignees", "current": "marta", "proposed": "jose" }
  ],
  "errors": []
}
```

Field notes: `code` is a stable machine string (clients switch on it, not on `type`);
`errors[]` carries per-field validation problems; `conflicts[]` appears on `stale_revision`
only and lists the fields the refused write would still have changed, judged against the
content on disk now (`03-data-model.md` R-REV-3a). An empty `conflicts[]` on a stale
revision means the change had already been made by whoever wrote first.

```json
{
  "type": "https://git-in-track.dev/problems/validation-failed",
  "title": "Validation failed",
  "status": 422,
  "detail": "2 fields are invalid.",
  "code": "validation_failed",
  "errors": [
    { "field": "status", "code": "unknown_status",
      "message": "\"in-progress\" is not in the ACME workflow (backlog, todo, in_progress, in_review, done, cancelled)" },
    { "field": "parent", "code": "wrong_parent_type",
      "message": "Task parent must be a story; ACME-EP-0007 is an epic." }
  ]
}
```

Catalog of `code` values: `unauthorized`, `forbidden`, `not_found`, `invalid_request`,
`validation_failed`, `invalid_front_matter`, `precondition_required`, `stale_revision`,
`conflict`, `duplicate_id`, `workflow_transition_denied`, `read_only`,
`repo_not_registered`, `repo_not_cloned`, `wip_limit_exceeded`, `sprint_overlap`,
`sprint_already_active`, `board_in_use`, `project_exists`, `team_exists`,
`team_project_exists`, `team_project_referenced`, `tunnel_requires_token`, `tunnel_failed`,
`git_dirty`,
`git_auth_failed`,
`git_conflict`, `index_unavailable`, `rate_limited`, `not_implemented`, `internal`,
and the CORS proxy's own: `cors_proxy_disabled`, `cors_proxy_forbidden`,
`cors_proxy_bad_target`, `cors_proxy_host_not_allowed`, `cors_proxy_target_blocked`,
`cors_proxy_too_large`, `cors_proxy_upstream_failed` (see the CORS proxy under §5.2),
and the YouTrack connection's own: `youtrack_not_configured`,
`youtrack_unauthorized`, `youtrack_forbidden`, `youtrack_not_found`,
`youtrack_unreachable` (see YouTrack under §5.5).

`read_only` also answers **every write** to a project whose `project.yaml` declares no `schema`
or one newer than the build supports: the project is open read-only (doc 03 R-EVO-2, ADR-037
§11). Reads keep working. The other side of the same rule: `item.create` and `item.update`
results (and those of `item.move`, `requirement.create` and `requirement.update`, §6.7) carry
`"schemaUpgraded": 2` when the write introduced the first spec construct into a
`schema: 1` project and raised `project.yaml` in the same write, which is then in `writes`
(doc 03 §21.10). `gintrack item new --json` reports it the same way.

`wip_limit_exceeded` (HTTP 409) is a *refusal the caller may repeat*: a board's WIP limit is
advisory (doc 04 R-COL-5), so the move is declined once with the column and the limit in `detail`,
and the same request with `force` goes through. It exists so that a limit is never exceeded
silently, and never blocks a team that has decided to exceed it.

`sprint_overlap` and `sprint_already_active` (HTTP 409) have the same shape: two sprints of one
board sharing a day, and a second active sprint on one board, are refused once with the other
sprint named in `detail`. `sprint_already_active` is repeatable with `force`; `sprint_overlap` is
not — the caller has to change the dates.

`project_exists` (HTTP 409) refuses to scaffold a project into a documentation folder that
already holds a `project.yaml` (doc 03 R-NEW-2). A backlog is never overwritten, so the caller
picks another folder or opens the one that is there.

`team_project_exists` (HTTP 409) refuses to declare a project key a team already declares: the
`projects:` list is keyed by project key alone (doc 04 R-PROJ-1), so a second entry would make
every reference into it ambiguous. It is not repeatable with `force` — the caller edits the
entry that is there.

`team_project_referenced` (HTTP 409) has the shape of `wip_limit_exceeded`: disconnecting a
project a board, a sprint or a retro action still points at is declined once, with the
references named in `detail`, and the same request with `force` goes through. It exists so that
`ref:` entries are never orphaned silently (doc 04 §3.9).

`duplicate_id` and `board_in_use` (HTTP 409) guard a board's life cycle: a board file is named
after its id, so `POST /boards` refuses a slug that is already taken rather than replacing
somebody else's board, and `DELETE /boards/{slug}` refuses while a sprint file still names the
board (`sprint_already_active` when that sprint is running). Neither is repeatable with `force`:
the caller picks another name, or moves the sprint first.

`tunnel_requires_token` (HTTP 409) refuses to open a public tunnel over a server started
with `--token none`. It is a refusal that cannot be repeated with `force`: the token is the
only thing guarding a read-write server on a public URL (§5.1.1), so the answer is to
restart with a token, and the problem detail says so.

`not_implemented` (HTTP 501) is what a route of a later phase answers: the path exists so
that a client learns "not yet" from the code instead of guessing from a 404.

### 5.5 Endpoints

#### Health and capabilities

```http
GET /api/v1/health            # unauthenticated liveness probe
200 {"status":"ok","version":"0.4.0","uptimeSeconds":8123}
```

```http
GET /api/v1/capabilities
200
{
  "version": "0.4.0",
  "schema": "v1",
  "ui": "embedded",
  "features": {
    "watcher": true,
    "nativeIndex": true,
    "git": true,
    "gitBackend": "system",
    "gitVersion": "2.45.2",
    "mcpHttp": false,
    "mcpWrite": false,
    "mcpTools": [],
    "search": "pando",
    "renderer": "goldmark",
    "tunnel": true,
    "agent": false,
    "youtrackSupported": true,
    "youtrack": false,
    "write": true
  },
  "limits": { "maxUploadBytes": 5242880, "maxItemsPerPage": 500 },
  "workspaces": ["work", "oss"],
  "activeWorkspace": "work"
}
```

The web app calls `/api/v1/capabilities` on load (with a 300 ms timeout) to decide between
browser-only and companion mode; failure is a normal, silent fallback.

`features.youtrackSupported` says this build can reach a tracker at all — only
the companion can, because browser-only mode has neither the network reach nor
the token — and `features.youtrack` says at least one served project actually
declares an `integrations.youtrack` block. The settings card is shown on the
first flag and filled from the second; browser-only mode reports neither and
hides the whole feature (GIT-US-0048).

`features.agent` is true only when **both** halves are in place: the feature is
switched on (`gintrack serve --agent` or `agent.enabled: true`) **and** an
upstream URL is configured. It is a companion-only capability — browser-only mode
has no server to proxy through and reports `false`. Nothing about the upstream
token is reported here or anywhere else.

`features.search` names the search backend that will answer the **next** query:
`"core"` for the substring index every mode ships with, `"pando"` when the
optional semantic accelerator of GIT-US-0082 is configured **and** answering.
A Pando that is configured but unreachable, unauthorized or over its latency
budget reports `"core"`, so a client never promises an accelerator that is down;
it flips back to `"pando"` by itself on the first query that succeeds again.
The web client maps this value onto `Capabilities.fullTextSearch`, whose union is
`'core' | 'bleve' | 'pando'` — `bleve` is documented, not shipped (docs/02 §8) —
and browser-only mode always reports `"core"`.

#### Workspaces and repositories

```http
GET    /api/v1/workspaces
GET    /api/v1/workspaces/{name}
POST   /api/v1/workspaces               {"name":"oss"}
GET    /api/v1/repos                    ?workspace=work&role=project
POST   /api/v1/repos                    {"path":"~/code/acme-api","role":"project","docs":"docs"}
GET    /api/v1/repos/{key}
DELETE /api/v1/repos/{key}              (unregisters; never deletes files)
POST   /api/v1/repos/{key}/reindex      {"full":true}
POST   /api/v1/repos/{key}/projects     {"docsFolder":"docs","key":"ACME","name":"ACME Platform"}
POST   /api/v1/repos/{key}/team         {"key":"ACME-TEAM","name":"ACME Delivery Team"}
```

**`POST /api/v1/repos` answers `501 not_implemented`, on purpose.** Registering a repository
writes the user's configuration file, and that file belongs to the CLI: the companion read it
at startup, so a server-side write would make the running process disagree with the file on
disk, and it would let a browser tab add arbitrary paths of the machine to the workspace. The
response carries the exact command instead of a bare refusal, built from the `path`, `role`
and `docs` of the request ([ADR-020](./adr/ADR-020-creating-a-team-repository.md)):

```json
POST /api/v1/repos
{ "path": "/home/jose/code/acme-team", "role": "team" }
501
{ "type": "…/problems/not-implemented", "title": "Not implemented", "status": 501,
  "code": "not_implemented",
  "detail": "Registering a repository is a change to your gintrack configuration, which only the CLI writes. Run this command, then reload: gintrack add /home/jose/code/acme-team --team" }
```

`POST /api/v1/repos/{key}/projects` scaffolds a backlog in a registered repository that has
none: it writes what doc 03 §2.2 prescribes and answers `201` with the project as
`GET /api/v1/projects` reports it, plus the files it wrote. It goes through the same
`core.CreateProject` the CLI and the browser use, so all three produce identical files, and it
declares the folder for the life of the process, which keeps a nested one discoverable
([ADR-018](./adr/ADR-018-bounded-project-discovery.md)).

```json
POST /api/v1/repos/greenfield/projects
{ "docsFolder": "docs", "key": "ACME", "name": "ACME Platform" }
201
{
  "project": { "key": "ACME", "name": "ACME Platform", "docsPath": "docs",
               "backlogPath": "docs/.pmngr", "writable": true, "statuses": [ … ] },
  "writes": { "written": [{ "path": "docs/.pmngr/project.yaml", "text": "schema: 1\n…" }],
              "removed": [] }
}
```

Failures use the codes of §5.4: `422 validation_failed` for a key outside
`[A-Z][A-Z0-9]{1,9}`, `409 project_exists` for a folder that already holds a `project.yaml`,
`404 repo_not_registered` for an unknown repository. The registration in the configuration
file is not rewritten — that is a CLI concern (`gintrack add`, `gintrack init`).

`POST /api/v1/repos/{key}/team` is its team counterpart (GIT-US-0034). It turns a registered
repository into a team repository: `team.yaml` at the root plus the `.pmngr/` artifact folders
and the `knowledge/` base of [doc 04 §2](./04-team-repository.md), through the same
`core.CreateTeam` the CLI runs. It answers `201` with the team as `GET /api/v1/teams/{key}`
reports it, plus the files it wrote.

```json
POST /api/v1/repos/acme-team/team
{ "key": "ACME-TEAM", "name": "ACME Delivery Team" }
201
{
  "team": { "key": "ACME-TEAM", "name": "ACME Delivery Team", "root": ".",
            "knowledgePath": "knowledge", "members": [], "projects": [], "diagnostics": [ … ] },
  "writes": { "written": [{ "path": "team.yaml", "text": "schema: 1\n…" }], "removed": [] }
}
```

Its body accepts `root`, `key`, `name`, `description`, `timezone`, `knowledgePath` and
`members`. Failures: `422 validation_failed` for a key outside `[A-Z][A-Z0-9-]{1,15}` or a
malformed member handle, `409 team_exists` for a folder that already holds a `team.yaml`,
`404 repo_not_registered` for an unknown repository.

```json
GET /api/v1/repos/ACME
200
{
  "key": "ACME",
  "role": "project",
  "name": "ACME API",
  "path": "/home/jose/code/acme-api",
  "docs": "docs",
  "pmngr": "docs/.pmngr",
  "git": { "branch": "main", "clean": true, "ahead": 0, "behind": 2,
           "remote": "git@github.com:acme/acme-api.git", "lastCommit": "9f2c1ab" },
  "counts": { "epics": 12, "stories": 58, "tasks": 138, "milestones": 6, "comments": 402 },
  "workflow": ["backlog","todo","in_progress","in_review","done","cancelled"],
  "lastIndexed": "2026-09-03T09:14:02Z"
}
```

#### Teams and cross-repository references

```http
GET /api/v1/workspace                   # every open repository, its projects, the teams among them
GET /api/v1/teams                       # every mounted team repository, in mount order
GET /api/v1/teams/{key}                 # team.yaml: members, projects, policies, diagnostics
POST /api/v1/teams/{key}/projects       # declare a project repository in team.yaml
DELETE /api/v1/teams/{key}/projects/{project}   # ?force=true to break references
GET /api/v1/refs?ref=ACME/ACME-US-0042  # where a cross-repository reference points
```

`GET /api/v1/teams` answers `{ "teams": [...], "total": n }` — a list even when it holds one entry,
so a client can tell "no team repository is registered" from an error. `{key}` in
`GET /api/v1/teams/{key}` is the `key:` of a `team.yaml` or the id of the repository holding it,
and it is resolved against every mounted team.

**Naming the team.** A workspace may hold several team repositories (doc 04 §3.8), so every
team-scoped route — `/boards`, `/sprints`, `/retros`, `/snapshots` and `/teams/{key}/kb` — takes
the team it acts on:

```http
GET   /api/v1/boards?team=ACME-TEAM
GET   /api/v1/sprints?team=ACME-TEAM&board=delivery
POST  /api/v1/retros?team=ACME-TEAM      # or "team": "ACME-TEAM" in the body
```

Omitting `team` selects the only registered team, which is why a single-team workspace and every
CLI verb keep working unchanged. Omitting it while two or more are registered is `400
invalid_request` naming the field to set, never an answer from an arbitrary team; an unknown team
is `404 not_found`. No active team is stored server-side — the client sends its choice on every
call ([ADR-019](./adr/ADR-019-active-team-is-client-state-threaded-per-call.md)).

`GET /api/v1/teams/{key}` answers with the parsed `team.yaml` (doc 04 §3) plus, for every declared
project, whether a clone of it is open in this workspace:

```json
{
  "key": "ACME-TEAM",
  "name": "ACME Delivery Team",
  "knowledgePath": "knowledge",
  "vaultId": "acme-team",
  "members": [{ "handle": "jose", "name": "Jose Ruiz", "role": "lead", "active": true }],
  "projects": [
    { "key": "ACME", "name": "ACME Platform", "repo": "https://github.com/acme/platform.git",
      "docsPath": "docs", "cloned": true,  "vaultId": "acme-api", "localDocsPath": "docs" },
    { "key": "WEB",  "name": "Marketing Website", "repo": "https://gitlab.com/acme/website.git",
      "docsPath": "documentation", "cloned": false }
  ],
  "diagnostics": []
}
```

**The project list (GIT-US-0037).** `POST /api/v1/teams/{key}/projects` declares a project
repository in that team's `team.yaml`, and `DELETE /api/v1/teams/{key}/projects/{project}`
disconnects it. Both go through the vault methods `team.project.add` and `team.project.remove`,
so the file a companion writes and the file a browser writes are the same bytes, and both answer
with the team as `GET /api/v1/teams/{key}` reports it, the entry that changed, the references a
removal broke and the write set. The team travels in the path here rather than as `?team=`,
because the project list belongs to one team repository and to nothing else.

```json
POST /api/v1/teams/ACME-TEAM/projects
{ "key": "TOOLS", "name": "Internal Tools", "repo": "https://github.com/acme/tools.git",
  "defaultBranch": "main", "docsPath": "docs" }
201
{
  "team": { "key": "ACME-TEAM", "projects": [ … ] },
  "project": { "key": "TOOLS", "name": "Internal Tools", "docsPath": "docs" },
  "writes": [{ "vaultId": "acme-team", "written": [{ "path": "team.yaml", "text": "schema: 1\n…" }],
               "removed": [] }]
}
```

The body is one entry of doc 04 §3.3: `key`, `name`, `repo`, `defaultBranch`, `docsPath`, `host`,
`webUrl`, `color` and `archived`. Failures: `400 invalid_request` for a body with no `key`,
`422 validation_failed` for a key outside `[A-Z][A-Z0-9]{1,9}` or a missing `repo`/`docsPath`,
`409 team_project_exists` for a key the team already declares (R-PROJ-1), `404 not_found` for an
unknown team.

A removal that would leave a board, a sprint or a retro action pointing at an undeclared project
is `409 team_project_referenced`, whose `detail` names what breaks; `?force=true` repeats it and
accepts that, and the answer then lists the references under `references`. `local_hints` is not
written by either route — hints are per-machine and belong in the user's own configuration
(R-PROJ-3).

`GET /api/v1/refs` resolves `<projectKey>/<itemId>` across every mounted repository. A reference
into a project nobody cloned is **not** a 404 — it is the normal state of a team board (doc 04 §7):

```json
{ "ref": "WEB/WEB-US-0031", "project": "WEB", "item": "WEB-US-0031",
  "declared": true, "cloned": false,
  "reason": "project WEB is not cloned on this machine" }
```

A malformed reference (no `/`, a lowercase key, an id whose prefix disagrees with the key) is a
`400` with the `invalid_request` problem code.

#### Projects

```http
GET /api/v1/projects                    # merged view across the workspace
GET /api/v1/projects/{key}              # project.yaml: key, name, workflow, labels,
                                        # members, templates, id counters
PATCH /api/v1/projects/{key}            # If-Match required; writes project.yaml
POST /api/v1/projects/{key}/inbox       # If-Match optional (configRev); adds the triage status
```

Every project answer carries `writable` and `configRev`, the revision of its `project.yaml`.
`POST /projects/{key}/inbox` (story GIT-US-0100, ADR-033) gives a project created before the inbox
existed one: it inserts `{id: triage, name: Triage, category: triage}` as the **first** workflow
status and changes nothing else — `initial` and `transitions` are untouched, and the rest of the
file keeps its bytes (the new entry is written as a line in the style of the list's first entry).
It is the core method `project.inbox.enable` (`{project, rev?}`), so browser-only mode writes the
same file. `If-Match` carries `configRev` and is honored when sent (`412 stale_revision`); without
it the write is unconditional, which is safe because it only ever adds one status. A project that
already declares a triage status is `409 inbox_already_enabled`; one whose workflow already has an
ordinary status called `triage` is `409 triage_status_id_taken` and the file is left alone. The
answer is `{project, writes}` with the new `configRev` as `ETag`, and the write is announced with
`file.changed` plus a full `index.updated` and staged by commit-on-save like any other.

#### Knowledge base

```http
GET /api/v1/projects/{key}/kb/tree      ?depth=3
GET /api/v1/projects/{key}/kb/page?path=architecture/overview.md&format=raw|html|both
PUT /api/v1/projects/{key}/kb/page      If-Match; body {"path":…,"content":"…"}
POST /api/v1/projects/{key}/kb/feedback If-Match optional; body {"path":…,"notes":[…]}
GET /api/v1/teams/{key}/kb/tree         # team knowledge/ folder, same shape, {key} is the team key
GET /api/v1/kb/tree                     ?project=ACME    # flat form, vault-relative
GET /api/v1/kb/page?path=docs/index.md  ?project=ACME
PUT /api/v1/kb/page                     If-Match; body {"path":…,"content":"…"}
```

The flat `/api/v1/kb/…` form addresses a page by its vault-relative path and needs
`?project=` only when the repository holds more than one project. Writing a page that does
not exist yet needs no `If-Match`; overwriting one does.

`POST …/kb/feedback` (every scope form: `/projects/{key}/kb`, `/teams/{key}/kb`, `/kb`) attaches
feedback notes to lines of a page and appends them to the page's feedback block (docs/03 §14.4,
ADR-030). Each note is `{"startLine":3,"endLine":4,"quote":"the migration","note":"Which one?"}`,
lines being 1-based lines of the page `body`. `author`, `authorName` and `authorEmail` are optional:
when none is sent the notes are attributed to the git identity of the repository. `If-Match` is
honored when sent (`412` on a stale revision); without it the notes are written against the page as
it is now. A note with no text or with lines outside the page content is `400 invalid_request`. The
answer is the page, with its new `ETag`. Every page write — this one and `PUT …/kb/page` — drops
the notes whose text has changed since they were written.

```json
GET /api/v1/projects/ACME/kb/page?path=architecture/overview.md&format=both
200
{
  "path": "architecture/overview.md",
  "title": "Architecture overview",
  "frontmatter": { "tags": ["architecture"], "updated": "2026-08-30" },
  "raw": "# Architecture overview\n\nSee [[Auth Overview]].\n\n```mermaid\ngraph TD; A-->B;\n```\n",
  "html": "<h1 id=\"architecture-overview\">Architecture overview</h1>…",
  "links": { "wiki": [{"target":"Auth Overview","resolved":"auth/overview.md"}],
             "external": ["https://openid.net/specs/"] },
  "backlinks": ["adr/0003-oidc.md"],
  "mermaid": 1,
  "rev": "sha256:2a90…f31",
  "updated": "2026-08-30T16:02:11Z"
}
```

Rendering happens server-side with goldmark (GFM tables, task lists, footnotes,
admonitions, wikilinks) and matches the client-side unified/remark pipeline; Mermaid is
left as fenced code and rendered in the browser. `format=raw` is the default because the
web app usually renders locally and agents want the source.

#### Items

```http
GET    /api/v1/items
POST   /api/v1/items
GET    /api/v1/items/{id}
PATCH  /api/v1/items/{id}               If-Match: <rev>
PUT    /api/v1/items/{id}               If-Match: <rev>   (full replace incl. body)
DELETE /api/v1/items/{id}               If-Match: <rev>   ?hard=true removes the file
GET    /api/v1/items/{id}/references                      what still points at the item
POST   /api/v1/items/{id}/tasks         If-Match: <rev>   {"line":34,"checked":true}
POST   /api/v1/items/{id}/move          If-Match: <rev>   {"status":"in_review"}
POST   /api/v1/items/{id}/triage        If-Match: <rev>   {"action":"accept|reject|snooze|duplicate", …}
GET    /api/v1/inbox                                      the triage queue of a project
GET    /api/v1/items/{id}/comments
POST   /api/v1/items/{id}/comments   If-Match: <item rev> optional, honored when sent
POST   /api/v1/items/{id}/comments/tasks  If-Match: <comment rev>  {"path":"…/comments/…md","line":3,"checked":true}
GET    /api/v1/items/{id}/links
POST   /api/v1/items/{id}/links         {"relation":"blocks","target":"ACME-T-0500"}
DELETE /api/v1/items/{id}/links/{relation}/{target}
```

`GET /api/v1/items` query parameters: `project`, `type`, `status`, `assignee`, `label`,
`parent`, `milestone`, `priority`, `q` (full text), `updatedSince`, `hasBody`,
`sort`, `limit`, `offset`, `cursor`, `fields`.

```json
GET /api/v1/items?project=ACME&type=story&status=todo&status=in_progress&sort=-priority,updated&limit=2
200
X-Total-Count: 37
{
  "items": [
    {
      "id": "ACME-US-0042", "type": "story", "project": "ACME",
      "title": "Login with SSO", "status": "in_progress", "priority": "high",
      "assignees": ["jose"], "labels": ["auth","q3"],
      "parent": "ACME-EP-0007", "milestone": "ACME-M-0002", "estimate": 5,
      "due": "2026-09-19",
      "created": "2026-08-11T08:00:00Z", "updated": "2026-09-03T07:41:11Z",
      "author": "jose",
      "links": [{"relation":"blocked_by","target":"ACME-T-0300"}],
      "commentCount": 4,
      "path": "docs/.pmngr/stories/ACME-US-0042-login-with-sso.md",
      "rev": "sha256:6f1c…a09"
    }
  ],
  "total": 37, "limit": 2, "offset": 0, "nextCursor": "eyJvIjoyfQ"
}
```

```json
POST /api/v1/items
Content-Type: application/json
Idempotency-Key: 4f1e-…

{
  "project": "ACME", "type": "task", "title": "Wire OIDC discovery endpoint",
  "parent": "ACME-US-0042", "assignees": ["marta"], "labels": ["auth"],
  "priority": "high", "effort": 6,
  "body": "## Description\nFetch /.well-known/openid-configuration and cache for 1h.\n\n## Acceptance Criteria\n- [ ] Discovery cached\n- [ ] Failure falls back to static config\n"
}

201 Created
Location: /api/v1/items/ACME-T-0311
ETag: "sha256:11c3…5de"
{ "id": "ACME-T-0311", "path": "docs/.pmngr/tasks/ACME-T-0311-wire-oidc-discovery-endpoint.md",
  "rev": "sha256:11c3…5de", "created": "2026-09-03T10:02:00Z" }
```

```json
PATCH /api/v1/items/ACME-T-0311
If-Match: sha256:11c3…5de
{"status":"in_review","addLabels":["needs-review"],"assignees":["marta","jose"]}

200 OK
ETag: "sha256:7ab0…d12"
{ "id":"ACME-T-0311", "status":"in_review", "rev":"sha256:7ab0…d12",
  "updated":"2026-09-03T10:31:44Z",
  "changed":["status","labels","assignees","updated"] }
```

PATCH semantics: only supplied keys change. `addLabels`/`removeLabels` and
`addAssignees`/`removeAssignees` provide set operations so concurrent clients do not clobber
each other's lists. `body` replaces the whole body; `bodyAppend` appends; `bodySections`
lets a client replace a single `##` section by name (used by agents to update
`## Acceptance Criteria` without touching `## Description`).

A comment body may name its writer with `author` (a handle) and/or `authorName`/`authorEmail`.
When it names nobody — or sends the placeholder handle `me` older web builds used — the comment is
attributed to the git identity of the repository the item lives in: `user.name` and `user.email`,
or the `git.authorName`/`authorEmail` overrides. The handle is then derived from the name
(`Marta Alonso` → `marta-alonso`), and `author_name`/`author_email` are recorded in the comment's
front matter (docs/03 §11.2). `gintrack item comment` does the same when neither `--author` nor
`git.authorName` is set.

```json
POST /api/v1/items/ACME-T-0311/comments
{"body":"Blocked on the identity provider sandbox.","author":"marta"}

201 Created
{ "id":"ACME-T-0311#20260903T104012Z-marta",
  "path":"docs/.pmngr/comments/ACME-T-0311/20260903T104012Z-marta.md",
  "created":"2026-09-03T10:40:12Z", "rev":"sha256:c41a…9f0" }
```

There is no REST route that edits a comment, and there is deliberately none: a
thread is a conversation, not a record the tool may quietly rewrite. The core
method **`comment.update`** exists for the one caller that must — the YouTrack
comment push, writing back the remote comment id it has just learned. It takes
`{id, path, rev, body?, external?, setExternal?}` under the same optimistic lock
as every other write (`rev`, with `"*"` as the deliberate wildcard), and
`setExternal` upserts one entry per system, so a re-delivered push replaces its
reference instead of appending a second one. It is the reason `internal/server`
writes no repository file of its own.

**Deleting.** `DELETE` soft-deletes by default (docs/03 §7.1): the file keeps its id, its
path and its history and gains `deleted: true`, so the id is never reused, a merge cannot
resurrect a stale copy, and everything that referenced it still resolves — to an item marked
deleted. `?hard=true` removes the file instead. Both answer `204 No Content`.

```json
GET /api/v1/items/ACME-US-0042/references
200
{
  "id": "ACME-US-0042",
  "references": [
    {"kind":"board","id":"delivery","path":".pmngr/boards/delivery.md","title":"Delivery",
     "field":"order.in_progress","ref":"ACME/ACME-US-0042"},
    {"kind":"item","id":"ACME-T-0311","path":"docs/.pmngr/tasks/ACME-T-0311-wire-oidc.md",
     "title":"Wire OIDC discovery endpoint","type":"task","field":"parent","ref":"ACME-US-0042"},
    {"kind":"sprint","id":"ACME-TEAM-S-0004","path":".pmngr/sprints/ACME-TEAM-S-0004.md",
     "title":"Sprint 4","field":"items","ref":"ACME/ACME-US-0042"}
  ],
  "children": [
    {"kind":"item","id":"ACME-T-0311","path":"docs/.pmngr/tasks/ACME-T-0311-wire-oidc.md",
     "title":"Wire OIDC discovery endpoint","type":"task","field":"parent","ref":"ACME-US-0042"}
  ]
}
```

The search covers `parent`, `milestone` and every `links[]` entry of every item of the
project, plus the column orders and `filters.milestone` of every board, the `items` and
`committed` of every sprint, and the `task` of every promoted retro action, across every
open team repository. It is a read: it never refuses a delete, it tells a client what a
delete would touch (`core.ItemReferences`, the item-level sibling of the project-level
`core.TeamProjectReferences` behind `team.project.remove`).

```json
POST /api/v1/items/ACME-US-0042/tasks
If-Match: sha256:6f1c…a09
{"line": 34, "checked": true}

200 OK
ETag: "sha256:9d22…7b1"
{ "id":"ACME-US-0042", "rev":"sha256:9d22…7b1", "body":"…\n- [x] Discovery cached\n…" }
```

`line` is the 1-based line of the task-list marker **inside the body**, which is what the
web app's renderer stamps on every rendered checkbox. The core rewrites the single character
between the brackets on that line and leaves every other byte of the document alone; a line
that is not a task-list item — prose, or a `- [ ]` inside a fenced code block — is refused
with `task_list_mismatch` (422), which means the body has changed since it was rendered.

#### Boards, sprints, retrospectives

Boards are served since GIT-US-0017, sprints since GIT-US-0018, retrospectives since GIT-US-0027
and sprint metrics since GIT-US-0028. Nothing on this surface is deferred any more.

```http
GET  /api/v1/snapshots                      committed index snapshots, with their age
POST /api/v1/snapshots                      refresh them; body {projects?, generatedBy?,
                                            includeClosed?, dryRun?}
GET  /api/v1/boards                         list the boards of the team repository
POST /api/v1/boards                         create a board; no If-Match, a taken slug is 409
GET  /api/v1/boards/{slug}                  always resolved against the open repositories
POST /api/v1/boards/{slug}/cards/move       If-Match: <board rev>; body carries itemRev
PATCH /api/v1/boards/{slug}                 If-Match (title, projects, columns, wip, filters, sprint)
DELETE /api/v1/boards/{slug}                If-Match; refused while a sprint names the board
GET  /api/v1/sprints                        ?board=platform-scrum&state=active
GET  /api/v1/sprints/{id}                   scope, candidates and metrics; ETag: <sprint rev>
POST /api/v1/sprints                        create a sprint; the core allocates the id
PATCH /api/v1/sprints/{id}                  If-Match (goal, dates, addItems, removeItems)
POST /api/v1/sprints/{id}/start             If-Match; {force?} to run two at once
POST /api/v1/sprints/{id}/close             If-Match; {carry:[…], transfer:{mode,target?}, dryRun?}
POST /api/v1/sprints/{id}/transfer          If-Match; {mode,target?,carry:[…],dryRun?}
GET  /api/v1/sprints/{id}/burndown          burndown, cumulative flow, flow times, provenance
GET  /api/v1/retros                         ?sprint=&board=&state=; carries the open actions
GET  /api/v1/retros/{id}                    notes, themes by votes, actions; ETag: <retro rev>
POST /api/v1/retros                         create a retro; the core allocates the id
PATCH /api/v1/retros/{id}                   If-Match (notes, themes, votes, actions)
POST /api/v1/retros/{id}/actions/promote    If-Match; {"action":"a1","project":"ACME"}
```

**Sprint metrics.** `GET /api/v1/sprints/{id}/burndown` answers with the burndown, the cumulative
flow diagram, the flow statistics and the **provenance** of the history all three were reconstructed
from ([doc 04 §12](./04-team-repository.md), [ADR-017](./adr/ADR-017-metrics-history-from-git-not-a-stored-time-series.md)).
The three come back together because they are one reconstruction of one window; splitting them
across routes would walk the git history twice.

```json
GET /api/v1/sprints/ACME-TEAM-S-0007/burndown

200
{ "sprint": { "id":"ACME-TEAM-S-0007", "…": "the same header GET /api/v1/sprints/{id} returns" },
  "burndown": {
    "sprint":"ACME-TEAM-S-0007", "start":"2026-08-24", "end":"2026-09-06",
    "committedPoints": 34,
    "points":[
      {"date":"2026-08-24","day":1,"ideal":34,"observed":true,
       "remaining":34,"scope":34,"done":0,"items":4,"completed":0,"unknown":0},
      {"date":"2026-09-06","day":14,"ideal":0,"observed":false,
       "remaining":0,"scope":0,"done":0,"items":0,"completed":0,"unknown":0}]},
  "flow": {
    "bands":["done","cancelled","in_progress","todo","unknown"],
    "days":[{"date":"2026-08-24","day":1,"observed":true,
             "counts":{"done":0,"cancelled":0,"in_progress":1,"todo":3,"unknown":0},"total":4}]},
  "stats": {
    "throughput": 3, "throughputPerWeek": 1.5,
    "cycleTime": {"count":3,"mean":2.4,"median":2.1,"p85":3.8,"min":1.2,"max":3.8},
    "leadTime":  {"count":3,"mean":9.1,"median":8.0,"p85":12.4,"min":6.0,"max":12.4},
    "excluded": 0},
  "provenance": {
    "source":"git", "approximate": false, "from":"2026-07-30",
    "commits": 41, "items": 4, "covered": 3,
    "note":"Reconstructed from the git history of the item files, back to 2026-07-30."},
  "items": [ "…the scope, so every chart has its data table without a second call…" ] }
```

`observed: false` marks a day that has not happened yet: it carries the ideal value and **no
measurement**, and a client must not plot it. `unknown` counts the references whose state that day
the history cannot state — a card resolved from a committed snapshot, or a day before the oldest
commit read. `provenance.source` is `git` where the host can read a repository and `updated` where
it cannot (browser-only mode), and `approximate` is the single flag a UI should branch on.

```json
POST /api/v1/snapshots
{"generatedBy":"jose"}

200
{ "snapshots":[
    {"project":"ACME","path":".pmngr/index/ACME.json","status":"written","items":151,
     "info":{"project":"ACME","path":".pmngr/index/ACME.json","present":true,
             "enabled":true,"generated":"2026-09-04T10:00:00Z","generatedBy":"jose",
             "items":151,"freshness":"fresh","stale":false}},
    {"project":"AWEB","path":".pmngr/index/AWEB.json","status":"skipped","items":0,
     "reason":"no open repository serves this project; clone it to refresh its snapshot",
     "info":{"project":"AWEB","path":".pmngr/index/AWEB.json","present":true,
             "enabled":true,"generated":"2026-09-01T18:00:00Z","items":88,
             "freshness":"ageing","stale":false}}
  ],
  "writes":[{"vaultId":"acme-team",
             "written":[{"path":".pmngr/index/ACME.json","text":"…"}],"removed":[]}] }
```

A `status` of `written` means the file changed; `unchanged` means the regenerated document
matched the one on disk and nothing was written (ADR-014); `skipped` means no open
repository serves that project. `dryRun: true` computes everything and writes nothing.

```json
GET /api/v1/boards/platform-kanban?resolve=true
200
{
  "slug":"platform-kanban","kind":"kanban","title":"Platform Kanban",
  "team":"acme-team","rev":"sha256:88fa…101",
  "filters":{"projects":["ACME","AWEB"],"labels":[]},
  "columns":[
    {"name":"In progress","statuses":["in_progress"],"wip":5,
     "cards":[
       {"ref":"ACME/ACME-T-0311","remote":false,"title":"Wire OIDC discovery endpoint",
        "type":"task","status":"in_progress","assignees":["marta"],"priority":"high",
        "rev":"sha256:7ab0…d12"},
       {"ref":"AWEB/AWEB-T-0090","remote":true,"title":"Login screen states",
        "type":"task","status":"in_progress","source":"snapshot",
        "snapshotAt":"2026-09-01T18:00:00Z",
        "remoteUrl":"https://github.com/acme/acme-web/blob/main/documentation/.pmngr/tasks/AWEB-T-0090-login-screen-states.md"}
     ]}
  ]
}
```

A board is created with one call and no precondition — there is nothing yet to conflict with —
and the core allocates the slug from the title, refuses to overwrite, and fills in the default
columns when the caller proposes none (doc 04 §5.2, R-COL-2):

```json
POST /api/v1/boards
{"title":"Platform Kanban","kind":"kanban","projects":["ACME","AWEB"],
 "filters":{"types":["story","task"]}}

201
{ "board": { "id":"platform-kanban","kind":"kanban","title":"Platform Kanban",
             "columns":[{"id":"todo","name":"To Do"},{"id":"in_progress","name":"In Progress"},
                        {"id":"done","name":"Done"}], … },
  "writes":[{"vaultId":"acme-team",
             "written":[{"path":".pmngr/boards/platform-kanban.md","text":"…"}],"removed":[]}] }
```

`DELETE /api/v1/boards/{slug}` takes the board's revision in `If-Match` and removes the file —
a view and nothing else, since every item its cards referenced lives in its own repository. It is
refused while a sprint file still names the board.

```json
POST /api/v1/boards/platform-kanban/cards/move
If-Match: sha256:88fa…101
{"ref":"ACME/ACME-T-0311","toColumn":"in_review","position":0,
 "itemRev":"sha256:7ab0…d12"}

200
{ "board": { …the whole board, re-rendered… },
  "item":  {"id":"ACME-T-0311","status":"in_review","rev":"sha256:5e88…4b1"},
  "move":  {"ref":"ACME/ACME-T-0311","fromColumn":"in_progress","toColumn":"in_review",
            "status":"in_review","statusChanged":true,"choices":["in_review"],
            "wip":{"column":"in_review","used":3,"limit":4,"exceeded":false}},
  "writes":[{"vaultId":"acme-platform","written":[{"path":"docs/.pmngr/tasks/…md","text":"…"}],
             "removed":[]},
            {"vaultId":"acme-team","written":[{"path":".pmngr/boards/platform-kanban.md","text":"…"}],
             "removed":[]}] }
```

Notes on the move:

- `toColumn` is a **column id**, not its display name.
- `If-Match` carries the revision of the *board*; `itemRev` carries the revision of the *item*.
  They live in different repositories and therefore hold two independent optimistic locks; either
  may be omitted to skip that check, and `If-Match: *` skips the board's.
- `position` is the 0-based index in the target column; `-1` appends. A move into the column the
  card already sits in is a re-order: `statusChanged` is `false`, no item file is written, and
  `writes` carries the team repository only.
- `status` may be sent to pick one of `choices` when a column maps several statuses for the
  project; otherwise the first mapped status wins (doc 04 R-MOVE-2).
- `force: true` (or `?force=true`) confirms a move over a WIP limit, and a transition the project
  workflow does not declare.
- A card whose project nobody cloned answers `repo_not_cloned` (404): the board's `order:` still
  holds it, but nothing here can write its status.
- `writes[]` is what a host without a file system of its own must persist — the browser build
  writes each set into the folder its `vaultId` names.
- On a scrum board, a move out of the `backlog_column` also commits the card to the sprint: the
  answer carries `move.sprint` and `move.sprintAdd`, and `writes[]` holds the sprint file as well
  (doc 04 R-SCRUM-4).

```json
GET /api/v1/sprints/ACME-TEAM-S-0007
200
ETag: "sha256:c1d2…"
{ "sprint": {"id":"ACME-TEAM-S-0007","title":"Sprint 7 — SSO end to end","board":"platform-scrum",
             "state":"active","start":"2026-08-24","end":"2026-09-06","goal":"…",
             "items":["ACME/ACME-US-0042","WEB/WEB-US-0031"],
             "committed":["ACME/ACME-US-0042","WEB/WEB-US-0031"],
             "totalDays":14,"remainingDays":5,
             "metrics":{"items":2,"resolved":2,"done":0,"points":13,"committedPoints":13,
                        "donePoints":0,"added":0,"unresolved":0},
             "rev":"sha256:c1d2…"},
  "cards":[ …the scope, live or snapshot-resolved, in the order of the file… ],
  "backlog":[ …what the board shows that the sprint does not list… ],
  "diagnostics":[] }

PATCH /api/v1/sprints/ACME-TEAM-S-0007
If-Match: "sha256:c1d2…"
{"goal":"Ship SSO to staging","addItems":["ACME/ACME-T-0108"],"removeItems":["WEB/WEB-US-0031"]}

POST /api/v1/sprints/ACME-TEAM-S-0007/close
If-Match: "sha256:c1d2…"
{"carry":[{"ref":"ACME/ACME-T-0108","action":"next","sprint":"ACME-TEAM-S-0008"},
          {"ref":"ACME/ACME-US-0042","action":"backlog"}]}

200
{ "sprint": { …the sprint, now closed… },
  "report": {"sprint":"ACME-TEAM-S-0007","board":"platform-scrum",
             "completed":[…],"incomplete":[…],"unresolved":[],
             "completedPoints":8,"incompletePoints":5,
             "carried":[{"ref":"ACME/ACME-T-0108","action":"next","sprint":"ACME-TEAM-S-0008"},
                        {"ref":"ACME/ACME-US-0042","action":"backlog","status":"backlog"}]},
  "writes":[ …one set per repository written… ] }
```

Notes on sprints:

- Every write carries `If-Match` with the *sprint's* revision (`*` to overwrite unconditionally).
  A sprint edit writes the sprint file only: membership, the goal and the dates are team-repository
  state, so they stay editable for a project nobody cloned (doc 04 R-SPR-2).
- `addItems` and `removeItems` edit the scope without resending it, which keeps a planning drag a
  one-line diff. `items` replaces the whole list when a client really means to.
- `POST /sprints` allocates the id from the team key and the sprints already on disk; the body
  never carries one. Dates overlapping another sprint of the same board are refused with
  `sprint_overlap` (409) naming the other sprint.
- `POST /sprints/{id}/start` copies `items` into `committed` and points the board at the sprint,
  answering with the re-rendered board. A board already running a sprint answers
  `sprint_already_active` (409); `{"force":true}` runs two at once.
- `POST /sprints/{id}/close` writes nothing but the sprint file unless `carry` says so: `leave`
  changes nothing, `next` appends the reference to another sprint of the same board (the earliest
  planned one when `sprint` is absent), and `backlog` writes the first `todo` status of that
  project's workflow into the item's own repository. A decision that could not be applied comes
  back with `error` on its `carried` entry, and the closing still goes through.
- `POST /sprints/{id}/close` also accepts a **bulk** decision and a preview
  (GIT-US-0085): `{"transfer":{"mode":"next|backlog|none","target":"TEAM-S-0009"}}`
  expands into a carry decision for every unfinished reference the report grades,
  and an explicit entry in `carry` always wins over it for the reference it names.
  `mode` absent, or `none`, is exactly the behaviour above, so an existing caller
  is unaffected. `{"dryRun":true}` computes the whole report and **writes
  nothing**: the answer carries `"dryRun": true`, an empty `writes`, and no event
  is published at all — no `sprint.changed`, no `item.changed`, no commit-on-save.
- `POST /sprints/{id}/transfer` moves the unfinished references of one sprint
  without closing anything: `{"mode":"next|backlog|none","target":"…","carry":[…],"dryRun":false}`,
  `mode` defaulting to `next`. It never edits the source sprint — `items` and
  `committed` come back untouched — which is what distinguishes it from a close.
  It requires `If-Match` on the sprint, and a stale revision is `412` carrying
  the current one.

```json
POST /api/v1/sprints/TEAM-S-0008/transfer   If-Match: sha256:a1b2…
{"mode":"next","target":"TEAM-S-0009","carry":[{"ref":"ACME/ACME-US-0042","action":"backlog"}]}
200
{ "sprint":{ "sprint":{"id":"TEAM-S-0008","state":"active","items":["…"]} },
  "report":{"incomplete":[{"ref":"ACME/ACME-US-0042"}],
            "carried":[{"ref":"ACME/ACME-US-0042","action":"backlog","status":"todo"},
                       {"ref":"AWEB/AWEB-T-0110","action":"next","sprint":"TEAM-S-0009"},
                       {"ref":"OPS/OPS-T-0004","action":"backlog","error":"repo_not_cloned"}]},
  "writes":[{"vaultId":"TEAM","written":[…]},{"vaultId":"ACME","written":[…]}],
  "dryRun":false }
```

  A **per-item** failure is not an error: it comes back on its own
  `report.carried[].error` line with a `200`, and `repo_not_cloned` — a project
  this machine has not cloned — is the common one (doc 04 R-SPR-8). `writes` is
  merged to one entry per repository, so a bulk transfer of ten items into one
  sprint arrives as one entry for the team repository plus one per project clone.
  A transfer aimed at a sprint whose derived status is `completed` is refused
  outright with `sprint_target_completed` (409): moving work into a sprint that
  is over would make its numbers lie.

#### The public tunnel (GIT-US-0043, ADR-027)

Three routes, inside the versioned prefix and behind the bearer token like the
rest of the API. They read and set one process-wide switch: the companion holds
at most one tunnel at a time.

```http
GET    /api/v1/tunnel        # the current status; never opens anything
POST   /api/v1/tunnel        # open a tunnel;  202 Accepted
DELETE /api/v1/tunnel        # close it;       200 with the status, now `off`
```

All three answer the same document:

```json
{"supported":true,"provider":"cloudflare","state":"off","url":"","connections":0,"since":null,"error":"","tokenConfigured":true}
```

| Field | Meaning |
|---|---|
| `supported` | False when this build or runtime cannot tunnel at all. A client hides the affordance rather than offering a switch that would do nothing. |
| `provider` | The tunnelling service, `cloudflare` today. The field exists so a second provider does not change the shape. |
| `state` | One of `off`, `starting`, `connected`, `reconnecting`, `error`. |
| `url` | `https://<random-words>.trycloudflare.com`, empty when there is no tunnel. **Never cache or persist it**: a new hostname is minted on every enable. |
| `connections` | Established edge connections. `0` unless the state is `connected`. |
| `since` | RFC 3339 UTC instant the current state was entered; `null` when the tunnel is off. |
| `error` | Why the tunnel failed, when `state` is `error`. Empty otherwise, and cleared by a successful start. |
| `tokenConfigured` | False when the server was started with `--token none`, in which case `POST` is refused. |

**`POST` answers `202 Accepted`, not `200`**, and it means what the code says: the
tunnel is provisioned but not yet reachable. The body carries `state: "starting"`
with `url` **already populated** — the broker hands over the hostname before any
edge connection exists and before DNS has propagated — so a client polls `GET`
until the state settles rather than sharing the URL straight away. Posting to an
already-running tunnel is not an error; it returns the current status.

```http
POST /api/v1/tunnel
202
{"supported":true,"provider":"cloudflare","state":"starting",
 "url":"https://gentle-pine-mist-42.trycloudflare.com","connections":0,
 "since":"2026-09-06T09:12:44Z","error":"","tokenConfigured":true}
```

`DELETE` tears the tunnel down and waits for it, then answers `200` with the
status back at `off`, `url` empty. Deleting a tunnel that is not running is a
no-op with the same answer. Closing it invalidates every link that was shared,
because the next enable gets a different hostname.

**`POST` over a server with no token is refused with `409`:**

```json
{
  "type": "https://git-in-track.dev/problems/tunnel-requires-token",
  "title": "Tunnel requires token",
  "status": 409,
  "detail": "This companion runs without authentication, so a tunnel would publish the workspace to anyone holding the URL. Restart it with a token (`gintrack serve --token new`) and try again.",
  "code": "tunnel_requires_token",
  "instance": "/api/v1/tunnel"
}
```

Clients switch on `code`, as everywhere else in §5.4; the `type` URI is the same
slug with hyphens. Two other codes can come back from these routes:
`not_implemented` (501) when `server.tunnel.provider` names a service this build
does not implement — the same condition `supported: false` reports — and
`tunnel_failed` (500) when the provider could not be reached, or the tunnel died
while it was being started or stopped.

A status change is broadcast on the WebSocket stream as the `tunnel.changed`
topic (§5.6) carrying the same document, so a second open tab does not keep
showing a workspace as private after somebody published it.
`GET /api/v1/capabilities` reports `features.tunnel`.

**A toggle made here is not written back to the configuration file.** It lasts
for the life of the process and nothing more: an API toggle that persisted would
republish the workspace on the next `gintrack serve` with nobody watching, which
is a worse failure than having to flip the switch again. `server.tunnel.enabled`
is the only thing that opens a tunnel at startup, and only `gintrack config`
edits it.

Read §5.1.1 before using any of this: what these three routes publish is a server
with read and write access to every mounted repository, guarded by one token.

#### The MCP write tools

Two routes, behind the bearer token like the rest of the API. They read and set
whether the MCP server advertises its write tools — the setting `gintrack mcp`
reads from `mcp.allowWrite` (§3.2) and `gintrack serve --mcp-allow-write` sets
for the endpoint of one process.

```http
GET   /api/v1/mcp/settings     # the current write mode
PATCH /api/v1/mcp/settings     # {"allowWrite": true|false}
```

Both answer the same document:

```json
{"supported":true,"allowWrite":false,"http":false,"persisted":true,
 "configPath":"/home/dev/.config/gintrack/config.yaml","tools":[]}
```

| Field | Meaning |
|---|---|
| `supported` | False when the change could not take effect at all — no configuration file **and** no MCP endpoint in this process. A client hides the affordance rather than offering a switch that does nothing. |
| `allowWrite` | Whether the write tools are advertised. |
| `http` | Whether this process also serves `POST /mcp`, in which case the change is live here too. |
| `persisted` | Whether the configuration file took the change. False on a `serve --repo` with no configuration file, where the switch lasts for the life of the process. |
| `configPath` | The file the choice was written to, empty when there is none. |
| `tools` | What the endpoint of this process advertises; empty when it serves none. |

Unlike the tunnel above, **this toggle is written back to the configuration
file**, and deliberately so: the whole point of the setting is that an agent
runtime spawns a bare `gintrack mcp`, which reads `mcp.allowWrite` at startup
(§4.9). A switch that did not outlive the process would leave every agent
read-only, which is the problem it exists to solve. The direction of the risk is
also the other way around: the tunnel publishes the workspace to the internet,
while this grants an agent the user already runs the same edits the user can make
in the UI, in files git tracks.

A `PATCH` on a process with neither a configuration file nor an endpoint is
refused with `not_implemented` (501) rather than reporting a success that
changes nothing. A body without `allowWrite` is `invalid_request` (400).

The change reaches a **mounted `/mcp` endpoint immediately**: the server is
rebuilt in place, so a client connecting afterwards is offered the write tools
without the companion restarting. Sessions the previous server held end with it
and a connected client initializes again, exactly as it does across a restart. A
**stdio** server — the usual case — reads the file when it starts, so an agent
picks the change up the next time its MCP server does.

#### Search

```http
GET /api/v1/search?q=oidc+discovery&scope=items,kb&project=ACME&limit=20
```

```json
{
  "query":"oidc discovery",
  "hits":[
    {"kind":"item","id":"ACME-T-0311","project":"ACME","vaultId":"acme","title":"Wire OIDC discovery endpoint",
     "score":8.42,"snippet":"Fetch /.well-known/openid-configuration and cache…",
     "path":"docs/.pmngr/tasks/…","source":"core"},
    {"kind":"page","project":"ACME","vaultId":"acme","path":"docs/architecture/auth.md","title":"Authentication",
     "score":0.0163,"snippet":"…discovery documents are cached for one hour…","source":"pando","index":"kb"},
    {"kind":"item","id":"ACME-US-0042","project":"ACME","vaultId":"acme","title":"Login with SSO",
     "score":0.0141,"snippet":"…we settled on rotating the refresh token…","source":"pando","index":"kb",
     "match":"comment","moreMatches":2},
    {"kind":"file","project":"ACME","vaultId":"acme","path":"internal/auth/oidc.go",
     "score":0.88,"snippet":"func discoverOIDC(ctx context.Context…","source":"pando","index":"code"}
  ],
  "total":7,"engine":"pando","degraded":false
}
```

Every hit carries `source`, the backend that produced it (GIT-US-0082):

- `"core"` — the substring index. Every term must match; the score is the sum of
  the field weights it matched (id 100, title 3, label 2, body 1).
- `"pando"` — a semantic candidate from the optional accelerator, **resolved back
  into this companion's own index**: the title, path and project are re-read
  locally, and a candidate that no longer resolves is dropped rather than
  returned. The score is Pando's reciprocal-rank-fusion value, which lives in a
  different space from the core one and must never be compared with it.

A Pando hit carries three more fields (GIT-US-0096, GIT-US-0098):

- `index` — which of Pando's two indexations produced it, `"kb"` or `"code"`. It
  is absent on a core hit, which has only one index to come from. A `docs/*.md`
  file both indexations returned is shown once, merged by resolved path with each
  leg normalised by its own top score (docs/21 §0.2).
- `match: "comment"` — the fragment came from a comment file, and the hit was
  resolved back to the **item the comment belongs to**. `moreMatches` counts the
  further comments of the same item that also matched and were collapsed into
  this row, so a thread that answers a query in five places does not fill the
  result list with one item.
- `kind: "file"` — a path inside the indexed tree that this index owns neither as
  an item nor as a page. It carries a path and a snippet and nothing else. Only a
  path that is gone from disk is dropped.
- `kind: "requirement"` — a chunk of a spec file that lies inside a requirement
  block, resolved to that block (GIT-US-0118, docs/21 §2.1): `id` is the ref
  (`ACME-SP-0003.R2`), with `spec`, `anchor` (`acme-sp-0003-r2`) and the
  requirement's `status`. A chunk outside every block stays the spec's `item` row.

**The order is the contract.** Exact hits lead, in the order the substring index
ranked them; semantic hits the exact half did not already find follow. The two
lists are concatenated, never re-ranked together.

`engine` repeats the `features.search` capability for this one answer, and
`degraded` is `true` when the semantic half could not be obtained — Pando
unreachable, unauthorized, or over its 300 ms budget. A degraded answer is still
a `200` with the exact hits in it: **search never fails over its accelerator**.
A companion with no Pando configured answers `engine: "core"`,
`degraded: false`, and every hit `source: "core"`.

Without `?project=`, the query spans **every mounted repository** — the team knowledge base
included — and each hit carries the `project` it belongs to (the team key for a team
knowledge-base page) plus the `vaultId` of the repository that answered, so a workspace never
returns a row whose source is ambiguous (GIT-US-0016). With `?project=<KEY>`, only the repository
exposing that key is searched, and an unknown key is a `404`.

`project` scopes to **several projects** too (GIT-US-0102). Like every list filter it is
repeatable and OR within the field, and each value may also be a comma-separated list, so
`?project=ACME&project=WEB` and `?project=ACME,WEB` are the same query. A team key is a valid
scope and selects that team's knowledge base. Every key must name a mounted project or team,
or the answer is a `404`. The scope applies to both halves: exact hits outside it are never
ranked, and semantic candidates are filtered after they are resolved — Pando has no project
filter of its own, so the companion over-fetches and drops the rest, and skips the code
projects whose repository holds none of the selected keys. A `kind: "file"` hit names no
project and is therefore outside any scope. Omitting `project` searches everything, which is
what the web app sends when every project is selected.

The core contract spells the same scope `{"q":…,"project":"ACME","projects":["WEB"]}` on the
`search` method; the two fields add up, and `SearchQuery.projectKeys` is the web provider's
name for it.


#### Semantic search settings and reindex (GIT-US-0091)

Three companion-only endpoints, inside the bearer-auth group. They are what makes
semantic search diagnosable: where Pando is, what it was pointed at for every
mounted repository, whether it answered, and a button to reindex.

```http
GET   /api/v1/search/settings
PATCH /api/v1/search/settings   {"mcpUrl":"http://127.0.0.1:9777/mcp","projectId":"acme-api"}
POST  /api/v1/search/reindex
```

`GET` answers:

```json
{
  "backend":"pando",
  "configured":true,
  "mcpUrl":"http://127.0.0.1:9777/mcp",
  "restUrl":"http://127.0.0.1:9778",
  "projectId":"home_dana_src_acme-api",
  "allowRemote":false,
  "reachable":true,
  "reachableError":"",
  "indexed":[
    {"repo":"acme-api","root":"/home/dana/src/acme-api","docs":["docs"],
     "items":412,"pages":38,"comments":167,
     "code":{"project":"home_dana_src_acme-api","status":"indexing","job":"idx-7741",
             "note":"Indexing the repository root."}}
  ],
  "reindex":null,
  "persisted":false
}
```

- `backend` is the value `features.search` reports, and `reachable` is a **live
  probe** of the MCP endpoint run while answering (`null` when none is
  configured, with `reachableError` saying why a probe failed).
- `indexed` is one entry per mounted repository, and it replaces the report on
  the exported corpus that used to live here (`corpusDir`, `corpora`,
  `documents` and `lastExport` are gone with it — GIT-EP-0020, ADR-036). There is
  no second copy to date any more, so the honest answer to *"is my search
  current?"* is **where Pando was pointed and what git-in-track's own index found
  there**: `root` is the working tree registered as a Pando code project, `docs`
  are the repository's documentation directories — one of them is Pando's
  `KBPath`, and the backlog lives under it in `.pmngr/` — and `items`, `pages`
  and `comments` are what this companion indexed underneath them. A row reporting
  0 items is a misconfigured `KBPath`, and that is what this makes visible.
- `indexed[].code` is the repository's code-project registration, which runs when
  the server starts (docs/21 §0.1). `status` is `off` (no Pando endpoint),
  `registered` (Pando already knew the project and was not asked again),
  `indexing` (this start handed Pando a job, whose id is in `job`) or
  `unavailable` (Pando refused or did not answer). `note` says it in words, so
  the reason there is no code search is readable in the UI rather than only in
  the log. The field is absent until that pass has run.
- Neither Pando token is ever reported. They are resolved from
  `GINTRACK_PANDO_MCP_TOKEN` / `GINTRACK_PANDO_REST_TOKEN` or the configuration
  file (docs/07 §3.3) and stay in the companion process.

`PATCH` takes any subset of `mcpUrl`, `restUrl`, `projectId` and `allowRemote`;
an absent field is left alone, and an unknown one — `corpusDir` included — is
ignored and changes nothing. **Tokens are not patchable**: a credential enters
the process from the environment or the file, never over the API. The change is
adopted by the running process immediately — the Pando client and the semantic
searcher are rebuilt — and then written to the configuration file, with the
tokens already in that file left untouched. The response repeats the settings and
adds `persisted`, exactly as `PATCH /api/v1/git/settings` does: `false` means the
companion was started without a configuration path (a test, or `serve --repo`)
and the change lives only until it exits. A non-loopback URL without
`allowRemote`, or a URL that is not one, is refused with `invalid_request` (400)
and nothing is adopted.

`POST /api/v1/search/reindex` answers `202` with the job, then runs in the
background and publishes `search.progress` (§5.6). It asks Pando to index each
repository's source tree, then reindexes the knowledge base once. It is a
**catch-up pass**, not how the index normally changes: Pando's watcher follows
the documentation folder and reindexes an edit as it happens.

```json
202
{"jobId":"reindex-1","startedAt":"2026-09-15T10:04:00Z","phase":"code","repos":[]}
```

Poll `GET /api/v1/search/settings`, whose `reindex` field carries the running job
and, afterwards, the last finished one:

```json
{"jobId":"reindex-1","phase":"completed","kbNote":"Reindexed.",
 "kb":{"scanned":450,"added":3,"updated":0,"unchanged":447,"deleted":0},
 "repos":[{"repo":"acme-api","codeJob":"idx-7741"}]}
```

- `phase` walks `code` → `kb` → `completed` or `failed`.
- The halves are independent. One repository whose source tree Pando refused must
  not stop the knowledge base being reindexed: `codeError` is reported per
  repository in `repos[]`, and the job ends `failed` with the successful halves
  still in it.
- **The knowledge-base half is honest.** The reindex route lives on Pando's REST
  surface only, so without a `restUrl` there is nothing to call and `kbNote` says
  so, adding that Pando's own watcher still follows the documentation directory —
  it never claims a reindex that did not happen. With `restUrl` configured the
  job calls the route and `kb` carries the real
  `scanned/added/updated/unchanged/deleted` counts.
- A second call while one is running is refused with `search_reindex_running`
  (409) and the running job is untouched. A companion with no Pando endpoint
  answers `search_not_configured` (400).
- **One repository (GIT-US-0101).** The body is optional; `{"repo":"<id>"}`
  scopes the code half to that mounted repository. It is how the workspace list
  switches semantic search on for a repository whose code index is `off` or
  `unavailable`: `code_index_project` registers and indexes that working tree
  alone, no other repository is touched, and the knowledge base is reindexed as
  usual. The job carries `"scope":"<id>"`, and its outcome lands in that
  repository's `indexed[].code` (`indexing`, or `unavailable` when Pando refused
  it). An id that is not a ready mount answers `repo_not_registered` (404)
  before the reindex slot is claimed. The scope is a body field of this route
  rather than a route of its own because a scoped run is the same job — the
  same single slot, `202`, `search.progress` frames and `reindex` record — over
  fewer repositories.

```json
POST /api/v1/search/reindex
{"repo":"acme-api"}

202
{"jobId":"reindex-2","scope":"acme-api","startedAt":"2026-09-17T10:00:00Z","phase":"code","repos":[]}
```

> **Operations: the embedding model is pinned configuration.** Pando skips any
> chunk whose vector length differs from the query's — silently, with no
> dimension guard and no error — and the model is configured **per Pando
> instance, not per corpus**. Changing it therefore degrades recall invisibly for
> *every* consumer of that instance, not just git-in-track, until a full reindex
> has re-embedded everything. Treat the model as a pinned value and reindex
> deliberately when it changes.

#### The inbox (GIT-US-0056, ADR-033)

A project whose workflow declares a status in the reserved `triage` category has
an inbox: a queue of submissions waiting for a decision. A project that declares
none simply has no inbox — listing it answers an empty queue, and filing
something into it is refused with `no_triage_status` (409), which the user fixes
in `project.yaml` rather than by retrying.

```json
GET /api/v1/inbox?project=ACME&status=pending&limit=50
200
X-Total-Count: 12
{
  "items":[{"id":"ACME-US-0101","type":"story","title":"Checkout times out",
            "status":"triage","inbox":{"status":"pending","source":"web",
                                       "received":"2026-09-13T09:12:00Z"}, "rev":"sha256:…"}],
  "nextCursor":"","total":12,
  "counts":{"pending":9,"accepted":1,"rejected":1,"snoozed":1,"duplicate":0},
  "pending":9
}
```

Query parameters: `project`, `status` (repeatable), `type`, `label`, `assignee`,
`q`/`text`, `sort`, `order`, `limit` (capped at 500), `cursor`, `fields`.
`status` is the **triage** state — `pending`, `accepted`, `rejected`, `snoozed`,
`duplicate` — and never a workflow status; anything else is `invalid_request`.
`counts` and `pending` are computed over the **whole queue**, not the page, and a
snoozed item whose date has arrived is counted as pending again, because that is
what a reader sees.

One decision empties one row:

```json
POST /api/v1/items/ACME-US-0101/triage    If-Match: sha256:…
{"action":"accept","status":"backlog","parent":"ACME-EP-0007"}
200
ETag: "sha256:…"
{"item":{"id":"ACME-US-0101","status":"backlog", …},"action":"accept",
 "pending":8,"writes":{"written":[…]}}
```

`action` is `accept`, `reject`, `snooze` (with `snoozedUntil`, `YYYY-MM-DD`) or
`duplicate` (with `duplicateOf`). The route requires `If-Match` on the item:
without it, `precondition_required` (428); with a revision that is no longer
current, `412` carrying `currentRev` and the conflicting fields. `If-Match: *`
overwrites unconditionally, as everywhere else. A `duplicate` decision writes two
files — the entry and the item it points at — in one write set.

Every triage, and every create that files an item straight into the queue,
publishes `inbox.changed` (§5.6).

**What the exclusion covers, and what it does not.** A triage item is invisible
to every *planning* surface: `GET /api/v1/items` excludes it by default, and
board views, sprint views, sprint candidates and sprint metrics exclude it
**unconditionally** — a hand-edited sprint file naming a triage item reports it
as unresolved, never as work, so a busy inbox moves no burndown point and a
board column that names the `triage` status still renders empty (ADR-033,
`internal/core/triageexclusion_test.go`).

**Search is deliberately not one of those surfaces.** `GET /api/v1/search` and
the MCP `search_items` tool still find a triage item by text. The exclusion is a
property of a `Filter`, and search takes none: excluding there would make a
submission unfindable from every surface at once — the quick switcher included —
which contradicts the rule that an item is real from the moment it is submitted
and still readable by id. A search hit carries no estimate, no status category
and no column, so nothing reaches a planning number through it.
`TestSearchStillFindsATriageItem` pins that behaviour, and changing it means
changing ADR-033 first.

A project created before the inbox existed declares no `triage` status and
therefore has no inbox at all: the listing is empty, the sidebar entry and the
capture form render nothing, and filing into it is `no_triage_status` (409).
There is no migration — adding a status in the `triage` category to
`project.yaml` is the whole opt-in.

#### Sync and git

```http
GET   /api/v1/git/settings                   effective commit-on-save settings
PATCH /api/v1/git/settings                   {"commitOnSave":true,"messageTemplate":"…"}
GET   /api/v1/git/status?repo=ACME           backend, identity, branch, dirty set
POST  /api/v1/git/commit                     {} flushes what is batched, or
                                             {"repo":"ACME","paths":[…],"message":"…"}
GET   /api/v1/git/cors-proxy                 where the CORS proxy is, and its allow-list

GET  /api/v1/sync/status                    per-repo ahead/behind/dirty
POST /api/v1/sync/run                       {"repos":["ACME"],"dryRun":false,"push":true}
GET  /api/v1/sync/conflicts
GET  /api/v1/sync/conflicts/file?repo=TEAM&path=…   base/ours/theirs + the proposed merge
POST /api/v1/sync/conflicts/resolve         {"repo":"TEAM","path":"…",
                                             "resolution":"ours|theirs|merged|manual",
                                             "content":"…","fields":{…},"hunks":{…},
                                             "hunkText":{…},"continue":true}
POST /api/v1/sync/abort
GET  /api/v1/sync/jobs                      ?state=&kind=&limit=&cursor=
GET  /api/v1/sync/jobs/{id}                 one job, with its attempts and redacted error
POST /api/v1/sync/jobs/{id}/retry           re-queue a failed or cancelled job
POST /api/v1/sync/jobs/{id}/cancel          withdraw a queued or running job
GET  /api/v1/sync/settings                  the git half and the engine half together
PATCH /api/v1/sync/settings                 {"pullStrategy":"rebase","workers":4,"rate":10}
GET  /api/v1/git/log?item=ACME-T-0311&limit=20
```


#### Background jobs and the engine settings (GIT-US-0078)

Six endpoints over the companion's job engine, inside the `/sync` subtree
because it is the same feature area. All of them sit behind the bearer token.
There is deliberately **no** endpoint that enqueues an arbitrary job: kinds are
created by the feature that owns them (import, comment push, knowledge-base
publish), and a generic enqueue would be an unauthenticated-by-shape way to
drive this companion's outbound HTTP.

```http
GET /api/v1/sync/jobs?state=queued&kind=youtrack.import&limit=100&cursor=job_000021
200
{
  "jobs":[
    {"id":"job_000021","kind":"youtrack.import","key":"ACME","state":"queued",
     "attempts":0,"createdAt":"2026-09-13T11:00:00Z","updatedAt":"2026-09-13T11:00:00Z"},
    {"id":"job_000022","kind":"youtrack.import","key":"ACME","state":"failed",
     "attempts":5,"createdAt":"2026-09-13T10:58:00Z","updatedAt":"2026-09-13T10:59:12Z",
     "nextAttempt":"2026-09-13T11:01:00Z","deadLetter":true,
     "lastError":{"attempt":5,"class":"terminal","message":"403 Forbidden",
                  "at":"2026-09-13T10:59:12Z"}}
  ],
  "nextCursor":"job_000031","total":42,
  "counts":{"queued":12,"running":2,"done":26,"failed":2,"cancelled":0},
  "running":2,"deadLetter":2,"engine":true
}
```

| Parameter | Meaning |
| --- | --- |
| `state` | Repeatable: `queued`, `running`, `done`, `failed`, `cancelled`. Any other value is `invalid_request`. |
| `kind` | Repeatable; matches the job kind exactly. |
| `limit` | Page size, default 100, capped at `maxItemsPerPage` (500). |
| `cursor` | The `nextCursor` of the previous page — the id the next page starts at. |

`counts` is the whole queue, not the page, and `X-Total-Count` carries the
number of jobs that matched the filter. **A job's payload is never rendered**:
it is the one field of a job this layer cannot vouch for, and nothing in the UI
reads it. `lastError.message` is the message the engine recorded, with every
credential it recognized already redacted.

`GET /api/v1/sync/jobs/{id}` answers the same object for one job, or
`sync_job_not_found` (404) — which is also what a job pruned after the retention
window answers.

**Retry and cancel** are the only two transitions a caller may ask for, and each
is refused from a state it cannot be made from, with a problem document naming
that state:

| Endpoint | Allowed from | Refused with |
| --- | --- | --- |
| `POST /api/v1/sync/jobs/{id}/retry` | `failed`, `cancelled` | `sync_job_not_retryable` (409) |
| `POST /api/v1/sync/jobs/{id}/cancel` | `queued`, `running` | `sync_job_not_retryable` (409) |

A **failed** job is re-queued in place: it keeps its id, its history and its
last error, and gets a fresh attempt budget. A **cancelled** job cannot be — the
engine's state machine has no edge out of `cancelled`, by design — so it is
re-queued as a *new* job with the same kind, key and payload, and the answer
carries the new id. Both publish the matching `sync.job.*` event (§5.6).

```http
POST /api/v1/sync/jobs/job_000022/retry
200
{"id":"job_000022","kind":"youtrack.import","key":"ACME","state":"queued","attempts":0, …}

POST /api/v1/sync/jobs/job_000021/cancel
200
{"id":"job_000021","kind":"youtrack.import","state":"cancelled","attempts":0, …}

POST /api/v1/sync/jobs/job_000030/cancel
409
{"type":"https://git-in-track.dev/problems/sync-job-not-retryable",
 "title":"Sync job not retryable","status":409,"code":"sync_job_not_retryable",
 "detail":"Job job_000030 is done: only a queued or running job can be cancelled."}
```

**Settings.** `GET /api/v1/sync/settings` answers both halves of the sync
configuration in one document — the git half of GIT-US-0021 and the engine half
of GIT-US-0084 — and `PATCH` changes either:

```http
GET /api/v1/sync/settings
200
{ "pullStrategy":"rebase","pushOnSync":true,"maxPushRetries":3,"supported":true,
  "engine":{"workers":2,"batchSize":20,"rate":5,"maxAttempts":5,
            "retentionHours":168,"drainSeconds":5,"running":true},
  "persisted":false }

PATCH /api/v1/sync/settings   {"workers":4,"rate":10}
200
{ …, "engine":{"workers":4,"batchSize":20,"rate":10, …}, "persisted":true }
```

The four engine knobs may be sent flat, as above, or nested under `"engine"`;
the nested form wins when both are present. `workers`, `batchSize` and `rate`
**take effect on the running engine at once** — the pool is resized and the
shared limiter re-rated without a restart. `maxAttempts` is fixed when the
engine is built, so it is recorded and applies from the next start.

Out-of-range values are refused with `invalid_request` (400) naming the field;
the ranges are the table in §4.1. `persisted` follows the same contract as
`PATCH /api/v1/git/settings`: it is `true` only when the change reached the
configuration file, which for the engine half means the `sync.engine` section
of §3.2. A companion started without a configuration file — `serve --repo`, or a
test — keeps the change for the life of the process and answers `false`.

| Code | Status | Meaning |
| --- | --- | --- |
| `sync_job_not_found` | 404 | No job of this queue has that id. |
| `sync_job_not_retryable` | 409 | The job exists and is in a state the transition cannot be made from. |
| `sync_engine_not_running` | 503 | The engine has been closed, or was never started. |

#### YouTrack (GIT-US-0052, GIT-EP-0011, ADR-032)

The browser never talks to YouTrack. It asks the companion, the companion holds
the token and the companion makes the call, which is what keeps the credential
on one machine and out of every devtools network log. There is no generic
pass-through endpoint, for the same reason ADR-025 refuses to generalise the
CORS proxy: only the calls the UI needs exist.

```http
GET   /api/v1/youtrack/settings?key=DEMO     the connection of one project, token excluded
PATCH /api/v1/youtrack/settings?key=DEMO     sparse write of both halves
POST  /api/v1/youtrack/test?key=DEMO         probe the connection, saved or typed
GET   /api/v1/youtrack/projects?key=DEMO&q=  the instance's projects, for the autosuggest
POST  /api/v1/youtrack/projects?key=DEMO     the same list, read with a connection typed but unsaved
GET   /api/v1/youtrack/fields?key=DEMO&project=ACME   the remote project's custom fields
GET   /api/v1/youtrack/issues?key=DEMO&q=&preset=&limit=&cursor=   search, for the import picker
POST  /api/v1/youtrack/import/preview?key=DEMO   what an import would do; writes nothing
POST  /api/v1/youtrack/import?key=DEMO           queue the import; answers a job id
POST  /api/v1/youtrack/comments/push?key=DEMO    queue a comment push; answers a job id
GET   /api/v1/youtrack/kb/status?key=DEMO&path=&recursive=&remote=   page sync states
POST  /api/v1/youtrack/kb/publish?key=DEMO       queue a publish; answers a job id
POST  /api/v1/youtrack/kb/pull?key=DEMO          queue a pull; answers a job id
POST  /api/v1/youtrack/kb/unlink?key=DEMO        forget the article one page mirrors
```

`/kb/unlink` takes `{"path"}` and answers
`{"project", "path", "unlinked", "articleId"}`. It is the odd one of the four:
no job, no call to the instance, and no connection required. It removes the
page's `external:` entry and nothing else — the article is neither deleted nor
archived, the project stays connected, and a page that carried no reference
answers `unlinked: false` rather than failing. Publishing the page afterwards
creates a new article instead of updating the one it forgot.

The item half of the same operation is an ordinary item patch:
`PATCH /api/v1/items/{id}` with `{"removeExternal": [{"system": "youtrack"}]}`.
An entry with no `id` forgets every reference of that system, which is what
unlinking one tracker from an item that mirrors several has to mean —
`unset: ["external"]` would take the others with it.

`key` is the **git-in-track** project key and may be omitted when the companion
serves exactly one project; with several it is required, and its absence is
`400 invalid_request`. `project` on `/fields` is the **YouTrack** project, short
name or internal id, and defaults to the linked one.

```json
GET /api/v1/youtrack/settings?key=DEMO
200
{ "projectKey": "DEMO", "configured": true,
  "url": "https://yt.example.com/youtrack", "project": "ACME",
  "fieldMap": { "status": { "field": "State",
                           "values": { "In Progress": "in_progress", "Fixed": "done" } },
                "priority": { "field": "Priority" } },
  "pushComments": "manual", "kbSync": "manual", "kbSyncDirection": "push",
  "hasToken": true, "tokenSource": "file", "persisted": false,
  "repo": "acme-api", "projectPath": "docs/.pmngr/project.yaml" }
```

There is no `token` field and there never will be one: `hasToken` and
`tokenSource` (`flag` | `env` | `file` | `none`) report everything a UI needs
about the credential without rendering it.

##### The shape of `fieldMap` (GIT-US-0065)

A field map answers two questions, and it used to be able to answer only the
first: *which* YouTrack custom field carries a git-in-track field, and *what*
one of that field's values means here.

Over the wire every entry is an object:

```json
"fieldMap": {
  "status":   { "field": "State", "values": { "In Progress": "in_progress" } },
  "priority": { "field": "Priority" }
}
```

`field` is the YouTrack custom field **name** — not its id, which is
instance-local. `values` maps a YouTrack value **name** onto the git-in-track
value it means: a status id for `status`, one of the four core priorities for
`priority`, an item type for `type`. The lookup is case-insensitive on the
trimmed value, and a configured value map is *overlaid on* the shipped
translations rather than replacing them, so mapping one unusual state does not
forget what `Fixed` means.

The whole block — the field names *and* the value maps — is what the companion
hands to the importer, so the preview and the import that follows it read one
table. A value map is therefore never preview-only: the status an issue is shown
as landing on is the status it lands on.

Only those three keys accept `values`. They are exactly the three the importer
translates value by value; a `values` block on any other key is refused with
`field_map.<key>.values` rather than stored and ignored. A `PATCH` may still send
the flat string form — `{"status": "State"}` — and it means the same as
`{"status": {"field": "State"}}`.

In `project.yaml` the flat form is likewise still a bare scalar, and an entry
with no value map is *written back* as one, so the file only grows nesting a
project actually asked for:

```yaml
integrations:
  youtrack:
    field_map:
      priority: Priority          # the flat form, unchanged
      status:
        field: State
        values:
          In Progress: in_progress
          Fixed: done
```

The accepted keys are `status`, `priority`, `type`, `assignee`, `estimate` and
`milestone`. `labels`, `due` and `sprint` were accepted by earlier builds and
read by nothing: they are now refused with a message saying why — labels travel
as YouTrack tags rather than through a custom field, and neither a due date nor a
sprint is read from one.

##### The issue search (GIT-US-0054)

`GET /api/v1/youtrack/issues` is what the import dialog types into. It is a
search and not a cache: nothing it returns is written to the index, and the
instance stays the authority on what matches.

| Parameter | Meaning |
| --- | --- |
| `q` | a YouTrack issue query, as the user typed it |
| `preset` | one of `epics`, `stories`, `tasks`, `versions`, `unresolved`; anything else is `400 invalid_request` naming the field |
| `limit` | page size, default 50, clamped to 200 |
| `cursor` | opaque; the `nextCursor` of the previous page |

The effective query is `project: {<shortName>}`, then `q`, then the preset
clause, and it **always ends with `order by: created asc`**. The ordering is not
a nicety: YouTrack is free to re-order between two requests, so a `$skip` walk
over an unordered query silently skips and duplicates rows between keystrokes.
The composed query is echoed in the answer so a user can see what their typing
became; it carries no credential.

```json
GET /api/v1/youtrack/issues?key=DEMO&q=checkout&preset=stories&limit=2
200
{ "projectKey": "DEMO", "project": "ACME",
  "query": "project: {ACME} checkout Type: {User Story} order by: created asc",
  "preset": "stories", "limit": 2,
  "items": [
    { "id": "2-1041", "idReadable": "ACME-42", "summary": "Guest checkout",
      "type": "User Story", "state": "In Progress", "assignee": "marta",
      "updated": "2026-09-13T10:00:00Z",
      "url": "https://yt.example.com/youtrack/issue/ACME-42",
      "linked": { "itemId": "DEMO-US-0001", "type": "story",
                  "status": "in_progress", "title": "Guest checkout" } },
    { "id": "2-1042", "idReadable": "ACME-43", "summary": "Saved cards",
      "type": "Task", "state": "Open", "assignee": "jose",
      "updated": "2026-09-12T08:14:00Z",
      "url": "https://yt.example.com/youtrack/issue/ACME-43",
      "linked": null }
  ],
  "nextCursor": "Mg" }
```

`linked` is resolved **locally**, from the index, by the pair
`(external.system, external.id)`: whether an issue has already been imported is
a fact about this repository, so the picker gets its "already imported" badge
without a second round trip. It is `null` when nothing claims the issue.

The `versions` preset is not a query at all. A version is a value of the
project's version bundle, resolved through the custom-field settings of the
field the project's `field_map` names for `milestone` (default `Fix versions`),
and the values are returned in the same envelope with `"type": "version"` so the
picker renders one list:

```json
GET /api/v1/youtrack/issues?key=DEMO&preset=versions
200
{ …, "items": [
    { "id": "v-1", "idReadable": "1.0", "summary": "1.0", "type": "version",
      "released": true, "linked": null },
    { "id": "v-2", "idReadable": "2.0", "summary": "2.0", "type": "version",
      "releaseDate": "2026-01-01", "linked": null } ] }
```

Archived versions are left out unless `archived=true` is passed. Upstream
failures map to the `youtrack_*` codes of the table below, and neither the token
nor the `Authorization` header ever reaches a response, a problem document or a
log line.

##### Running an import (GIT-US-0047, GIT-US-0050)

Both import routes take `internal/vault.YouTrackImportParams` as the body —
`{query | ids[], depth, includeLinks, includeComments, includeAttachments}` —
and the `?key=` of this subtree. They differ in who waits:

`POST /api/v1/youtrack/import/preview` is **synchronous**. It writes nothing, so
the dialog can wait for it, and the answer is the plan: what each issue would
become, which item an update would patch and what could not be resolved.

```json
POST /api/v1/youtrack/import/preview?key=DEMO
{"ids":["ACME-42"],"depth":1,"includeComments":true}
200
{ "project": "DEMO",
  "issues": [ { "youtrackId": "ACME-42", "title": "Guest checkout",
                "mappedType": "story", "action": "create", "depth": 0,
                "comments": 3, "warnings": [] } ],
  "warnings": [] }
```

`POST /api/v1/youtrack/import` **queues**. A hundred issues are a hundred
requests against somebody else's rate limit, and holding an HTTP response open
across that is exactly what the background engine exists to avoid, so the answer
is `202 Accepted` with a job id:

```json
POST /api/v1/youtrack/import?key=DEMO
{"query":"#Unresolved","includeAttachments":true}
202
{ "jobId": "job_000021", "projectKey": "DEMO", "repo": "acme-api", "queued": true }
```

From there the job narrates itself on `sync.job.*` (§5.6) and is inspectable at
`GET /api/v1/sync/jobs/{id}` (§5.5). It imports in batches — one batch is one
vault call and one commit — so a cancelled import keeps every batch that already
landed and reports how far it got. **This route is always asynchronous.** There is no request that makes it answer
a finished import, so a client needs no branch for one: the answer is always
`202` with `{jobId, projectKey, repo, queued}` and `queued` is always `true`.
The only synchronous half of an import is `/import/preview`, which writes
nothing. `queued` is spelled out anyway so that a client reading either this
shape or a `vault.YouTrackImportResult` from the MCP tool can tell them apart
without inspecting which keys are present. A project with no usable connection is refused **here**,
before anything is queued, rather than inside a job nobody is watching.

##### Pushing a comment (GIT-US-0068, GIT-US-0076)

`POST /api/v1/youtrack/comments/push` is what the **Send to YouTrack** action on
a comment calls. Like every outbound write to a tracker it **queues**, and the
answer is `202 Accepted` carrying the job id and what the vault decided about
each comment:

```json
POST /api/v1/youtrack/comments/push?key=DEMO
{"itemId":"DEMO-US-0001","commentPath":"docs/.pmngr/comments/DEMO-US-0001/20260901T104512Z-marta.md"}

202
{ "project": "DEMO", "itemId": "DEMO-US-0001", "jobId": "job_000032",
  "pushed":  [ { "commentPath": "docs/.pmngr/comments/DEMO-US-0001/20260901T104512Z-marta.md" } ],
  "skipped": [ { "commentPath": "docs/.pmngr/comments/DEMO-US-0001/20260902T091200Z-jose.md",
                 "youtrackCommentId": "4-19",
                 "url": "https://yt.example.com/youtrack/issue/DEMO-42#focus=Comments-4-19",
                 "reason": "the comment is already on the issue" } ],
  "failed":  [] }
```

Give `commentPath` for one comment or `all: true` for the whole thread — one or
the other; neither and both are refused with a field-level `invalid_request`.

Three things a client must not get wrong:

- **`pushed` means *queued*.** It is not evidence the comment arrived. The
  evidence is the comment's own `external` entry, which the job writes when the
  post came back, and which `skipped` echoes for a comment that already had one.
- **The coalescing key is the comment path, not the item id.** A burst of edits
  to one comment is one push; two comments of the same item stay two.
- **An item that mirrors no issue is refused here**, before anything is queued,
  and non-retryably: there is nowhere to post, and no number of attempts creates
  a link only a user can create. A project with no usable connection is likewise
  refused in this call rather than inside a job nobody is watching, with the same
  five `youtrack_*` problem codes as the rest of the subtree.

A comment deleted locally is **never** deleted remotely: there is no job for it
and deliberately none.

With `integrations.youtrack.push_comments: auto` the route is unnecessary — every
comment written on a linked item is queued by the same seam in the vault's
`comment.add`, on every surface. Flipping the setting is not retroactive: it
governs comments written from then on.

##### Knowledge-base synchronization (GIT-US-0087, GIT-US-0090)

Three routes over the three core methods `youtrack.kb.status`,
`youtrack.kb.publish` and `youtrack.kb.pull`. The handlers hold no sync logic:
status compares what the repository knows, publish and pull queue a background
job, and the engine does the work.

They are mounted under `/youtrack/kb/…` with the `?key=` convention of the rest
of that subtree, and — from the same handlers — inside every `/kb` mount, so the
scoped spellings work too and cannot drift from the flat one:

```http
GET  /api/v1/projects/{key}/kb/youtrack/status
GET  /api/v1/teams/{key}/kb/youtrack/status
GET  /api/v1/kb/youtrack/status?project=DEMO
```

A project with no `integrations.youtrack` block answers **404** on all three:
the routes are gated on `features.youtrack`, which is false precisely when
nothing is linked.

```json
GET /api/v1/youtrack/kb/status?key=DEMO&path=docs&recursive=true
200
{ "project": "DEMO",
  "pages": [ { "path": "docs/index.md", "linked": true, "articleId": "DEMO-A-1",
               "url": "https://yt.example.com/youtrack/articles/DEMO-A-1",
               "state": "in_sync", "syncedAt": "2026-09-13T12:00:00Z" },
             { "path": "docs/architecture/overview.md", "linked": false,
               "state": "unlinked" } ],
  "remote": false }
```

`state` is one of `unlinked`, `in_sync`, `local_ahead`, `remote_ahead` and
`conflict`. `remote` asks the route to read each linked article and is **never
defaulted on**: it is one request per page, and a tree view of a documentation
folder would otherwise become hundreds of them against somebody else's rate
limit. The answer repeats `remote` so a caller can tell "in sync as far as the
repository knows" from "in sync, checked". A page whose article could not be read
carries an `error` and does not fail the call: one unreachable article must not
hide the state of every other page.

```json
POST /api/v1/youtrack/kb/publish?key=DEMO
{"path":"docs","recursive":true}
202
{ "project": "DEMO", "jobId": "job_000031",
  "pages": ["docs/index.md","docs/architecture/overview.md"] }
```

`POST /api/v1/youtrack/kb/pull` takes and answers the same shape. Both queue: the
answer names the job and the pages it selected, and the job narrates itself on
`sync.job.*` (§5.6). A host with no engine or no client is `unavailable`; a path
that selects no page is `404`.

A page **both sides changed** is never merged. The job writes
`<page>.conflict.md` beside it, leaves the original untouched and reports the
page as `conflict` — which is what makes `gintrack youtrack kb push --wait` exit
`5` rather than claiming success (§4.15).

```json
PATCH /api/v1/youtrack/settings?key=DEMO
{"url":"https://yt.example.com/youtrack","project":"ACME",
 "fieldMap":{"status":"State"},"pushComments":"auto","token":"perm:…"}

200
{ …, "pushComments": "auto", "hasToken": true, "tokenSource": "file",
  "persisted": true }
```

The body is sparse: an absent key is left alone, a present one is written. `url`,
`project`, `fieldMap`, `pushComments`, `kbSync` and `kbSyncDirection` are the
committed half and go into `project.yaml` through a surgical YAML edit that keeps
every comment and every key the Go structs do not model. `token` is write-only
and goes into the machine-local configuration file; an empty string forgets the
stored credential. A settings change that would not load back is refused with
`400 invalid_request` **before** anything is written, so `project.yaml` is never
left half-edited.

`persisted` follows the git-settings contract exactly: `false` means the running
process took the token change but the server has no configuration file to write
it to (`serve --repo`, or a test), so it will not survive a restart. It says
nothing about the `project.yaml` half, which is written to a file by definition.

```json
POST /api/v1/youtrack/test?key=DEMO
{}                                  // or {"url":"…","token":"…"} to test before saving

200
{ "ok": true, "baseUrl": "https://yt.example.com/youtrack", "login": "jose",
  "fullName": "Jose F. Rives", "email": "jose@example.com", "project": "ACME" }
```

Testing with a body carries a URL and a token that have not been saved, which is
what the settings card uses before the user presses save; nothing is written
either way. Every failure is an RFC 7807 document whose `code` says which of
them it was:

| `code`                    | Status | Means                                                        |
| ------------------------- | ------ | ------------------------------------------------------------ |
| `youtrack_not_configured` | 409    | no `integrations.youtrack` block, or no stored token          |
| `youtrack_unauthorized`   | 502    | YouTrack answered 401: the permanent token was rejected       |
| `youtrack_forbidden`      | 502    | YouTrack answered 403: the account lacks permission           |
| `youtrack_not_found`      | 502    | YouTrack answered 404: the URL is missing its context path    |
| `youtrack_unreachable`    | 502    | transport failure, a 5xx after the retries, or a timeout      |
| `rate_limited`            | 429    | the instance is throttling this client                        |

An upstream failure is a **502**, not the status YouTrack returned: answering
401 here would tell a browser that its own session had expired, which is exactly
the wrong thing to believe. The `code` carries the distinction instead. No
`detail` ever contains the token — `internal/youtrack` redacts it on every error
path and the handler never renders an error's cause itself.

```json
GET /api/v1/youtrack/projects?key=DEMO&q=ac
200
{ "projects": [ { "id": "0-1", "shortName": "ACME", "name": "ACME API",
                  "archived": false } ],
  "total": 1, "limit": 100 }
```

The `POST` form takes `{"url", "token", "q"}` and answers the same body. It
exists because choosing the remote project is part of *connecting* to it: the
settings picker has to list projects while the URL and the token are still being
typed, and a credential belongs in a body rather than in a URL a proxy or a log
would keep. A body with neither `url` nor `token` reads the saved connection.

```json
GET /api/v1/youtrack/fields?key=DEMO&project=ACME
200
{ "project": "ACME",
  "fields": [ { "id": "f1", "name": "State", "type": "state[1]",
                "bundleId": "b1", "bundleType": "StateBundle",
                "canBeEmpty": false, "bundled": true,
                "values": [
                  { "id": "v1", "name": "In Progress", "label": "In Progress",
                    "ordinal": 1, "archived": false,
                    "background": "#25a4d4", "foreground": "#ffffff",
                    "isResolved": false },
                  { "id": "v2", "name": "Fixed", "label": "Fixed",
                    "ordinal": 2, "archived": false, "isResolved": true } ] },
              { "id": "f3", "name": "Customer", "type": "string",
                "canBeEmpty": true, "bundled": false,
                "warnings": ["field \"Customer\" has no bundle, so it has no enumerable values"] } ],
  "total": 2,
  "gintrackFields": ["status","priority","type","assignee","estimate","milestone"],
  "valueMappableFields": ["status","priority","type"] }
```

The endpoint resolves each field's **values** as well as its name, in one call,
which is what makes a per-value mapping configurable at all. Read it this way:

- `name` is the key a mapping is written against; `label` is what to display. A
  localized name changes with the UI language and an id is instance-local, so
  neither may be persisted.
- `bundled` says whether the field's values are enumerable. It is `false` for a
  text, date, integer or period field, and for a bundle kind this build cannot
  read — and `values` is then empty **without that being a failure**. Nothing is
  dropped silently: the field comes back in place with a `warnings` entry saying
  why it is empty. Warnings are already token-redacted.
- `isResolved` is **omitted** rather than `false` when the instance did not say.
  An unknown flag must not read as "not done", because that is what a default
  status proposal would act on.
- `archived` marks a value that still exists on old issues but is no longer
  offered: grey it out rather than hiding a mapping that is still in force.
- `valueMappableFields` is the subset of `gintrackFields` whose values can be
  mapped one by one. Render a value table only for those three.

Only a rejected token (401) fails the whole call — nothing else would succeed
either. A bundle that cannot be read for any other reason leaves its field in
the answer with a warning.

Both discovery endpoints are safe to call on a keystroke: the page is capped at
`limit` server-side, the client is cached per project so every call shares one
token-bucket limiter (five requests per second), and the UI debounces on top.
`gintrackFields` is the left-hand side of a field mapping, so the settings card
gets both halves from one call instead of hard-coding the list in the frontend.

Each call is bounded by a 15 s timeout inside the router's 30 s one and honours
request cancellation: closing the settings card cancels the call in flight.

#### The agent proxy (GIT-US-0049)

`gintrack serve --agent` mounts a relay to a local Pando AG-UI adapter
(`pando agui-serve`). The browser talks AG-UI to the companion; the companion
talks AG-UI to Pando. The point of the hop is that **the browser never holds the
Pando token and never learns the Pando origin**: the credential is injected as an
`Authorization: Bearer` header server-side, and the discovery document is
rewritten before it is forwarded.

The whole group sits inside the bearer-auth group of §5.1, so every request below
needs this run's companion token, and every one of them answers `401` without it.

```http
GET    /api/v1/agent/info                     # discovery, rewritten (see below)
GET    /api/v1/agent/health                   # upstream liveness probe
POST   /api/v1/agent/run?repo=<id>            # run a turn; SSE response
GET    /api/v1/agent/threads
GET    /api/v1/agent/threads/{id}/messages
GET    /api/v1/agent/threads/{id}/stream      # reattach to a live run; SSE
DELETE /api/v1/agent/threads/{id}
POST   /api/v1/agent/runs/{id}/cancel
```

Every route maps one-to-one onto the upstream route of the same name under
`agent.pando.path` (default `/api/v1/agui`), except two: `/health` probes the
adapter's unauthenticated `{path}/healthz` and is the only hop that carries no
token, and `/run` posts to `{path}/{agent}`, where `{agent}` is the configured
`agent.pando.agent` — the browser does not choose which agent runs.

**Routing.** The deployment is one `agui-serve` process per repository, so
`?repo=<id>` selects the upstream from the `agent.pando.repos` table, falling
back to the section-wide URL. An id that names neither a table row nor a mounted
repository is an `agent_repo_unknown` **404** — never a silent fallback to
another repository's agent.

**Streaming.** `POST /run` and `/threads/{id}/stream` answer
`Content-Type: text/event-stream` with `Cache-Control: no-cache` and
`X-Accel-Buffering: no`. Frames are Pando's own: bare `data: {json}` lines whose
discriminator is the JSON `type` field (`RUN_STARTED`, `TEXT_MESSAGE_CONTENT`,
`RUN_FINISHED`, `RUN_ERROR`, …), not the SSE `event:` field. The relay flushes
every write as it arrives (`httputil.ReverseProxy` with `FlushInterval: -1`),
both streaming routes are exempt from the 30 s request deadline of §5.3, and the
inbound request context is passed straight through: **closing the browser
connection cancels the upstream run**.

**Header hygiene.** The browser's `Origin`, `Referer`, `Cookie` and
`Authorization` headers are dropped before dialling Pando — Pando skips its CORS
check when no `Origin` is present, which is why its allow-list is deliberately
left empty — and no `X-Forwarded-*` header is added. The `repo` parameter and
anything that looks like a credential (`token`, `access_token`, `api_key`) are
stripped from the forwarded query string: the Pando token travels in a header, in
one direction, and never in a URL. The upstream's own CORS headers are removed
from the response, since they describe the Pando listener's policy and not this
one's.

**`GET /info`** is the one route that is not a byte-for-byte relay. Pando answers
with absolute URLs into its own origin, so the proxy rewrites every
`agents[].url` to the companion-relative `/api/v1/agent/run`, rewrites `path` to
`/api/v1/agent`, and **removes** every remaining string anywhere in the document
that parses as an absolute URL. Removing rather than rewriting is deliberate: a
key the browser never sees cannot leak an origin.

```http
GET /api/v1/agent/info
200
{
  "protocol": "ag-ui",
  "path": "/api/v1/agent",
  "capabilities": { "humanInTheLoop": true, "sharedState": true },
  "agents": [
    { "name": "backlog-assistant", "description": "…", "url": "/api/v1/agent/run" }
  ]
}
```

**Concurrency.** Pando enforces no limit of its own, so the companion does:
`agent.pando.maxRuns` (default 8) caps the runs in flight across the whole
proxy. Over the cap a run is refused immediately rather than queued:

```http
POST /api/v1/agent/run
503
Retry-After: 5
{ "code": "agent_busy", "status": 503, "detail": "This companion already has 8 agent runs in flight…" }
```

**Problem codes.** Every failure is an RFC 7807 document (§5.4), never a
half-written stream without a terminal frame:

| Code | Status | Meaning |
| --- | --- | --- |
| `not_implemented` | 501 | the feature is off, or no upstream is configured |
| `unauthorized` | 401 | the companion bearer token is missing or wrong |
| `agent_repo_unknown` | 404 | `?repo=` names no upstream and no mounted repository |
| `invalid_request` | 400 | the request body is above the 1 MiB cap |
| `agent_busy` | 503 | the in-flight run cap is reached; retry after `Retry-After` |
| `agent_upstream` | 502 | the adapter could not be reached or refused the hop |

An `agent_upstream` detail never echoes the upstream body or names the upstream
host: the browser is not supposed to learn either, and the full error goes to the
companion's log instead. No response, header or log line this feature writes ever
contains the Pando token.

The feature is **companion-only**. Browser-only mode has no server to proxy
through, reports `features.agent: false` and offers nothing to enable.

#### The CORS proxy (GIT-US-0042, docs/06 §6.3, ADR-025)

Browser-only mode cannot reach a git host directly, so the companion forwards the
three git smart-HTTP endpoints for it. The mount point is outside the versioned
prefix, because `isomorphic-git` builds the URL by concatenating the configured
proxy with the remote's host and path:

```http
GET  /cors-proxy/<host>/<repo path>/info/refs?service=git-upload-pack
GET  /cors-proxy/<host>/<repo path>/info/refs?service=git-receive-pack
POST /cors-proxy/<host>/<repo path>/git-upload-pack
POST /cors-proxy/<host>/<repo path>/git-receive-pack
```

Every request needs **both** an `Origin` the companion trusts and this run's
bearer token in **`X-Gintrack-Token`** — not `Authorization`, which is left free
for the git host's own credential and is the one header forwarded upstream. Its
refusals, all `application/problem+json`:

| Code | Status | When |
|---|---|---|
| `cors_proxy_disabled` | 501 | `git.corsProxy.enabled: false` |
| `cors_proxy_forbidden` | 403 | The `Origin` is absent or not trusted |
| `unauthorized` | 401 | No token, or the wrong one, in `X-Gintrack-Token` |
| `cors_proxy_bad_target` | 400 / 403 / 405 | Not one of the four requests above: a path outside the git surface, `info/refs` with no smart service, a relative segment, the wrong method |
| `cors_proxy_host_not_allowed` | 403 | The host is not a remote of a registered repository and is not in `git.corsProxy.allowedHosts` — including after a redirect |
| `cors_proxy_target_blocked` | 403 | The host resolves to a loopback, private, link-local, CGNAT, multicast or reserved address, or redirected off HTTPS |
| `cors_proxy_too_large` | 413 / 502 | More than 32 MiB of request body, or a response over 256 MiB |
| `cors_proxy_upstream_failed` | 502 | The git host did not answer, or redirected more than three times |

`GET /api/v1/git/cors-proxy` (bearer token, like the rest of the API) reports
what a client needs to adopt it:

```json
{ "enabled": true, "url": "http://127.0.0.1:7317/cors-proxy",
  "tokenHeader": "X-Gintrack-Token",
  "allowedHosts": ["github.com:443"],
  "maxRequestBytes": 33554432, "maxResponseBytes": 268435456 }
```

The `/git` routes are served since GIT-US-0020 and the `/sync` routes since GIT-US-0021,
which also adds `PATCH /api/v1/sync/settings` (`pullStrategy`, `pushOnSync`,
`maxPushRetries`). `/git/log` still answers `not_implemented`. The conflict resolver of
GIT-US-0022 serves the last two: `GET /api/v1/sync/conflicts/file` returns the three
versions of one conflicted path (base, ours and theirs, read from the index stages, with
the sides swapped back into the user's frame during a rebase) plus the merge the core
proposes — the per-field decisions, the body hunks and the canonical merged file — and
`POST /api/v1/sync/conflicts/resolve` applies a resolution, stages it and continues the
rebase or merge unless `"continue": false`. `resolution` is `ours` or `theirs` (keep one
whole side), `manual` (write `content` verbatim) or `merged` (the automatic merge plus the
`fields` and `hunks` overrides the user flipped). A resolution that would still leave a
conflicted hunk is refused with `validation_failed` before anything is written; a path that
is no longer conflicted answers `404 not_found`, because the integration moved on while
the resolver was open. Applying a resolution needs the system-git backend: go-git cannot
finish a rebase, so it answers `git_unsupported` (docs/06 §5.7).

```json
GET /api/v1/git/settings
200
{ "commitOnSave": false, "commitDebounceMs": 2000,
  "messageTemplate": "pmngr: update {{.ItemID}} \"{{.Title}}\"",
  "backend": "auto", "resolvedBackend": "system", "gitVersion": "2.45.2",
  "signCommits": false, "pending": 0, "persisted": false }
```

```json
PATCH /api/v1/git/settings
{"commitOnSave": true, "messageTemplate": "{{action}} {{id}}: {{title}}"}

200
{ …, "commitOnSave": true, "persisted": true }
```

An invalid template is refused with `400 invalid_request` **before** anything is
applied, so neither the running process nor the configuration file can end up
with a template that cannot render. A settings change first commits whatever is
already batched, so a new template never rewrites the message of an edit that
was already made.

```json
GET /api/v1/git/status
200
{ "repos": [
    { "repo": "acme-api", "path": "/home/jose/code/acme-api", "git": true,
      "backend": "system", "identity": "Jose <jose@digio.es>",
      "status": { "branch": "main", "clean": false, "staged": [],
                  "modified": ["docs/.pmngr/stories/ACME-US-0042-login-with-sso.md"],
                  "untracked": [] },
      "capabilities": { "backend": "system", "version": "2.45.2", "hooks": true,
                        "signing": true, "credentialHelpers": true,
                        "pathspecCommit": true } } ],
  "settings": { "commitOnSave": true, "pending": 1, … } }
```

A repository that is not a git working tree answers `"git": false` with a
`reason`, which is a normal state for a folder someone opened without cloning
it, not an error.

Commit-on-save is **debounced**, so a commit cannot be part of the write
response that triggered it. The write responses therefore carry no `commit`
field; the outcome arrives on the event stream as `git.commit`:

```json
{ "type": "git.commit", "seq": 412, "ts": "2026-09-04T10:31:55Z",
  "data": { "repo": "acme-api", "sha": "4e5f1c2…",
            "subject": "pmngr: update ACME-US-0042 \"Login with SSO\"",
            "empty": false,
            "paths": ["docs/.pmngr/stories/ACME-US-0042-login-with-sso.md"] } }
```

A failed commit publishes the same event with `code` and `message` instead of a
`sha` (`git_hook_failed`, `git_no_identity`, `git_commit_failed`). The write
itself already reached disk, so nothing is lost.

```json
GET /api/v1/git/log?item=ACME-T-0311&limit=3
200
{
  "item":"ACME-T-0311","repo":"ACME",
  "path":"docs/.pmngr/tasks/ACME-T-0311-wire-oidc-discovery-endpoint.md",
  "commits":[
    {"sha":"3c9a1f0","author":"Marta R <marta@acme.dev>","date":"2026-09-03T10:31:52Z",
     "subject":"docs(ACME): update ACME-T-0311 — Wire OIDC discovery endpoint",
     "trailers":{"Agent":"claude-code"},"insertions":3,"deletions":1},
    {"sha":"1b77de2","author":"Jose <jose@digio.es>","date":"2026-09-03T10:02:10Z",
     "subject":"docs(ACME): create ACME-T-0311","insertions":18,"deletions":0}
  ]
}
```

`POST /api/v1/sync/run` streams progress over the WebSocket as `sync.progress` events while
it works.

```json
202 Accepted
{"operationId":"sync-01J9Z7","repos":["ACME","AWEB","TEAM"],"startedAt":"2026-09-03T11:00:00Z"}
```

*As built (GIT-US-0021).* The call answers `200 OK` with the finished report rather than
`202` with a handle to poll: a sync of a backlog repository is a bounded operation, the web
client needs the `SyncResult` to render the panel, and cancelling the HTTP request cancels
the run through its context — which is the cancellation the story asks for. The response
still carries `operationId` and `startedAt`, and every `sync.progress` event carries the
same id, so nothing about the event contract changes when a resumable `202` form is added
for long-running multi-repository runs.

```json
POST /api/v1/sync/run   {"repos":["ACME"],"dryRun":true}
200
{ "operationId":"sync-3", "startedAt":"2026-09-04T11:00:00Z", "dryRun":true,
  "results":[
    { "repo":"ACME", "dryRun":true, "strategy":"rebase", "phase":"done",
      "before":{ "…":"SyncStatus" }, "after":{ "…":"SyncStatus" },
      "pulled":0, "pushed":0, "retries":0, "durationMs":184,
      "incoming":[{"sha":"9f2c1ab…","subject":"docs: teammate work"}],
      "outgoing":[{"sha":"4e5f1c2…","subject":"pmngr: update ACME-US-0042"}] }
  ] }
```

A failure is reported in the result, not as a problem document: the run is non-destructive
at every step, so `phase` (`done` | `conflicts` | `failed`), `code` and `message` say what
happened and what to do next. The codes are the `git_*` set of doc 06 §12:
`git_dirty_tree`, `git_no_remote`, `git_no_upstream`, `git_unexpected_branch`,
`git_operation_in_progress`, `git_auth_required`, `git_network_unavailable`,
`git_host_key_unverified`, `git_conflict`, `git_push_rejected`, `git_cancelled`,
plus `vcs_jujutsu_write_refused`, `vcs_jujutsu_unsupported` and `vcs_jujutsu_too_old`
(doc 06 §14). A repository managed with Jujutsu carries
`"vcs": {"kind":"jj","layout":"colocated"}` in the status and `"jujutsu": true`; since
GIT-US-0040 its `state` is the truthful one (`up_to_date`, `ahead`, `behind`, `diverged`,
`dirty`, `conflicted`), and the `jujutsu` state is now what a repository reports when no jj
binary is installed and the read-only git guard is driving it. Since GIT-US-0041 a write
goes through jj — commit, fetch, integrate, push, undo and conflict resolution — and each
row of `GET /api/v1/sync/status` carries `"writes": true|false`, which is what a surface
disables its write actions from. `vcs_jujutsu_write_refused` with `409 Conflict`, naming
the `jj` command to run instead, is now only what a jj repository with **no jj binary**
answers.
`POST /api/v1/sync/abort` undoes a half-finished rebase or merge and answers with the
repository's fresh status.

`git_auth_required` is the credential case (GIT-US-0023, doc 06 §8.1). The
companion never prompts for or stores a secret: it delegates to the user's
credential helper and ssh-agent, so the message names the repository, the remote,
the host and the command that fixes it, and distinguishes "no helper answered"
from "the host refused what the helper supplied". No credential ever reaches a
response, an event or a log line: URL userinfo, `token=`/`password=` parameters
and `Authorization` headers are redacted out of everything git prints before it
is reported.

### 5.6 WebSocket event stream

```
GET /api/v1/events        (Upgrade: websocket)
```

Subscription: after connecting, the client sends a `subscribe` frame; without one it
receives all events for the active workspace.

```json
{"op":"subscribe","topics":["item.changed","index.updated","sync.progress"],"projects":["ACME"]}
```

Every server frame shares an envelope:

```json
{
  "type": "item.changed",
  "id": "evt_01J9Z7B3",
  "ts": "2026-09-03T10:31:52.104Z",
  "workspace": "work",
  "seq": 4821,
  "data": { }
}
```

`seq` is a monotonic counter per server run; a client that reconnects may send
`{"op":"resume","seq":4700}` and the server replays buffered events (ring buffer of 1000)
or answers `{"type":"resume.gap"}` telling the client to re-fetch.

Event types and `data` schemas:

```jsonc
// file.changed — raw watcher event, debounced
{ "type":"file.changed",
  "data": { "repo":"ACME", "path":"docs/.pmngr/tasks/ACME-T-0311-….md",
            "op":"create|write|remove|rename", "size":1842,
            "isPmngr":true, "isKb":false } }

// index.updated — the indexer finished a pass
{ "type":"index.updated",
  "data": { "repo":"ACME", "full":false, "durationMs":18,
            "added":0, "updated":1, "removed":0,
            "counts":{"epics":12,"stories":58,"tasks":139,"milestones":6},
            "warnings":0 } }

// item.changed — a parsed item was created/updated/deleted/moved
{ "type":"item.changed",
  "data": { "repo":"ACME", "id":"ACME-T-0311", "op":"updated",
            "fields":["status","updated"],
            "status":{"from":"in_progress","to":"in_review"},
            "rev":"sha256:7ab0…d12",
            "origin":"api|watcher|mcp",
            "actor":"jose" } }

// git.commit — commit-on-save produced (or refused) a commit. Commits are
// debounced, so this is where a write learns what git did with it.
{ "type":"git.commit",
  "data": { "repo":"ACME", "sha":"4e5f1c2…",
            "subject":"pmngr: update ACME-T-0311 \"Wire OIDC discovery\"",
            "empty":false,
            "paths":["docs/.pmngr/tasks/ACME-T-0311-wire-oidc-discovery-endpoint.md"],
            "code":"", "message":"" } }

// sync.progress — long-running sync operation
{ "type":"sync.progress",
  "data": { "operationId":"sync-01J9Z7", "repo":"ACME",
            "phase":"fetch|commit|integrate|push|done|failed",
            "percent":60, "message":"rebasing 1 commit onto origin/main",
            "ahead":1, "behind":0 } }

// search.progress — the reindex of GIT-US-0091 (GIT-EP-0020 retired the export
// that used to report here). `operationId` is the reindex job id; `repo` is
// empty for the whole-workspace phases.
{ "type":"search.progress",
  "data": { "operationId":"reindex-1", "repo":"ACME",
            "phase":"code|kb|completed|failed",
            "percent":72, "done":324, "total":450,
            "message":"indexing the source tree" } }

// conflict.detected — a merge/rebase produced conflicts
{ "type":"conflict.detected",
  "data": { "operationId":"sync-01J9Z7", "repo":"TEAM",
            "paths":[".pmngr/boards/platform-kanban.md"],
            "kind":"content|order|delete-modify",
            "resolvable":"assisted",
            "operation":"rebase|merge", "status": { /* SyncStatus */ } } }

// conflict.resolved — one conflicted path was written, staged and (maybe) continued
{ "type":"conflict.resolved",
  "data": { "repo":"TEAM", "path":".pmngr/boards/platform-kanban.md",
            "resolution":"merged", "continued":true, "remaining":0 } }

// inbox.changed — one triage decision, or one submission filed straight into
// the queue. `pendingCount` is the whole queue, so a sidebar badge never needs
// a second call (§5.5, ADR-033).
{ "type":"inbox.changed",
  "data": { "repo":"acme-api", "project":"ACME", "id":"ACME-US-0101",
            "action":"created|accept|reject|snooze|duplicate",
            "pendingCount":8, "origin":"api", "requestId":"…" } }

// sprint.changed — a sprint whose scope moved: a close, or a transfer of its
// unfinished work. One `item.changed` is published per reference that actually
// moved, so a backlog view refreshes the items and not only the board. A dry
// run publishes neither (§5.5, GIT-US-0085).
{ "type":"sprint.changed",
  "data": { "sprint":"TEAM-S-0008", "board":"platform-scrum", "state":"active",
            "carried":3, "failed":1, "origin":"api", "requestId":"…" } }

// sync.job.* — the companion's background job engine (GIT-US-0074). Five
// topics share one payload shape:
//
//   sync.job.queued     a job entered the queue for the first time
//   sync.job.started    a worker picked it up
//   sync.job.progress   a coalesced count of how far a batch has got
//   sync.job.done       it succeeded, or was cancelled (`state` says which)
//   sync.job.failed     it exhausted its attempts or hit a terminal error
//
// The events of one job arrive in the order its state changed: a retried job
// is announced `queued` before the worker that picks it up announces
// `started`, even when the two happen at the same instant (GIT-US-0146).
{ "type":"sync.job.queued",
  "data": { "id":"job_000021", "kind":"youtrack.import", "key":"ACME",
            "state":"queued", "attempt":0, "processed":0, "total":20 } }

{ "type":"sync.job.progress",
  "data": { "id":"job_000021", "kind":"youtrack.import", "key":"ACME",
            "state":"done", "attempt":1, "processed":7, "total":20 } }

// A handler that knows how far it has got publishes the same topic with a
// wider payload: `jobId`, `done`, `total` and `currentId` — the unit it has
// just finished — plus `failed`, the per-unit failures it accumulated without
// failing the job. `id` and `processed` repeat `jobId` and `done` under the
// names the engine's own events use, so one client-side reader handles both
// sources without branching on which produced the frame. It is published once
// per batch (an import) or once per page (a knowledge-base job), never per
// item.
{ "type":"sync.job.progress",
  "data": { "jobId":"job_000021", "id":"job_000021", "kind":"youtrack.import",
            "key":"ACME", "state":"running", "done":40, "processed":40,
            "total":120, "currentId":"ACME-57", "failed":1 } }

// youtrack.kb.conflict — a knowledge-base page and the article it mirrors both
// changed since the last synchronization (GIT-US-0087). The page is left
// exactly as it is and the incoming content is written to `conflictPath`;
// there is no three-way merge and there is deliberately none. `direction` is
// the job that found it, `publish` or `pull`.
{ "type":"youtrack.kb.conflict",
  "data": { "project":"DEMO", "path":"docs/handbook/onboarding.md",
            "conflictPath":"docs/handbook/onboarding.conflict.md",
            "articleId":"ACME-A-3", "direction":"pull" } }

{ "type":"sync.job.failed",
  "data": { "id":"job_000022", "kind":"youtrack.import", "key":"ACME",
            "state":"failed", "attempt":5, "processed":8, "total":20,
            "error":"403 Forbidden", "errorClass":"terminal" } }

// tunnel.changed — the public tunnel moved between states. `data` is exactly
// the document GET /api/v1/tunnel returns, so a tab that was not the one to
// open the tunnel stops showing this workspace as private.
{ "type":"tunnel.changed",
  "data": { "supported":true, "provider":"cloudflare", "state":"connected",
            "url":"https://gentle-pine-mist-42.trycloudflare.com",
            "connections":4, "since":"2026-09-06T09:12:49Z",
            "error":"", "tokenConfigured":true } }
```

**`sync.job.*` payloads carry bookkeeping only** — ids, kinds, counts, a
message the engine already redacted — and never the job's payload, for the same
reason the queue journal does not: everything here reaches every connected
browser. `processed` and `total` count the *coalescing group* the job belongs to
(its kind and its key), which is the unit the engine hands to a handler as one
batch and the unit a progress bar renders.

`sync.job.progress` is **coalesced**: at most one every 500 ms per coalescing
group, carrying the running counts rather than one frame per item. A terminal
event — `done` or `failed` — is never throttled, so the last thing a client
hears about a job is always the truth about it. `sync.job.started` is part of
the contract and is published when the engine reports the queued → running
transition.

Even so, a client **may miss frames**. The hub's back-pressure policy is the one
in §6.2: a client that lets its 256-event buffer fill up is sent
`stream.overflow` and disconnected, and a long import can outrun a slow tab.
The stream is therefore a live hint, never the source of truth: after a
reconnect, or after any `stream.overflow` or `resume.gap`, a client
**reconciles from `GET /api/v1/sync/jobs`**, which answers the engine's own
consistent snapshot. A reconnect with `resume` is served the missed events from
the replay ring when they are still in it, exactly as for every other topic.

Client→server frames: `subscribe`, `unsubscribe`, `resume`, `ping`. The server sends a
protocol-level ping every 30 s and closes idle connections after two missed pongs.
Events caused by an API mutation carry `origin:"api"` plus the caller's request id so the
originating tab can skip its own optimistic-update echo.

---

## 6. Internal package design

```
cmd/gintrack/           cobra command tree, flag binding, config loading, exit codes
internal/config/        the configuration file: schema, defaults, precedence, registry
internal/server/        chi router, middleware, handlers, WS hub, embedded assets
internal/watcher/       fsnotify wrapper, debouncer, ignore matcher
internal/gitops/        go-git and system-git backends behind one interface
internal/core/          model, frontmatter, ids, index, query, validate, render, links
internal/mcp/           MCP server (see 08-mcp-server.md)
wasm/                   GOOS=js GOARCH=wasm entry point + JS bridge
```

### 6.1 `cmd/gintrack`

```
cmd/gintrack/
  main.go          // sets version vars via -ldflags, calls Execute(), exits with the code
  root.go          // persistent flags, configuration resolution, logger setup
  exit.go          // the exit codes of section 4 and the mapping from core errors
  workspace.go     // opens the registered repositories and indexes them
  mountfs.go       // mounts every repository of a workspace as one core.FS
  overlayfs.go     // in-memory overlay that makes --dry-run run the real write path
  format.go        // the small text helpers the commands share
  serve.go add.go ls.go rm.go index.go doctor.go config.go version.go completion.go
  item.go item_list.go item_get.go item_new.go item_edit.go item_move.go
  item_comment.go item_link.go
  output/          // table + json renderers shared by all commands
```

Rules: command files contain *no* business logic — they parse flags, build a
`core.Filter`/`core.ItemDraft`/`core.ItemPatch`, call the core or a server-side service, and
render. This keeps CLI and HTTP handlers behaviorally identical and makes both testable
against the same golden fixtures.

Two pieces of plumbing keep that rule affordable. `mountFS` presents the repositories of a
workspace side by side under their registration ids, so that one `core.Index` sorts,
filters and paginates across all of them instead of the command line stitching per-repository
results together; item paths are reported as `<repo>:<path inside the repository>`.
`overlayFS` buffers writes in memory over the same file system, so `--dry-run` executes the
real `core.FileStore` write path and then reports what it would have written, instead of a
second implementation that could disagree with the first.

### 6.2 `internal/server`

```go
package server

type Options struct {
    Bind        string
    Port        int
    Token       string
    Dev         bool
    OpenBrowser bool
    IdleTimeout time.Duration
}

type Server struct {
    opts    Options
    core    core.Workspace   // index + store for the active workspace
    git     gitops.Backend
    watch   *watcher.Watcher
    hub     *Hub             // WebSocket fan-out
    router  chi.Router
}

func New(opts Options, ws core.Workspace, g gitops.Backend) (*Server, error)
func (s *Server) Start(ctx context.Context) error   // blocks until ctx done
func (s *Server) Addr() string
```

Router composition (chi):

```go
r := chi.NewRouter()
r.Use(middleware.RequestID, middleware.RealIP, requestLogger(log), middleware.Recoverer)
r.Use(middleware.Timeout(30 * time.Second))
r.Use(corsMiddleware(allowedOrigins))
r.Use(securityHeaders)          // CSP, X-Content-Type-Options, Referrer-Policy
r.Route("/api/v1", func(api chi.Router) {
    api.Get("/health", h.Health)                    // unauthenticated
    api.Group(func(p chi.Router) {
        p.Use(bearerAuth(token))
        p.Use(rateLimit(600, time.Minute))          // per-connection token bucket
        p.Get("/capabilities", h.Capabilities)
        p.Route("/items", h.MountItems)
        p.Route("/boards", h.MountBoards)
        p.Route("/projects", h.MountProjects)
        p.Route("/sync", h.MountSync)
        p.Get("/search", h.Search)
        p.Get("/events", h.Events)                  // websocket upgrade
    })
})
r.Mount("/mcp", mcpHTTPHandler)                     // behind bearerAuth; 501 when disabled
r.Handle("/cors-proxy/*", corsProxyHandler)         // git smart-HTTP only; ADR-025
r.NotFound(spaHandler(webFS))                       // SPA fallback
```

Embedded assets and SPA fallback:

```go
//go:embed all:../../web/dist
var webDist embed.FS

// spaHandler serves web/dist. If the requested path exists it is served with
// long-lived cache headers for hashed assets (immutable) and no-cache for index.html.
// Anything else that is not under /api/ falls back to index.html so client-side
// routes (TanStack Router) survive a hard refresh.
func spaHandler(fsys fs.FS) http.HandlerFunc
```

Under the `noembed` build tag `webDist` is replaced by a tiny in-memory page explaining
that the UI was not built, so `go install` still yields a working API/MCP binary.

The WebSocket hub:

```go
type Hub struct {
    mu      sync.RWMutex
    clients map[*Client]struct{}
    ring    *ringBuffer   // last 1000 events for `resume`
    seq     atomic.Uint64
}

func (h *Hub) Publish(ev Event)                 // non-blocking; slow clients are dropped
func (h *Hub) Register(c *Client)
```

Back-pressure policy: each client has a 256-event buffered channel; when it overflows the
server sends `{"type":"stream.overflow"}` and closes, letting the client reconnect and
re-fetch rather than accumulating unbounded memory.

### 6.3 `internal/watcher`

```go
package watcher

type Event struct {
    Repo string
    Path string          // repo-relative, forward slashes
    Op   Op              // Create | Write | Remove | Rename | Chmod
    Time time.Time
}

type Options struct {
    Debounce   time.Duration   // default 250ms
    Ignore     []string        // gitignore-style globs
    Recursive  bool
    MaxWatches int             // guard against inotify exhaustion
}

type Watcher struct{ /* … */ }

func New(opts Options) (*Watcher, error)
func (w *Watcher) AddRepo(key, root string) error
func (w *Watcher) Events() <-chan []Event   // batched per debounce window
func (w *Watcher) Close() error
```

Implementation notes:

- fsnotify is not recursive on Linux/Windows: the watcher walks the tree and registers
  every directory under the docs folder, adding/removing watches as directories appear and
  disappear. `.git`, `node_modules` and the ignore list are skipped at walk time, which
  keeps the descriptor count on the order of hundreds, not thousands.
- **Debounce and coalescing**: events are accumulated per path in a 250 ms window; a
  `Write` followed by another `Write` collapses to one; `Create`+`Remove` for the same
  path within a window collapses to nothing. This absorbs the multi-event save patterns of
  editors (VS Code writes to a temp file then renames; JetBrains writes twice).
- **Atomic saves**: a rename into place appears as `Create` on the destination; the watcher
  reconciles by hashing content rather than trusting the op name.
- Every batch triggers an incremental index pass, which emits `index.updated` and any
  derived `item.changed` events.
- Watch failures are non-fatal: the watcher degrades to a 5-second polling scan of mtimes
  and logs a warning that `gintrack doctor` also reports.

### 6.4 `internal/gitops`

A `Backend` is bound to one working tree at construction, because that is what
the caller has — a mounted repository — and it removes a repository argument
from every call:

```go
package gitops

// Open binds a backend to a working tree; Kind is auto | go-git | system | jj.
// A Jujutsu working tree is bound to the jj backend whatever Kind says, in
// either layout (GIT-US-0040, doc 06 §14): the git backends read HEAD, which
// sits at @-, and the index, which jj keeps synchronized with @. The jj backend
// reads and writes through the jj binary (GIT-US-0041): jj commit -- <paths>
// plus a fast-forward jj bookmark move, jj git fetch, jj rebase, jj git push,
// jj undo and a resolution squashed into the conflicted commit. A jj older than 0.41 fails with
// vcs_jujutsu_too_old; with no jj binary a colocated repository falls back to
// the read-only guard of GIT-US-0038 and one whose git store lives inside .jj
// is refused with vcs_jujutsu_unsupported.
func Open(path string, opts Options) (Backend, error)

// DetectVCS reports git | jj | none, and the jj layout: colocated | internal.
func DetectVCS(path string) core.VCSInfo

// ResolveJujutsu locates the jj binary and reads `jj --version`. Every read of
// the backend carries --ignore-working-copy, because without it jj snapshots the
// working copy first — a write, and one a status poll must never make. Only the
// writes that have to see the disk (commit, rebase, squash, undo) drop it.
func ResolveJujutsu(binary string) (path, version string, err error)

// Every concept in this interface exists in git and in Jujutsu, so a jj
// backend can implement it without faking an index, a MERGE_HEAD or a branch
// (GIT-US-0039, ADR-022). Where the two differ, the difference is data the
// backend reports — Line.Kind, Integration.Undo/Resume, ConflictVersions.Markers
// — and never an assumption the caller makes.
type Backend interface {
    Name() string                                     // "go-git" | "system" | "jj"
    Path() string
    Capabilities() Capabilities
    Identity(ctx context.Context) (Identity, error)
    Status(ctx context.Context) (Status, error)
    Commit(ctx context.Context, req CommitRequest) (CommitResult, error)
    // Fetch, Integrate, Push, Undo, Resume and Commits are added by
    // GIT-US-0021; ConflictFile and ResolvePath by GIT-US-0022.
}

type CommitRequest struct {
    Paths      []string // repo-relative; the whole of the commit, nothing else
    Message    Message  // Subject + Body (the trailers)
    Author     Identity // empty -> resolved from the git configuration chain
    Sign       bool     // system backend only; go-git fails with git_unsupported
    AllowEmpty bool
}

type CommitResult struct {
    SHA     string
    Empty   bool // nothing had changed; a no-op write is not an error
    Subject string
    Author  Identity
    Paths   []string
}

type Capabilities struct {
    Backend, Version                                 string
    Hooks, Signing, CredentialHelpers, ScopedCommit  bool  // JSON: pathspecCommit
    VCS, VCSLayout                                   string
    Writes                                           bool
}

// Line is the current line of work and where publishing it sends it. It
// replaces the branch name plus detached flag of GIT-US-0021, so that a jj
// bookmark — which is not a branch and does not move on commit — can satisfy it.
type Line struct {
    Name       string    // JSON: branch. "main", or the VCS's name for an
                         // unnamed working copy ("HEAD", "@")
    Anonymous  bool      // JSON: detached. No named line of work at all
    Kind       LineKind  // JSON: lineKind. branch | bookmark | working-copy | ""
    PushTarget string    // JSON: pushTarget. What a publish updates on the remote
}

type Status struct {
    Line
    Clean                       bool
    Staged, Modified, Untracked []string  // Staged is empty in a VCS with no index
}

// Integration is an integration that has not settled, in terms both VCSs have.
// git: a half-finished rebase or merge, undone with --abort, carried forward
// with --continue. jj: nothing is ever half-finished, conflicts live inside the
// commits, `jj undo` takes the operation back and there is nothing to continue.
type Integration struct {
    Operation  string        // JSON: operation. "" | "rebase" | "merge" | …
    Unfinished bool          // JSON: unfinished. Nothing else may run yet
    Undo       UndoMethod    // JSON: undo. "abort" | "operation_log" | ""
    Resume     ResumeMethod  // JSON: resume. "continue" | ""
}

// SyncStatus is the status indicator: everything a repository row shows.
type SyncStatus struct {
    Line                                            // branch, detached, lineKind, pushTarget
    Integration                                     // operation, unfinished, undo, resume
    Remote, RemoteURL, Upstream         string      // RemoteURL is credential-free
    Clean, Tracked                      bool
    Dirty                               []string
    Ahead, Behind                       int
    Conflicted                          []Conflict
    Jujutsu                             bool
    State                               State       // up_to_date | ahead | behind |
}                                                   // diverged | dirty | conflicted | …

// The sync half of the Backend interface (GIT-US-0021, generalized by
// GIT-US-0039).
SyncStatus(ctx) (SyncStatus, error)
Fetch(ctx, FetchRequest) (FetchResult, error)
Integrate(ctx, IntegrateRequest) (IntegrateResult, error)   // rebase or merge
Push(ctx, PushRequest) (PushResult, error)                  // req.Target, not a branch
Undo(ctx) error                                             // git: --abort; jj: jj undo
Resume(ctx) (IntegrateResult, error)                        // git: --continue; jj: nothing
Commits(ctx, LogRequest) ([]Commit, error)                  // dry-run previews

// The structured conflict surface (GIT-US-0022, doc 06 section 5.7).
ConflictFile(ctx, path string) (ConflictVersions, error)    // the three sides, however
                                                            // the backend produces them
ResolvePath(ctx, ResolveRequest) (ResolveResult, error)     // record, then carry forward

// The pipeline over them: preflight, fetch, integrate, push, with the
// non-fast-forward retry ladder of doc 06 section 4.2.
func Sync(ctx context.Context, b Backend, opts SyncOptions) (SyncResult, error)

// Committer batches writes so one logical edit is one commit.
func NewCommitter(opts CommitterOptions) *Committer
func (c *Committer) Enqueue(change Change)
func (c *Committer) Flush(ctx context.Context) []Outcome
func (c *Committer) Close(ctx context.Context) []Outcome
func (c *Committer) Pending() int
```

Two implementations (`goGitBackend`, `systemBackend`); `auto` picks between them
at construction and falls back to go-git when no usable `git` is on `PATH`.

Failures carry a machine code that the API and the web provider pass through
unchanged: `git_not_a_repository`, `git_no_identity`, `git_hook_failed`,
`git_commit_failed`, `git_unsupported`, `git_template_invalid`. Credentials: the system backend inherits credential helpers, SSH agent and
`GIT_ASKPASS`; the go-git backend supports SSH agent and token-in-URL only, and reports
`git_auth_failed` with an actionable message when it cannot authenticate. The
`git.backend` setting exists precisely because these differ.

Commit message templating uses `text/template` against the context defined in
`06-git-sync.md` (`.ItemID`, `.Title`, `.Type`, `.Status`, `.PrevStatus`, `.ProjectKey`,
`.Board`, `.Action`, `.Count`, `.User`, `.Date`). Commits also carry the machine-readable
trailers specified there (`Item:`, `Type:`, `Status:`, `Tool:`), plus `Agent:` when the
change originated from the MCP server. Every placeholder also has a short
lowercase spelling (`{{action}} {{id}}: {{title}}`), bound as a niladic template
function over the same fields, so both forms render identically.

### 6.5 `internal/core` public interfaces

The shared core is the only place that understands the on-disk format. It is compiled
natively for the CLI and to WASM for the browser, so it must not import anything
OS-specific: all file access goes through an injected filesystem abstraction.

```go
package core

// FS is the seam that lets the same code run over os.DirFS natively and over
// File System Access API handles in the browser (via the JS bridge).
type FS interface {
    ReadFile(path string) ([]byte, error)
    WriteFile(path string, data []byte) error
    Remove(path string) error
    Rename(old, new string) error
    Stat(path string) (FileInfo, error)
    ReadDir(path string) ([]DirEntry, error)
    MkdirAll(path string) error
}

// Store is CRUD over item files. It never mutates the index directly; the Index
// observes the Store's change notifications.
type Store interface {
    Get(ctx context.Context, id ItemID) (*Item, error)
    GetRaw(ctx context.Context, id ItemID) (frontmatter []byte, body []byte, rev Rev, err error)
    Create(ctx context.Context, draft ItemDraft) (*Item, error)
    Update(ctx context.Context, id ItemID, patch ItemPatch, expected Rev) (*Item, error)
    Delete(ctx context.Context, id ItemID, expected Rev) error
    Move(ctx context.Context, id ItemID, status Status, expected Rev) (*Item, error)
    AddComment(ctx context.Context, id ItemID, c CommentDraft) (*Comment, error)
    ListComments(ctx context.Context, id ItemID) ([]Comment, error)
    ReadPage(ctx context.Context, project ProjectKey, path string) (*Page, error)
    WritePage(ctx context.Context, project ProjectKey, path string, content []byte, expected Rev) (*Page, error)
}

// Index is the in-memory (and cacheable) projection of the repositories.
type Index interface {
    Build(ctx context.Context, full bool) (IndexStats, error)
    ApplyFileEvents(ctx context.Context, events []FileEvent) (IndexDelta, error)
    Snapshot() Snapshot                    // the whole index, for the local cache
    Load(snap Snapshot) error
    // ProjectSnapshot builds the committed, reduced form of one project:
    // .pmngr/index/<KEY>.json in the team repository (doc 04 §6).
    ProjectSnapshot(key ProjectKey, opts ProjectSnapshotOptions) (ProjectSnapshot, error)
    LinkGraph() Graph                      // parent/child, typed links, wikilinks, backlinks
    Stats() IndexStats
    Warnings() []Warning
}

// The reader side of the committed snapshots is a set of plain functions, because
// it needs no state beyond the files it is given (doc 04 §§6 and 7):
//
//   func ReadSnapshots(fs FS, teamDir string, keys []ProjectKey,
//       policy SnapshotPolicy, now time.Time) *SnapshotSet
//   func (s *SnapshotSet) Item(key ProjectKey, id ItemID) (ProjectSnapshotItem, bool)
//   func (s *SnapshotSet) Info(key ProjectKey) SnapshotInfo   // age, staleness, errors
//   func SameSnapshotContent(a, b ProjectSnapshot) bool       // ADR-014
//   func (p TeamProject) FileURL(path string) string          // doc 04 §7.3
//
// BuildBoardView takes the set on its BoardInput and renders the cards of the
// projects nobody cloned from it.

// Query is the read side used by the CLI, HTTP handlers, MCP tools and the WASM bridge.
type Query interface {
    Items(ctx context.Context, f Filter) (Page[Item], error)
    Item(ctx context.Context, id ItemID) (*Item, error)
    Children(ctx context.Context, id ItemID) ([]Item, error)
    Board(ctx context.Context, slug string, resolve bool) (*Board, error)
    Sprint(ctx context.Context, id string) (*Sprint, error)
    Search(ctx context.Context, q SearchRequest) (SearchResult, error)
    KBTree(ctx context.Context, scope Scope) (*TreeNode, error)
}

// IDAllocator hands out the next per-project id, collision-safe under concurrent writers.
type IDAllocator interface {
    // Next reserves the next id for a type, e.g. ACME-US-0043.
    Next(ctx context.Context, project ProjectKey, t ItemType) (ItemID, error)
    // Peek returns the next id without reserving it.
    Peek(ctx context.Context, project ProjectKey, t ItemType) (ItemID, error)
    // Reconcile scans the tree and repairs the counter after a git merge brought in
    // ids allocated by someone else.
    Reconcile(ctx context.Context, project ProjectKey) (Reconciliation, error)
}

// Validator enforces the schema, the project workflow, and referential integrity.
type Validator interface {
    ValidateItem(ctx context.Context, it *Item) []Violation
    ValidateTransition(ctx context.Context, from, to Status, p *Project) error
    ValidateProject(ctx context.Context, p *Project) []Violation
    ValidateBoard(ctx context.Context, b *Board) []Violation
    ValidateWorkspace(ctx context.Context) []Violation   // powers `gintrack doctor`
}

// Rev is the optimistic-concurrency token: a content hash of the canonical file bytes.
type Rev string
func ComputeRev(fileBytes []byte) Rev   // "sha256:" + hex[:12] of the normalized content
```

ID allocation deserves emphasis because it is the one place a distributed, git-synced tool
can corrupt itself. The allocator never relies solely on a counter in `project.yaml`: it
takes the maximum of (counter, highest id found in the index) and, after writing, verifies
no file with the same id appeared (a lost-update guard against two `gintrack` processes and
against a concurrent `git pull`). `Reconcile` is what `gintrack doctor --renumber` calls.

### 6.6 WASM build of `internal/core`

```
wasm/
  main_js.go        // //go:build js && wasm — registers exported functions, blocks forever
  bridge.go         // Go<->JS value marshalling helpers
  fs_js.go          // core.FS implemented over File System Access API handles
  ../web/src/core/  // TypeScript wrapper: worker.ts, api.ts, types.ts
```

`main_js.go` exports a namespace on `globalThis.gintrack`:

```go
//go:build js && wasm

func main() {
    ns := js.Global().Get("Object").New()
    ns.Set("version",      js.FuncOf(version))       // () -> {version, schema}
    ns.Set("mountFS",      js.FuncOf(mountFS))       // (handles) -> repoId
    ns.Set("indexBuild",   js.FuncOf(indexBuild))    // (repoId, full) -> Promise<IndexStats>
    ns.Set("indexApply",   js.FuncOf(indexApply))    // (repoId, events[]) -> Promise<IndexDelta>
    ns.Set("itemList",     js.FuncOf(itemList))      // (filterJSON) -> Promise<PageJSON>
    ns.Set("itemGet",      js.FuncOf(itemGet))       // (id, opts) -> Promise<ItemJSON>
    ns.Set("itemCreate",   js.FuncOf(itemCreate))    // (draftJSON) -> Promise<ItemJSON>
    ns.Set("itemUpdate",   js.FuncOf(itemUpdate))    // (id, patchJSON, rev) -> Promise<ItemJSON>
    ns.Set("itemMove",     js.FuncOf(itemMove))
    ns.Set("commentAdd",   js.FuncOf(commentAdd))
    ns.Set("boardGet",     js.FuncOf(boardGet))
    ns.Set("search",       js.FuncOf(search))
    ns.Set("kbTree",       js.FuncOf(kbTree))
    ns.Set("kbRead",       js.FuncOf(kbRead))
    ns.Set("validate",     js.FuncOf(validate))      // doctor-equivalent
    ns.Set("snapshot",     js.FuncOf(snapshot))      // -> index JSON for IndexedDB cache
    ns.Set("loadSnapshot", js.FuncOf(loadSnapshot))
    js.Global().Set("gintrack", ns)
    select {}   // keep the Go runtime alive for the worker's lifetime
}
```

Bridge conventions:

- Every exported function is **async from JS**: it returns a `Promise` resolved from a
  goroutine, so long index passes never block the worker's event loop.
- Arguments and results cross as **JSON strings**, not `js.Value` graphs. This keeps
  marshalling cost predictable, avoids reference leaks, and means the TypeScript types in
  `web/src/core/types.ts` are generated from the same Go structs that serve the REST API.
- Errors reject the promise with `{code, message, details}` mirroring the RFC 7807 `code`
  catalog, so UI error handling is identical in both modes.
- The module runs in a **Web Worker**; the main thread talks to it with Comlink-style
  message passing. `wasm_exec.js` from the Go distribution is vendored into `web/public`
  and pinned to the Go version used in CI (a mismatch is a hard error, checked by `make wasm`).
- `core.wasm` is roughly 6–9 MB before compression, ~2–3 MB with Brotli; it is fetched once
  and cached by the service worker. `make wasm` builds with `-ldflags="-s -w"`; TinyGo is
  evaluated but not required because reflection-heavy YAML handling is involved.
- The WASM `FS` implementation wraps `FileSystemDirectoryHandle`/`FileSystemFileHandle`.
  Writes use `createWritable()` (atomic replace). When only `<input webkitdirectory>` is
  available the FS is mounted read-only and every mutating export rejects with
  `forbidden: read-only mount`.
- One implementation, no drift: the API handlers and the WASM exports are thin adapters
  over `Store`/`Query`, and a shared golden-fixture test suite runs against both
  (`go test ./internal/core/...` natively and `GOOS=js GOARCH=wasm go test` under
  `wasmbrowsertest`).

### 6.7 Requirement methods of the CoreApi contract

`internal/vault` serves four methods that treat one requirement of a spec (ADR-037, doc 03 §21)
as a unit of its own. They are part of the `CoreApi` contract of `web/src/core-bridge/api.ts`, so
browser-only mode reaches them through the WASM module's `gintrackCore.call` and a workspace
routes them to the repository that owns the spec (by `ref`, or by `spec`). The MCP tools
`list_requirements`, `create_requirement`, `update_requirement` and `get_item` on a requirement
ref are shims over them (doc 08 §4.20); no REST route or web screen uses them yet.

| Method | Params | Result |
|---|---|---|
| `requirement.list` | `{project?, spec?, status?[], q?, text?, includeDeleted?}` | `{requirements: Requirement[], total}` — one row per requirement, sorted by spec and body order; `text`, `statement` and `scenarios` only with `text: true` |
| `requirement.get` | `{ref}` | `{requirement: Requirement, specRev}` |
| `requirement.create` | `{spec, title, text?, status?, trace?, links?}` | `{requirement, specRev, writes, schemaUpgraded?}` |
| `requirement.update` | `{ref, patch, rev}` | `{requirement, specRev, writes, schemaUpgraded?}` |

`ref` is `<SPEC-ID>.R<n>`, optionally `<KEY>/`-qualified. A `Requirement` carries `ref`, `spec`,
`project`, `path`, `anchor`, `line`, `title`, `status` (`statusImplicit: true` when the entry has
none and the workflow's initial status stands in), `text` (the block below its heading, without
the blank lines around it), `statement`, `scenarios`, `trace`, `verified`, `links`, and **two
hashes**:

- **`rev` — the requirement rev** (doc 03 R-REQ-REV-2): the block bytes plus the canonical JSON of
  the requirement's `requirements:` entry. It is the **write token**: `requirement.update` quotes
  it, and nothing else. It changes on any change to the block or the entry, and on nothing else.
- **`blockRev` — the block rev** (R-REQ-REV-1): the block bytes alone, the value a `verified.rev`
  stamp records. A status or trace change leaves it alone; it is never accepted as a write token.
- `specRev` is the spec's file rev (R-REV-1), for a spec-level `item.update` that follows.

**`requirement.update`.** `patch` is sparse: `title` rewrites the heading (with the em dash),
`text` replaces the block below it, and `status`, `trace`, `verified` and `links` change the
requirement's entry; `unset` clears `trace`, `verified` or `links`. Nothing else in the file
changes except `updated`, so no other requirement's `rev` moves. The rules:

- `rev` is required; without it the call fails with `precondition_required` (R-REV-3b).
  `rev: "*"` is the explicit, unsafe waiver.
- A `rev` that is no longer the requirement's current rev fails with `stale_revision`, carrying
  `currentRev` (the requirement rev now) and `conflicts[]` over `text` (named, never quoted),
  `title`, `status`, `trace`, `verified` and `links`, judged against the file as it is now
  (R-REV-3a). An empty `conflicts[]` means the change had already been made. A concurrent write to
  **another** requirement of the same spec, or to the spec's own front matter, does not make the
  rev stale.
- A status change follows the project workflow exactly like `item.move`
  (`workflow_transition_denied`), and the status is always materialized in the entry.
- `text` may not contain a level 1–3 ATX heading outside a fence, nor leave a fence open (either
  would end or swallow blocks); a title is one line of 1–200 characters. Both fail as
  `validation_failed` (`E-REQ-FIELD`), like any other error-severity finding of the spec.

**`requirement.create`** appends a block `### <REF> — <title>` after the last requirement block,
else at the end of the `## Requirements` section, else in a new `## Requirements` section at the
end of the body, and materializes the entry with `status` (default: `workflow.initial`). `R<n>` is
max + 1 over the spec's block headings, its `requirements:` keys and every inbound ref in the
project index (R-REQ-5), so a removed number is never handed out again. A create needs no rev.

Both writes go through the same canonical serializer as `item.update` (front-matter key order,
`requirements:` keys in numeric order, unknown keys preserved), refuse a project gated read-only
(`read_only`, R-EVO-2) and report `schemaUpgraded` like `item.update` does, although a requirement
write only ever touches a spec that already exists.

**Applying a Spec Delta on done (`GIT-US-0110`).** `item.move`, and `item.update` whose patch
sets a status, apply the item's `## Spec Delta` (doc 03 §21.8, R-DELTA-12 to R-DELTA-16) when
they move a story or a task into a `done`-category status; so does a board card move, which goes
through the same store write. The story, every spec it changes and, when the move raises the
schema, `project.yaml` are validated together and written as one staged transaction, all of them
in `writes`. The result then carries:

```json
"specDelta": {
  "item": "ACME-US-0042",
  "added": [{"ref": "ACME-SP-0003.R5", "supersedes": "ACME-SP-0001.R2"}],
  "modified": ["ACME-SP-0003.R2"],
  "removed": ["ACME-SP-0001.R2"],
  "unchanged": [],
  "specs": ["ACME-SP-0003", "ACME-SP-0001"]
}
```

`specs` lists the spec files written; `unchanged` the refs an operation named that already held
what it asks for (a re-applied delta). A delta that cannot be applied refuses the whole move and
changes nothing: `conflict` for a missing spec or block, a spec of another project, a requirement
modified twice or modified after its removal, or a workflow with no `cancelled`-category status;
`stale_revision` for a stale item `rev` or a spec edited on disk after it was read;
`validation_failed` for any error-severity finding, including `LINT-REQ-*` at `specs.lint: error`.
`item.move` also reports `schemaUpgraded` now. The `verified` stamp written on done (R-REQ-11a) is
not written yet: `GIT-US-0116` installs it through the same transition (`FileStore.DoneHook`).

**Search.** `search` with `requirements: true` adds one hit of `kind: "requirement"` per matching
requirement, with `id` set to the ref, `path` to the spec's file, and `spec` and `status` set; it
is off by default so a client that only opens items and pages never receives one. The web app's
`search` params and `SearchHit` type do not declare it yet; the requirement screens (epic
`GIT-EP-0027`) add it together with a way to open such a hit.

**The requirement trace (`GIT-US-0114`).** Two more methods answer from the requirement trace
graph of doc 03 §21.7. The graph needs the native marker scanner, so the vault reaches it through
a host seam, `Vault.SetRequirementTracer` (`vault.RequirementTracer`, implemented by
`internal/trace`'s `Engine`); the companion installs one per repository, and a browser-only
session installs none, where both methods fail with `unavailable` — never an empty trace.
`gintrack mcp` over stdio installs the same three seams of this section (trace, coverage,
impact) through the companion's constructor, `server.InstallTraceSeams` (`GIT-US-0124`), reading
test results from the cache directory `gintrack spec ingest` writes to; it wires no Pando, so its
impact tiers 2 and 3 report `unavailable`.

| Method | Params | Result |
|---|---|---|
| `trace.requirement` | `{ref}` | `{trace: {ref, project, code: TraceEdge[], tests: TraceEdge[], work: {id, kind, wholeSpec?}[], broken?: TraceBroken[]}}` |
| `trace.touching` | `{vaultId?, changes: {path, oldPath?, lines?: {start, count}[], symbols?}[]}` | `{hits: (TraceEdge & {reason, changed?})[]}` |

A `TraceEdge` is `{ref, role: "code" \| "tests", path, symbol?, sources: ("marker" \| "trace")[],
lines?}`; `broken` lists the `W-TRACE-BROKEN` warnings of the requirement's `trace:` entries.
`trace.touching` rescans the changed paths first and reports each hit's `reason` (`file`,
`symbol`, `marker`, `renamed`, `removed`). Without a code watcher the companion's engine also
rescans the whole working tree on the first call after 30 s. Both answers are sorted and derived:
nothing is written.

**Coverage and the verification stamp (`GIT-US-0116`).** Two more methods answer from the
coverage backend of doc 03 §21.6 (R-REQ-12a), a second host seam, `Vault.SetRequirementCoverage`
(`vault.RequirementCoverage`, implemented by `internal/trace`'s `Coverage` over the trace engine,
the test-result cache of `gintrack spec ingest` and the repository's git history). The companion
installs one per repository; a browser-only session installs none, where both fail with
`unavailable`.

| Method | Params | Result |
|---|---|---|
| `coverage.list` | `{project?, spec?, refs?: string[], status?: ("untested" \| "passing" \| "failing" \| "suspect")[]}` | `{coverage: {ref, status, reasons?: string[], tests?: {test, result}[]}[], total}` |
| `requirement.stamp` | `{refs?: string[], spec?, by, rev?}` | `{stamped: {ref, verified: {rev, commit, at, by}}[], unstamped: {ref, reason}[], writes}` |

A coverage row is deliberately compact — about 40 tokens with one linked test, plus about 15 per
further test — because the MCP tools of `GIT-US-0123`/`GIT-US-0124` return it as is. `reasons` are
the short codes of doc 03 §21.6; a test's `result` is `pass`, `fail`, `skip` or `missing`.
`requirement.stamp` is what `gintrack spec verify --commit` (`GIT-US-0125`) sends after running
the tests: it writes `requirements.R<n>.verified` for each named requirement whose evidence
passed at one commit, through `UpdateRequirement` under the requirement rev, and lists every
other one in `unstamped` with its reason; `by` is recorded when the evidence names nobody. It
never refuses a requirement and never writes anything but `verified`. The done-transition stamp
of `GIT-US-0110` calls the same code from inside its own write. The coverage state itself is
never written. With `rev` (`GIT-US-0124`, the MCP `verify_requirement`) the call names exactly
one ref — `invalid_request` otherwise, and for `rev: "*"` — and stamps it only under that
requirement rev: a rev that moved fails with `stale_revision`, `currentRev` and a `verified`
conflict, instead of being listed as `stale`. A requirement whose evidence allows no stamp is still
listed in `unstamped`: nothing was written, so there was nothing to lock.

**Requirement impact (`GIT-US-0119`).** One more method resolves the requirements a diff affects
(doc 03 §21.11), through a third host seam, `Vault.SetRequirementImpact`
(`vault.RequirementImpact`, implemented by `internal/impact`'s `Resolver` over the trace engine,
the coverage backend, `gitops.ChangedFiles` and, for tiers 2 and 3, Pando). The companion
installs one per repository that has git history; a browser-only session, or a repository
without history, installs none, where the method fails with `unavailable`.

| Method | Params | Result |
|---|---|---|
| `impact.query` | `{base?, head?, story?, title?, tiers?: (1 \| 2 \| 3)[], depth?, limit?}` | `{impact: {base, head?, files, symbols, tiers: {tier, status, hits, truncated?, message?}[], hits: {ref, title, tier, candidate?, score?, status?, suspect?, reasons, pending?}[]}}` |

`base` defaults to `HEAD` and an empty `head` is the working tree, so `{}` asks what the
uncommitted changes affect. `status` of a tier is `ok`, `unavailable` (no Pando, or Pando not
answering), `error` or `skipped`; tier 1 always answers when the method does. An unknown revision
is `invalid_request`, an unknown `story` `not_found`; `depth` is at most 5 and `limit` at most 50.
A hit's `reasons` are short codes — `symbol:src/alloc.go#NextID`, `delta:ACME-US-0001`,
`call:src/format.go#Format calls NextID d1`, `semantic` — and its `status` is the coverage state
of `coverage.list`. The answer is compact: the three-requirement fixture of `internal/impact`
is about 750 bytes (≈ 190 tokens); the token-budgeted report of `GIT-US-0120` renders from it.

**Impact report (`GIT-US-0120`).** `impact.report` runs the same query and renders one page of
the ranked, token-budgeted report agents read (doc 03 §21.11, R-IMP-8 to R-IMP-10). It is the
one renderer the MCP tool `spec_impact` (`GIT-US-0124`), `gintrack spec impact`
(`GIT-US-0125`) and the HTTP API share.

| Method | Params | Result |
|---|---|---|
| `impact.report` | `impact.query`'s params and `{budget?, cursor?, format?: "json" \| "text"}` | `{report: {base, head?, files, symbols, tiers?, hits?, text?, total, offset?, truncated?, nextCursor?, budget, tokens}}` |

`budget` is in tokens (default 1500, at most 20000), estimated as `ceil(bytes / 3)` of the
report's compact JSON. The `json` form (the default) carries `tiers` and the ranked `hits`; the
`text` form carries `text`, one line per requirement, with the tier status in its second line.
Hits are ranked failing, suspect, then tier (candidates last), then ref; the lowest-ranked are
cut, `truncated` counts them and `nextCursor` — passed back as `cursor` with the query unchanged —
fetches the rest. A cursor from another result, a budget out of range or an unknown `format` is
`invalid_request`; browser-only mode answers `unavailable`.

**Spec context (`GIT-US-0123`).** `spec.context` renders the context of one story or task — step 1
of the agent loop — through the same token estimator, budget bounds and cursor shape as
`impact.report` (the shared helpers of `internal/core/budget.go`). It is the method behind the MCP
tool `spec_context` (doc 08 §4.22).

| Method | Params | Result |
|---|---|---|
| `spec.context` | `{id, budget?, cursor?, format?: "json" \| "text"}` | `{report: {item, title, coverage, detail, requirements?, pages?, morePages?, text?, total, offset?, truncated?, nextCursor?, budget, tokens}}` |

`requirements` are those the item's `implements` and `modifies` links name (a link to a whole spec
names each of its requirements, `wholeSpec: true`) plus the targets of its unapplied Spec Delta
(doc 03 §21.8) — MODIFIED and REMOVED refs, `Supersedes:` targets and ADDED blocks not yet
numbered, whose `ref` is the spec they will join — ordered by spec, then number, unnumbered
blocks last. Each carries `via` (`implements`, `modifies`, `delta-added`, `delta-modified`,
`delta-removed`, `delta-superseded`), a one-line `statement` (clipped at 160 characters), its
`scenarios` and, from the coverage seam, `status` and `reasons`; `proposed: true` marks a statement
and scenarios read from the delta's proposal rather than the spec. `pages` are the
knowledge-base pages the item and those specs wikilink (at most ten, `morePages` counts the rest),
on the first page only. The method reads only the index, so it answers in browser-only mode too,
with `coverage: "unavailable"` and no `status`. Degradation is fixed: scenario steps are kept only
when every remaining requirement fits with them (`detail: "steps"`); otherwise the page drops to
scenario names (`detail: "names"`) and then cuts the list, with `truncated` and `nextCursor`. An
unknown item is `not_found`; a foreign cursor, a budget out of range or an unknown `format` is
`invalid_request`.

---

## 7. Cross-platform concerns

### 7.1 Path handling

- **Internal representation is always forward-slash, repo-relative.** Item `path`, KB
  `path`, board refs and API payloads never contain a drive letter or a backslash. The
  boundary conversion happens once, in the `FS` implementation
  (`filepath.FromSlash`/`ToSlash`).
- Absolute paths appear only in config and in `gintrack ls`. `~` is expanded at load time;
  the config keeps the user's literal form so it stays portable across machines.
- Paths are cleaned and constrained: any request path is rejected if, after `path.Clean`,
  it escapes the repo's docs folder (`..` traversal, absolute paths, Windows device names
  like `CON`, `NUL`, `AUX`, or an alternate data stream `file.md:zone`).
- Windows `MAX_PATH` (260 chars) still bites tools that lack long-path support. Slugs are
  truncated to 60 characters and `gintrack doctor` warns when a generated path exceeds 240
  characters on Windows.
- Symlinks inside the docs folder are followed for reads but never written through; a
  symlink escaping the repo root is skipped with a warning.

### 7.2 File watching limits

| Platform | Mechanism           | Practical limits and mitigations                                                                                                                                              |
| -------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Linux    | inotify             | `fs.inotify.max_user_watches` (often 8k–128k) and `max_user_instances`. One watch per directory. We watch only the documentation folders the vault indexes — plus the repository root and its first-level directories, shallowly, so a backlog appearing later is still seen — skip ignores, cap at `MaxWatches` (default 8192) and degrade to polling with a clear warning plus the `sysctl fs.inotify.max_user_watches=524288` hint. |
| macOS    | FSEvents (via kqueue in fsnotify) | fsnotify uses kqueue, which needs an **open file descriptor per watched file/directory**; `ulimit -n` (default 256 in some shells) is hit quickly. We watch directories only, raise `RLIMIT_NOFILE` at startup where permitted, and cap watches. Coalescing latency means events can arrive in bursts. |
| Windows  | ReadDirectoryChangesW | Recursive watching is supported natively but fsnotify registers per directory; the buffer can overflow under mass changes (e.g. a `git checkout` of a large branch), which surfaces as a lost-events error. On overflow we schedule a full re-index instead of trusting the delta. |

Common mitigations: after any git operation the watcher is paused and a full incremental
re-index is scheduled on completion, since `git checkout`/`rebase` can rewrite thousands of
files faster than the event pipeline can drain. Network filesystems (NFS, SMB, virtualised
Docker/WSL mounts) frequently deliver no events at all; `gintrack doctor` detects a
non-local filesystem and recommends `--watch=false` plus periodic `gintrack index`.
Whatever the watcher misses, a single-file read (`item.get`, `comment.list`, `kb.page`)
re-checks size and mtime of the files it returns and re-indexes the stale ones first
(docs/06 §9.2), so an opened task is never older than the file on disk.

### 7.3 Line endings

- All files are written with **LF** endings and a trailing newline. This keeps `rev` hashes
  stable across platforms and avoids whole-file diffs.
- The repository template ships a `.gitattributes` containing `*.md text eol=lf` and
  `*.yaml text eol=lf`; `gintrack doctor --fix` offers to add it.
- On read, CRLF is tolerated and normalized before parsing and before hashing, so a file
  checked out with `core.autocrlf=true` on Windows does not produce a phantom `rev`
  mismatch. `rev` is computed over the **normalized** bytes for exactly this reason.
- The Markdown renderer preserves hard line breaks; normalization touches only the EOL
  sequence, never trailing whitespace inside a line.

### 7.4 Case sensitivity and Unicode

- macOS (APFS default) and Windows (NTFS) are case-**insensitive but case-preserving**;
  Linux is case-sensitive. `ACME-US-0042-Login.md` and `acme-us-0042-login.md` are the same
  file on two of three platforms. Slugs are therefore generated lowercase, and the indexer
  keys by a case-folded path while preserving the on-disk name for display.
- Duplicate detection is done on the case-folded path so a repository that accidentally
  carries both spellings (created on Linux, cloned on macOS) is reported by `doctor` as a
  hard error rather than silently losing an item.
- macOS stores filenames in NFD, Linux and Windows usually in NFC. Filenames are normalized
  to **NFC** for comparison and for anything written into front matter, links or boards, so
  accented titles do not break refs across platforms.
- Item IDs are ASCII by construction (`<KEY>-<TYPE>-<NNNN>`), which keeps refs immune to
  all of the above; only slugs carry user text, and slugs are never load-bearing (the ID is
  the identity; `doctor --fix` may rename a slug freely).

### 7.5 Processes, ports and the browser

- Default bind is `127.0.0.1`. Binding a non-loopback address requires an explicit
  `--bind` and refuses to start without a token.
- If the configured port is busy the server tries `port+1..port+9` unless `--port` was given
  explicitly, in which case it fails with a clear message.
- Browser opening uses `xdg-open` (Linux), `open` (macOS), `rundll32 url.dll,FileProtocolHandler`
  (Windows); failures are non-fatal and simply print the URL. `--no-open`, `BROWSER=none`
  and headless environments (`$SSH_CONNECTION` set, no `$DISPLAY`/`$WAYLAND_DISPLAY`) skip it.

---

## 8. Logging, metrics and diagnostics

### 8.1 Logging

- `log/slog` with a text handler for terminals and a JSON handler for `log.format: json`.
- Levels: `debug` (per-file index decisions, watcher events, git command lines),
  `info` (startup, index summaries, sync phases), `warn` (degraded watcher, validation
  warnings, WIP breaches), `error` (failed requests, git failures).
- Every HTTP request logs one line: method, path, status, duration, bytes, request id.
  Request bodies are never logged; the `Authorization` header is redacted everywhere, and
  the token is redacted in `doctor` output (first 5 and last 3 characters only).
- File logging to `<configdir>/logs/gintrack.log` with size-based rotation (5 MB × 3 files)
  when `log.file` is set. No log is written by default beyond stderr.
- Paths inside logs are repo-relative; absolute paths appear only at `debug`.

### 8.2 Metrics

Metrics are opt-in and local-only — there is no exporter to any remote endpoint.

```
GET /api/v1/metrics        # Prometheus text format, requires the bearer token,
                           # served only when log.level=debug or --metrics is passed
```

Instruments:

| Metric                                   | Type      | Labels                  |
| ---------------------------------------- | --------- | ----------------------- |
| `gintrack_http_requests_total`           | counter   | method, route, status   |
| `gintrack_http_request_duration_seconds` | histogram | route                   |
| `gintrack_index_items`                   | gauge     | repo, type              |
| `gintrack_index_pass_duration_seconds`   | histogram | repo, full              |
| `gintrack_index_warnings`                | gauge     | repo                    |
| `gintrack_watcher_events_total`          | counter   | repo, op                |
| `gintrack_watcher_watches`               | gauge     | repo                    |
| `gintrack_ws_clients`                    | gauge     | —                       |
| `gintrack_ws_events_total`               | counter   | type                    |
| `gintrack_ws_dropped_clients_total`      | counter   | reason                  |
| `gintrack_git_operations_total`          | counter   | backend, op, result     |
| `gintrack_git_operation_duration_seconds`| histogram | backend, op             |
| `gintrack_mcp_tool_calls_total`          | counter   | tool, result            |

`/debug/pprof` is mounted only under `--dev`.

### 8.3 Diagnostics bundle

`gintrack doctor --bundle report.zip` collects, for bug reports: `gintrack version --json`,
the redacted config, per-repo status and counts, the last 500 log lines, index statistics
and warnings, and platform details (OS, arch, filesystem type, inotify/ulimit values). It
never includes item content, KB pages, tokens, remote URLs with credentials, or author
email addresses beyond the local git identity, and prints the exact list of included files
before writing.

---

## 9. Testing strategy for this surface

- **Core** — table-driven tests over golden fixtures in `internal/core/testdata`, run both
  natively and under `GOOS=js GOARCH=wasm` with `wasmbrowsertest`, guaranteeing the two
  builds agree.
- **HTTP** — `httptest` suites per route group, plus a contract test that replays the
  request/response examples in this document (they live as fixtures, so the docs cannot
  drift silently).
- **CLI** — golden-output tests using `testscript`, covering exit codes and `--json`
  shapes.
- **Watcher** — integration tests that perform editor-style save patterns (temp file +
  rename, double write, mass checkout) on a temp repo and assert the emitted event batches.
- **Git** — tests against fixture repositories created in `t.TempDir()`, executed against
  both backends; the system-git suite is skipped when no `git` binary is present.
- **End-to-end** — Playwright drives the real embedded UI against a `gintrack serve`
  instance seeded from fixtures, in both companion and browser-only mode.

---

## 10. Related documents

- Architecture overview and repository model — earlier documents in `docs/`.
- Data model and front-matter schema — the data-model document.
- Git synchronization and conflicts — the sync document (Phase 4).
- MCP server and agent workflows — `docs/08-mcp-server.md` (Phase 5).
