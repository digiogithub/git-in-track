---
title: Research — git-in-track technical map for integrations
type: page
tags: [research, youtrack, sync-engine, agent]
---

# git-in-track — technical map for YouTrack integration + sync engine + agentic UI

Repo root: `/www/git-in-track`. Module `github.com/digiogithub/git-in-track`. Go 1.26, chi, cobra,
go-git, MCP Go SDK, React 18 + Vite + TS in `web/`.

---

## 1. Data model

### 1.1 Item front matter — all fields

Authoritative spec: `/www/git-in-track/docs/03-data-model.md` §7.1 (epics, line 656), §8.1 (stories,
line 754), §9.1 (tasks, line 863), §10.1 (milestones, line 930).
Go type: `core.Item` at `/www/git-in-track/internal/core/model.go:324-376`.

| Front-matter key | Go field (`model.go` line) | Notes |
|---|---|---|
| `id` | `ID ItemID` :329 | immutable, `<KEY>-<EP\|US\|T\|M>-<NNNN>` |
| `type` | `Type ItemType` :330 | `epic\|story\|task\|milestone` (also `comment`, `board`, `sprint`, `retro` elsewhere) |
| `title` | `Title` :331 | |
| `status` | `Status Status` :332 | id from `project.yaml workflow.statuses` |
| `priority` | `Priority` :333 | `critical\|high\|medium\|low` |
| `parent` | `Parent ItemID` :337 | epic for a story, story/epic for a task |
| `epic` | `Epic ItemID` :338 | deprecated alias of `parent` |
| `milestone` | `Milestone ItemID` :339 | |
| `sprint` | `Sprint string` :340 | soft ref into team repo |
| `assignees` | `Assignees []string` :343 | |
| `author` | `Author` :344 | |
| `owner` | `Owner` :345 | milestones only |
| `labels` | `Labels []string` :346 | |
| `estimate` / `effort` / `spent` | `*float64` :349-351 | nil-able to distinguish absent from 0 |
| `created` / `updated` / `started` / `closed` | `Timestamp` :354-357 | |
| `start` / `due` | `Date` :358-359 | |
| `links` | `Links []Link` :362 | `{kind,target,note}`; kinds `blocks`, `blocked_by`, `relates_to`, `duplicates`, `duplicated_by` (`model.go:104-144`) |
| `attachments` | `Attachments []string` :363 | filenames under `.pmngr/attachments/<ID>/` |
| `custom` | `Custom map[string]any` :364 | declared custom fields |
| `deleted` | `Deleted bool` :365 | soft delete |
| *(unknown keys)* | `Extra map[string]any` :370 | **preserved verbatim on rewrite**, incl. `x-` keys |
| — derived, never on disk | `Body`, `Path`, `Rev` :373-375 | |

Canonical key order for the emitter: `docs/03-data-model.md:185-198`; implemented in
`internal/core/yamlemit.go` and `SerializeItem` (`internal/core/frontmatter.go:362`).
`R-FMT-6` (`docs/03:172`) requires unknown keys be preserved.

### 1.2 Where extensible metadata for YouTrack could live — four candidate homes

1. **`custom:` mapping** (`docs/03 §13.2`, line 1128; `CustomField` in
   `/www/git-in-track/internal/core/project.go:140-149`). Rule **R-CF-1**: values live under
   `custom:`, never at top level, precisely so a third-party field cannot collide with a future
   core field. Declared in `project.yaml:custom_fields` with types
   `string|text|number|bool|date|timestamp|enum|person|list|url`. An undeclared key is
   `W-CF-UNDECLARED` and is preserved. **This is the sanctioned extension point** and needs no
   data-model change — e.g. `custom: { youtrack_id: ACME-1234, youtrack_url: https://… }`.
2. **`x-` prefixed top-level keys** — **R-CF-4** (`docs/03:1150`): "Top-level keys prefixed `x-` are
   reserved for third-party tools; `gintrack` preserves them verbatim and never validates them."
   They land in `Item.Extra` (`model.go:370`) and round-trip. Zero-code option, but invisible to the
   index/query layer and to `ItemDraft`/`ItemPatch`.
3. **New first-class front-matter fields** (e.g. `external: {system, id, url, syncedAt, syncedRev}`).
   Requires: `core.Item` fields, `frontmatter.go` reader + `SerializeItem`, the canonical key order in
   `docs/03 §3.2`, `ItemDraft`/`ItemPatch` (`internal/core/store.go:118`, `:154`), the JSON Schema in
   `docs/03 §18` — **plus an ADR** (AGENTS.md mandatory rule).
4. **`links[]`** — *not* suitable: `LinkKind.Valid()` (`model.go:116-124`) hard-codes the five kinds
   and `target` is an item ID (`R-LINK-2`), not a URL. Adding an `external` link kind is a data-model
   change requiring an ADR.

`ItemDraft.ID` (`internal/core/store.go:152`) already exists **"for importers"** — it pins the
identifier instead of allocating one. Note it pins a *gintrack* id, not a foreign one.

### 1.3 Comments (ADR-012)

`/www/git-in-track/docs/adr/ADR-012-comments-as-separate-files.md`; spec `docs/03 §11` (line 985).

- Path: `<docs>/.pmngr/comments/<ITEM-ID>/<YYYYMMDDTHHMMSSZ>-<author>.md`; collisions get `-2`, `-3`.
- Go type `core.Comment` at `internal/core/model.go:377-400`: `item`, `author`, `author_name`,
  `author_email` (ADR-030), `created`, `updated`, `in_reply_to`, `kind`
  (`comment|status_change|system`, `model.go:402-421`), `reactions`, `attachments`, plus `Extra`.
- Parser/emitter: `ParseComment` `internal/core/frontmatter.go:289`, `SerializeComment` `:404`.
- Core surface: `internal/core/comments.go`; vault methods `comment.list` / `comment.add`
  (`internal/vault/vault.go:1170`, `:1185`).
- REST: `GET|POST /api/v1/items/{id}/comments` (`internal/server/items.go:33-34`, handler
  `handleCommentAdd` `internal/server/items.go:399-…`, body `{body, author, authorName, authorEmail,
  inReplyTo}`).
- Comment `rev`: adding a comment quotes the **item's** rev, not the thread's (`docs/03` R-REV-6).
- Unknown comment front-matter keys are preserved (`Comment.Extra`), so a `youtrack_comment_id`
  marker on a mirrored comment round-trips without a schema change.

### 1.4 Feedback notes (ADR-030)

`/www/git-in-track/docs/adr/ADR-030-feedback-notes-on-items-and-kb-pages.md`; spec `docs/03 §14.4`
(line 1215). Two different sinks:

- **On an item** → collected in the browser (localStorage) and saved as **one ordinary comment**
  whose body quotes each passage with its line range. No new file kind.
- **On a KB page** → an HTML-comment-delimited block appended to the page itself:
  `<!-- gintrack:feedback:begin -->` … `## Feedback` … `<!-- gintrack:feedback:note id="fb-XXXXXXXX"
  anchor="sha256:…" lines="3-4" author=… email=… created=… -->` … `<!-- gintrack:feedback:end -->`.
  Rules R-FB-1..5.
- Implementation: `/www/git-in-track/internal/core/kbfeedback.go` — `AddKbFeedback` :113,
  `PruneKbFeedback` :166, `ParseKbFeedback` :179, anchor `kbFeedbackAnchor` :369, id
  `kbFeedbackID` :383, render :323/:410. **Every page write through the core prunes the block**
  (R-FB-4), so the rule lives in `internal/core` and applies in WASM and companion alike.
- Vault: `kb.feedback.add` (`internal/vault/vault.go:1391-1456`), params
  `{path, rev, author, authorName, authorEmail, notes:[{startLine,endLine,quote,note}]}`.
- REST: `POST /api/v1/kb/feedback` (`internal/server/kb.go:22`).

### 1.5 KB page format

- Any `.md` under the documentation folder; free Markdown, optional YAML front matter.
- Go type `core.KBPage` at `/www/git-in-track/internal/core/kb.go:20-45`: `Path`, `RelPath`,
  `Project`, `Title`, `Tags`, `FrontMatter map[string]any` (**arbitrary keys preserved**),
  `Headings`, `Links []Wikilink`, `External []string`, `Updated`, `Size`, `Rev`, `Body`.
- Parser `ParsePage` `internal/core/kb.go:98`; wikilinks `ParseWikilink` `:206`; tree `buildTree` `:258`.
- Read/write in the store: `FileStore.ReadPage` `internal/core/store.go:968`, `WritePage` `:986`.
- Vault: `kb.tree`, `kb.page`, `kb.write`, `kb.feedback.add`
  (`internal/vault/vault.go:1227`, `:1290`, `:1356`, `:1391`).
- REST: `GET /kb/tree`, `GET /kb/page`, `PUT /kb/page`, `POST /kb/feedback`
  (`internal/server/kb.go:19-23`); also mounted per project/team at
  `/projects/{key}/kb` and `/teams/{key}/kb` (`internal/server/api.go:53`, `:58`, `:70`).
- **KBPage.FrontMatter is a free map**, so a YouTrack article id/url can be stored as page front
  matter with no core change.

### 1.6 Project config file

`<docsFolder>/.pmngr/project.yaml` — the only non-Markdown file in `.pmngr/`.
Spec `docs/03 §6` (line 490); Go type `core.ProjectConfig` at
`/www/git-in-track/internal/core/project.go:23-40`, loader `LoadProjectConfig` `:198`.
Live example for this repo: `/www/git-in-track/docs/.pmngr/project.yaml`.

Fields: `schema`, `key`, `name`, `description`, `timezone`, `docs` (`DocsConfig` :43),
`workflow` (`Workflow` :54 → `StatusDef` :61 with `id/name/category/wip/color/terminal`),
`id_allocation` (`IDAllocation` :72), `labels`, `priorities`, `estimation`, `defaults`,
`custom_fields` (`CustomField` :140), `people` (`Person` :151), `team` (`TeamLink` :161 →
`{repo, key}`), `links` (`LinksConfig` :167 → `{host, web_url}`).

**Gotcha:** `ProjectConfig` has **no `Extra` map** — unknown `project.yaml` keys are dropped by the
decoder. But the file is **never fully re-serialized**: the only writer is
`Allocator.writeCounter` (`internal/core/allocator.go:414`) which does a surgical YAML-node edit via
`setYAMLPath` (`:545`) preserving everything else. `internal/core/scaffold.go` writes it once at
creation (`core.CreateProject`). So a hand-added `youtrack:` block survives on disk but is
invisible to Go until a field is added to `ProjectConfig`.

### 1.7 Where a per-project YouTrack integration config should live — and how secrets work today

**There is no secret store. None.** The evidence:

- `/www/git-in-track/docs/10-development-guidelines.md:707-711`:
  "**Git credentials are never stored by git-in-track.** Native mode delegates to the user's
  existing git credential helper and SSH agent. Browser mode asks for a personal access token per
  session, keeps it **in memory only (never `localStorage`)**, and scopes it to the single remote it
  was entered for. Tokens are redacted from all logs and error messages."
- `internal/config.Config` (`/www/git-in-track/internal/config/config.go:80-90`) has sections
  `Server`, `Git`, `Index`, `MCP`, `Log`. The **only** secret it holds is `Server.Token`
  (`config.go:127`) — the companion's own bearer token — and the file is written `0600`
  (`docs/07-cli-and-api.md:157`).
- `config.Git` (`config.go:165-200`) carries `backend`, `commitOnSave`, `commitDebounce`,
  `messageTemplate`, `authorName`, `authorEmail`, `signCommits`, `pullStrategy`, `pushOnSync`,
  `maxPushRetries`, `corsProxy` — **no credential fields**.
- No keyring / keychain dependency in `/www/git-in-track/go.mod`.
- AGENTS.md mandatory rule: "**Never commit secrets**, tokens, credentials or personal data."

Consequences for the planner. The integration config splits naturally in two:

- **Non-secret, per-project, committed**: YouTrack base URL, YouTrack project short name, field
  mappings, sync direction. Natural home is a new `project.yaml` block (`ProjectConfig` +
  `docs/03 §6.1` table + ADR), because it is project-scoped and belongs in the repo with the backlog
  — the same shape `team:` and `links:` already have (`project.go:161`, `:167`).
- **The token**: must NOT go in `project.yaml` (committed). Options, in order of fit with existing
  precedent: (a) machine-local `~/.config/gintrack/config.yaml` under a new `integrations:` /
  `youtrack:` section keyed by project key — the file is already `0600` and already holds
  `server.token`; (b) env var (`GINTRACK_YOUTRACK_TOKEN`), matching the precedence chain in
  `docs/07 §3.3` (flag > env > file > default); (c) browser-only mode: **in-memory per session**,
  exactly as the git PAT rule above demands. A new ADR is required either way, and it should say
  explicitly what "never stored" now means.

---

## 2. Go core

### 2.1 `internal/core` — key types

- `internal/core/model.go` — `ItemID`:20, `ItemType`:26, `Status`:54, `StatusCategory`:58,
  `Priority`:79, `Rev`:101, `LinkKind`:104 (+`Inverse()`:126), `Link`:145, `Timestamp`:153,
  `Date`:236, `Item`:324, `Comment`:377, `CommentKind`:402.
- `internal/core/frontmatter.go` — `SplitFrontMatter`:52, `ParseDocument`:103, `ParseItem`:189,
  `ParseComment`:289, `SerializeItem`:362, `SerializeComment`:404.
- `internal/core/store.go` — `Store` interface:18, `StaleRevisionError`:59 (+`ConflictField`:51),
  `TransitionError`:89, `ItemDraft`:118, `ItemPatch`:154, `DeleteOptions`:200, `MoveOptions`:205,
  `FileStore`:214, `NewStore`:241.
- `internal/core/fs.go` — the **host seam**: `FS` interface:18 (`ReadFile/WriteFile/Remove/Rename/
  Stat/ReadDir/MkdirAll`), `FileInfo`:29, `DirEntry`:38, `MemFS`:45. Native impl in
  `internal/core/osfs/osfs.go`; browser impl in `wasm/`.
- `internal/core/index.go` — `Index`:308, `NewIndex`:343, `Build`:445, `ApplyFileEvents`:843,
  `IndexDelta`:253, `IndexStats`:277, `ProjectRef`:62, `DiscoverProjectsWith`:123, `FileEvent`:243.
- `internal/core/allocator.go` — `Allocator`:113, `Next`:147, `Peek`:154, `Reconcile`:358,
  `scanItems`:457, `writeCounter`:414. ID allocation is **scan-based** (`docs/03 §4.1`): the
  backlog folder is scanned, the highest number wins, `project.yaml` counters are hints only.
- `internal/core/kb.go`, `kbfeedback.go`, `comments.go`, `query.go` (`Filter`:33, `SearchHit`:518,
  `Index.Search`:544), `graph.go`, `references.go`, `merge.go`, `validate.go`, `workflow.go`,
  `project.go`, `scaffold.go`, `rev.go`, `snapshot.go`, `metrics.go`, `board*.go`, `sprint*.go`,
  `retro*.go`, `team*.go`, `vcs.go`, `doctor.go`.

**Purity constraint** is written into `/www/git-in-track/internal/core/doc.go:11-12`: no `os`,
`os/exec`, `syscall`, `syscall/js`, **no `net/http`**, no fsnotify, no go-git. Verified: nothing
under `internal/core` imports those. **A YouTrack HTTP client therefore cannot live in
`internal/core`.**

### 2.2 Create / update

- `FileStore.Create(ctx, draft ItemDraft) (*Item, error)` — `internal/core/store.go:321`. Allocates
  an id via the `Allocator` unless `draft.ID` is set, applies `project.yaml` defaults
  (`applyDefaults`:401), validates, writes atomically (`writeItem`:622 → `writeFileAtomic`:1051,
  tmp + rename).
- `FileStore.Update(ctx, id, patch ItemPatch, expected Rev) (*Item, error)` — `:423`. Sparse patch;
  `applyPatch`:754; Add/Remove set operations for `assignees`, `labels`, `links` so concurrent
  clients don't clobber lists.
- `FileStore.Move(...)` `:495` / `MoveWith` `:500` (status change, `checkTransition`:532,
  `stampTransition`:550 sets `started`/`closed`).
- `FileStore.Delete` `:466` / `DeleteWith` `:471` (soft by default, ADR-026).
- `FileStore.ReadPage`:968 / `WritePage`:986 for KB pages.

### 2.3 rev / optimistic locking

- Definition `docs/03 §5` (line 435): `rev = "sha256:" + hex(sha256(canonical bytes))[0:16]`,
  never stored in the file. Implementation `internal/core/rev.go` (`ComputeRev`).
- Enforcement: `FileStore.readChecked` (`store.go:574`) with a `conflictIntent` func (`:569`) that
  produces `[]ConflictField` — the fields the refused write *would still have changed* against
  current content. Error type `StaleRevisionError` (`store.go:59-87`), code `stale_revision`.
- HTTP spelling: `If-Match` header; `ifMatch` `internal/server/api.go:172`, `requireIfMatch` `:189`,
  wildcard `*` = `wildcardRev` `internal/server/api.go:18`. Maps to 412.
- MCP spelling: `rev` argument, `rev:"*"` waiver.

### 2.4 `internal/vault` — the CoreApi contract

`internal/vault/vault.go`. `Vault` :39 wraps one repository (an `core.FS` + `core.Index` + per-project
`core.FileStore`s). The **entire API surface is one string-dispatch table**:
`Vault.Dispatch(ctx, method, raw []byte) (any, error)` — `internal/vault/vault.go:304-405`.
`Vault.Call(method, params string) string` :287 is the WASM-facing JSON-in/JSON-out form.

Methods (vault.go:320-404):
`ping`, `version`, `vault.load`, `vault.apply`, `vault.stats`, `snapshot.export`, `snapshot.load`,
`project.list`, `project.create`, `team.get`, `team.create`, `ref.resolve`, `board.list`, `board.get`,
`item.list`, `item.get`, `item.children`, `item.create`, `item.update`, `item.move`, `item.delete`,
`item.task.set`, `item.validate`, `item.parse`, `item.serialize`, `comment.list`, `comment.add`,
`kb.tree`, `kb.page`, `kb.write`, `kb.feedback.add`, `conflict.merge`, `search`.

`Workspace` (`internal/vault/workspace.go`, dispatch in `internal/vault/dispatch.go:73-260`) is the
multi-repository layer used by the companion; it routes by `vaultId`/`project`/`id`/`ref`
(`route` :261) and owns the cross-repo methods: `workspace.list/mount/unmount`, `team.*`,
`board.*`, `sprint.*`, `retro.*`, `item.references`, `metrics.*`.

**Every write holds the vault mutex for the whole call** (`vault.go:315-317`) and a refresh hook fires
after the unlock. `v.fs.begin()` / `v.commit(ctx)` (`vault.go:1511`) collect the `WriteSet`
(`internal/vault/wire.go:56`) that the server turns into git commits and WS events.

**Adding a new capability to the core API = adding a `case` to these two dispatch tables**, which
automatically serves both the WASM browser build and the companion.

### 2.5 Item creation over REST

Routes `internal/server/items.go:19-36`, mounted at `/api/v1/items` (`internal/server/api.go:73`):

| Method | Path | Handler |
|---|---|---|
| GET | `/api/v1/items` | `handleItemList` |
| POST | `/api/v1/items` | `handleItemCreate` |
| POST | `/api/v1/items/validate` | `handleValidate` |
| GET | `/api/v1/items/{id}` | `handleItemGet` |
| PATCH | `/api/v1/items/{id}` | `handleItemUpdate` (If-Match) |
| PUT | `/api/v1/items/{id}` | 501 |
| DELETE | `/api/v1/items/{id}` | `handleItemDelete` |
| POST | `/api/v1/items/{id}/move` | `handleItemMove` |
| GET | `/api/v1/items/{id}/references` | `handleItemReferences` |
| POST | `/api/v1/items/{id}/tasks` | `handleItemTaskSet` |
| GET/POST | `/api/v1/items/{id}/comments` | `handleCommentList` / `handleCommentAdd` |
| * | `/api/v1/items/{id}/links` | 501 |

Handlers are thin: they parse the request and call `s.call(w, r, mount, "item.create", params)`
(`internal/server/api.go:134-156`) which marshals to JSON and hands it to `vault.Dispatch`.
`maxItemsPerPage = 500` (`items.go:16`), `maxRequestBody = 1 MiB` (`api.go:14`).

### 2.6 WS events and the watcher

- Hub: `internal/server/hub.go` — `Event`:36, `Hub`:141, `Publish(topic, data)`:167, per-client topic
  subscription :72-104, ring buffer replay `since`:234.
- Event publishers: `internal/server/events.go` — `publishIndexUpdated`:229, `publishWrite`:256,
  `publishPageWrite`:282, `publishBoardMove`:307, `publishWriteSets`:338, `publishDelta`:372.
- Topics in use: `item.changed`, `file.changed`, `index.updated`, `git.commit`
  (`internal/server/git.go:544`), `sync.progress` (`internal/server/sync.go:239`),
  `conflict.detected` (`sync.go:277`), `conflict.resolved` (`internal/server/conflicts.go:169`),
  `tunnel.changed` (`internal/server/tunnel.go:260`).
  Wire contract documented at `docs/07-cli-and-api.md:2353-2452`.
- Endpoint `GET /api/v1/events` (WebSocket) — `internal/server/events.go:44`; client frames
  `subscribe`/`unsubscribe`/`resume`/`ping` (`clientFrame` :34).
- Watcher: `internal/watcher/watcher.go` (fsnotify), wired by `internal/server/watch.go` —
  `startWatch`:64, `watchLoop`:190, `applyBatch`:216, debounced, converts to `core.FileEvent` and
  calls `Vault.ApplyEvents`.

---

## 3. Server

### 3.1 Router

`internal/server/server.go:343-371` `routes()`:
middleware chain `middleware.RequestID` :345 → `s.requestLogger` :349 → `middleware.Recoverer` :350
→ `s.corsMiddleware` :351 (`internal/server/cors.go`) → `securityHeaders` :352 →
`s.timeoutExceptStream` :355 (30 s, `requestTimeout` :56; streams and the proxy exempt) →
`r.Route("/api/v1", s.mountAPI)` :357. SPA fallback `internal/server/spa.go`.

`mountAPI` — `internal/server/api.go:22-101`. `/health` is public; everything else is inside
`api.Group` with `s.bearerAuth` (`server.go:467`; token from `Authorization: Bearer` or
`?token=`/`Sec-WebSocket-Protocol`, `presentedToken` :487).

Registered subtrees (`api.go:27-96`): `/capabilities`, `/events`, `/workspace`, `/workspaces[/{name}]`,
`/repos`, `/repos/{id}`, `/repos/{id}/reindex`, `/repos/{id}/projects`, `/repos/{id}/team`,
`/projects`, `/projects/{key}`, `/projects/{key}/kb`, `/teams`, `/teams/{key}`, `/teams/{key}/kb`,
`/teams/{key}/projects[/{project}]`, `/refs`, `/snapshots`, `/kb`, `/items`, `/search`, `/validate`,
`/boards`, `/sprints`, `/retros`, `/tunnel`, `/mcp`, `/git`, `/sync`.
Sub-mounts: `mountItems` `items.go:19`, `mountKB` `kb.go:18`, `mountBoards` `boards.go:20`,
`mountSprints` `sprints.go:19`, `mountRetros` `retros.go:20`, `mountGit` `git.go:358`,
`mountSync` `sync.go:94`, `mountMCPSettings` `mcp.go:186`.

Errors: RFC 7807 problem+json, `internal/server/problem.go`; codes `not_found`, `unauthorized`,
`invalid_request`, `not_implemented`, `precondition_required`, `repo_not_registered`,
`index_unavailable`, `internal`, `tunnel_requires_token`, `tunnel_failed` (`problem.go:18-31`).

**Adding a route = one `p.Route("/youtrack", s.mountYouTrack)` line in `api.go` plus a
`mount*.go` file.** No plugin/registry indirection exists.

### 3.2 Server state

`Options` `server.go:59-131` (Bind, Port, Token, Dev, Repos, Workspace, Watch, Debounce,
ExtraOrigins, NewWatcher, Now, `Git config.Git`, `ConfigPath`, MCPHTTP, MCPAllowWrite, MCPAgent,
`Tunnel config.Tunnel`, NewTunnel). `Server` `:134-166` owns `repos *registry`, `hub *Hub`,
`watch watchState`, `git *gitState`, `mcp *mcpState`, `proxy *corsProxy`, `tunnel *tunnelState`.
Mounts: `internal/server/mount.go` — `mount`:60, `openMount`:77, `registry`:183, `forProject`:239,
`forItem`:257, `reindex`:271.

### 3.3 Long-running / background work today — and what is missing

**There is no generic job queue, no worker pool, no retry/backoff framework, no persistent job
store.** What exists:

1. **Commit debouncer** — `internal/gitops/committer.go`: `Committer`:76, `Enqueue`:160, `arm`:192
   (timer per coalesce key), `fire`:212, `batch`:93 with `merge`:355/`absorb`:377, `Flush`:240,
   `Pending`:264, `Close`:273, in-flight counter `startCommit`/`finishCommit` :123/:126. This is the
   **closest existing thing to a worker queue**: keyed batching + debounce + async execution +
   an outcome published as a `git.commit` WS event (`internal/server/git.go:544`).
2. **Sync pipeline** — `internal/server/sync.go`: `handleSyncRun`:145 (**synchronous** per request,
   `syncOne`:230), progress published as `sync.progress` events :239, conflicts as
   `conflict.detected` :277. Settings `PATCH /api/v1/sync/settings` :379.
3. **File watcher** — `internal/server/watch.go:64-240`, a goroutine loop with debounced batches.
4. **Tunnel** — `internal/server/tunnel.go`, a long-lived driver goroutine with state published as
   `tunnel.changed`.
5. **Idle-timeout / lifecycle goroutines** in `Server.Start` `server.go:289`.

A background YouTrack sync engine with a worker queue and batches therefore has **no existing
abstraction to plug into**; `Committer` is the pattern to copy (keyed batches, debounce, async fire,
WS progress events), and `sync.progress` is the event shape to imitate.

### 3.4 Settings persistence

- File: `os.UserConfigDir()` chain — `--config` > `GINTRACK_CONFIG` > `$XDG_CONFIG_HOME/gintrack/
  config.yaml` > platform default (`docs/07 §3.1`, line 139; `internal/config/path.go:27` `DefaultPath`,
  `:38` `PathFor`, `:92` `StateDir`). Written `0600`.
- Load/save: `config.Load` `internal/config/load.go:44`, `config.Parse` :56, `config.Save` :74,
  `config.Resolve(flags, env)` :146, `applyEnv` :186, `applyFlags` :224.
  Precedence: **flag > env > file > built-in default** (`docs/07 §3.3`, line 271).
- Validation: `internal/config/validate.go:53` `Validate()`.
- Only two routes persist to the file today, and both use the same shape:
  - `PATCH /api/v1/git/settings` — handler `internal/server/git.go:374-393`, `gitState.apply` :278,
    `gitState.persist` :336 (re-`Load`s the file, replaces `cfg.Git`, `config.Save`); the response
    carries `persisted bool` (`git.go:69`) so the UI can say whether only the process took it.
  - `PATCH /api/v1/mcp/settings` — `internal/server/mcp.go:186-228`, `mcpState.persist` :134.
  - `PATCH /api/v1/sync/settings` — `internal/server/sync.go:379`, writes through the same `gitState`.
- `Options.ConfigPath` (`server.go:112`) is what makes persistence possible; empty = process-only.
- **Precedent to copy exactly** for a `youtrack`/`integrations` section: new struct in
  `internal/config/config.go`, new `*State` in `internal/server`, `GET|PATCH /api/v1/…/settings`,
  `persist()` that reloads-mutates-saves, `persisted` in the response.

### 3.5 The CORS proxy (ADR-025) — and why YouTrack cannot use it

`/www/git-in-track/docs/adr/ADR-025-the-cors-proxy-security-model.md`;
`/www/git-in-track/internal/server/cors_proxy.go` (mounted `:203-204` at `/cors-proxy` and
`/cors-proxy/*`, `corsProxyPath` :56).

It is **a git proxy, not a forwarder**. Hard constraints, all in code:
- Must present a trusted `Origin` **and** the bearer token in `X-Gintrack-Token` (`proxyTokenHeader`
  :60, `tokenMatches` :266) — never `Authorization`, which is reserved for the git host credential.
- **Only three paths**: `GET /info/refs?service=git-upload-pack|git-receive-pack`,
  `POST /git-upload-pack`, `POST /git-receive-pack`.
- **Host allow-list derived from registered repositories' remotes** + `git.corsProxy.allowedHosts`
  (`config.CORSProxy` `internal/config/config.go:202-216`), equality-compared, no patterns; cache TTL
  `proxyRemoteCacheTTL = 60s` :78.
- Scheme is always `https`; the dialer resolves the name itself once and **refuses any non-public
  unicast address** (loopback, RFC1918, link-local incl. 169.254.169.254, CGNAT, multicast…).
- Bounds: 32 MiB request, 256 MiB response, 120 s (`:65-71`); redirects re-validated, max 3.
- Header allow-lists both directions; `Set-Cookie` cannot reach the tab.
- `GET /api/v1/git/cors-proxy` (`internal/server/git.go:366`) advertises it so the browser adopts it
  automatically.

**Implication:** browser-only mode cannot call a YouTrack REST API through this proxy — the path
allow-list alone forbids it, and a self-hosted YouTrack on a private address is refused by design.
A YouTrack integration needs either (a) the companion doing all YouTrack HTTP (recommended), or
(b) a **new, separately-specified** proxy endpoint with its own ADR — which ADR-025's own reasoning
argues strongly against generalising the existing one.
---

## 4. Frontend (`/www/git-in-track/web/`)

React 18.3 + Vite 6.4 + TS 5.9, Tailwind 3.4, TanStack Router 1.170 / Query 5.102 / Table 9.2,
Zustand 5, zod 4, CodeMirror 6, dnd-kit, vendored shadcn-style UI.

### 4.1 Routing — TanStack Router, code-based

`/www/git-in-track/web/src/app/router.tsx` holds **the entire route tree in one file**
(`createRoute`/`createRouter`, explicitly *not* file-based — rationale at `:18-22`; there is no
generated `routeTree.gen.ts`). Root layout `web/src/app/rootRoute.tsx:11-14` →
`AppShell` (`web/src/app/layout/AppShell.tsx:68`), 404 `web/src/app/layout/NotFound.tsx`.

| Path | Component | router.tsx |
|---|---|---|
| `/` | `WorkspaceHome` | :23-27 |
| `/repos/add` | `AddRepositoryPage` | :29-33 |
| `/p/$project` | *(layout, no component)* | :35-38 |
| `/p/$project/kb/$` | `KbViewer` | :41-45 |
| `/p/$project/items` | `ItemTable` (validateSearch) | :48-53 |
| `/p/$project/items/$id` | `ItemDetail` | :55-59 |
| `/p/$project/items/new` | lazy `NewItemPage` | :62-67 |
| `/p/$project/items/$id/edit` | lazy `ItemEditorPage` | :69-73 |
| `/p/$project/epics` | `EpicTree` | :75-79 |
| `/p/$project/milestones` | `MilestoneList` | :81-85 |
| `/boards`, `/boards/$slug` | `BoardList`, lazy `BoardView` | :87-98 |
| `/sprints` | lazy `SprintList` | :101-109 |
| `/retros`, `/retros/$retroId` | lazy `RetroList`, `RetroBoard` | :112-122 |
| `/metrics`, `/metrics/$sprintId` | lazy `MetricsIndex`, `SprintMetrics` | :125-135 |
| `/sync` | `SyncPanel` | :138-142 |
| `/settings` | `SettingsPage` | :144-148 |

Tree assembly :150-171; `createAppRouter` (`defaultPreload:'intent'`) :173-179; `Register`
augmentation :183-187. Sidebar nav list `AppShell.tsx:38-53`.
**Adding a `/integrations` or `/p/$project/youtrack` route = one `createRoute` + one entry in the
tree array + one sidebar item.**

### 4.2 `web/src/features/` layout

`backlog/` (item table, filters, detail, epic tree, milestones, bulk move, query hooks) ·
`boards/` (kanban + sprints) · `editor/` (item create/edit, front-matter model + validation, drafts,
conflict dialog, templates, project schema) · `feedback/` (feedback mode) · `kb/` (KB viewer, tree,
TOC, backlinks) · `metrics/` · `retros/` · `settings/` · `sync/` (sync panel, conflict resolver,
credential prompt) · `workspace/` (home, onboarding, team selector, folder pickers, search, identity).

### 4.3 State — Zustand (two stores, no middleware)

- `web/src/app/store.ts:175-225` `useAppStore` — `ModeSlice` :115-135 (`mode:'browser'|'companion'|
  'detecting'`, `companionVersion`, `companionUrl`, `connection`, `modeNotice`, `companionAuth`,
  `capabilities`) + `WorkspaceSlice` :96-113 (`pendingVaultId/Name`, `activeRepoId`,
  `activeTeamKey`, `readOnlyNoticeDismissed`). Active team persisted to `localStorage`
  `gintrack:active-team:<companionUrl|browser>` :29-59. Documented rule :168-174 — **server state
  goes to TanStack Query, navigational state to the URL; this store holds neither.**
- `web/src/app/ui-prefs.ts:102-150` `useUiPrefs` — sidebar/KB panel layout, persisted to
  `localStorage` `gintrack:ui-prefs`.
- Non-Zustand external stores: feedback drafts via `useSyncExternalStore`
  (`features/feedback/feedback-store.ts:147-196`); the companion token via a listener set
  (`api/token.ts:90-95`).

### 4.4 API client layer — `web/src/api/`

- **The seam**: `DataProvider` interface at `web/src/api/provider.ts:866-1193` — ~70 methods
  (workspace, items, KB, boards, sprints, retros, snapshots, git/MCP/tunnel settings, sync,
  `subscribe()`). `provider.ts:6` and `docs/05-web-app.md` §4 **forbid feature code from importing
  isomorphic-git, the WASM bridge, or calling `fetch('/api')` directly.** Every new backend
  capability must be added as a `DataProvider` method and implemented in all four providers.
- Implementations: `web/src/api/companion-provider.ts` (2018 l), `web/src/api/browser-provider.ts`
  (1686 l), `web/src/api/fake-provider.ts` (2743 l, the test double).
- Dispatch: `web/src/api/provider-factory.ts:23-30` (`mode==='companion'` → Companion else Browser),
  `whenProviderReady` :36-38, `disposeProvider` :41-43; wired in `web/src/app/providers.tsx:62-102`;
  React context `web/src/api/provider-context.ts:5-19` (`useProvider`).
- Companion transport: one private `#send` at `companion-provider.ts:1748-1776` — `fetch`,
  `mode:'cors'`, `credentials:'omit'`, `Authorization: Bearer` from `api/token.ts:133-136`.
  Prefix `/api/v1` :103. `#hydrate` :1785-1793 re-reads an item when a write returns only `{id,rev}`.
- Browser transport: `browser-provider.ts` composes `web/src/fs/` (File System Access handles) with
  the WASM bridge `web/src/core-bridge/{client,worker,protocol,api}.ts`. It is the **only** file
  allowed to import them.
- Errors: `ProviderError` with a stable `code` (`provider.ts:1282-1292`); `ProviderErrorCode` union
  `provider.ts:812-851`; RFC 7807 mapping `companion-provider.ts:194-252` (`ProblemDocument`,
  `PROBLEM_CODES`, `codeFromStatus` — 409/412/428 → `stale_revision`, 422 → `validation_failed`);
  401 → `CompanionUnauthorizedError` :180-191.
- **rev handling**: every mutation sends `If-Match: <rev>` (`companion-provider.ts:1755-1756`);
  board/sprint/retro writes pass `rev ?? '*'`. Optimistic update + rollback in
  `features/backlog/queries.ts:202-225` (`useMoveItem`), cache patcher `patchCachedStatus` :166-186.
  `updateMany` runs sequentially so one stale rev doesn't abort a batch (`:1469-1485`).
- Query keys: `web/src/app/queryClient.ts:8-19` (staleTime 5 s, gcTime 15 m, retry 1); backlog key
  factory `features/backlog/queries.ts:32-43` + `stableFilterKey` :46-51; KB keys
  `features/kb/useKbData.ts:21-27`; boards/sprints/retros/metrics each have their own `*-queries.ts`.
- Events → cache: `CompanionProvider.subscribe` :1725-1732 opens `/api/v1/events` (WS), backoff
  500 ms→30 s :110-112, degrades to 30 s polling after 3 failed opens :115-118, token as `?token=`
  (`token.ts:142-147`). Bridged into Query by `useBacklogEvents` (`queries.ts:130-148`) and
  `useKbInvalidation`.

### 4.5 Mode detection / capabilities

`web/src/api/detect.ts` — `resolveCompanionBaseUrl()` :31-39 (`VITE_COMPANION_URL` > same-origin >
`http://127.0.0.1:7317`), `probeCompanion()` :73-101 (`GET /api/v1/health`, 1.5 s abort),
`detectCompanion()` :119-141 (honours `VITE_FORCE_PROVIDER`), `watchCompanion()` :173-216 (30 s
re-probe + `visibilitychange`), `probeCompanionNow()` :165-171. Orchestrated in
`web/src/app/providers.tsx:115-132`.

`Capabilities` = `{write, git, ssh, watch, fullTextSearch:'core'|'bleve', mcp, openInEditor,
maxBatchWrite}` (`provider.ts:681-693`); defaults `provider.ts:1295-1304` and
`companion-provider.ts:132-141`; mapped from `GET /api/v1/capabilities` by `toCapabilities`
`companion-provider.ts:751-766` (reads `features.{write,git,ssh,watcher,search,mcpHttp,
openInEditor}`, `limits.maxBatchWrite`). Browser capabilities derived from the mount
(`browser-provider.ts:242-259`) — `git/ssh/watch/mcp/openInEditor` **always false**.
**UI branches on capabilities, never on provider kind** (`AppShell.tsx:442-445`).
→ A `youtrack` capability flag belongs in the server's `features` map
(`internal/server/server.go:411-435`) and in `toCapabilities`.

### 4.6 Settings pages

`web/src/features/settings/SettingsPage.tsx:40-136` composes cards: Runtime (mode, companion
version/URL, event-stream state, "Check again"), `CompanionTokenCard` :153+, `TunnelCard`,
`TeamProjectsCard`, `GitSettingsCard`, `SyncProxyCard`, `McpToolsCard`, Capabilities :117-133.

Existing settings and their transport:
- Companion bearer token → `sessionStorage` `gintrack:companion-token` (`api/token.ts:21`),
  auto-captured from `?token=` and stripped with `history.replaceState` :101-130.
- Commit-on-save → `GitSettingsCard.tsx:47,62-66` via `provider.getGitSettings/updateGitSettings`;
  browser mode persists to `localStorage` prefix `gintrack.git.settings` (`web/src/git/settings-store.ts:25`).
- CORS proxy / sync → `SyncProxyCard.tsx:44-72` (`getSyncSettings`/`updateSyncSettings`).
- MCP write tools → `McpToolsCard.tsx:34,55`. Tunnel → `TunnelCard.tsx`.
  Team projects → `TeamProjectsCard.tsx` (the only card using TanStack Query, :101-110, :147/:162).

**Form patterns: no react-hook-form, no zod-in-forms.** Plain `useState` + controlled inputs +
`useEffect` load + imperative save. zod is used only for router search params
(`features/backlog/search.ts`, `features/editor/search.ts`, `features/boards/board-form.ts`).
Hand-rolled validation in `features/editor/front-matter.ts:273` (`validateValues → Diagnostic[]`).
A YouTrack settings card fits this pattern exactly: new card in `features/settings/`, new
`DataProvider` methods, new `GET|PATCH /api/v1/…/settings` pair.

### 4.7 Backlog view

- `features/backlog/ItemTable.tsx` (483 l) — TanStack Table `useTable` :260-266,
  `createSortedRowModel` :49; sortable columns only `id,title,priority,updated` :57-60; sorting is
  **URL state** :136-145; data from `useItems(filter)` :112 (infinite query + "Load more").
- **The URL is the filter**: `features/backlog/search.ts:58-73` zod schema
  (`q,type,status,category,priority,label,assignee,milestone,parent,view,sort,order`),
  `validateItemSearch` :109-111, `toItemFilter` :169-202, `quickViews` :229-271;
  setter hook `features/backlog/use-search.ts`.
- Pieces: `FilterBar.tsx` (native `<details>` + checkbox multiselects :28-45), `QuickViews.tsx`,
  `BulkMoveBar.tsx`, `Badges.tsx`, `item-meta.ts`, `identity.ts`, `FeatureLink.tsx`, `NewItemLink.tsx`.
- Detail: `features/backlog/ItemDetail.tsx` (653 l) — `ItemBody.tsx` (markdown, interactive task
  checkboxes), `CommentsPanel` :175-265, `DeleteItemDialog.tsx`, children/epic context.
- Create/edit: `features/editor/NewItemPage.tsx:24+`, `ItemEditorPage.tsx:43+` — `FrontMatterForm`
  (`components/editor/FrontMatterForm.tsx`) + `MarkdownEditor` (CodeMirror) + `DiagnosticList`;
  rev-checked save with `ConflictDialog.tsx`, 2 s autosave, `localStorage` drafts
  (`features/editor/drafts.ts`), `useBlocker` on unsaved changes.

### 4.8 Autosuggest / combobox — what exists

- **No `cmdk`, no shadcn `Command`, no command palette.** (Not in `web/package.json`, not in
  `web/src/components/ui/`.)
- The one typeahead: `/www/git-in-track/web/src/components/editor/ItemPicker.tsx:29-163` — a
  hand-rolled ARIA combobox over `<Input>` (`role="combobox"`, `aria-expanded`, `aria-controls`,
  `aria-autocomplete="list"` :90-94) with a `role="listbox"` popup :133-160, 200 ms debounce
  :23/:51-58, backed by `useQuery(['items', project,'list','picker',…])` → `provider.listItems({limit:10})`
  :77-81, blur close with 150 ms grace :111-115. Used by `FrontMatterForm` for parent/milestone.
  **This is the component to copy for a YouTrack project picker.**
- Editor-local autosuggest: CodeMirror `autocompletion` for `[[wikilinks]]` in
  `components/editor/MarkdownEditor.tsx:1-8`, :47/:59-60.
- `features/workspace/WorkspaceSearch.tsx` is a debounced search panel (`useDeferredValue`, min 2
  chars :11-14), not a palette. `components/ui/select.tsx:1-12` is a styled **native** `<select>`.

### 4.9 Comments + feedback mode (commit `07105d7`)

- Comments: `useComments` → `provider.listComments` (`features/backlog/queries.ts:102-109`);
  rendered in `CommentsPanel` (`ItemDetail.tsx:175-217`) via `ItemBody`, cache key
  `${comment.path}@${comment.rev}` :207-210. Create via `useAddComment` (`queries.ts:292-304`),
  composer `ItemDetail.tsx:219-262`, gated on `capabilities.write`.
- Feedback mode files added by `07105d7`: `web/src/features/feedback/{feedback-store.ts,
  FeedbackSelection.tsx,FeedbackPanel.tsx,format.ts,selection.ts,feedback.test.tsx}`, plus edits to
  `ItemDetail.tsx`, `ItemBody.tsx`, `kb/KbViewer.tsx`, `kb/useKbData.ts`,
  `markdown/{pipeline,sanitize,types}.ts`, new `markdown/source-lines.ts`, and provider methods in
  all four `api/*provider*.ts`.
- Selection → note: markdown rendered with `sourceLines:true` stamps `data-line-start/-end`
  (`web/src/markdown/source-lines.ts`, opt-in `ItemBody.tsx:30-31,48-52`);
  `features/feedback/selection.ts:1-9` reads the DOM selection into `{quote,startLine,endLine,rect}`
  with a `locateQuote` fallback :50-60; `FeedbackSelection.tsx:37+` positions a 320 px overlay.
- Drafts: `feedback-store.ts` — `localStorage` key
  `gintrack:feedback:<item|kb>:<project>:<ref>` :42-48, `useSyncExternalStore` + cross-tab
  `storage` sync :111-125, API `{draft,setActive,addNote,updateNote,removeNote,clear}` :136-196.
- Save targets: **item → one comment** (`ItemDetail.tsx:547-566` + `features/feedback/format.ts:9-22`
  `formatFeedbackComment`, POST `/api/v1/items/{id}/comments`); **KB page → feedback block**
  (`KbViewer.tsx:162-191` → `useAddPageFeedback` `features/kb/useKbData.ts:63-79` →
  `provider.addPageFeedback` `provider.ts:982-988` → `POST <kbBase>/feedback`), rev-guarded.
- Shared `FeedbackPanel.tsx:27+` with `destination:'comment'|'page'`.

### 4.10 KB view/edit

- **Viewer only — there is no KB edit UI.** `provider.writePage` exists (`provider.ts:975`) but its
  only caller in `src/` is a test (`features/kb/KbViewer.test.tsx:316`).
- `features/kb/KbViewer.tsx` (535 l): tree / page / outline columns, `useKbTree` :74 + `useKbPage`
  :89, splat resolution via `kb-links.ts`, raw-source toggle :97, maximize, feedback mode.
  Supporting: `KbTree.tsx`, `KbToc.tsx`+`toc.ts`, `KbFrontMatter.tsx`, `KbBacklinks.tsx`,
  `KbLink.tsx`, `useKbData.ts`.
- Markdown pipeline `web/src/markdown/pipeline.ts:1-25`: remark-parse → frontmatter → gfm → math →
  wikilink → callout → remark-rehype (`allowDangerousHtml:false`) → rehype-slug + heading anchors →
  mermaid placeholder → task-list → source-lines (opt-in) → asset resolution → shiki (lazy) →
  **rehype-sanitize last** (`markdown/sanitize.ts`). Rendered by `MarkdownContent.tsx` via
  `hast-util-to-jsx-runtime`; 50-entry cache keyed by `cacheKey` :59-61. Public surface only via
  `web/src/markdown/index.ts`.
- CodeMirror is used for the **item** editor only (`components/editor/MarkdownEditor.tsx`,
  `MarkdownPreview.tsx`, `editor-theme.ts`, `markdown-commands.ts`).

### 4.11 i18n

**Not implemented.** No i18next/react-intl/formatjs in `web/package.json`, no `src/i18n/`, no locale
files, no `t()`/`useTranslation`. Every string is a hardcoded English literal.
`docs/05-web-app.md:1240-1253` (§11) *plans* i18next with `src/i18n/locales/<lang>/<ns>.json`,
English + Spanish; `docs/02-architecture.md:817` (§11.3) covers the same. Only `Intl`-adjacent usage
today is `toLocaleString`/`localeCompare`.

### 4.12 UI kit and tokens

`web/src/components/ui/` (18 files): `badge, button(+test), card, checkbox, dialog, field.ts, input,
label, logo, progress, select, switch, table, textarea, theme-toggle, toast, tooltip`.
shadcn config `web/components.json` (slate, cssVariables, `vendored:["button","card","input"]`).
Radix is used for exactly two primitives — `dialog.tsx:1`, `tooltip.tsx:1`. **No `Command`,
`Popover`, `DropdownMenu`, `Sheet`, `Tabs`, `Accordion`.**
Tailwind `web/tailwind.config.ts` — `darkMode:['class','[data-theme="dark"]']` :13, every colour an
`hsl(var(--token)/<alpha-value>)` alias :22-77, intent-named shadows :83-91, no plugins :129.
Tokens in `web/src/index.css` (light + `prefers-color-scheme:dark` + `[data-theme="dark"]`),
enforced by `web/scripts/check-design-tokens.mjs` (`npm run tokens:check`).
Styleguide `web/styleguide.html` + `web/src/dev/Styleguide.tsx`.
---

## 5. MCP server (`/www/git-in-track/internal/mcp/`)

### 5.1 The 13 tools

6 read + 7 write. Registered by `registerTools(s)` — `internal/mcp/tools.go:24` → `registerItemTools`
(`tools_items.go:145`), `registerBoardTools` (`tools_board.go:42`), `registerKBTools` (`tools_kb.go:58`).

| Tool | Mode | Defined | Handler |
|---|---|---|---|
| `list_items` | read | `internal/mcp/tools_items.go:147` | `listItems` :234 |
| `search_items` | read | `tools_items.go:157` | `searchItems` :275 |
| `get_item` | read | `tools_items.go:166` | `getItem` :306 |
| `create_epic` | write | `tools_items.go:175` | `createTool(core.TypeEpic)` :359 |
| `create_story` | write | `tools_items.go:182` | `createTool(core.TypeStory)` |
| `create_task` | write | `tools_items.go:190` | `createTool(core.TypeTask)` |
| `create_milestone` | write | `tools_items.go:198` | `createTool(core.TypeMilestone)` |
| `update_item` | write | `tools_items.go:207` | `updateItem` :401 |
| `add_comment` | write | `tools_items.go:219` | `addComment` :458 |
| `move_on_board` | write | `internal/mcp/tools_board.go:44` | `moveOnBoard` :59 |
| `list_kb_pages` | read | `internal/mcp/tools_kb.go:60` | `listKBPages` :87 |
| `get_kb_page` | read | `tools_kb.go:68` | `getKBPage` :211 |
| `search_kb` | read | `tools_kb.go:77` | `searchKB` :262 |

*Doc drift to fix if touched:* `docs/07-cli-and-api.md:896-909` lists 12 names (no `create_milestone`)
and line 916 says "six write tools"; there are 7. `docs/08-mcp-server.md` is correct.

### 5.2 How a tool is defined

`toolDef` — `internal/mcp/tools.go:31-47`: `Name`, `Title`, `Description`, `Write bool`,
`Idempotent bool`, `Untrusted bool`.

```go
// internal/mcp/tools.go:59
func register[In, Out any](s *Server, def toolDef, handle func(context.Context, *Server, In) (Out, error)) {
```

A complete small definition, verbatim (`internal/mcp/tools_kb.go:76-81`):

```go
	register(s, toolDef{
		Name:        "search_kb",
		Title:       "Search the knowledge base",
		Description: "Ranked full-text search over knowledge-base pages, with an excerpt around each match.",
		Untrusted:   true,
	}, searchKB)
```

Mechanics inside `register`:
- `tools.go:60-62` — **the write gate**: `if def.Write && !s.allowWrite { return }`. A write tool on a
  read-only server is **not advertised at all**, not merely refused.
- `tools.go:63-66` — appends `untrustedNote` (`tools.go:52`) to the description when `Untrusted`.
- `tools.go:69-79` — `*sdk.Tool` with `Annotations{ReadOnlyHint: !def.Write, IdempotentHint,
  OpenWorldHint:&false}`.
- `tools.go:80` — `sdk.AddTool`; **the SDK infers both JSON schemas by reflection over `In`/`Out`**
  and validates arguments in and answers out. Property descriptions come from `jsonschema:"…"`
  struct tags (`tools_items.go:21-36`).
- `tools.go:82-87` — second gate returning `codeWriteDisabled`.
- `tools.go:93-95` — `Meta{"dev.git-in-track/contentTrust":"untrusted-repository-content"}`
  (constants `internal/mcp/server.go:21,25`).
- `tools.go:98` — `s.tools = append(...)`, sorted in `New` (`server.go:153`).

### 5.3 Errors and results

`internal/mcp/errors.go` — codes `invalid_request` :18, `forbidden_path` :20, `write_disabled` :24,
`invalid_cursor` :27, `not_found` :29, `precondition_required` :33. `toolError` :50-67
(`code, message, field?, path?, currentRev?, conflicts[]?, retry?, expected?`); `Error()` :70
marshals to `{"error":{…}}` — **the error text is the machine-readable payload**. Builders
`failf` :83, `invalidField` :89; `fromVault(err)` :97 attaches `currentRev`/`conflicts` for
`stale_revision`; `requiredRev` :130 (empty → `precondition_required`; `"*"` → `""`).
`internal/mcp/result.go` — `writeSet` :19 / `paths()` :28 (which files changed, never contents),
`decodeResult[T]` :41, `writeResultOf` :57.

### 5.4 Wiring

- `internal/mcp/server.go` — `Dispatcher` interface :56 (`Dispatch(ctx, method string, params []byte)
  (any, error)`) is **the package's entire coupling to the product**; `*vault.Workspace` satisfies it.
  `Options` :79-102 (`Core`, `Version`, `Agent`, `AllowWrite`, `Roots []string`, `AfterWrite`,
  `Logger`, `Now`); `New` :121 sets `Instructions` (:30-47) and calls `registerTools`; generic
  `dispatch[T]` :172, `dispatchRaw` :198, `announce` :211, `authorName` :220, `Tools()` :160.
- `internal/mcp/transport.go` — `ServeStdio` :26, `HTTPHandler` :40, `HTTPHandlerFor` :56.
- `internal/mcp/wire.go` — output shapes + `fields` projection (`Item` :20, `Comment` :53, `Page` :64,
  `Hit` :78, `itemOf` :92, `defaultItemFields` :136, `projectItem` :144).
- `internal/mcp/page.go` — pagination (`defaultPageSize=20`, `maxPageSize=100` :24-27, opaque base64
  `cursor{Offset,Filter}` :45, `fingerprint` :92). `internal/mcp/paths.go` — `cleanVaultPath` :27,
  `PathGuard` :90 / `Check(field,p)` :122 (symlink resolution against roots).
- Host: `internal/server/mcp.go` (`mcpPath="/mcp"` :25, `mcpState` :40, `newMCPServer` :73 with
  `Roots` + `AfterWrite: s.publishAgentWrite` :87, `setWrites` :116 rebuilds the live server,
  `persist` :134, `mountMCPSettings` :185, mount :248) and `cmd/gintrack/mcp.go:91-100` for stdio.

### 5.5 Adding a YouTrack MCP tool — the checklist

1. **Add the core method first** — `internal/vault/vault.go:321` (per-vault) or
   `internal/vault/dispatch.go:73` (workspace). **No business logic may live in `internal/mcp`.**
2. Define `In`/`Out` structs in a new `internal/mcp/tools_youtrack.go`, with
   `json:"…,omitempty"` + `jsonschema:"…"` tags; every returned item carries `id` and `rev`.
3. One `register(s, toolDef{…}, handler)` call in a `registerYouTrackTools(s)` added to
   `registerTools` (`tools.go:24`).
4. Handler: validate with `invalidField`, call `requiredRev` before any write, `s.guard.Check` on any
   path, dispatch via `dispatch[T]`/`dispatchRaw`, project through `wire.go`/`writeResultOf`, and
   `s.announce(ctx, WriteEvent{…})` on a write (`tools_items.go:388-391`).
5. **Update the surface-pinning tests** or they fail: `internal/mcp/tools_test.go:16` (`readTools`),
   `:21` (`writeTools`), `TestToolSurface` :26; `cmd/gintrack/mcp_test.go:28`, `:37-40`.
   Behaviour test via the in-memory client harness `internal/mcp/mcp_test.go:57`.
6. Update prose: `cmd/gintrack/mcp.go:36-38` (long help), `docs/08-mcp-server.md` §4 table (line 199),
   `docs/07-cli-and-api.md` §4.9.

No transport, server-registration or schema-file changes.

---

## 6. CLI (`/www/git-in-track/cmd/gintrack/`)

Root: `main()` `cmd/gintrack/main.go:25`; `Execute` `root.go:43`; `newRootCommand` `root.go:50`,
command at `:53` with `SilenceUsage/SilenceErrors` :65-66, `PersistentPreRunE: configureLogging` :68
(`:105`), `SetFlagErrorFunc` → exit 2 :74.
Persistent flags `root.go:77-83`: `--config`, `--workspace/-w`, `--log-level`, `--no-color`,
`--quiet/-q`, `--verbose/-v`. `--json` is per-command, never global.

Registered at `root.go:85-100`:

| Command | File:line |
|---|---|
| `serve` | `cmd/gintrack/serve.go:55` |
| `mcp` | `cmd/gintrack/mcp.go:31` |
| `version` | `cmd/gintrack/version.go:18` |
| `completion [bash\|zsh\|fish\|powershell]` | `cmd/gintrack/completion.go:14` |
| `init [path]` | `cmd/gintrack/init.go:58` |
| `add <path>` | `cmd/gintrack/add.go:47` |
| `ls` | `cmd/gintrack/ls.go:59` |
| `rm <id>` | `cmd/gintrack/rm.go:17` |
| `index [id]` | `cmd/gintrack/index.go:74` |
| `snapshot [KEY...]` | `cmd/gintrack/snapshot.go:61` |
| `sync` | `cmd/gintrack/sync.go:59` |
| `item` | `cmd/gintrack/item.go:21` |
| `doctor` | `cmd/gintrack/doctor.go:79` |
| `config` | `cmd/gintrack/config.go:23` |

`item` subcommands (`item.go:33-41`): `list` `item_list.go:45`, `get` `item_get.go:29`,
`new` `item_new.go:52`, `edit` `item_edit.go:40`, `move` `item_move.go:24`,
`comment` `item_comment.go:27`, `link` `item_link.go:42`.
`config` subcommands (`config.go:33-37`): `path` :45, `show` :82, `init` :113.
**There is no `board`/`sprint`/`retro` CLI command** despite `docs/07 §4.6` (line 731) specifying one.

`serve` flags `serve.go:78-93`: `--port --bind --no-open --token --dev --watch --idle-timeout
--repo --mcp-http --mcp-allow-write --mcp-agent --tunnel`. `mcp` flags `mcp.go:57-64`:
`--allow-write --agent --repo --list-tools`.

Config loading: `globalFlags` `root.go:26-40`; `resolve()` `root.go:142` calls `config.Resolve` once
per command tree; `save()` `root.go:156`. Vault opening shared via `cmd/gintrack/workspace.go` —
`openVault` :78, `project` :123, `store` :164, `repoPath` :173.

Output: `cmd/gintrack/output/output.go` — `Table` :27, `JSON` :90, `Printer` :103 (`New` :111,
`Notes()` :126 — **human lines go to stderr in JSON mode**, which makes `… --json | jq` safe,
`Table` :134 no-ops in JSON, `Warnf` :162 always stderr); built by `flags.printer` `root.go:172`.
`cmd/gintrack/format.go` holds pure formatters (`render` :13, `orDash` :24, `ago` :60, `countsByType` :82).
Exit codes `cmd/gintrack/exit.go:14-22` (0–6), `exitCode` :61, arg validators `exactArgs` :86 /
`rangeArgs` :96 / `noArgs` :106 (use these, not cobra's raw ones — they carry exit 2).

**Adding a top-level command**: new `cmd/gintrack/<name>.go` with
`func new<Name>Command(flags *globalFlags) *cobra.Command`; use the `exit.go` arg validators; in
`RunE` do `flags.resolve()` → `flags.printer(cmd, asJSON)` → `openVault(...)`; register in
`root.go:85-100`; test with the in-process harness `cmd/gintrack/harness_test.go:28`; document in
`docs/07 §4`. No business logic in the command file (`main.go:5-7`).

---

## 7. Testing, lint, Makefile

### 7.1 Go tests

119 test files, always co-located, **white-box** (the only `_test` external package is
`internal/core/osfs/osfs_test.go:1`). Stdlib `testing` only — no assertion library, mandated at
`AGENTS.md:337`. Table-driven + `t.Run` (81 / 101 files); `t.Parallel()` at 359 sites.
No benchmarks, no fuzz targets.

- Harness: `cmd/gintrack/harness_test.go` runs commands **in-process** — `newHarness(t)` :28
  (TempDir + copies `testdata/fixtures/project-basic` + fake `.git` :34 + `t.Setenv("GINTRACK_CONFIG",…)` :38),
  `run` :51, `mustRun` :74, `decode[T]` :84.
- Two `TestMain`s, both about external-tool hygiene: `internal/server/main_test.go:18` and
  `internal/gitops/main_test.go:19` (pin `gc.auto=0`, `maintenance.auto=false`; jj config dir :41).
- MCP tests drive a real client session over an in-memory transport —
  `internal/mcp/mcp_test.go:57`.
- Fixtures: `testdata/fixtures/{project-basic,team-basic,team-second}` and
  `internal/core/testdata/{golden,index,invalid,store,validate}`.
- **Golden files exist on both sides**: `internal/core/frontmatter_test.go:16` has the repo's only
  `-update` flag (`compareGolden` :373), `internal/core/snapshot_test.go:13`,
  `internal/core/tasklist_test.go:169`; web `web/src/markdown/golden.test.ts:59`
  (`toMatchFileSnapshot`). Policy `AGENTS.md:339-341` — "never regenerate them to make a test pass".
- Race-tag pairs declaring `raceEnabled`: `internal/gitops/race_{on,off}_test.go`,
  `internal/tunnel/race_{on,off}_test.go`.

### 7.2 Vitest

**No `vitest.config.*`** — the config is in `web/vite.config.ts` (imported from `vitest/config`, l. 4),
`test` block lines 44-56: `globals:true` :45, `environment:'jsdom'` :46,
`setupFiles:['./src/test/setup.ts']` :47, `css:false` :48,
`include:['src/**/*.{test,spec}.{ts,tsx}']` :49, `restoreMocks:true` :50, timeouts 20 s :54-55.
Setup `web/src/test/setup.ts` (jest-dom, `asyncUtilTimeout:5000`, `afterEach(cleanup)`).
~70 web test files. **No MSW** — the network is faked at the provider seam
(`web/src/api/fake-provider.ts`, `web/src/test/router.tsx:44-94` `renderWithRouter`); companion HTTP
is tested with an injected `fetchImpl`/`webSocketFactory` (`companion-provider.ts:159-172`).
Planned but **absent**: Playwright e2e, axe scans, bundle budgets (`docs/05 §14`, line 1309).

### 7.3 Makefile (`/www/git-in-track/Makefile`, 135 lines)

`help` :38 · `deps` :42 · `wasm` :46 · `wasm-smoke` :59 · `web` :62 · `build` :65 (default goal) ·
`test` :69 · `test-go` :72-74 · `test-web` :76 · `lint` :79 · `lint-go` :81 · `lint-web` :98 ·
`lint-ci` :101 · `fmt` :112 · `run` :115 · `dev` :118 · `release-check` :121 ·
`release-snapshot` :124 · `clean` :131.

- `make test` = `CGO_ENABLED=1 go test -race -covermode=atomic -coverprofile=coverage.out $(GO_PKGS)`
  (`:72-74`; race needs cgo, the rest of the build is `CGO_ENABLED=0` `:33`) **then**
  `cd web && vitest --run` (`:76`). Same command in `.github/workflows/ci.yml:150`.
- `make lint` = gofmt check (non-empty ⇒ exit 1) → `go vet` → `golangci-lint run --timeout=5m`, and
  when the binary is missing, `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2`
  — **it never silently skips** → `eslint . --max-warnings 0` + `tsc -b` → workflow YAML parse +
  `actionlint`. `GO_PKGS` at `:26`, `GOLANGCI_LINT_VERSION ?= v2.13.2` at `:31`.

### 7.4 `.golangci.yaml` (79 lines, `version: "2"`, `tests: true`)

17 linters :9-26: `errcheck govet staticcheck ineffassign unused revive errorlint wrapcheck
bodyclose contextcheck noctx gocritic misspell unconvert predeclared copyloopvar nilerr`.
`revive` rules: `exported`, `package-comments`, `error-strings`, `error-naming`,
`context-as-argument`, `indent-error-flow`. `errcheck.check-type-assertions: true`.
`gocritic` tags `[diagnostic,style,performance]` minus `hugeParam`/`rangeValCopy`.
Formatters: `gofmt` + `goimports` with `local-prefixes: github.com/digiogithub/git-in-track`.
Exclusions: `web/node_modules`; `_test\.go` ⇒ no errcheck/wrapcheck/gocritic.

**`bodyclose`, `noctx`, `contextcheck` and `wrapcheck` are all on** — directly relevant to writing a
YouTrack HTTP client: every response body must be closed, every request built with
`http.NewRequestWithContext`, every error wrapped.

---

## 8. Constraints and gotchas the planner MUST respect

1. **`internal/core` must compile to WASM.** `internal/core/doc.go:11-12` and `AGENTS.md`:
   no `os`, `os/exec`, `syscall`, `syscall/js`, **no `net/http`**, no fsnotify, no go-git.
   → **The YouTrack HTTP client cannot live in `internal/core`.** Put it in a new native package
   (`internal/youtrack`) and let `internal/core` own only the pure mapping/merge/anchoring rules if
   any are shared. Cf. `internal/gitops`, which is the existing "native-only side-effect" package.
2. **Browser-only mode has no network reach.** `browser-provider.ts:242-259` reports
   `git/ssh/watch/mcp/openInEditor: false`; the CORS proxy (ADR-025) refuses everything but the
   three git smart-HTTP paths and refuses private addresses. So **YouTrack features must be
   companion-only and gated on a capability flag**, the way `git`/`mcp`/`tunnel` already are.
3. **The frontend must not import Node modules** (`AGENTS.md`), and feature code must not call
   `fetch('/api')` directly — everything goes through `DataProvider`
   (`web/src/api/provider.ts:6`, `docs/05 §4`). Adding a method means implementing it in
   **all four** providers (companion, browser, fake, and the interface).
4. **Data-model changes need docs + an ADR in the same change** (`AGENTS.md`, mandatory rules;
   `docs/10-development-guidelines.md:888-890`). Any new front-matter key requires:
   `docs/03-data-model.md` (field table, §3.2 canonical key order, §18 JSON Schema) **plus**
   `docs/adr/ADR-031-*.md` (next free number — 030 is the highest). ADRs are immutable once
   accepted; negative consequences are mandatory (`docs/adr/README.md:9-31`).
5. **Conventional Commits, exactly one scope** from `core, cli, server, web, wasm, mcp, docs, ci`
   (`docs/10-development-guidelines.md:481-494`; the scope↔directory table is there). Split
   cross-cutting work into one commit per scope. Nothing enforces this in CI — it is a review rule.
6. **No secret store exists, and "we store no credentials" is a written product promise**
   (`docs/10-development-guidelines.md:707-711`). Changing it is an ADR-level decision, not an
   implementation detail. No keyring dependency in `go.mod`.
7. **`GINTRACK_TOKEN` is already overloaded** — it is the companion's API bearer token
   (`internal/config/load.go:30`, `:201`) *and* the go-git HTTP fallback password
   (`internal/gitops/credentials.go:351`). Do not add a third meaning; pick a distinct
   `GINTRACK_YOUTRACK_TOKEN`.
8. **Per-repository settings do not exist.** `CHANGELOG.md:374` — "Per-repository `git:` overrides
   are not implemented; settings are per workspace." `config.Repo` (`internal/config/config.go:100-121`)
   holds only `ID/Path/Role/DocsFolder(s)/Enabled`. A **per-project** YouTrack link therefore has no
   existing per-project settings home outside `project.yaml`.
9. **`ProjectConfig` drops unknown keys** (`internal/core/project.go:23-40`, no `Extra` map) — safe
   today only because `project.yaml` is never fully re-serialized (only `Allocator.writeCounter`
   `internal/core/allocator.go:414` touches it, surgically via `setYAMLPath` :545). Any code that
   rewrites `project.yaml` wholesale would silently delete a hand-added block.
10. **Every write is rev-checked.** No surface may split a multi-field write into two
    (`docs/03` R-REV-3c). An importer writing many items must carry each item's rev or be creating.
11. **No job queue, no worker pool, no persistent job store, no embedded DB.** No `errgroup`,
    `semaphore` or `singleflight` anywhere. The whole codebase runs 7 non-test goroutines.
    Git sync is **synchronous inside the HTTP handler** (`internal/server/sync.go:145`).
12. **Untrusted-content discipline.** Repository text is data, never instructions
    (`internal/mcp/tools.go:52` `untrustedNote`; `docs/10:717-724`). **Imported YouTrack issue
    bodies and comments are untrusted third-party content and must inherit the same treatment**,
    plus Markdown sanitisation (`web/src/markdown/sanitize.ts`, rehype-sanitize runs last).
13. **Path safety** (`docs/10:713-716`, `internal/mcp/paths.go`): every path from external input is
    cleaned and verified inside the vault root. YouTrack article paths mapped to KB pages must go
    through the same guard.
14. **Both operating modes, or scope the story explicitly** (`docs/10:845`).
15. **Performance targets are CI-enforced** (`docs/02 §9`, line 664): a cold index of 10 000 items
    must stay under 2 s native / 8 s WASM. A large YouTrack import must not inflate the indexed
    corpus in a way that blows these, and `core.wasm` must stay under 3 MB Brotli.

---

## 9. Recommendations — where each feature area goes

### (a) Linking a gintrack project to a YouTrack project

**New package `internal/youtrack/`** (native only): a small typed REST client over
`net/http` — `Client`, `Options{BaseURL, Token, HTTPClient}`, `Projects(ctx, query)` for the
autosuggest, `Issues(ctx, …)`, `Articles(ctx, …)`, `AddComment(ctx, …)`. Model it on
`internal/gitops` (native side-effect package, injected seams for tests) and on
`internal/server/cors_proxy.go` for the defensive HTTP posture. Remember `bodyclose`/`noctx`/`wrapcheck`.

**Config, split in two:**
- *Committed, per project* → a new `integrations.youtrack:` block in `project.yaml`
  (`ProjectConfig` `internal/core/project.go:23-40` + `docs/03 §6.1` table + ADR-031):
  `{url, project, enabled, field_map, direction}`. It sits naturally beside `team:`
  (`project.go:161`) and `links:` (`project.go:167`), both of which are exactly this shape.
  **Do not put the token here.**
- *Machine-local secret* → a new `integrations:` section in `internal/config/config.go`
  keyed by project key, in the existing `0600` file, **plus** a `GINTRACK_YOUTRACK_TOKEN` env
  override registered in `internal/config/load.go:186` `applyEnv`. Persist through the exact
  `gitState.persist()` pattern (`internal/server/git.go:336-352`: reload → mutate one section →
  `config.Save`), and return `persisted bool` as `gitSettings` does (`git.go:69`).
  **Requires an ADR** because `docs/10:707-711` currently promises no credential is stored.
  Browser-only mode: in-memory per session only, as the git-PAT rule already demands.

**Server**: `p.Route("/youtrack", s.mountYouTrack)` in `internal/server/api.go` (next to `:94-96`)
serving `GET|PATCH /api/v1/youtrack/settings`, `GET /api/v1/youtrack/projects?q=` (the autosuggest
proxy — the companion calls YouTrack, the browser never does), `POST /api/v1/youtrack/test`.
Add `features.youtrack` to `handleCapabilities` (`internal/server/server.go:411-435`) and to
`toCapabilities` (`web/src/api/companion-provider.ts:751-766`).

**Frontend**: a `YouTrackCard` in `web/src/features/settings/` added to `SettingsPage.tsx:40-136`,
using the existing plain-`useState` card pattern. **The project picker should be a copy of
`web/src/components/editor/ItemPicker.tsx:29-163`** — the repo's only combobox (ARIA roles, 200 ms
debounce, `useQuery`-backed). There is **no `cmdk`/shadcn `Command`**; either vendor one (new
dependency, needs justification per AGENTS.md) or extract `ItemPicker` into a generic
`Combobox` in `web/src/components/ui/`.

### (b) Importing YouTrack issues into the backlog

**Mapping + write logic in `internal/vault`**, not `internal/mcp` and not `internal/server`.
Add methods to the dispatch table — `youtrack.import.preview`, `youtrack.import.run` —
in `internal/vault/vault.go:320-404` (per-vault) or `internal/vault/dispatch.go:73` (workspace),
which automatically serves REST, MCP and (where sensible) the browser.
Writes go through `core.FileStore.Create` (`internal/core/store.go:321`) — **`ItemDraft.ID`
(`store.go:152`) already exists "for importers"**, though it pins a gintrack id, not a foreign one.
Use `WriteSet`/`v.commit(ctx)` (`vault.go:1511`) so the import lands as commits and WS events like
any other write.

Type mapping: YouTrack Epic→`epic`, User Story→`story`, Task→`task`, Milestone/Version→`milestone`;
subtasks → `parent`; YouTrack links → `links[]` (`blocks`/`blocked_by`/`relates_to`/`duplicates`
only — `LinkKind.Valid()` `internal/core/model.go:116` rejects anything else);
comments → one file each per ADR-012.

**Storing the YouTrack link/id — recommended: `custom:`.** `custom.youtrack_id` /
`custom.youtrack_url`, declared in `project.yaml:custom_fields`
(`internal/core/project.go:140-149`). It is the sanctioned extension point (**R-CF-1**,
`docs/03:1148`), it is already surfaced in the UI (`web/src/components/editor/CustomFieldsEditor.tsx`),
it is queryable, and **it needs no data-model change or ADR**.
The zero-code alternative is an `x-youtrack:` top-level key (**R-CF-4**), preserved in `Item.Extra`
(`internal/core/model.go:370`) but invisible to index, query, `ItemDraft` and `ItemPatch`.
A first-class `external:` field is the cleanest long-term shape but costs: `core.Item`,
`frontmatter.go` reader + `SerializeItem`, `docs/03 §3.2` key order, `ItemDraft`/`ItemPatch`,
the JSON Schema, and a mandatory ADR.

### (c) Pushing comments/feedback to YouTrack

The hooks already exist and should be reused rather than duplicated:
- `internal/server/events.go:256` `publishWrite` and `internal/vault` `comment.add`
  (`vault.go:1185`) are where a new comment becomes observable.
- MCP writes already fan out through `Options.AfterWrite` → `s.publishAgentWrite`
  (`internal/server/mcp.go:87`, `:260`) — the same seam shape works for a YouTrack push.
- Feedback on an item is **already just a comment** (ADR-030 decision 2,
  `web/src/features/feedback/format.ts:9-22`), so (c) needs **no separate feedback path** — push the
  comment and you have pushed the feedback. Feedback on a KB page is a block in the page and belongs
  to (d), not (c).
- Push must be enqueued, not done inline, or it blocks the write. See (e).

### (d) KB pages ↔ YouTrack Knowledge Base articles

Core read/write already exist: `FileStore.ReadPage` / `WritePage`
(`internal/core/store.go:968`, `:986`), vault `kb.page` / `kb.write` (`vault.go:1290`, `:1356`),
REST `GET|PUT /api/v1/kb/page` (`internal/server/kb.go:20-21`).
`core.KBPage.FrontMatter` is a **free `map[string]any`** (`internal/core/kb.go:20-45`), so the
article id/url can live in page front matter with **no core change**.

Two gotchas:
- **`WritePage` prunes the feedback block on every write** (ADR-030 R-FB-4,
  `internal/core/kbfeedback.go:166`). A sync that round-trips a page through YouTrack must not carry
  the `## Feedback` block upstream, and must not treat its removal as a remote change.
- **There is no KB edit UI today** — `provider.writePage` (`web/src/api/provider.ts:975`) has no
  caller outside a test. A KB sync feature that needs conflict resolution in the UI will have to
  build that surface, or restrict itself to CLI/server-driven sync.

### (e) Background sync engine with worker queue and batches

**Nothing to plug into — this must be built.** No job/queue/worker abstraction, no persistent job
store, no embedded DB (no bolt/sqlite in `go.mod`), and git sync itself is synchronous
(`internal/server/sync.go:145`).

Build `internal/syncengine/` (native, `internal/server`-adjacent) and **copy
`internal/gitops/committer.go` closely** — it is the proven local pattern:
- `Committer` :76 with per-key `batch` :91, `Enqueue(ctx, Change)` :157 with coalescing key
  `repo + "\x00" + coalesceKey(fields)` :181/:405, `arm` :189 → `time.AfterFunc` :205,
  `DefaultDebounce=2s` :12 capped by `maxDebounce=15s` :18, `fire` :211,
  inflight accounting :119/:122/:135 over a `sync.Cond`, `Flush` :240, `Close` :277 (idempotent),
  `Pending` :267.
- **`context.WithoutCancel(ctx)`** (`committer.go:167`) so an HTTP response closing cannot cancel the
  work it caused — the same idiom is at `internal/server/tunnel.go:198` and `server.go:304`.
- Retry/backoff: reuse the shape of `internal/gitops/sync.go:766` `sleep(ctx,d)` +
  `defaultBackoff` :755 (500 ms / 1.5 s / 4 s) and the push-retry loop :661-684.
- For a supervised long-lived worker with restart and stale-event fencing, `internal/tunnel/tunnel.go`
  is the template (`generation atomic.Uint64` :114, `set(generation, mutate)` :321,
  `applyEvent` :395 dropping stale generations, `ensureObserver` :368).
- **Progress and results go over the existing Hub** — `s.hub.Publish("youtrack.sync.progress", …)`,
  mirroring `sync.progress` (`internal/server/sync.go:239`) and `git.commit` (`git.go:544`).
  `Hub.Publish` (`internal/server/hub.go:167`) never blocks; clients get a 256-event buffer with
  drop-on-overflow (`:31`, `deliver` :104) and a 1000-event replay ring (`:27`, `since` :234).
  The frontend already bridges these into TanStack Query (`useBacklogEvents`
  `web/src/features/backlog/queries.ts:130-148`), and the new event type must be documented in
  `docs/07-cli-and-api.md §5.6` (line 2353).
- **State persistence** (cursors, last-synced revs, failure counts): there is no store.
  Options are a JSON file next to the index cache (`config.CacheDir` `internal/config/path.go:96`)
  or — better for auditability and for git-native honesty — per-item `custom.youtrack_synced_at`.
  Do **not** introduce an embedded database without an ADR.
- **Settings UI**: a `SyncEngineCard` next to `SyncProxyCard.tsx`, backed by
  `GET|PATCH /api/v1/youtrack/settings` following `handleGitSettingsPatch`
  (`internal/server/git.go:374-393`) exactly, including the `persisted` flag.
- Register lifecycle in `Server.Start` beside `startWatch` :299 / `startTunnel` :303, with a
  `defer engine.Close(context.WithoutCancel(ctx))` beside `s.git.close` :308.

### (f) Later — agentic UI (AG-UI / CopilotKit) and Pando semantic search

**Agentic UI.** The cleanest seam is the existing MCP surface: `internal/mcp` already exposes the
whole product to an agent behind one interface (`Dispatcher`, `internal/mcp/server.go:56`) with a
read/write gate and an untrusted-content contract, and `HTTPHandlerFor` (`transport.go:56`) already
lets the host swap the server behind one mount. An AG-UI/CopilotKit front end should consume that,
not a parallel API. Frontend-wise it is a new route in `web/src/app/router.tsx` + a
`web/src/features/agent/` folder + new `DataProvider` methods; expect it to need the streaming
plumbing that only `subscribe()` (`companion-provider.ts:1725`) has today. Warnings: CopilotKit is a
large dependency (AGENTS.md: justify any new dependency), and it must degrade in browser-only mode
where `capabilities.mcp` is false.

**Pando semantic search.** `docs/02 §8` (line 631) already settles the architecture for this exact
case, using bleve as the worked example: an external engine is only ever "an **optional native
accelerator** behind the same `core/search` interface, never the only backend", because
`internal/core` must stay WASM-clean (ADR-003). Concretely: keep `Index.Search`
(`internal/core/query.go:544`) as the contract, add an optional native implementation in a new
package, select it in `internal/server`, and report it through the capability that **already exists
for this purpose** — `fullTextSearch: 'core' | 'bleve'` (`web/src/api/provider.ts:681-693`,
`internal/server/server.go` `features.search`). Extend that enum rather than adding a parallel flag.

### Summary of what does not exist yet

| Needed | Exists? |
|---|---|
| Job/worker queue, scheduler, retry framework | **No** — copy `gitops.Committer` |
| Persistent job/sync state store, embedded DB | **No** — no bolt/sqlite in `go.mod` |
| Secret store / keyring | **No** — and "no credentials stored" is a documented promise |
| Per-repository or per-project settings in `config.yaml` | **No** — settings are per workspace |
| Any external-system integration (YouTrack, Jira, …) | **No** — zero hits in the tree |
| Combobox / `Command` / cmdk / command palette | **No** — only `ItemPicker.tsx` |
| i18n | **No** — planned in `docs/05 §11`, not started |
| KB page edit UI | **No** — `writePage` exists, unused |
| `board`/`sprint`/`retro` CLI commands | **No** — REST/web only |
| Playwright e2e, MSW | **No** |
| Background/async HTTP jobs of any kind | **No** — git sync is synchronous |
