# Changelog

All notable changes to **git-in-track** are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project follows
[Semantic Versioning](https://semver.org/) as specified in
[docs/09-ci-cd-and-releases.md](docs/09-ci-cd-and-releases.md) §7.

Each release also carries a generated changelog on its GitHub Release page, grouped from
the Conventional Commit subjects that landed on `main`. This file is the hand-written
companion: migrations, deprecations, compatibility statements and known issues live here,
because a commit list cannot express them.

## [Unreleased]

### Added

- **The background job engine actually runs YouTrack work: four job kinds, and the HTTP
  surface that starts them** (GIT-US-0050, GIT-US-0054, GIT-US-0068, GIT-US-0087,
  docs/07 §4.1, §5.5, §5.6). `internal/server` registers `youtrack.import`,
  `youtrack.comment.push`, `youtrack.kb.publish` and `youtrack.kb.pull` with the engine
  before the journal is replayed, and installs the two seams every mounted vault needs —
  the provider that resolves a client and the enqueuer that hands a job to the queue — so a
  write path decides a job is needed and never performs it inline.
  **Import** pages issues with `$top`/`$skip` over a query that always carries
  `order by: created asc` (without it a `$skip` walk over a live instance silently skips and
  duplicates rows), imports in batches of 20 — one batch is one vault call and one commit —
  publishes `sync.job.progress` with `{jobId, done, total, currentId}` per batch, accumulates
  per-issue failures into the result instead of failing the job, and downloads attachments to
  a `.part` file that is renamed only after the stream closed at the size YouTrack reported,
  skipping a file already there. Cancellation is observed between batches and mid-download:
  the batches already committed survive, the temporary file is removed, and the job ends
  `cancelled` rather than being retried. **Comment push** posts a comment once and writes the
  remote id into the comment's `external` block rev-guarded, so a retry, a journal replay or
  a dead-letter retry edits that comment instead of leaving a second copy; the trailing
  attribution line renders from a per-project `integrations.youtrack.comment_template`, with
  the item id emitted bare because YouTrack auto-links what it recognises. A comment deleted
  locally is **never** deleted remotely: no job is enqueued for it, by design.
  **Knowledge base** publishes a page or a whole folder — one parent article per directory
  through `parentArticle`, parents before children, ordering taken from creation order
  because `ordinal` is read-only and is silently ignored when sent — and pulls articles back
  through the vault so `WritePage` and its feedback pruning apply normally. Change detection
  compares the fingerprint recorded in the page's `external.key` against both sides, never
  raw bytes, so a local feedback note is not mistaken for an edit; when both sides moved the
  incoming content is written to `<page>.conflict.md`, the page is left untouched and
  `youtrack.kb.conflict` says so. There is no three-way merge and deliberately none.
  Three routes make all of it reachable: `GET /api/v1/youtrack/issues` searches the instance
  for the import picker with the five presets `epics`, `stories`, `tasks`, `versions` and
  `unresolved`, bounded pages behind an opaque cursor and an `already imported` badge
  resolved **locally** from the index by `(external.system, external.id)`;
  `POST /api/v1/youtrack/import/preview` answers the plan synchronously; and
  `POST /api/v1/youtrack/import` queues the job and answers `202` with its id. No response,
  log line, event payload or journalled job payload carries the token.

- **The configuration file has a `sync.engine` section, so an engine setting survives a
  restart** (GIT-US-0084, docs/07 §3.2, §3.3, §4.1). `workers`, `batchSize`, `rate`,
  `maxAttempts` and `retention` are read from `config.yaml`, validated against the same
  ranges the running engine applies — an out-of-range value is refused with the dotted key
  that carries it — and layered under the `GINTRACK_SYNC_*` variables and the four
  `gintrack serve` flags, so the documented **flag > environment > file > default**
  precedence now holds in full. `PATCH /api/v1/sync/settings` writes the section back and
  reports `persisted: true`; a companion started without a configuration file still keeps
  the change for the life of the process and answers `false`.

- **The companion runs a background job engine, and it is inspectable over REST and
  live on the WebSocket** (GIT-US-0074, GIT-US-0078, GIT-US-0084, docs/07 §4.1, §5.5, §5.6).
  `internal/syncengine` now starts with `gintrack serve`, beside the watcher, the committer
  and the tunnel, and stops on the same path: shutdown drains the queue for a bounded grace
  period on a context **detached** from the shutdown itself — so a job halfway through a call
  to a tracker is not cancelled by the very stop that is waiting for it — and journals
  whatever is left for the next run. With no integration configured it starts **idle**: an
  empty queue, nothing published, nothing measurable at start-up. Four flags configure it,
  `--sync-workers`, `--sync-batch`, `--sync-rate` and `--sync-max-attempts`, with the
  matching `GINTRACK_SYNC_*` environment variables and the shipped defaults of 2 workers,
  batch 20, 5 req/s and 5 attempts; an out-of-range value fails the command **before the
  listener opens**. Six endpoints make the queue visible and controllable without a restart:
  `GET /api/v1/sync/jobs` (filter by state and kind, bounded page, cursor),
  `GET /api/v1/sync/jobs/{id}`, `POST .../retry`, `POST .../cancel` and
  `GET|PATCH /api/v1/sync/settings`, where a change to the worker count, the batch size or
  the rate limit reaches the **running** engine at once. Five topics narrate it —
  `sync.job.queued`, `.started`, `.progress`, `.done`, `.failed` — published by a thin
  observer in `internal/server`, so the engine keeps no dependency on the transport. Progress
  is coalesced to at most one frame every 500 ms per coalescing group and a terminal event is
  never throttled; even so the hub drops a client that cannot keep up, so a client reconciles
  from `GET /api/v1/sync/jobs` after a gap rather than trusting the stream. No response, log
  line or event payload carries a job payload or a credential. The four job handlers that
  run on it — the import, the comment push and the two knowledge-base directions — ship in
  the entry above.

- **The inbox is served: `GET /api/v1/inbox` and `POST /api/v1/items/{id}/triage`**
  (GIT-US-0056, ADR-033, docs/07 §5.5). The queue the previous release modelled is now
  reachable: a paginated listing whose `counts` and `pending` are computed over the whole
  queue rather than the page — an expired snooze already counted as pending — and one
  decision per row (`accept`, `reject`, `snooze`, `duplicate`) behind the ordinary `If-Match`
  precondition, with a stale revision answering `412` and the current rev. A project that
  declares no `triage` status has an empty inbox, and filing something into it is refused
  with `no_triage_status` (409). Every triage, and every create that files an item straight
  into the queue, publishes `inbox.changed` with the pending count, so a badge never needs a
  second call.

- **Moving a sprint's unfinished work is a route of its own, and every close can preview
  itself** (GIT-US-0085, docs/07 §5.5). `POST /api/v1/sprints/{id}/transfer` moves the
  incomplete references of one sprint into another sprint or back to their project backlogs
  without closing anything — `items` and `committed` come back untouched — and
  `POST /api/v1/sprints/{id}/close` now accepts the same bulk `transfer` decision, which an
  explicit per-item `carry` entry still overrides. Both accept `dryRun`, and **a dry run
  writes nothing and publishes nothing**: no `sprint.changed`, no `item.changed`, no
  commit-on-save, with `"dryRun": true` on the answer so a preview can never be mistaken for
  a commitment. A per-item failure is still a `200` with an `error` on its own `carried`
  line; a transfer aimed at a sprint that is already over is refused outright with
  `sprint_target_completed` (409).

- **A file can say where it came from: `external`** (ADR-031, docs/03 §12.5). Items, comments
  and knowledge-base pages carry a list of `{system, id, url?, key?, synced_at?}`, and the
  pair (`system`, `id`) is the identity of an entry — the idempotency key an importer matches
  on, so re-running an import updates what it finds instead of duplicating it or guessing
  from titles. `system` is deliberately **not an enumeration**: any
  `[a-z0-9][a-z0-9._-]{0,31}` token round-trips untouched, so a file written by a tool this
  project has never heard of survives a pass through an older binary. Writes are set
  operations keyed on the pair, the discipline `labels` and `links` already use, so two
  writers recording two different systems never clobber each other. The index keeps a map
  from (`system`, `id`) to item id (`Index.ItemByExternal`), so the idempotence check an
  importer performs a thousand times is one map read. The key sits between `links` and
  `attachments` in the canonical order: it is a relation, just one that leaves the
  repository. **Groundwork only so far** — the field parses, validates, round-trips and is
  indexed in `internal/core`; no REST route, MCP tool or screen reads or writes it yet.

- **The inbox: a reserved `triage` status category and an `inbox:` block** (ADR-033, docs/03
  §6.4). Work that arrives from a form, an agent or an importer has to be real from the
  moment it is submitted — a permanent id, a file, a git history, comments — and invisible to
  planning until somebody accepts it. It is therefore an ordinary item whose `status` belongs
  to a fifth reserved category, `triage`, carrying an `inbox:` block that records how it
  arrived (`status`, `snoozed_until`, `duplicate_of`, `source`, `received`). **The category
  is the truth and the block is metadata**, so no stored `is_inbox` boolean can disagree with
  it, and accepting an item is one status change rather than an import step. Queries exclude
  triage by default — `Filter.Inbox` is tri-state, so every filter written before the inbox
  existed keeps its meaning — and board views, sprint views, sprint candidates and sprint
  metrics exclude it unconditionally: a hand-edited sprint file naming a triage item reports
  it as unresolved, not as work, so a thousand spam submissions change no burndown.
  **Snoozing needs no scheduler**: a snoozed item whose `snoozed_until` has passed matches a
  query for `pending`, compared against an instant the caller supplies, so a repository left
  alone for a year answers correctly the moment somebody opens it. Two things to know: a
  project that declares no status in the category simply has no inbox, with no error and no
  warning, and every consumer that switches on a status category — including tools outside
  this repository — now has a fifth case it did not have. **Groundwork only so far**: the
  model, the parser and the query layer; there is no submission endpoint and no triage screen
  yet.

- **Sprint dates are optional, a sprint's status is derived from them, and closing one
  freezes a snapshot** (ADR-034, docs/04 §8.2, §8.4, §12.1). `state` stays what it always
  was, the record of the explicit start and close; what a reader is shown is now computed at
  every read — `draft | upcoming | current | completed` — so the status on the board can no
  longer disagree with the date on the wall, and a sprint becomes current on its start date
  with nobody writing a file. `closed` always wins over the calendar. `start` and `end` are
  given together or not at all, and a sprint with neither is a **draft**: a legal, listable
  sprint with a goal and a scope and no place on the calendar yet, exempt from the no-overlap
  rule — which is what finally makes it possible to plan three sprints ahead without
  inventing date ranges that do not collide. The refusal a collision produces now names that
  escape hatch. Closing a sprint writes a `snapshot` block — totals, the three distributions,
  the observed burndown days and the provenance of the history they were frozen from — and it
  is the one derived number this product stores, because after a close the items leave the
  scope and the numbers stop being recomputable at all. It is written exactly once, never for
  an open sprint, never recomputed, and honest about itself: a snapshot taken where no git
  history could be read is marked approximate rather than passed off as a reconstruction. Two
  prices are worth stating: "today" is a day in the **team timezone**, so clients that
  disagree about it disagree about a sprint's status for a few hours around midnight, and a
  closed sprint file grows by one burndown row per sprint day. **How much of this is wired:**
  the derived status travels on every sprint payload today. Dateless sprints are accepted by
  the model but still refused by `sprint.create` and `sprint.update`; the `snapshot` block
  parses, round-trips and reaches the API, but `sprint.close` does not write one yet and the
  metrics do not read one back yet. docs/04 §8.2 marks each half.

- **`internal/youtrack`, a typed client for the YouTrack REST API** (`GIT-US-0046`, docs/02
  §6): issues with their links, comments and attachments, projects, the authenticated user,
  custom-field settings, version bundles and the article endpoints, with a shared 5 req/s
  limiter, retries that honour `Retry-After` in both its forms, paging that stabilises the
  order first — YouTrack guarantees none, and a `$skip` walk without an `order by:` clause
  silently skips and duplicates rows — and a token that appears in no error and no rendering
  of the client. It is native-only (`net/http`), so it sits beside `internal/gitops` rather
  than in `internal/core`, and it decodes into its own types and stops there — mapping an
  issue onto an item is the caller's job. **Nothing imports it yet**: there is no YouTrack
  feature in the product, only the client the import story will use. Everything it returns —
  issue descriptions, comment text, article content — is untrusted third-party Markdown,
  returned verbatim for the caller to sanitize.

- **`internal/syncengine`, a persistent background job queue** (`GIT-US-0063`, `GIT-US-0067`,
  `GIT-US-0070`, docs/02 §6): a worker pool with keyed batching, a retry ladder with backoff
  and jitter, a dead-letter list, and a JSON journal under the cache directory that replays
  on start, so work started by an HTTP request outlives the response and the process. It is
  generic — it schedules, batches, rate-limits, retries and journals, and knows nothing about
  any tracker — and its one contract on callers is that **handlers must be idempotent**,
  because a job is replayed after a retryable error, after a crash mid-run, and when somebody
  retries it from the dead-letter list. The journal holds bookkeeping only: ids, kinds, keys,
  attempt counts, states and redacted errors, never item content and never a credential, so
  deleting it loses queued work and no user data, and a corrupt journal is moved aside rather
  than being fatal. **Not reachable from any surface yet**: no route, no command, no
  importer.

- **A project can be connected to a YouTrack project** (`GIT-US-0048`, `GIT-US-0052`,
  `GIT-US-0058`, ADR-032, docs/03 §6.5, docs/07 §4.15). The connection is deliberately split
  in two. The half that belongs to the team is committed: an `integrations.youtrack` block in
  `project.yaml` naming the instance, the YouTrack project, the field map and whether
  comments and knowledge-base pages sync. The half that belongs to one person on one machine
  is not: the permanent token lives in the companion's `0600` configuration file, keyed by
  project, overridable with `GINTRACK_YOUTRACK_TOKEN`, and it appears in no API response, no
  event payload, no log line and no `gintrack config show` output. **This is the first
  credential the product stores**, and ADR-032 records why the promise in docs/10 that it
  stored none had to be revised rather than worked around. `/api/v1/youtrack/settings`,
  `/test`, `/projects` and `/fields` serve the settings screen, and `gintrack youtrack
  connect|status` does the same from a terminal. An upstream refusal is reported as `502`
  with its own problem code, never by forwarding YouTrack's `401`, which would tell a browser
  its own session had expired. Companion-only: the browser-only mode cannot reach a YouTrack
  instance, and the capability flags say so.

- **A mapping layer from YouTrack issues to backlog items** (`GIT-US-0045`,
  `internal/youtrack/mapping`). A pure package: decoded YouTrack types in, `core.ItemDraft`
  or a sparse `core.ItemPatch` out, plus the warnings a value the field map did not
  understand produced — because an import that drops a field silently is worse than one that
  says what it could not translate. The `Subtask` link type is read as the hierarchy and
  everything else becomes a `links[]` entry of a kind the model accepts. Nothing that names
  another item is written onto the draft: parents, milestones and link targets come back as
  YouTrack ids for the importer to resolve, so a half-imported set cannot produce a file that
  fails validation. **Not reachable from any surface yet.**

- **Inbox and sprint operations over the vault and MCP** (`GIT-US-0056`, `GIT-US-0085`,
  docs/08 §4.11–§4.15). `inbox.list` and `inbox.triage` accept, reject, snooze and mark
  duplicate in one rev-checked write, and `item.create` can drop work straight into triage.
  Closing a sprint now transfers what is unfinished — to the next sprint, to a named one or
  to the backlog — and both closing and transferring answer a dry run that reports the counts
  without writing, which is what the confirmation dialog shows. Five MCP tools were added
  (`create_inbox_item`, `list_inbox`, `triage_inbox_item`, `close_sprint`,
  `transfer_sprint_items`), taking the surface to seven read-only and eighteen with
  `--allow-write`. Accepting an item cannot change its type: an id encodes its type, so the
  answer to "this should have been an epic" is a new item and a duplicate marker, not a
  rewrite. **No REST route or screen yet** — the operations exist for agents and for the
  wiring still to come.

- **Feedback mode on items and knowledge-base pages** (ADR-030, docs/05 §8.5, docs/03 §14.4).
  A **Feedback** button on the item view and the page viewer — or simply selecting text in
  the description or the page — turns it on. Each selection opens an overlay for a comment
  or clarification, and the notes collect in a Feedback panel. They are kept in the browser's
  `localStorage` per item and per page until they are saved, so unsaved feedback on several
  items survives a reload. Saving on an item writes **one comment** that quotes every
  selection with its description lines. Saving on a page appends the notes to a
  **feedback block at the end of the file**, `### <author> feedback: <id>` followed by the
  quoted source lines and the note, each anchored to a hash of the lines it refers to. Every
  page write through the core then drops the notes whose text has changed, so an agent
  reading the page is never told about text that no longer exists. New: `kb.feedback.add`
  in the core, `POST …/kb/feedback` in the companion, and `author_name` / `author_email` in
  comment front matter.

- **The MCP write tools can be switched on from the web app** (docs/07 §5.5, docs/08 §7.1).
  Settings gained an **Agent tools (MCP)** card whose switch writes `mcp.allowWrite` to the
  configuration file, so agents get the write tools without anybody editing YAML or teaching
  an agent runtime to pass `--allow-write` — which is not usually possible, since an MCP
  entry is a bare `gintrack mcp`. `GET|PATCH /api/v1/mcp/settings` read and set it. A
  companion serving `--mcp-http` rebuilds its endpoint in place, so the change is live there
  at once; a stdio server reads the file the next time it starts. Turning it on is a grant —
  every agent the user runs may then create and edit items — so it is stated as one in the
  card and is never a default.

- **A retrospective can be run by the whole room at once** (`GIT-US-0027`, ADR-028,
  docs/04 §9.1–§9.5). Everyone who opens the retro — over the tunnel of ADR-027, with no
  account and no login — names themselves once; the handle is kept in their own browser
  and is written as the author of the notes, votes and comments they add. Voting is now
  **per note**: a note nobody grouped becomes a theme of its own the first time somebody
  votes for it, the vote button disappears once a participant has spent the budget, and a
  vote can always be taken back. The `discussing` stage gained a **comment box on every
  card**, stored as the new `comments` key in the retro's front matter (one remark per
  block of lines, so two people commenting at once still merge). The wall refreshes
  itself as other participants write, and the retro carries **Copy link / Copy link with
  access** buttons for the tunnel while one is open.

- `gintrack serve` can **publish itself through a free Cloudflare quick tunnel**
  (`*.trycloudflare.com`), from the `--tunnel` flag or from a switch in the web app's
  settings, and shows the temporary public `https` address to share (`GIT-US-0043`,
  ADR-027, docs/07 §4.1 and §5.5, docs/05 §3.1). No Cloudflare account, no DNS record and
  no inbound port: cloudflared is linked as a **library** in `internal/tunnel`, never
  spawned as a binary and never through its `cmd/` tree. `GET|POST|DELETE /api/v1/tunnel`
  read and set it, `tunnel.changed` broadcasts every state change to other tabs,
  `features.tunnel` reports whether the runtime can tunnel at all, and `server.tunnel`
  configures it.
  **Read this before turning it on:** an open tunnel publishes a server with read *and*
  write access to every mounted repository, and the bearer token is the only thing
  guarding it — which is why opening a tunnel over a companion started with `--token none`
  is refused outright (`tunnel_requires_token`, HTTP 409). The web app is served without
  authentication, so anyone holding the address loads the interface; only `/api/v1`
  requires the token. A `https://<host>/?token=<token>` link is a full credential and must
  be treated like a password. Cloudflare gives quick tunnels no uptime guarantee and
  reserves the right to investigate their use: this is a sharing convenience, never a
  production path. A new hostname is minted on every enable, so nothing survives a toggle
  and turning the tunnel off invalidates every link already shared.
- The companion serves the **git CORS proxy** browser-only mode needs at
  `http://127.0.0.1:7317/cors-proxy/`, and the web app adopts it automatically the moment
  it detects a companion, so the "browser UI plus local companion for networking" setup of
  docs/06 §6.3 needs no configuration (`GIT-US-0042`, ADR-025). It is deliberately narrow:
  the three git smart-HTTP endpoints only, an allow-list of hosts derived from the
  registered repositories' remotes plus `git.corsProxy.allowedHosts`, HTTPS only, public
  unicast addresses only — resolved once and dialed at the address that was checked, so DNS
  rebinding reaches nothing — 32 MiB request and 256 MiB response caps, a 120 s deadline,
  redirects re-validated at every hop, and headers copied through allow-lists in both
  directions. It requires this run's bearer token in `X-Gintrack-Token` and a trusted
  `Origin`; `Authorization` is reserved for the git host's credential and is the one header
  forwarded upstream. `GET /api/v1/git/cors-proxy` reports the mount point and the
  allow-list, and `git.corsProxy.enabled: false` removes the endpoint.
- The nginx and Caddy reverse-proxy recipe docs/06 §6.3 had claimed to ship now exists, in
  §6.3.2, with the same allow-list discipline.

- A workspace can hold several team repositories, and the web app chooses which one is
  active. The choice is remembered per workspace and sent on every call that reads a
  board, a sprint, a retro or a team knowledge base, so the companion and browser-only
  mode behave identically (`GIT-US-0036`, ADR-019, docs/04 §3.8).
- `GET /api/v1/teams` lists every mounted team, `GET /api/v1/teams/{key}` resolves the key
  it is given, and every team-scoped route accepts `?team=`.
- A team's project list is editable from the product. Settings holds a **Team projects**
  card that connects a registered repository to the active team and disconnects it again,
  offering the locally indexed projects as candidates and pre-filling key, name, docs
  folder, remote URL and branch (`GIT-US-0037`, docs/04 §3.9). The write path is
  `core.AddTeamProject`/`core.RemoveTeamProject`, the vault methods `team.project.add` and
  `team.project.remove`, and `POST`/`DELETE /api/v1/teams/{key}/projects`. A duplicate
  project key is refused with `team_project_exists`; a removal that would orphan a board,
  sprint or retro reference is refused with `team_project_referenced` unless it is forced.

- Repositories managed with **Jujutsu** are recognized as such, in both layouts —
  colocated (`.jj/` beside `.git/`) and with the git store inside `.jj/` — and reported as
  their own kind by `gintrack ls`, `gintrack add`, `gintrack doctor`, the repository and
  sync payloads and the web app (`GIT-US-0038`, ADR-021, docs/06 §14). A jj workspace with
  no colocated git working tree is registered and indexed instead of being refused.
- A **`jj` backend** reads a Jujutsu repository through `jj` itself, alongside `system` and
  `go-git` and selected for a jj working tree in either layout (`GIT-US-0040`, docs/06
  §14.4-14.5). It reports the real bookmark as the line of work with the bookmark as its
  push target, jj's own dirty set instead of git's index, the tracked remote bookmark with
  ahead/behind counted over a revset against it, `undo: operation_log` with nothing to
  resume, and the three sides of a conflict materialized out of the commit that records
  them with `markers: "jj"`. A **non-colocated repository is now readable at all** — status,
  conflicts and, for the first time, sprint metrics. Every read carries
  `--ignore-working-copy`, so looking at a repository never snapshots the user's working
  copy or adds an entry to the operation log.
- **Jujutsu repositories are writable.** Commit on save, explicit commits, fetch,
  integrate, push, undo and conflict resolution all go through `jj`, so a jj repository is
  a first-class repository rather than a read-only special case (`GIT-US-0041`, ADR-024,
  docs/06 §14.6-14.7). A commit is `jj commit -m <message> -- <paths>`, which records
  exactly those paths and leaves every other edit in the new working-copy commit, followed
  by a fast-forward `jj bookmark move` — without it the work would be reachable from `@`
  alone and `jj git push` would never publish it. A sync runs `jj git fetch`,
  `jj rebase -b @ -d <upstream>` (or a `jj new` merge commit) and
  `jj git push -b <bookmark>` with its dry run; `jj undo` takes an operation back over the
  operation log; a conflict is resolved by writing the merged file and squashing it into
  the commit that records it. Each row of `GET /api/v1/sync/status` now carries `writes`,
  which is what the sync panel disables its buttons from.

### Changed

- **Release artifacts are now signed** (ADR-029, superseding ADR-011; docs/09 §3–§4). A
  `v*` tag builds the embedded web assets once and then builds each platform on the runner
  that can sign it: macOS binaries carry a Developer ID signature with the hardened
  runtime and are notarized by Apple, Windows binaries are Authenticode-signed through
  Azure Trusted Signing (OIDC, no long-lived secret), Linux stays unsigned. `checksums.txt`
  still covers every artifact, and `workflow_dispatch` can re-cut a release for an existing
  tag without moving it. **The macOS archives are now `.zip`** (`notarytool` takes a zip),
  not `.tar.gz` — a scripted download URL for darwin needs updating. Setting this up in a
  fork needs the `MACOS_SIGNING_BUNDLE` secret, the `release` environment and six `AZURE_*`
  repository variables.

  **Paused by the same change:** the Homebrew cask, the Scoop manifest and the GHCR images
  were outputs of the GoReleaser run that the tag pipeline no longer performs (consuming
  pre-signed binaries is a GoReleaser Pro feature). `.goreleaser.yaml` keeps them for
  `make release-snapshot`; they have to be re-plumbed on top of the signed archives before
  a tag publishes them again.

- **Building git-in-track now requires Go 1.26** (was 1.25). `go.mod` declares `go 1.26.0`
  and CI's `GO_VERSION` moved to `1.26`. This is not a choice: cloudflared's own `go.mod`
  declares 1.26, and embedding it (`GIT-US-0043`, ADR-027) raises this module's directive
  with it. Nothing changes for users of the release binaries; contributors and anyone
  running `go install` need a 1.26 toolchain. The same dependency adds **13.9 MB** to
  the binary (54.4 MB → 68.3 MB, stripped) and about 60 indirect modules —
  `quic-go`, the Prometheus client stack, seven OpenTelemetry modules, `sentry-go`,
  `gopsutil`, `urfave/cli`. `sentry-go` is linked but never initialised: `sentry.Init` is
  called only under cloudflared's `cmd/` tree, which this module does not import, so there
  is no telemetry egress from it. cloudflared publishes no semver tags, so the dependency
  is pinned to the pseudo-version `v0.0.0-20260903222438-2253eeeb25a4`. ADR-027 records the
  trade-off in full.
- golangci-lint is pinned to **v2.13.2** (was v2.5.0). The Go 1.26 directive forced it:
  the linter refuses a config whose target Go is newer than the toolchain the linter
  binary itself was built with, and the v2.5.0 release is built with Go 1.25. The newer
  release enforces `noctx` inside tests, so the server's tests now build their requests
  with `httptest.NewRequestWithContext`.
- A second team repository is no longer reported as an error and ignored. What is reported
  now is two mounted repositories declaring the same team `key:`.
- The version-control backend interface (`internal/gitops.Backend`) is expressed only in
  concepts git and Jujutsu both have, so the coming jj backend can implement it without
  faking an index, a `MERGE_HEAD` or a branch (`GIT-US-0039`, ADR-022, docs/06 §14.6): a
  commit covers *exactly* a set of paths, an unfinished *integration* reports how it is
  undone (`abort` or `operation_log`) and resumed (`continue` or nothing), a conflict is
  three sides the backend produces however it can plus the marker dialect of the working
  file, and a *line of work* (a branch or a bookmark) replaces the branch-plus-detached
  pair. `Backend.Abort`/`Continue` are now `Undo`/`Resume`. **No API and no behavior
  changed:** every JSON field and every `git_*`/`vcs_*` code is where it was, and the new
  fields (`lineKind`, `pushTarget`, `unfinished`, `undo`, `resume`, `markers`) are
  additive.

### Fixed

- **Comments written from the web app are attributed to the git user of the repository**
  (docs/07, ADR-030). Both providers sent the literal author `me`, so every comment posted
  from the UI was named `…-me.md` and signed `me`. No author is sent now: the companion
  takes `user.name` and `user.email` from the repository's git configuration chain, the
  browser reads the repository's `.git/config` and then the author in Settings → Sync, and
  `gintrack item comment` does the same when neither `--author` nor `git.authorName` is set.
  The handle in the file name is derived from the name, and the name and email are stored
  as `author_name` / `author_email`.

- **A large repository no longer silently switches off live updates for the others**
  (docs/06 §9.2, docs/07 §7.2). The companion registered one inotify watch per directory
  of every mounted repository, whole source tree included, so a repository of ten thousand
  directories exhausted the 8192-watch budget: the repositories registered after it got no
  watches at all, and a task file created by an agent or by the CLI never reached the web
  UI until the next reload. The watch is now confined to what the vault actually indexes —
  the declared and discovered documentation folders plus the root-level `.pmngr/` of a team
  repository — with the repository root and its first-level directories still watched
  shallowly so a documentation folder created later is noticed. The warning that used to
  say only that the limit was reached now names the repository and the directory it gave up
  on.
  A project whose documentation folder is the repository root no longer falls back to
  watching the whole tree: it watches its `.pmngr/` backlog plus the folders that hold an
  indexed page, and the watch walk now skips `dist/` and `vendor/` like the index does.

- **Opening a task or a page shows the file as it is on disk now**, even when no file event
  announced the change (a folder outside the watched scopes, an exhausted watch budget, a
  network file system). `item.get`, `comment.list` and `kb.page` stat the files behind what
  they return — the item file, its comments and its comment folder, or the page — and
  re-index any whose size or modification time moved, or that appeared or vanished, before
  answering. What that refresh changes is announced on the event stream like a watcher
  batch, so boards and lists other people have open follow. The check is one `stat` per
  file, so no per-file watch is needed.

- **The light/dark choice survives a reload** (docs/05 §12, docs/13 §2). The theme was
  applied before first paint by an inline script in `index.html`, and the companion serves
  the app under `script-src 'self'` — so the browser dropped that script without an error,
  nothing ever stamped `data-theme` on `<html>`, and every reload fell back to
  `prefers-color-scheme`, which for most people meant dark. The bootstrap is now
  `public/theme-boot.js`, a served file the CSP allows, and `main.tsx` applies the stored
  preference again on mount.

- **`gintrack mcp` honours `mcp.allowWrite`** (docs/07 §4.9, docs/08 §2.1). Writes over
  stdio could only be enabled with `--allow-write`, so an agent runtime configured as
  `"command": "gintrack", "args": ["mcp"]` — the shape every MCP client generates — always
  got the read-only surface and could not create an epic, a story, a task or a milestone
  even for a user who had set `mcp.allowWrite: true`. The flag still wins when it is typed,
  `--allow-write=false` included; the configuration decides otherwise.

- The **board list no longer crashes** with `Cannot read properties of null (reading
  'map')` when a team declares no project: a board's project scope now serializes as an
  empty list rather than `null`.

- **git no longer writes behind Jujutsu.** In a jj repository git's `HEAD` sits at the
  parent of the working-copy commit, so a `git commit` there landed on `@-`, moved no
  bookmark and was abandoned as an orphan by the next `jj` command — unreachable from any
  bookmark and unpublishable by `jj git push`. Every such write was first refused with
  `vcs_jujutsu_write_refused` (HTTP 409) and a message naming the `jj` command to run
  instead (`GIT-US-0038`), and now goes through `jj` itself (`GIT-US-0041`). The refusal
  remains for the one case that has no safe answer: a jj repository with **no jj binary
  installed**.
- A jj repository is no longer shown with a destructive "Detached HEAD" badge or as a
  permanently dirty tree. Its branch is reported as `@` — as its bookmark, once the `jj`
  backend of `GIT-US-0040` is driving it — and the sync panel says "Managed by Jujutsu —
  reads and writes go through jj" instead of advising a branch checkout that is
  impossible there.
- A jj repository's sync state is the truthful one (`up_to_date`, `ahead`, `behind`,
  `diverged`, `dirty`, `conflicted`) instead of the `jujutsu` placeholder, which now means
  "no jj binary is installed, so the read-only git guard is driving this repository"
  (`GIT-US-0040`). The `jujutsu` flag on the status is unchanged, and is what the UI reads.
- A jj older than 0.41 is refused with `vcs_jujutsu_too_old` when a repository is opened,
  instead of being only a `gintrack doctor` warning: the backend does not parse output it
  has not been verified against (`GIT-US-0040`).
- Ahead and behind no longer fail in a jj repository whose bookmark jj marks as conflicted
  after a fetch — the state the sync preflight meets when both sides moved. The counters
  are computed against the bookmark's local position, so the repository reads as
  `diverged` (`GIT-US-0041`).
- The two sides of a jj conflict are no longer swapped. jj materializes the incoming work
  as the `+++++++` snapshot and the user's own commit as the `%%%%%%%` diff — the opposite
  of git's index during a rebase — so "keep mine" kept the wrong side in a jj repository
  (`GIT-US-0041`).
- A dirty working copy no longer blocks a sync in a jj repository. There the working copy
  *is* a commit, so a rebase carries it along instead of overwriting a checkout
  (`GIT-US-0041`).

## [1.0.0] — unreleased, prepared

> **This entry is prepared, not published.** No `v1.0.0` tag has been pushed; the
> repository carries no tags at all. The maintainer cuts the tag, and the release
> workflow does everything else. The remaining steps are listed in
> [docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) §6.

First stable release. git-in-track is a project management tool with **no server and no
database**: epics, stories, tasks, milestones, comments, boards, sprints, retrospectives
and knowledge-base pages are Markdown files with YAML front matter, stored in your git
repositories. If you can clone the repo, you have the backlog, its whole history, and the
ability to work offline. Sync is `git fetch` / `git rebase` / `git push`, and nothing else.

### What 1.0 does

**Your backlog is files.** Epics, stories, tasks, milestones and comments live under
`.pmngr/` inside your documentation folder. Every field is documented in
[docs/03-data-model.md](docs/03-data-model.md); unknown keys survive a round trip, so a
tool someone else writes cannot lose your data. `rev`, the optimistic-locking hash, is
computed when a file is read and is never stored.

**Two ways to run it.**

- *Browser-only.* Open a local folder from a Chromium-based browser through the File
  System Access API. The same Go core runs as WebAssembly in a Web Worker, and the index is
  cached in IndexedDB. No installation.
- *Companion CLI.* `gintrack serve` binds `127.0.0.1:7317` and serves the same web app from
  the binary, with fsnotify file watching, a native indexer, a REST + WebSocket API and
  native git. The web app detects the companion and upgrades itself without a reload.

**A knowledge base.** Any documentation folder renders as a first-class KB: GFM tables,
task lists, footnotes, callouts, wikilinks `[[Page]]`, Mermaid diagrams and optional math,
with an outline and a backlinks panel built from the link graph.

**Teams.** A team repository (`team.yaml`) aggregates several project repositories onto
shared Kanban and Scrum boards. Boards hold *references* — `ref: <projectKey>/<itemId>` —
never copies. A project you have not cloned still renders, read-only, from the index
snapshot committed at `.pmngr/index/<projectKey>.json`.

**Sprints, retrospectives and metrics.** Plan a sprint, run it on the board, close it, and
record the retrospective — went well, to improve, actions — with improvement actions
promotable into real tasks in a project repository. Burndown, cumulative flow, cycle time,
lead time and throughput are **reconstructed from the git history of the item files**
(see [ADR-017](docs/adr/ADR-017-metrics-history-from-git-not-a-stored-time-series.md)).
Nothing is stored redundantly, and where the history cannot answer, the chart says
`unknown` instead of guessing.

**Git from inside the product.** `gintrack sync` fetches, integrates and pushes with a
dry-run preview and a clear status. Commit on save is off by default and takes a message
template. Conflicts get a three-way text UI for Markdown bodies and a field-level helper
for YAML front matter. Credentials go through your credential helper and SSH agent
natively; in the browser a token stays in memory for the session and is never written
anywhere.

**Agents are first-class.** `gintrack mcp` speaks MCP on stdio, and `gintrack serve
--mcp-http` serves the same tools at `POST /mcp`. Twelve tools — `list_items`,
`search_items`, `get_item`, `create_epic`, `create_story`, `create_task`, `update_item`,
`add_comment`, `move_on_board`, `list_kb_pages`, `get_kb_page`, `search_kb` — with compact
JSON, cursor pagination and `rev`-based optimistic locking, so two agents writing the same
item produce one success and one structured conflict, never a lost update.
[AGENTS.md](AGENTS.md) is the convention an agent reads before it picks up work.

### Compatibility promise

- **The on-disk layout is frozen at `schema: 1`.** The field is `schema` in `project.yaml`
  and `team.yaml`; `internal/core.SupportedSchema` is the constant. Items do not carry
  their own version.
- Within the `1.x` line, git-in-track will **not** rename or remove a front-matter field,
  change a file path, or change an ID format. Adding a new *optional* field with a default
  is a MINOR release and does not bump `schema`.
- The same promise covers the REST/WebSocket API, the MCP tool schemas, and the CLI
  commands and flags. A breaking change to any of them is a MAJOR release.
- A client that meets a **higher** `schema` opens the project read-only and says why,
  rather than corrupting it.
- Unknown keys are preserved on rewrite. Add your own — prefix them `x-` — and the tool
  will not eat them.

**Upgrading.** There is nothing to upgrade from: no 0.x release was ever published, and
1.0 is the first `schema: 1` in the field. `gintrack migrate` is specified
([docs/03-data-model.md](docs/03-data-model.md) §19, R-EVO-4) but **not implemented**; it
must exist before any `schema: 2` ships.

### Known limitations

The complete, evidence-backed list is
[docs/12-release-readiness-1-0.md](docs/12-release-readiness-1-0.md) §5. The ones most
likely to affect you:

**Browser-only mode is not the full product.**

- **Commit on save does not work in the browser.** The setting renders; nothing is
  committed. Use the companion CLI.
- **Firefox and Safari are read-only** — no File System Access API means no writes and no
  git. Files above 5 MB are indexed by metadata only in that fallback.
- **Browser git requires a CORS proxy**, because git hosts send no permissive CORS headers.
  Until one is configured, git in the tab is disabled. No SSH remotes. No rebase — the
  integration strategy is forced to `merge`. No signing, hooks, submodules or LFS. Clones
  are shallow (`depth: 50`, single branch).
- Browser metrics can only see back as far as each item's `updated` timestamp and report
  everything before it as `unknown`.

**Git sync.**

- `git.dirtyPolicy` (`stash`, `ask`) is documented but **not implemented** — the key is not
  read.
- **Branch policy is not implemented**: no `user-branch` mode, no `autoPr`, no host URL
  templates. Every repository syncs its checked-out branch against its own upstream.
- **Per-repository `git:` overrides are not implemented**; settings are per workspace.
- The pure-Go backend fast-forwards only, cannot sign, cannot abort or continue a rebase,
  and **cannot apply a conflict resolution**. Install system git and select that backend if
  you need any of that. Reading conflicts works on both.

**Metrics — read this before you trust a chart.**

- **A rebase or a squash rewrites history, and therefore rewrites the charts.** Metrics are
  derived from the commits that touched each item file. Squash a branch and every point
  derived from those commits moves with it. This is the price of storing no time series,
  and it is the deliberate trade of ADR-017.
- The history walk is **bounded at 2,000 commits per path**; beyond that the result is
  flagged `truncated` and is approximate.
- Cards from a project you have not cloned are `unknown` on every day.
- Metrics are **per sprint**: board-level cumulative flow and cross-sprint velocity are not
  built.

**MCP.**

- Twelve tools ship. `list_workspaces`, `list_projects`, `get_kb_tree`, `link_items`,
  `list_comments`, `list_boards`, `get_board`, `get_sprint`, `list_retros`,
  `get_sync_status` and `run_sync` are **planned, not built**.
- **There are no retrospective or metrics tools over MCP.** Agents read those files
  directly.
- Resources, prompts, dry-run, the `--tools` allowlist, the local audit log and rate
  limiting are all planned. `delete_item` is deliberately absent: an agent may move an item
  to `cancelled`, never delete it.
- **No golden snapshot pins the tool schemas.** The MINOR-bump policy is written down but
  not enforced by a test.

**API and CLI.**

- `POST /api/v1/sync/run` is synchronous: it answers `200` with the finished result, so a
  long sync holds the request open and cannot be polled or cancelled.
- These answer `501 Not Implemented` by design (configuration is CLI-only):
  `POST /api/v1/workspaces`, `POST /api/v1/repos`, `DELETE /api/v1/repos/{id}`,
  `PATCH /api/v1/projects/{key}`, `GET /api/v1/kb/asset`, `PUT /api/v1/items/{id}` (use
  `PATCH`), and `/api/v1/items/{id}/links`. `GET /api/v1/git/log` answers
  `not_implemented`.
- Observability documented in [docs/07](docs/07-cli-and-api.md) §8 is not implemented: no
  Prometheus endpoint, no `/debug/pprof`, no `gintrack doctor --bundle`, no rotating file
  log.
- `gintrack board`, `gintrack sprint` and `gintrack retro` are specified but not
  implemented; use the UI or edit the files.
- **ID collisions across concurrent branches are possible and there is no repair tool.**
  Two branches can allocate the same number and the clash surfaces at merge. The index
  reports duplicates; renumbering is manual.

**Process.** There is no Playwright end-to-end suite, no recorded WCAG 2.1 AA audit, and
no recorded demonstration of an agent completing a story end to end from `AGENTS.md`
alone.

### Operational notes

**Docker.** The image serves the working tree you mount:

```bash
docker run --rm -p 127.0.0.1:7317:7317 -v "$PWD:/work" \
  --user "$(id -u):$(id -g)" ghcr.io/digiogithub/git-in-track:1.0.0
```

- The mount **is** the point — there is no database, and the container keeps no state.
- The process binds `0.0.0.0` inside the container, because a loopback bind in a container
  is reachable by nothing. **What keeps it private is `-p 127.0.0.1:7317:7317`.** Writing
  `-p 7317:7317` publishes your repository on every host interface, behind nothing but the
  bearer token — which is printed on start, or supplied with `-e GINTRACK_TOKEN=…`.
- `--user "$(id -u):$(id -g)"` lets the container write to a tree you own.
- **File watching needs inotify to cross the bind mount.** That works on Linux; on Docker
  Desktop for macOS and Windows it does not, and the UI updates only on reload — pass
  `--watch=false` there.

**Unsigned artifacts.** Releases are unsigned and not notarized, by design
([ADR-011](docs/adr/ADR-011-goreleaser-unsigned-artifacts.md)). macOS Gatekeeper and
Windows SmartScreen will warn on first run; the bypass for each is in
[docs/09](docs/09-ci-cd-and-releases.md) §4. Verify every download against
`checksums.txt`. Homebrew is the recommended macOS route because the cask clears the
quarantine attribute for you.

**Installing.** `brew install digiogithub/tap/gintrack` is **macOS-only** — the tap ships a
cask, not a formula ([ADR-016](docs/adr/ADR-016-homebrew-cask-instead-of-formula.md)), and
Homebrew on Linux cannot install a cask. Linux users take the tarball, the image, or
`go install`. `go install` builds **without** the embedded web UI: `gintrack mcp` and every
file command work, `gintrack serve` reports there is no UI. Clone and `make build` for the
full product.

**For the maintainer cutting this release.** Before the tag exists, create
`digiogithub/homebrew-tap` and `digiogithub/scoop-bucket`, and set the Actions secrets
`HOMEBREW_TAP_TOKEN` and `SCOOP_BUCKET_TOKEN` — fine-grained PATs scoped to one repository
each, `Contents: read and write`. GHCR needs no secret. The release workflow verifies both
tokens before it builds anything and fails with the fix in the message when either is
missing. Full procedure: [docs/09](docs/09-ci-cd-and-releases.md) §9 and §10.

[Unreleased]: https://github.com/digiogithub/git-in-track/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/digiogithub/git-in-track/releases/tag/v1.0.0
