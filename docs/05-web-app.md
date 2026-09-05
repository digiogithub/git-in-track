# 05 — Web application architecture (`web/`)

Status: planning document. Applies to Phases 1–6 of the roadmap; each section marks
the phase in which the described capability lands.

The web application is the primary human interface of **git-in-track**. It is a
React 18 + Vite + TypeScript single-page application that lives in `web/` and is
built to `web/dist`, which the `gintrack` binary embeds with `go:embed`.

The same bundle serves two very different runtimes:

| Mode | How it is reached | Filesystem | Git | Index |
|---|---|---|---|---|
| Browser-only | Any static host, or `file://`-adjacent dev server, or the embedded server without a companion feature flag | File System Access API handles | `isomorphic-git` + CORS proxy | Go core compiled to WASM, in a Web Worker, cached in IndexedDB |
| Companion | `http://127.0.0.1:7317` served by `gintrack serve` | Native, via the CLI process | `go-git` or system `git` | Native Go core, fsnotify-driven |

Everything above the *data provider* boundary (see §4) is identical in both modes.
This is the single most important architectural constraint of the frontend: **no
feature code may import `isomorphic-git`, the WASM bridge, or `fetch('/api/...')`
directly.** Features talk to the provider interface only.

---

## 1. Folder structure

```
web/
  index.html
  vite.config.ts
  tsconfig.json
  tailwind.config.ts
  components.json              # shadcn/ui generator config
  public/
    core.wasm                  # produced by `make wasm`, git-ignored
    wasm_exec.js               # copied verbatim from $(go env GOROOT)/lib/wasm
    favicon.svg
    manifest.webmanifest
  e2e/                         # Playwright specs + fixture repo
    fixtures/acme-repo/        # a real git repo used by e2e and Vitest integration tests
  src/
    main.tsx                   # bootstrap: providers, router, error boundary
    app/
      router.tsx               # TanStack Router route tree
      routes/                  # one file per route (see §3)
      layout/                  # AppShell, Sidebar, CommandPalette, Titlebar
      providers.tsx            # QueryClientProvider, ThemeProvider, I18nProvider, DataProviderProvider
      error/                   # RouteErrorBoundary, GlobalErrorBoundary, crash report dump
    features/
      kb/                      # knowledge base viewer (project docs + team knowledge/)
      backlog/                 # epics, stories, tasks, milestones, comments
      boards/                  # kanban + scrum boards, sprint planning
      retros/                  # retrospectives and improvement actions
      sync/                    # sync panel, conflicts, credentials, git log
      settings/                # workspace, repos, appearance, agents/MCP status
    core-bridge/               # WASM worker client (browser-only mode)
      worker.ts                # the Web Worker entry point
      client.ts                # typed RPC client with request ids
      protocol.ts              # shared message types (generated-adjacent, hand-checked)
      fs-bridge.ts             # File System Access <-> worker file access
    api/                       # companion client
      client.ts                # REST client (fetch + zod parsing)
      ws.ts                    # WebSocket subscription with reconnect/backoff
      probe.ts                 # health probe for 127.0.0.1:7317
      types.ts                 # zod schemas mirroring internal/core model
    data/
      provider.ts              # the DataProvider interface (§4)
      browser-provider.ts      # BrowserProvider implementation
      companion-provider.ts    # CompanionProvider implementation
      select-provider.ts       # auto-detection + upgrade orchestration
    stores/                    # Zustand stores (§5)
    markdown/                  # unified pipeline, plugins, renderers (§7)
    editor/                    # CodeMirror 6 setup, front matter form (§8)
    components/
      ui/                      # shadcn/ui generated primitives (button, dialog, ...)
      common/                  # app-level shared components (ItemBadge, StatusPill, ...)
    lib/                       # pure utilities: ids, slugs, dates, refs, hashing
    i18n/                      # locales/en.json, locales/es.json, i18n.ts
    styles/                    # tailwind entry, tokens, prose styles
    types/                     # ambient declarations, Go/WASM type mirrors
```

Rules enforced by ESLint (`eslint-plugin-boundaries` or `import/no-restricted-paths`):

- `features/*` may import from `data`, `components`, `lib`, `markdown`, `editor`,
  `stores`, `i18n` — never from `core-bridge` or `api`.
- `data/browser-provider.ts` is the only file allowed to import `core-bridge`
  and `isomorphic-git`; `data/companion-provider.ts` is the only file allowed to
  import `api`.
- No feature imports another feature. Cross-feature needs go through `data` or
  `components/common`.
- `components/ui/*` is generated; it never imports from `features` or `data`.

---

## 2. Domain vocabulary used by the UI

Mirrors `internal/core` (see doc 02). The frontend re-declares it with zod so that
both providers validate identically.

- `ItemRef` — `{ projectKey: string; id: string }`, serialised as `ACME/ACME-US-0042`.
- `Item` — front matter + optional `body`. Types: `epic | story | task | milestone`.
- `Comment` — `{ item, author, created, body }`.
- `Board`, `Sprint`, `Retro` — team-repo artifacts.
- `rev` — content hash returned by the provider, never stored in the file. Used
  for optimistic concurrency on every write (see doc 06 §9).
- `RemoteRef` — a board card whose project repo is not available locally: a
  `BoardCard` with `remote: true`, `source: "snapshot"`, `snapshotAt`, `stale`
  and `remoteUrl`, filled from `.pmngr/index/<projectKey>.json`.
- `SnapshotInfo` — the state of one committed snapshot: `present`, `generated`,
  `generatedBy`, `items`, `freshness` (`fresh | ageing | stale | unknown`) and
  `error`. It hangs off every project of `TeamSummary` and off `RefResolution`.

---

## 3. Routing map (TanStack Router)

File-based-ish but declared explicitly in `src/app/router.tsx` for type safety.
Search params are typed and validated with zod through `validateSearch`, so filter
state is shareable by URL and survives reloads.

```
/                                        WorkspaceHome
/onboarding                              AddRepositoryWizard (modal-capable route)
/settings                                SettingsLayout
  /settings/workspace                      workspace + repo list
  /settings/repositories/$repoId            per-repo settings
  /settings/appearance                      theme, density, font, locale
  /settings/sync                            branch policy, commit-on-save, author
  /settings/credentials                     credential storage + CORS proxy
  /settings/agents                          agent / MCP status
/p/$projectKey                           ProjectLayout
  /p/$projectKey/kb/*                       KbViewer (splat path into the docs folder)
  /p/$projectKey/items                      ItemTable (list view, filters in search params)
  /p/$projectKey/items/$itemId              ItemDetail
  /p/$projectKey/items/$itemId/edit         ItemEditor
  /p/$projectKey/epics                      EpicTree
  /p/$projectKey/milestones                 MilestoneList
  /p/$projectKey/milestones/$milestoneId    MilestoneDetail
  /p/$projectKey/graph                      LinkGraph (Phase 6)
/team/$teamId                            TeamLayout
  /team/$teamId/kb/*                        TeamKbViewer
  /team/$teamId/boards                      BoardList
  /team/$teamId/boards/$boardSlug           BoardView (kanban or scrum)
  /team/$teamId/boards/$boardSlug/planning  SprintPlanning
  /team/$teamId/sprints/$sprintId           SprintDetail
  /retros                                   RetroList     (as built)
  /retros/$retroId                          RetroBoard    (as built)
  /metrics                                  MetricsIndex  (as built)
  /metrics/$sprintId                        SprintMetrics (as built)
/sync                                    SyncPanel
  /sync/conflicts/$conflictId              ConflictResolver
/search                                  GlobalSearch
```

### 3.1 Screen-by-screen

**WorkspaceHome (`/`)** — Landing surface. Cards for every mounted repository
(project or team) with: name, project key, branch, ahead/behind counters, dirty
file count, last index time, and mode badge (Browser / Companion). Empty state
launches the Add Repository wizard.

When a team repository is open, a **Team panel** (`features/workspace/TeamPanel.tsx`,
story GIT-US-0016) sits above the repository list: the team name and key, a link
into the team knowledge base (`/p/<TEAMKEY>/kb/`), the members with their role and
whether they are active, and every project `team.yaml` declares. A project the
workspace has open is marked *cloned* and links to its backlog; one nobody cloned
is marked *not cloned* and shows the `git clone` URL — it is listed, never hidden
(doc 04 §7). Each project also carries its `snapshot` state, so the panel can say
when the file its cards come from was last generated (GIT-US-0019). The provider
exposes `listSnapshots()` and `refreshSnapshots()` in both modes.

A **workspace search panel** (`features/workspace/WorkspaceSearch.tsx`) queries every
open repository at once and labels each row with the project it came from, because in
a workspace the same title can exist in two repositories. Below the repos: "Recently edited" (from the
index, `updated desc`, limit 20), "Assigned to me" (matching `team.yaml` identity
or the configured git author email), and a sync health strip.

**AddRepositoryWizard (`/onboarding`)** — Four steps.
1. *Kind*: project repository or team repository.
2. *Location*: in browser-only mode, "Choose folder" invokes
   `showDirectoryPicker()`; in companion mode, a path input with server-side
   autocompletion plus a "clone from URL" option. Firefox/Safari get the
   `webkitdirectory` read-only fallback with an explicit banner.
3. *Detection*: the provider scans for `.git`, for `project.yaml`/`team.yaml`, and
   proposes the docs folder (defaults: an existing `docs/`, else the folder
   containing `.pmngr`, else repo root). For a fresh project the wizard offers to
   scaffold `docs/.pmngr/` with `project.yaml` (`key`, `name`, status workflow
   picked from a template).
4. *Confirm*: shows what will be written, then runs the initial index with a
   progress bar (files scanned / items found / errors).

**KbViewer (`/p/$projectKey/kb/*`)** — Two panes. The team knowledge base uses the
same route with the team key in place of a project key, because the core indexes
`knowledge/` as a scope keyed by the team key (doc 04 §3.6). Left: a
virtualised file tree of the docs folder (or `knowledge/` for teams) with fuzzy
filter, folder collapse state persisted per repo, and an outline toggle showing
the current page's headings. Right: the rendered Markdown page (§7) with a sticky
breadcrumb, "Edit" button, backlinks section (from the core link graph), and
outgoing wikilink chips. Unresolved wikilinks render in a distinct style and offer
"Create page". Deep links `#heading-slug` scroll and highlight.

**ItemTable (`/p/$projectKey/items`)** — TanStack Table over the index. Columns:
id, type, title, status, assignees, labels, priority, estimate, milestone,
parent, updated. Features: column visibility + order persisted per project;
multi-sort; grouping by status/epic/milestone; row virtualisation (target: 10k
rows at 60fps); saved views stored in the workspace store and shareable as URLs.
Filters live entirely in search params:
`?q=&status=todo,in_progress&label=auth&assignee=jose&category=in_progress&milestone=M2&parent=ACME-EP-0001`
(the names match the provider's `ItemFilter` fields; `priority` and `updatedAfter` arrive with the
matching filter fields in the core query API).
Bulk actions (change status, add label, set milestone, assign) issue one provider
`updateMany` call which becomes one commit when commit-on-save is on.

**ItemDetail (`/p/$projectKey/items/$itemId`)** — Read view: title, id chip,
status pill, metadata grid, rendered body, acceptance-criteria checklist with
inline toggling (a checkbox toggle is a body write, so it goes through the same
rev-checked update), children (stories of an epic, tasks of a story), typed links
(`blocks`, `blocked_by`, `relates_to`, `duplicates`) rendered as navigable chips,
comments thread with a composer, and an activity strip from `git log` for that
path (Phase 4). A right rail shows file path, last commit, and "Open in editor"
(companion mode only, via a server endpoint that shells out to `$EDITOR`).

**ItemEditor (`/p/$projectKey/items/$itemId/edit`)** — §8.

**EpicTree (`/p/$projectKey/epics`)** — Three-level tree (epic → story → task)
with lazy expansion, per-node rollups (done/total, points sum, % complete), inline
status change, and drag to re-parent (a re-parent writes the child's `parent`
field). A "flatten" toggle switches to a table of all descendants.

**MilestoneList / MilestoneDetail** — Milestones sorted by `due`, each with a
progress bar, item count by status, overdue highlight, and a burnup sparkline
(Phase 6). Detail view lists member items with the same table component as
ItemTable, pre-filtered.

**BoardView (`/team/$teamId/boards/$boardSlug`)** — §9. Columns from the board
file, cards resolved from every configured project. Kanban and Scrum share the
component; Scrum adds a sprint selector and a backlog drawer.

**SprintPlanning (`/team/$teamId/boards/$boardSlug/planning`)** — Two panes:
candidate backlog (filterable, across projects) on the left, sprint contents on
the right; drag between them. Header shows sprint goal (editable), dates, total
points, and per-assignee capacity vs. committed points. "Start sprint" writes the
sprint file and updates the board's `sprints[]`.

**RetroList / RetroBoard (`/team/$teamId/retros...`)** — RetroBoard renders three
columns (Went well / To improve / Actions) backed by the retro Markdown sections.
Sticky-note cards are list items in the body; adding a note appends a bullet.
Voting (Phase 6) is stored as a `votes` map in front matter. Any action can be
"promoted to task": a dialog picks the target project, and the provider creates a
task in that repo and writes the produced ref back into the retro's `actions[]`.

*As built (GIT-US-0027).* The routes are `/retros` and `/retros/$retroId`. **RetroList** puts what
past retros left open *above* the list of retros, because the point of writing a retro down is
following through, and starts a retro for a closed sprint that has none in one click.
**RetroBoard** renders the three collection columns from the body bullets — adding a note appends
one line, which is what lets two participants write at once — plus the themes ranked by the votes
they got and the improvement actions. An action carries an owner, a due date and a "Promote to
task" control that names the target project; once promoted, the row shows the task reference and
its live status instead, and its checkbox is disabled because the task, not the retro, decides
whether the action is done (docs/04 R-RETRO-1). The provider members are `listRetros`, `getRetro`,
`createRetro`, `updateRetro` and `promoteRetroAction`, on all three providers.

**SyncPanel (`/sync`)** — Per-repo rows: branch, ahead/behind, dirty files,
last fetch, and buttons Fetch / Sync / Push. Expanding a row shows the staged
change set (path, item id, title) and the commit message that will be used.
Conflicts appear as a list linking to ConflictResolver (doc 06 §5).

*As built (GIT-US-0021).* Each row shows the branch, the tracking branch, the
ahead/behind counters, the uncommitted count and one state word (up to date,
ahead, behind, diverged, uncommitted changes, conflicts, rebase in progress,
detached HEAD, no remote, no upstream). Two buttons: **Preview**, a dry run that
fetches — which is read-only — and lists the incoming and outgoing commits
without changing anything, and **Sync**, the full run. The report under a row
explains a failure with the message the pipeline produced and names every
conflicted file. A runtime that cannot sync (browser-only mode with no CORS
proxy, doc 06 §6.3) shows the reason and disables both buttons rather than
offering an action that would fail. Both buttons are disabled while a run is in
flight; per-row progress from `sync.progress` arrives later.

*As built (GIT-US-0022).* A repository whose integration stopped lists every
conflicted file with a **Resolve** button, which opens the ConflictResolver
inline in the panel rather than on its own route — the resolver is addressed by
repository and path, and a path makes a poor route parameter, so
`/sync/conflicts/$conflictId` was not created.

*As built (GIT-US-0023).* The panel also owns the credential prompt of
browser-only mode. It is a modal that opens only when a transport actually asks
for a credential — never at mount time — and it names the host, shows the
redacted remote URL, warns which configured CORS proxy the request (and its
`Authorization` header) will travel through, and takes the token in a password
field. What the user types goes to the pending `onAuth` call and into memory for
that origin only; it is never written to `localStorage`, `sessionStorage`,
IndexedDB, a cookie or a file, which `credentials.test.ts` asserts by spying on
those APIs. While a token is held, the panel shows how many there are — never
the value — and a **Forget tokens** button; a reload, a sign-out and unmounting
a repository forget them too. Native mode never shows the prompt: the companion
delegates to the user's credential helper and ssh-agent (doc 06 §8.1).

**ConflictResolver (inside `/sync`)** — Front matter conflicts render as a
field-by-field table (mine / theirs / merged, with the auto-merge result
preselected). Body conflicts render three ways per hunk, labelled with the
Markdown heading they fall under. "Accept merged" writes the resolved file,
stages it and finishes the rebase or merge.

*As built (GIT-US-0022).* The component reads `provider.readConflict(repo, path)`
and renders what the core proposed (doc 06 §5.7): one row per front-matter field
a decision was made for, carrying both sides, the merged value, the rule that was
applied and a **review** badge when both sides changed it — every row can be
flipped to mine or theirs. Each body hunk shows mine, theirs and base with take
mine / take theirs / take both / take base and a free-text editor, and a hunk
both sides changed must be decided before "Accept merged" is enabled. Three
escape hatches are always present, whatever the shape of the conflict: **Keep
mine**, **Keep theirs** and **Edit manually** (which hands the merged file to a
textarea and writes it verbatim). **Abort and restore** undoes the integration at
any step. A binary conflict says so and offers only the two whole-side choices.
Nothing is decided silently: an automatic decision is a visible row or a badged
hunk, never an invisible one.

**SettingsLayout (`/settings/*`)** — Workspace (mounted repos, remove/repair,
re-index, clear caches), per-repo (docs folder, project key, default branch,
ignored globs), appearance (§12), sync (branch policy, commit-on-save toggle and
message template with live preview, author name/email override), credentials
(§ doc 06 §7 — storage mode, CORS proxy URL, "forget credentials"), agents. As
built, there is no credentials storage mode to choose: native mode uses the
user's helper and browser mode keeps a per-session token in memory only
(GIT-US-0023), so the CORS proxy is the only credential-adjacent setting and it
lives on the sync settings card.

**Agents / MCP status (`/settings/agents`)** — Companion mode only (Phase 5).
Shows whether `gintrack mcp` is reachable (stdio child or streamable HTTP),
the exposed tool list with descriptions, a live call log (tool, args summary,
duration, result size, error), rate/size limits, and a copy-ready client config
snippet. In browser-only mode the page explains that MCP requires the companion
and links to the install instructions.

**GlobalSearch (`/search`)** — Full-text over items and KB pages via the core
query engine. Results grouped by kind, with snippet highlighting, filter chips,
and keyboard navigation. The same component powers the ⌘K command palette, which
also lists commands ("Sync all", "New story", "Toggle theme").

---

## 4. The data provider boundary

`src/data/provider.ts` defines one interface. Both implementations satisfy it and
the whole UI is written against it.

```ts
export interface DataProvider {
  readonly kind: 'browser' | 'companion';
  readonly capabilities: Capabilities;

  // workspace
  listRepos(): Promise<RepoInfo[]>;
  listProjects(): Promise<ProjectSummary[]>;
  getTeam(): Promise<TeamSummary | null>;          // team.yaml of the open team repo, or null
  resolveRef(ref: string): Promise<RefResolution>; // "<KEY>/<ITEM-ID>" across every open repo
  mountRepo(input: MountInput): Promise<RepoInfo>;
  unmountRepo(repoId: string): Promise<void>;
  reindex(repoId: string, opts?: { full?: boolean }): Promise<IndexStats>;

  // read
  queryItems(q: ItemQuery): Promise<Page<Item>>;
  getItem(ref: ItemRef, opts?: { body?: boolean }): Promise<Item>;
  getChildren(ref: ItemRef): Promise<Item[]>;
  listComments(ref: ItemRef): Promise<Comment[]>;
  listKbTree(scope: KbScope): Promise<KbNode[]>;
  getKbPage(scope: KbScope, path: string): Promise<KbPage>;
  readAsset(scope: KbScope, path: string): Promise<Blob>;
  search(q: SearchQuery): Promise<SearchResult[]>;

  // write (all rev-checked)
  createItem(input: CreateItemInput): Promise<Item>;
  updateItem(ref: ItemRef, patch: ItemPatch, rev: string): Promise<Item>;
  updateMany(ops: UpdateOp[]): Promise<BatchResult>;
  deleteItem(ref: ItemRef, rev: string): Promise<void>;
  addComment(ref: ItemRef, body: string): Promise<Comment>;
  writeKbPage(scope: KbScope, path: string, content: string, rev?: string): Promise<KbPage>;

  // boards (implemented) / sprints / retros
  // A workspace holds at most one team repository, so no teamId is needed.
  listBoards(): Promise<BoardSummary[]>;
  getBoard(slug: string): Promise<BoardView>;
  // CardMove carries `board`, `ref`, `toColumn`, `position` and the two
  // revisions (`rev` for the board, `itemRev` for the item), plus `force`.
  moveCard(move: CardMove): Promise<BoardMoveResult>;
  getSprint(teamId: string, id: string): Promise<Sprint>;
  updateSprint(teamId: string, id: string, patch: SprintPatch, rev: string): Promise<Sprint>;
  // A workspace holds at most one team repository here too, so no teamId.
  listRetros(filter?: RetroFilter): Promise<RetroListing>;
  getRetro(id: string): Promise<RetroView>;
  createRetro(input: RetroDraft): Promise<RetroResult>;
  updateRetro(id: string, patch: RetroPatch, rev?: string): Promise<RetroResult>;
  // Creates the task in the named project and writes the ref back into the retro.
  promoteRetroAction(input: RetroPromotion): Promise<RetroResult>;

  // git — commit on save (GIT-US-0020, implemented)
  getGitSettings(): Promise<GitSettings>;
  updateGitSettings(patch: GitSettingsPatch): Promise<GitSettings>;
  getGitStatus(repoId?: string): Promise<GitRepoStatus[]>;
  commitNow(input?: { repoId?: string; paths?: string[]; message?: string }): Promise<GitCommit[]>;

  // git — sync (GIT-US-0021, implemented)
  getSyncStatus(repoId?: string): Promise<SyncRepoStatus[]>;
  getSyncSettings(): Promise<SyncSettings>;
  updateSyncSettings(patch: SyncSettingsPatch): Promise<SyncSettings>;
  /** With no repoId every repository is synced. A failure is reported in the
   *  result's `code`/`message`, not thrown: the tree is always recoverable. */
  sync(repoId: string | undefined, opts?: SyncOptions): Promise<SyncResult[]>;
  abortSync(repoId: string): Promise<SyncRepoStatus>;
  listSyncConflicts(repoId?: string): Promise<{ repo: string; paths: string[] }[]>;

  // git — conflict resolution (GIT-US-0022, implemented)
  /** The three versions of a conflicted path plus the merge the core proposes. */
  readConflict(repoId: string, path: string): Promise<ConflictAnalysis>;
  /** Writes a resolution, stages it and finishes the rebase or merge. */
  resolveConflict(
    repoId: string,
    path: string,
    resolution: ConflictResolution,
  ): Promise<ConflictResolveResult>;

  // events
  subscribe(handler: (e: ChangeEvent) => void): Unsubscribe;
}
```

`Capabilities` is what the UI branches on — never `kind`:

```ts
interface Capabilities {
  write: boolean;            // false for the webkitdirectory fallback
  git: boolean;
  ssh: boolean;              // companion only
  watch: boolean;            // fsnotify push events
  fullTextSearch: 'core' | 'bleve';
  mcp: boolean;
  openInEditor: boolean;
  maxBatchWrite: number;
}
```

`GitSettings` carries `commitOnSave`, `commitDebounceMs`, `messageTemplate`,
the configured and the resolved backend, and a `supported` flag with a `reason`.
The UI branches on `supported`, never on `kind`: browser-only mode stores the
settings and renders the preview but cannot commit until `isomorphic-git`
arrives with GIT-US-0021, and the Settings card says so instead of offering a
switch that would do nothing.

Message rendering is the one thing implemented twice on purpose. The companion
renders with Go `text/template` in `internal/gitops`; the browser cannot run that
code (ADR-006), so `src/git/message.ts` implements the documented format of
doc 06 §3.3 — the placeholders in both spellings, the 72-character subject, the
trailers — and both implementations are tested against the same cases.

### 4.1 BrowserProvider

Composes three things: File System Access handles (§6), the WASM core worker (§6.3
and §7 of doc 02), and `isomorphic-git` for git operations (doc 06 §6). It owns the
IndexedDB caches (`handles`, `index`, `blobs`, `prefs`). Writes go
`provider → worker (validate + serialise front matter) → FS handle write → worker
incremental reindex → emit ChangeEvent`.

### 4.2 CompanionProvider

Thin: every method is one REST call against the `/api/v1` surface defined in doc 07
plus zod parsing; `subscribe` attaches to the WebSocket at
`ws://127.0.0.1:7317/api/v1/events` and sends a `subscribe` frame scoped to the
topics and projects the current route needs. Every request carries
`Authorization: Bearer <token>` (the WebSocket uses the `?token=` query parameter,
since browsers cannot set headers on `WebSocket`); the token is supplied by the
companion when it serves the app and is entered once in settings when the app runs
on the Vite dev server. Errors are RFC 7807 problem documents; the client switches
on the stable `code` field, never on the HTTP status alone, and maps it to a typed
`ProviderError` (`stale_revision` → `RevConflict`, `validation_failed` →
`ValidationError` with per-field `errors[]` surfaced in the editor,
`git_conflict` → `ConflictPending`, `git_auth_failed` → `AuthRequired`,
`repo_not_cloned` → `RemoteOnly`). The client sends `If-Match: <rev>` on writes.

### 4.3 Auto-detection and upgrade

`select-provider.ts` runs a health probe:

1. On boot, `GET http://127.0.0.1:7317/api/v1/health` with
   `AbortSignal.timeout(700)` and `mode: 'cors'`. This route is unauthenticated and
   answers `{"status":"ok","version":"0.4.0","uptimeSeconds":8123}`. A successful
   probe is followed by `GET /api/v1/capabilities` (authenticated) to read
   `schema`, `ui` and the `features` map, which is what populates `Capabilities`.
   CORS is the companion's decision, not ours: it echoes the embedded origin
   always, `http://localhost:5173` only under `--dev`, and anything in
   `server.extraOrigins`; any other origin gets no CORS headers and the probe
   simply fails.
2. If the app is *served by* the companion (same origin), skip the probe and use
   `CompanionProvider` immediately.
3. If the probe succeeds cross-origin and `capabilities.schema` matches the
   bundle's expected schema, show a
   non-blocking toast: *"Companion detected — enable faster indexing, file
   watching and SSH git?"* with Enable / Not now / Never (persisted per origin in
   `localStorage`).
4. On Enable, the app re-runs provider construction, migrates open route state,
   and re-mounts repos by matching absolute paths reported by the companion
   against handle names; unmatched repos stay browser-mounted.
5. Re-probe on `visibilitychange` (throttled to once per 30 s) and after any
   WebSocket close, so starting `gintrack serve` mid-session upgrades within
   seconds. Downgrade is symmetric: if the WS closes and three probes fail, fall
   back to `BrowserProvider` (if handles are still granted) or to a read-only
   "companion offline" state.

Schema mismatch (`capabilities.schema` newer than the bundle's) shows a blocking dialog:
"Update the web app / use the embedded UI at 127.0.0.1:7317".

**As implemented (story GIT-US-0015).** `src/api/detect.ts` owns the probe
(`probeCompanion`, `detectCompanion`, `watchCompanion`, `probeCompanionNow`) and
`src/api/provider-factory.ts` builds the matching provider. Three details differ from the
sketch above and are deliberate:

- The upgrade is applied directly with a **non-blocking notice** ("Companion detected —
  native indexing and file watching enabled") instead of an Enable / Not now / Never
  prompt; the downgrade shows its own notice. Both are dismissible and never block a
  route. Re-probing runs on a 30 s interval and on `visibilitychange` (throttled), and
  `AppProviders` rebuilds the provider and invalidates the TanStack Query cache on a flip.
- The bearer token (docs/07 §5.1) lives in `src/api/token.ts`: read once from `?token=`
  and stripped from the URL with `history.replaceState`, kept in `sessionStorage`, sent as
  `Authorization: Bearer` and as the `?token=` query parameter on the WebSocket. A `401`
  clears it and raises `CompanionUnauthorizedError`, which surfaces as an actionable
  banner plus a token field in Settings.
- The event stream degrades to a plain interval refresh signal when the WebSocket cannot
  be opened (three failed attempts, or no `WebSocket` at all), and keeps trying to upgrade
  back to the socket. Settings shows the live connection state.

---

## 5. State management

Three layers, deliberately separated:

**TanStack Query — all server/provider state.** Query keys are structured:

```
['repos']
['items', projectKey, queryHash]
['item', projectKey, itemId]
['kb', scope, path]
['boards', 'detail', slug]  // ['boards','list'] for the index
['git', 'status', repoId]
```

Defaults: `staleTime` 30 s in companion mode (the WS invalidates precisely) and
5 s in browser-only mode; `gcTime` 15 min; `refetchOnWindowFocus` only for
`['git','status']`. Mutations use `onMutate` optimistic updates plus rollback, and
always carry `rev`; a `RevConflict` triggers a refetch and a "changed elsewhere"
diff dialog rather than a silent overwrite. The WS/worker `ChangeEvent` stream is
translated into `queryClient.invalidateQueries` calls with the narrowest key that
covers the changed path.

**Zustand — client-only state.** One store per concern, each with a small,
explicit `persist` partialize:

- `useWorkspaceStore` — mounted repos, active repo/team, provider kind, capability
  snapshot, onboarding progress. Persisted (IndexedDB via `idb-keyval` storage
  adapter, because handles cannot go in `localStorage`).
- `useUiStore` — sidebar width, open panels, tree expansion, table column config,
  saved views, density. Persisted to `localStorage`.
- `useEditorStore` — open buffers, dirty flags, autosave timers, last saved rev.
  Not persisted (drafts are persisted separately, see §8).
- ~~`useBoardStore`~~ — not needed: the drag session lives in `DndContext`, the
  optimistic card position in the TanStack Query cache (`applyMoveToView`), and
  the WIP condition is recomputed from the columns rather than stored.
  Ephemeral.
- `useSyncStore` — in-flight sync operations, per-repo progress, conflict list.
  Ephemeral.
- `useThemeStore`, `useI18nStore` — persisted to `localStorage`.

Selectors are always used (`useUiStore(s => s.density)`) to avoid whole-store
re-renders; stores expose actions, never raw setters, so the mutation surface is
testable in isolation.

**URL — navigational and filter state.** Anything a user might want to share or
bookmark (filters, sort, selected item, board column focus, KB path) lives in the
route, not in a store.

---

## 6. File System Access API (browser-only mode)

### 6.1 Acquiring and persisting permission

```ts
const handle = await window.showDirectoryPicker({ id: 'gintrack-repo', mode: 'readwrite' });
await idb.set(`handle:${repoId}`, handle);   // structured-clonable
```

Directory handles survive reloads when stored in IndexedDB, but the *permission*
does not always survive. On boot, for each stored handle:

```ts
let perm = await handle.queryPermission({ mode: 'readwrite' });
if (perm === 'prompt') perm = await handle.requestPermission({ mode: 'readwrite' });
```

`requestPermission` requires transient user activation, so the app never calls it
during boot render. Instead repos load in a `needs-permission` state and the
workspace shows a single "Reconnect folders" button that grants all pending
handles in one user gesture (Chromium allows several `requestPermission` calls
inside one activation). Denied handles are kept but marked; the repo card offers
"Choose folder again".

We request persistent permission where available (Chromium's persisted grant when
the site is installed as a PWA or the user allows "on every visit"), and we call
`navigator.storage.persist()` at first mount so the IndexedDB index cache is not
evicted under pressure.

### 6.2 Traversal, writes, and safety

- Traversal uses `for await (const [name, h] of dir.entries())` with a worker-side
  ignore list: `.git`, `node_modules`, `.DS_Store`, `*.swp`, `*.tmp`, `.#*`,
  `4913` (Vim's probe file), plus user globs from settings.
- Reads use `handle.getFile()` → `File.arrayBuffer()`; batches are transferred to
  the worker (§6.3).
- Writes use `createWritable()`; we write to a temp name in the same directory
  and rename via `move()` where supported, falling back to a direct writable when
  `move()` is unavailable. Every write is preceded by a re-read + rev comparison.
- `.git` is never touched by the FS layer directly except through
  `isomorphic-git`'s `fs` adapter, which is a thin shim over the same handles
  (doc 06 §6).

### 6.3 Fallback and support matrix

| Browser | Directory picker | Read | Write | Git in browser | Notes |
|---|---|---|---|---|---|
| Chrome / Edge 108+ | Yes | Yes | Yes | Yes | Full browser-only mode |
| Opera / Brave / Arc (Chromium) | Yes | Yes | Yes | Yes | Brave shields may block the CORS proxy |
| Safari 17+ | No (`showDirectoryPicker` absent) | via `<input webkitdirectory>` | No | No | Read-only KB + backlog viewer |
| Firefox 128+ | No | via `<input webkitdirectory>` | No | No | Read-only; companion recommended |
| Any browser + companion | n/a | Yes | Yes | Yes (native) | Recommended path everywhere |

The read-only fallback loads the selected directory into memory as a
`Map<path, File>`, feeds the same WASM indexer, and sets `capabilities.write =
false`; every write affordance renders disabled with a tooltip pointing at
"Install the companion" or "Use a Chromium browser". A dismissible banner states
the limitation at the top of the app shell. Files above a configurable size cap
(default 5 MB) are indexed by metadata only.

### 6.4 WASM core integration

- Build: `make wasm` runs `GOOS=js GOARCH=wasm go build -o web/public/core.wasm ./wasm`
  and copies `wasm_exec.js` from the toolchain. Both are git-ignored; CI builds
  them before `vite build`.
- Loading happens inside a dedicated Web Worker (`core-bridge/worker.ts`), so the
  main thread never blocks on a full index:

```ts
importScripts('/wasm_exec.js');
const go = new Go();
const { instance } = await WebAssembly.instantiateStreaming(fetch('/core.wasm'), go.importObject);
go.run(instance);                    // registers globalThis.gintrackCore
```

  `instantiateStreaming` needs `Content-Type: application/wasm`; the companion
  sets it, and static hosts are documented in the deployment notes. A fallback
  path uses `WebAssembly.instantiate(await (await fetch(...)).arrayBuffer(), ...)`.
- Method map: the authoritative contract is `CoreApi` in
  `web/src/core-bridge/api.ts` — one entry per method with its params and its
  result. `wasm/bridge.go` implements exactly those methods and nothing else, and
  the Go tests in `wasm/bridge_test.go` exercise them against the fixture vault
  in `testdata/fixtures/project-basic`.
- Message protocol — one envelope, request ids on both sides:

```ts
type CoreRequest = { id: number; method: CoreMethodName; params?: unknown };

type CoreResponse =
  | { id: number; ok: true; result: unknown }
  | { id: number; ok: false; error: { code: CoreErrorCode; message: string; path?: string } };
```

  Inside the worker the same shape crosses into Go as a pair of strings:
  `globalThis.gintrackCore.call(method, paramsJSON)` returns
  `{"ok":true,"result":…}` or `{"ok":false,"error":{code,message,path}}`.
  `ping` and `version` are answered by the worker itself, so the app boots and
  reports `wasm: false` when `core.wasm` is missing instead of failing; every
  other method answers `core_unavailable` in that state.

  `client.ts` keeps a `Map<number, Deferred>`, applies a per-call timeout, and
  rejects everything on worker crash then respawns the worker and replays the
  index from the IndexedDB snapshot. `CoreClient.call` is typed against
  `CoreApi`, so a wrong method name, wrong params or a mistyped result is a
  compile error.
- Pushing files in: the core cannot call an asynchronous browser API, so the main
  thread pushes file contents into the worker with `vault.load` (full) and
  `vault.apply` (incremental events carrying the new text). The core keeps them in
  a `core.MemFS`; every mutating method returns the `WriteSet` — the files it
  wrote and removed — which the host persists through the File System Access API.
  Nothing is acknowledged back: the in-memory copy is already up to date.
- Batching: `CoreClient.loadVault` sends one structured clone per message, capped
  at 256 files or 4 MB. A vault over the cap is split: the first message carries
  every `.pmngr/project.yaml`, because project discovery runs on it and a file
  that belongs to no known project would be indexed as a stray; the rest follow as
  `vault.apply` batches of `create` events, awaited one at a time so the worker
  applies back-pressure. `onProgress` drives the wizard's progress bar.
- Incremental reindex: after a write, only the touched paths are re-parsed. The
  core patches its in-memory index and `vault.apply` returns an `IndexStats` whose
  `delta` names exactly the items, pages and comment threads that changed, which
  the provider turns into precise query invalidations.
  The full index is snapshotted to IndexedDB by `web/src/cache/index-cache.ts`
  (one record per vault: `{ vaultId, fingerprint, snapshotJson, savedAt }`, raw
  IndexedDB, no extra dependency). `hydrateOrBuild` loads the cached snapshot
  first — `snapshot.load` hydrates the index without any files, so the UI paints
  the structure at once — then pushes the real files and rewrites the record only
  when the fingerprint moved. A snapshot the core refuses is dropped, never
  retried: the vault still opens, just cold. Cold index target: 5k files in under
  3 s on a mid-range laptop; warm boot under 400 ms.
- Memory: Go's WASM runtime grows its heap monotonically. The worker is
  terminated and respawned when the index is dropped (repo unmounted) or after a
  configurable idle period, to release memory back to the browser.

---

## 7. Markdown rendering pipeline

One `unified` processor, built once per scope and memoised, in `src/markdown/`.

```
remark-parse
  → remark-frontmatter(['yaml','toml'])  // strip + expose front matter
  → remark-gfm                           // tables, task lists, strikethrough,
                                         // autolinks, footnotes
  → remark-math (optional, per-repo)     // $...$ / $$...$$
  → remarkWikilink                       // [[Page]] / [[Page|alias]] / [[Page#heading]]
                                         // / [[ITEM-ID]] / [[KEY:page]] / ![[embed]]
  → remarkCallout                        // > [!WARNING] and Obsidian > [!info]-
  → remark-rehype({ allowDangerousHtml: false })
  → rehype-slug + rehypeHeadingAnchors
  → rehypeMermaidPlaceholder             // pre.mermaid, rendered client-side
  → rehypeResolveAssets                  // repo-relative images/links → assets / routes
  → shiki (lazily imported chunk)        // dual theme, see below
  → rehype-katex (only when math enabled)
  → rehype-sanitize(schema)              // always last
  → hast-util-to-jsx-runtime             // React 18 runtime, custom component map
```

**Sanitisation.** A hardened schema derived from `defaultSchema`:
allow `input[type=checkbox][checked][disabled]` for task lists; allow
`className` per element and per value, never freely — the allowlist covers
`language-*`, `shiki*`, `callout`/`callout-*`, `wikilink*`, `heading`,
`task-list-item`, `contains-task-list` and `footnotes`, so a document can never
choose an arbitrary class; allow `data-mermaid`, `data-callout`, `data-item-ref`,
`data-wikilink`, `data-kind`, `data-unresolved`, `data-kb-link`, `data-external`
and `data-asset-path` on the elements that carry them; allow `id` on headings
and list items, `href` restricted to `http`, `https`, `mailto` plus in-app
`#`/relative paths, and `src` to `http`/`https` (a repo-relative image carries
`data-asset-path` and gets its object URL after sanitisation); drop `iframe`,
`script`, `object`, event handlers, `javascript:` and `data:`, and `style`
everywhere except the `pre`/`code`/`span` that Shiki produces. Sanitisation runs **after** every transform, so no
plugin can inject unchecked HTML. Raw HTML in Markdown is off by default and, when
enabled per repo in settings, still passes through the same schema.

**Syntax highlighting.** Default: `shiki` with the `github-light`/`github-dark`
dual-theme output (CSS variables switch with the app theme, no re-highlight on
theme change). The highlighter lives behind a dynamic `import()` and is reached
only when a document actually contains a fenced code block, so it is its own
build chunk that a prose-only page never downloads. It is built on `shiki/core`
with the JavaScript regex engine (no Oniguruma WebAssembly) and the default
grammar set of ts, js, go, json, yaml, bash, sql, md and diff; JavaScript, JSX
and TSX are served by the TypeScript grammar through language aliases rather
than by their own grammars, which each re-embed the whole JavaScript grammar.
`highlight.js` remains a build-time alternative for environments where the extra
weight matters; the sanitize schema accepts both class prefixes.

**Mermaid.** Never at parse time. The rehype plugin emits
`<pre class="mermaid" data-mermaid>` holding the verbatim source, so the diagram
degrades to readable text; a React component intersection-observes
it, dynamically `import('mermaid')` on first visibility, initialises with
`{ startOnLoad: false, securityLevel: 'strict', theme: currentTheme }`, renders to
SVG in a detached container, sanitises the SVG, and injects it. Failures render
the source in a `<pre>` with an error note. Diagrams re-render on theme change
(debounced) and expose a zoom/pan overlay on click.

**Wikilinks.** Resolution order: exact relative path → path with `.md` appended →
unique basename match in the same scope → case-insensitive basename match →
unresolved. Resolution uses the link graph the Go core already computes, so the
frontend does not re-implement it. Resolved links become router `<Link>`s to
`/p/$projectKey/kb/<path>`; item references (`[[ACME-US-0042]]` or a link to a
`.pmngr` file) render as an `ItemChip` with live status from the index.
Unresolved links get `data-unresolved` styling and a "Create page" affordance.

**Images and assets.** `rehypeResolveAssets` rewrites repo-relative `src` values
to a sentinel; a React `<RepoImage>` component asks the provider for the bytes.
Browser mode: `URL.createObjectURL(blob)` with a per-page revocation registry on
unmount and an LRU cap (default 64 objects / 64 MB). Companion mode: a plain URL
`/api/v1/projects/:key/kb/asset?path=...` (the binary sibling of the KB page
endpoint in doc 07 §5.5) so the browser HTTP cache does the work. Absolute
external images are allowed but load through `referrerpolicy="no-referrer"` and
can be blocked entirely by a per-repo setting.

**Performance.** Rendering runs in `startTransition`; pages over a size threshold
(default 200 KB) render progressively by top-level block with `content-visibility:
auto` on sections. The processor result is cached per `(scope, path, rev, theme)`
in an LRU of 50 pages.

---

## 8. Editor

CodeMirror 6, wrapped in `src/editor/`.

- Extensions: `markdown()` with GFM plus nested code-language highlighting,
  `EditorView.lineWrapping`, history, search panel, bracket matching, active-line
  highlight, our own decorations for wikilinks (clickable, ⌘-click navigates),
  item refs, front matter delimiters (rendered as a folded region when the form
  editor is open), and a paste handler that turns pasted images into files written
  next to the page (`assets/<slug>-<n>.png`) plus a Markdown image link.
- Two-way front matter editing: a generated form (fields from the project schema:
  status select from the configured workflow, assignees multiselect from
  `team.yaml`, labels combobox with existing values, priority, estimate, due date
  picker, parent/milestone pickers with typeahead over the index) and a
  **Raw YAML** toggle. Both edit the same document: the form applies a
  `core.serialize` result as a single CodeMirror transaction restricted to the
  front matter range, so the cursor and undo history in the body are preserved.
  Invalid YAML in raw mode disables the form with an inline parse error and a
  "restore last valid" button.
- Live validation from the core (`op: 'validate'`) on a 300 ms debounce: unknown
  status, dangling `parent`, unknown milestone, malformed date, duplicate id.
  Errors show in a footer bar and as gutter markers.
- Preview: side-by-side (scroll-synced by source line ↔ rendered block mapping) or
  toggled; the preview uses the exact §7 pipeline.
- Autosave: debounced 800 ms after the last keystroke, and immediately on blur,
  route change, or `visibilitychange` to hidden. Each save sends the buffer with
  the `rev` observed when the buffer was opened or last saved. On `RevConflict`
  the editor does not overwrite: it opens a merge dialog (current disk version vs.
  buffer) built on CodeMirror's merge view.
- Drafts: unsaved buffers are mirrored to IndexedDB every 2 s keyed by
  `(repoId, path)`; on reload the editor offers to restore. Draft entries are
  cleared on successful save.
- Commit-on-save (Phase 4): when enabled, a save also produces a commit using the
  configured template; the editor footer shows the resulting message and a
  "amend last commit" option when the previous commit touched the same file within
  a configurable window (default 5 min).

---

## 9. Boards UX

**Status: boards are implemented** — kanban (GIT-US-0017), snapshot-backed
remote cards (GIT-US-0019) and scrum with sprint planning (GIT-US-0018) — at
`/boards` (the index) and `/boards/$slug` (the board).
Code: `features/boards/` — `BoardList`, `BoardView` (the route plus the
`BoardCanvas` a test renders directly), `BoardColumnPanel`, `BoardCardTile`,
`SprintPanel`, and the `queries.ts` and `sprint-queries.ts` hooks.

- **Library:** `dnd-kit` (`@dnd-kit/core`, `sortable`, `modifiers`). Keyboard
  sensor enabled, so cards can be moved with Space + arrows; a live region
  announces "Moved ACME-US-0042 to In progress, position 2 of 7". Every card
  also carries a "Move to…" menu, so a move needs neither a pointer nor a
  memorised gesture.
- **Data:** column definitions and card order come from the board Markdown file;
  card content comes from the per-project indexes.
- **Remote cards, as built:** a card whose project is not mounted is rendered by
  `BoardCardTile` from the committed `.pmngr/index/<projectKey>.json` of the team
  repository: muted dashed styling, a "remote" badge, the title, status,
  priority, assignees, labels and estimate the snapshot published, a
  "Snapshot from 6 hours ago" caption (amber "Stale snapshot, 9 days ago" past
  `snapshots.max_age_days`), the one-sentence reason it cannot be edited, and an
  "Open on the host" link built from `web_url` and `default_branch`
  (doc 04 §7.3). The card carries `source: "snapshot"`, `snapshotAt`, `stale`
  and `remoteUrl`; `features/boards/snapshot-age.ts` turns the timestamp into
  the caption. A project with no snapshot, an unreadable one or an item the
  snapshot omits degrades to the reference alone plus the reason — never a
  crash and never a card that pretends to be live. Cloning the project makes
  the same ref render live on the next board read, with no board edit at all.
- **Remote cards are read-only:** a remote card offers neither the drag handle
  nor the "Move to…" menu, and a client that asks anyway is refused with
  `repo_not_cloned` and a message saying to clone the project. What lives in the
  team repository stays editable: re-ordering a remote card inside its column
  writes the board file only (doc 04 R-REM-1).
- **Move semantics:** dropping in a different column maps to a status change in
  the item's project repo (using the column's `statuses` mapping — if a column
  maps several statuses, a small popover asks which one), plus an update to the
  board file's `order:` list. Both writes happen in one provider `moveCard` call
  so they can be one commit.
- **Move semantics, as built:** a move sends both revisions — the board's and
  the item's — because the two files live in different repositories. A re-order
  inside one column sends no status at all and writes the board file only.
- **Optimistic update:** the card moves instantly (`applyMoveToView` in
  `features/boards/queries.ts` recomputes the columns *and* the WIP flags, so
  the header turns red at the same moment the card lands) and TanStack Query
  holds the previous board snapshot for rollback. On failure the snapshot is
  restored and a toast explains why.
- **WIP limits:** a column over its limit shows a coloured header, a
  `count / limit` badge and a status line. Dropping into a full column shows a
  warning drop state and, on drop, the move is refused once with
  `wip_limit_exceeded` and a confirm dialog ("In progress is at its WIP limit of
  3. Move anyway?") repeats it with `force`. Limits are advisory, never
  blocking, and never silently exceeded.
- **Nothing is hidden:** items whose status maps to no column are listed under
  the board with their status and the reason, and a ref into an undeclared
  project renders as inert text with an "unknown project" badge.
- **Read-only workspaces** (the `webkitdirectory` fallback) render the board
  with no drag handles and no move menus, and say so once at the top.
- **Scrum extras, as built:** a `kind: scrum` board renders `SprintPanel` above
  the columns: the sprint title and state, the goal (editable in place, one
  write to the sprint file), the date range, "5 of 14 days left", and the
  metrics — committed points, completed against total points, items done, and
  how many references were added after the start. The working columns hold the
  sprint's scope; the `backlog_column` also offers the candidates the sprint
  does not list, and each card says which it is (doc 04 R-SCRUM-1 to R-SCRUM-3).
  Dragging a candidate out of the backlog commits it to the sprint.
- **Planning:** "Plan sprint" opens two lists — the scope and the candidates —
  with Add and Remove on each row. Both write the sprint file in the team
  repository and nothing else, so a card whose project nobody cloned moves in
  and out exactly like a local one (doc 04 R-SPR-2).
- **Starting and closing:** "Start sprint" freezes the commitment and points the
  board at the sprint; a board already running one is refused with
  `sprint_already_active`. "Close sprint" shows completed against incomplete
  work and a per-item choice — leave it, carry it to the next sprint, or send it
  back to the backlog — because closing writes nothing by itself (R-SPR-3). A
  decision that could not be applied (a project nobody cloned) is reported in a
  toast, and the rest of the closing still lands.
- **A new sprint** is created from the board with a title, a start, an end and a
  goal; the id is allocated by the core, and dates that overlap another sprint
  of the same board are refused with `sprint_overlap` and the offending sprint
  named.
- **Performance:** columns virtualise beyond 100 cards; cards are memoised on
  `(ref, rev, position)`; drag overlays use `transform` only.

---

## 10. Offline and PWA

- `vite-plugin-pwa` in `generateSW` mode: precache the app shell, `core.wasm`,
  `wasm_exec.js`, fonts and icons; runtime-cache shiki grammars and mermaid chunks
  with stale-while-revalidate. `/api/**` (including the `/api/v1/events` WebSocket
  upgrade) is **never** cached.
- Browser-only mode is fully offline-capable: handles, index snapshot, drafts and
  preferences all live locally. The only online need is git remote access.
- Companion mode degrades to a read-only "companion offline" banner with the last
  successful query results still visible from the Query cache.
- Installability matters for permission persistence on Chromium, so the manifest,
  icons and an "Install app" hint in settings are part of Phase 1.
- Update flow: on new service worker, show a "New version available — Reload"
  toast; never auto-reload with a dirty editor buffer.

---

## 11. Internationalisation

- `i18next` + `react-i18next`, namespaces per feature, JSON catalogues in
  `src/i18n/locales/<lang>/<ns>.json`. English is the source language and the only
  one guaranteed complete; Spanish ships as the second locale.
- All user-facing strings go through `t()`; ESLint `i18next/no-literal-string` is
  on for `features/**` with a small allowlist (ids, keyboard shortcuts).
- Dates/numbers via `Intl` with the active locale; relative times via
  `Intl.RelativeTimeFormat`. Repo content (Markdown, item titles) is never
  translated.
- Locale detection: stored preference → `navigator.languages` → `en`. RTL is
  prepared for (logical CSS properties, `dir` on `<html>`) though no RTL locale
  ships initially.

---

## 12. Accessibility

- shadcn/ui is Radix-based, so dialogs, menus, tabs, tooltips and comboboxes come
  with correct roles and focus management; we keep those primitives rather than
  hand-rolling.
- Keyboard: every action reachable without a pointer. Global shortcuts (⌘K search,
  `g` `b` boards, `g` `i` items, `e` edit, `s` sync) are listed in a shortcuts
  dialog and are disabled while an editor or input has focus.
- Drag & drop always has a keyboard equivalent plus a "Move to…" menu on each card.
- Focus is visible everywhere (`:focus-visible` ring from the token set), route
  changes move focus to the main heading, and a skip link precedes the sidebar.
- Colour is never the only signal: status pills carry text, WIP warnings carry an
  icon, diff views mark added/removed with symbols.
- Contrast targets WCAG 2.1 AA in both themes; the prose and code themes are
  checked with automated contrast tests.
- `prefers-reduced-motion` disables drag animations, page transitions and mermaid
  re-render animation.
- Automated checks: `axe-core` via `@axe-core/playwright` on every e2e screen, plus
  `jest-axe`-style assertions in component tests for the complex widgets.

---

## 13. Theming and design tokens

- Tailwind CSS with CSS custom properties for tokens (`--background`,
  `--foreground`, `--muted`, `--accent`, `--destructive`, plus semantic
  `--status-todo`, `--status-in-progress`, …, and `--priority-*`).
- Three theme states: `light`, `dark`, `system`. `system` sets no attribute and
  relies on `prefers-color-scheme`; explicit choices stamp `data-theme` on
  `<html>`. Tokens are defined on `:root`, redefined under the media query, and
  again under `[data-theme="dark"]`, so the toggle wins in both directions.
- Density setting (comfortable / compact) changes spacing tokens only.
- Typography: system UI stack by default, optional Inter + JetBrains Mono
  self-hosted (no external font CDN, so offline works). Prose styles are custom
  rather than `@tailwindcss/typography` defaults, to keep them token-driven.
- Status and priority colours are configurable per project in `project.yaml`;
  the UI maps unknown statuses to a neutral token instead of failing.

---

## 14. Testing strategy

**Unit (Vitest, jsdom).** `lib/` helpers (ref parsing, slugs, id formatting,
date math), the markdown pipeline (snapshot tests per feature: gfm table, task
list, callout, footnote, wikilink resolved/unresolved, mermaid placeholder, math,
XSS payloads that must be sanitised), zod schemas, Zustand store actions,
the WASM RPC client against a mocked worker (timeouts, cancellation, crash and
respawn).

**Component (Vitest + Testing Library).** Every screen rendered against an
in-memory `FakeProvider` implementing `DataProvider` over fixture data. This is
the main reason the provider interface exists: the whole UI is testable without a
browser filesystem, a Go binary, or a git repo. Covered: table filtering/sorting,
item editor save + conflict dialog, board drag via keyboard sensor, WIP warning,
retro action promotion, conflict resolver field merge.

**Contract tests.** One shared suite runs against `FakeProvider`,
`BrowserProvider` (with an in-memory FS handle shim) and `CompanionProvider`
(against a `gintrack serve` started by the test harness on a random port). Any
provider that passes the suite is interchangeable; drift between modes shows up
here, not in production.

**E2E (Playwright).** Chromium project with the File System Access API driven via
CDP (`browser_context` permission grant plus a fixture directory), and a second
project running against `gintrack serve` with the embedded UI. Fixture repo lives
at `web/e2e/fixtures/acme-repo` — a real git repo (two branches, a prepared
conflict, ~200 items) materialised into a temp dir per test via a setup script,
so tests can commit and reset freely. Scenarios: onboarding a project repo,
browsing the KB with mermaid and wikilinks, creating a story, editing with
autosave, moving a card across a board, running a sync that conflicts and
resolving it, upgrading from browser-only to companion mid-session.
Firefox and WebKit projects run a reduced suite covering the read-only fallback.

**Visual and a11y.** Playwright screenshots for the board, KB page and editor in
both themes, with a small pixel tolerance; `axe` scan per screen.

**Performance budgets in CI.** Initial JS ≤ 300 KB gzip (excluding `core.wasm`,
shiki grammars and mermaid, all lazily loaded); index 5k items ≤ 3 s in the
worker benchmark; board with 500 cards renders ≤ 100 ms per interaction. Budgets
are asserted by a script over the Vite bundle report and a Playwright trace.

---

## 15. Build and dev workflow

**Build.**

```
make wasm   # GOOS=js GOARCH=wasm go build -o web/public/core.wasm ./wasm
            # + copy $(go env GOROOT)/lib/wasm/wasm_exec.js to web/public/
make web    # npm ci && npm run build  -> web/dist
make build  # go build ./cmd/gintrack  (embeds web/dist via go:embed)
```

`internal/server` declares `//go:embed all:../../web/dist` and serves it with an
SPA fallback (any unknown path that is not under `/api` returns `index.html`).
`web/dist` contains a `.gitkeep` and a build-time generated `version.json` so the
embed never fails on a clean checkout; CI always builds the web app before the Go
binary. Vite config: `base: '/'`, hashed asset filenames, manual chunks for
`react`, `codemirror`, `shiki`, `mermaid`, `isomorphic-git`, and
`build.target: 'es2022'`. `core.wasm` is served from `public/` (not hashed) so the
service worker can precache it by a stable name; its version is checked against a
`version` string exported by the module at init and a mismatch forces a reload.

**Dev.**

```
npm run dev          # vite dev server on :5173
npm run dev:companion# vite dev with VITE_FORCE_COMPANION=1
gintrack serve --dev # companion on :7317 with permissive CORS for :5173
```

`vite.config.ts` proxies `/api` (with `ws: true`, so the `/api/v1/events` upgrade
works too) to
`http://127.0.0.1:7317`, so the dev server behaves like the embedded build.
`VITE_FORCE_PROVIDER=browser|companion` overrides auto-detection for debugging.
Scripts: `dev`, `build`, `preview`, `lint` (ESLint flat config + `eslint-plugin-react-hooks`
+ boundaries), `typecheck` (`tsc --noEmit`, `strict: true`, `noUncheckedIndexedAccess`),
`test`, `test:e2e`, `format` (Prettier). CI runs lint, typecheck, unit, contract
and the Chromium e2e project on every PR; the full browser matrix runs nightly.

---

## 16. Sprint metrics (as built, GIT-US-0028)

`/metrics` picks a sprint; `/metrics/$sprintId` draws it. The feature lives in
`src/features/metrics/`: `metrics-queries.ts` (one read-only query, nothing to invalidate),
`chart.ts` (the scales and the band tokens), `BurndownChart.tsx`, `CumulativeFlowChart.tsx` and
`SprintMetrics.tsx` (the page, the stat tiles, the provenance banner and the data tables).

**The provenance banner comes first, above every chart.** The companion reconstructs the series
from the git history of the item files; a browser-only session cannot and says so, showing the
approximation it can draw from the `updated` stamps instead
([doc 04 §12](./04-team-repository.md), [ADR-017](./adr/ADR-017-metrics-history-from-git-not-a-stored-time-series.md)).
The UI branches on one flag, `provenance.approximate`, and prints `provenance.note` verbatim: the
wording of an approximation is decided once, in the core, so every surface says the same thing.

**No charting library.** Both charts are polylines over a linear scale in hand-written SVG. That is
less code than the adapter a library would need, it adds no dependency to a bundle that ships inside
the binary, and it keeps every mark on the app's own tokens.

**Chart tokens are their own set** (`--chart-todo`, `--chart-progress`, `--chart-done`,
`--chart-cancelled`, `--chart-unknown`, `--chart-grid`, `--chart-ideal`), defined next to the badge
tokens in `index.css` and deliberately not equal to them. A badge is read on its own; a chart series
is read against its neighbours, so the steps are re-chosen until every adjacent pair stays separable
under protanopia, deuteranopia and tritanopia and each one clears the chart surface. Dark mode is
re-stepped against the dark surface rather than flipped. Changing one of these means re-validating
the set.

**Accessibility.**

- Every chart has a data-table equivalent, in a `<details>` directly under it, carrying every value
  that was plotted. Nothing is only in a tooltip.
- Colour is never the only channel: two or more series always carry a legend, the burndown's ideal
  line is dashed as well as neutral, and the cumulative flow's `unknown` band is hatched as well as
  grey.
- Each `<svg>` is `role="img"` with a label that names the chart and points at the table.
- A day that has not happened is `observed: false` and is simply not drawn — never plotted as zero.
  In the table it reads "not measured".

---

## 17. Phase mapping

| Phase | Frontend deliverables |
|---|---|
| 0 | `web/` scaffold, Tailwind + shadcn, router skeleton, `DataProvider` interface, `FakeProvider`, CI (lint/typecheck/test) |
| 1 | File System Access mount, WASM worker bridge, KB viewer with the full markdown pipeline, item table/detail/editor, epic tree, milestones, IndexedDB index cache, read-only fallback |
| 2 | `CompanionProvider`, health probe + upgrade toast, WS-driven invalidation, contract test suite across providers |
| 3 | Team repo mounting, boards (kanban + scrum) with dnd-kit, sprint planning, remote reference cards, multi-project item table |
| 4 | Commit-on-save settings card with a live message preview and per-repository git status (GIT-US-0020, done); sync panel with the status indicator and the dry-run preview, over the companion API and over isomorphic-git in the browser (GIT-US-0021, done); credential prompt in the sync panel, per-session in-memory tokens and the redaction rules (GIT-US-0023, done); conflict resolver UI (GIT-US-0022, done; git activity strips still to come) |
| 5 | Agent/MCP status screen, call log, agent-oriented empty states and AGENTS.md surfacing in the KB |
| 6 | Retro board and action promotion (GIT-US-0027, done); sprint metrics — burndown, cumulative flow, cycle/lead time and throughput, with the provenance of their history (GIT-US-0028, done, §16); link graph view, PWA polish, visual/a11y test gates, 1.0 |

---

## 18. Open questions

1. Should saved views and column layouts be committed to the repo (shareable,
   reviewable) instead of living in `localStorage`? Leaning: opt-in, stored under
   `.pmngr/views/`.
2. Shiki vs. highlight.js as the shipped default — decided in favour of shiki for
   dual-theme output; revisit if the lazy grammar loading proves fragile offline.
3. Whether the read-only fallback should attempt an OPFS copy of the selected
   folder to enable local-only editing without the File System Access API
   (writes would then need an explicit "export changes" step).
4. Multi-team workspaces: the router already namespaces by `teamId`, but the
   settings UI currently assumes one team. Revisit in Phase 3.
