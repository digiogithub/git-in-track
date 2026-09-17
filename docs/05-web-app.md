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
      boards/                  # kanban + scrum boards, sprint planning, active cycle
      editor/                  # item editor and create pages (§8)
      feedback/                # feedback notes on items and KB pages (ADR-030)
      inbox/                   # the triage queue and the accept flow (ADR-033)
      metrics/                 # sprint metrics and charts
      retros/                  # retrospectives and improvement actions
      search/                  # shared search hit rows, the Ctrl+Shift+F project overlay
      sync/                    # sync panel, conflicts, credentials, git log, job queue
      settings/                # workspace, repos, appearance, agents/MCP status, YouTrack
      workspace/               # the landing surface and the add-repository wizard
      youtrack/                # the import dialog and its query bar
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
/p/$projectKey                           ProjectLayout (mounts the Ctrl+Shift+F search overlay)
  /p/$projectKey/kb/*                       KbViewer (splat path into the docs folder)
  /p/$projectKey/items                      ItemTable (list view, filters in search params)
  /p/$projectKey/items/$itemId              ItemDetail
  /p/$projectKey/items/$itemId/edit         ItemEditor
  /p/$projectKey/inbox                      InboxPage     (as built, ?filter= &id=)
  /p/$projectKey/inbox/$itemId/accept       ItemEditor in `accept` mode
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
  /sprints                                  SprintList    (as built, ?board=)
  /retros                                   RetroList     (as built)
  /retros/$retroId                          RetroBoard    (as built)
  /metrics                                  MetricsIndex  (as built)
  /metrics/$sprintId                        SprintMetrics (as built)
/sync                                    SyncPanel
  /sync/conflicts/$conflictId              ConflictResolver
/agent                                   AgentPage     (as built, GIT-US-0057)
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

**Enable inbox (as built, story GIT-US-0100)** — each project chip in a repository card carries an
*Enable inbox* button when the project has no inbox yet: it declares no status in the `triage`
category (ADR-033), it opened writable, its repository is ready and the workspace is not read-only.
The button calls `enableInbox({project, rev})` (`project.inbox.enable` in the core, `POST
/api/v1/projects/{key}/inbox` on the companion), which adds `{id: triage, name: Triage, category:
triage}` as the first status of `project.yaml` and nothing else. `rev` is the project's `configRev`.
On success the answer replaces the project in the shared `['projects']` query, so the button goes
away and the sidebar *Inbox* entry appears without a reload; a refusal — an inbox that already
exists, an ordinary status already called `triage`, a stale revision — is a destructive toast.

A **workspace search panel** (`features/workspace/WorkspaceSearch.tsx`) queries every
open repository at once and labels each row with the project it came from, because in
a workspace the same title can exist in two repositories. With more than one project
(or team) open, a project multi-select (`SearchProjectFilter.tsx`) scopes the query:
every key is checked by default, "All" and "None" reset it, the selection survives a
new query, and an empty selection shows a hint instead of querying. All checked sends
no scope at all; otherwise the query carries `projectKeys` (GIT-US-0102). Below the repos: "Recently edited" (from the
index, `updated desc`, limit 20), "Assigned to me" (matching `team.yaml` identity
or the configured git author email), and a sync health strip.

Where the companion reports `fullTextSearch: 'pando'`, a second group, *Related by meaning*, follows the exact matches: each row carries the why-matched passage as escaped text with the query terms highlighted and a subdued relevance. A per-user toggle (`semanticResults` in `gintrack:ui-prefs`) hides it, and a `degraded` response says the semantic half was unreachable (docs/21).

**Project search overlay (`features/search/ProjectSearchOverlay.tsx`, story GIT-US-0103).**
Ctrl+Shift+F (Cmd+Shift+F on macOS) on any `/p/$projectKey/...` route — backlog, inbox,
KB — opens a modal search scoped to that project: the same `search` call as the workspace
panel with `projectKey` set, the same minimum query length, and the same rows (the hit
rendering lives in `features/search/SearchHits.tsx` and both surfaces use it), exact matches
first and *Related by meaning* after them under the same `semanticResults` toggle. Tabs
narrow the list: *All*, *Items* (item hits, comment matches included) and *KB* (page and
file hits). The query input keeps focus; ↑/↓ move the selection (`aria-activedescendant`),
and Enter or a click opens an item at `/p/$projectKey/items/$id` or a page at
`/p/$projectKey/kb/<vault path>` and closes the overlay. A `file` hit from the code index has
no screen, so it is listed but cannot be opened. Escape closes the overlay and focus returns
to the element that had it. `ProjectLayout` mounts the overlay, so the shortcut does nothing
outside a project. CodeMirror binds no Mod-Shift-F, but a keypress inside `.cm-editor` is
left to the editor anyway.

**AddRepositoryWizard (`/repos/add`)** — Three steps.
1. *Location*: in browser-only mode, "Choose folder" invokes
   `showDirectoryPicker()`; in companion mode, a path input with server-side
   autocompletion plus a "clone from URL" option. Firefox/Safari get the
   `webkitdirectory` read-only fallback with an explicit banner.
2. *Role and detection*: the provider scans for `.git` and for `project.yaml`/`team.yaml`
   (`fs/detect-project.ts`, four levels down) and lists every documentation folder
   it found, `docs/` first. Detection is deliberately deeper than discovery, which
   reaches the repository root and its first-level directories only (doc 03 §2.1,
   [ADR-018](./adr/ADR-018-bounded-project-discovery.md)): a nested candidate is
   marked *"indexed only if you choose it here"*, and picking it declares it on the
   mount, so it stays discoverable on every later scan.

   **When nothing is found, the wizard creates the project** rather than offering
   an empty workspace (`features/workspace/CreateProjectForm.tsx`, story
   GIT-US-0031). It asks for three things — the documentation folder, with the
   detected folders offered as one-click suggestions; the project key, validated
   against `[A-Z][A-Z0-9]{1,9}` before anything is sent; and the display name —
   then mounts the folder and calls `provider.createProject()`, which writes
   `<docsFolder>/.pmngr/project.yaml` and the layout of doc 03 §2.2 through the
   shared core. *Mount it anyway* stays available as an explicit choice, for
   someone who wants to browse the Markdown without starting a backlog.

   **The role is a deliberate choice, not an assumption** (story GIT-US-0035).
   `fs/detect-team.ts` looks for a `team.yaml` at the folder root — the discovery
   marker of doc 04 R-TEAM-LOC-1 — and reads its key and name; the radio group
   starts on the role the markers imply and the user can say otherwise. Choosing
   *team repository* mounts with `kind: 'team'`, which is what makes the
   workspace read boards, sprints and retrospectives out of the folder. When the
   folder holds no `team.yaml`, the wizard offers to create one
   (`features/workspace/CreateTeamForm.tsx`, story GIT-US-0034) instead of a dead
   end: it asks for the team key, validated against `[A-Z][A-Z0-9-]{1,15}`, the
   name and an optional description, then mounts the folder and calls
   `provider.createTeam()`.

   **In companion mode the wizard cannot register anything**, and says so with the
   exact `gintrack add` command to run: the workspace is the configuration file
   the companion read at startup, and only the CLI writes that file. `POST
   /api/v1/repos` answers 501 carrying that same command
   ([ADR-020](./adr/ADR-020-creating-a-team-repository.md)).
3. *Confirm*: shows what will be written, then runs the initial index with a
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
a create control on the children panel that opens the editor with `parent` set to
the item on screen (§8.1),
comments thread with a composer, and an activity strip from `git log` for that
path (Phase 4). A right rail shows file path, last commit, and "Open in editor"
(companion mode only, via a server endpoint that shells out to `$EDITOR`).

**ItemEditor (`/p/$projectKey/items/$itemId/edit`)** — §8.

**InboxPage (`/p/$projectKey/inbox`, as built, story GIT-US-0060, ADR-033)** — The triage queue: a
list of submissions on the left, the submission under triage on the right, and four decisions —
**accept**, **reject**, **snooze**, **duplicate of**. The route exists only for a project that
declares a status in the reserved `triage` category; a project without one has no inbox, so the
sidebar entry is absent rather than empty, and the sidebar shows the pending count beside it.

The filter and the row being shown live in the **search params** (`?filter=&id=`), so a half-finished
pass is a link: a person can hand the queue to someone else, or come back to it after a reload,
without losing their place. The pass is keyboard-driven — `j`/`k` walk, `a`/`r`/`s` decide — and the
keys are ignored while the focus is in a field or a dialog, because someone typing a snooze date into
the picker is not asking to reject the row behind it. After a decision the pane moves to the **next**
row, computed *before* the write: afterwards the row is gone from the list and there is nothing left
to compute a neighbour from.

**Add to inbox (`AddToInboxButton`, as built, story GIT-US-0066)** — the capture form, in the items
page header beside *New item* and in the Inbox header itself. It is the only create surface in the
app that asks **no type, no parent and no status question**: a title, optionally what happened, and
nothing else. That restraint is the point. Everything else that creates an item asks a person to
place the work in the plan before it exists, and a report is not a plan — it arrives, it is real
from that moment, and a triager decides the rest. The item is filed with the project's triage
status and `inbox.source: web`, and the dialog then swaps to a confirmation naming the id that was
allocated and linking to the queue, because "somebody will look at this" is the only thing the
person submitting wants confirmed. Like the sidebar entry, the control renders **nothing at all**
for a project that declares no triage status: a button leading to an explanation of why a
submission cannot be made is worse than no button.

The submission's comment thread is rendered **read-only** here. A triage pane answers one question —
does this belong in the backlog — and accepting opens the item itself, which is where the
conversation about it belongs; the composer is deliberately not lifted into the pane, where it would
turn the triage keys off for as long as someone was typing.

**Accepting (`/p/$projectKey/inbox/$itemId/accept`)** — the ordinary item editor with a different
verb. It is literally `ItemEditorPage` in `accept` mode (§8), not a second form: the same front
matter form and the same body editor, with the status arriving defaulted to the workflow's initial
non-triage status, a save that commits the acceptance itself rather than a plain patch — so a person
cannot half-accept an item by editing it and walking away — and a return to the queue when it lands.
The editor's session affordances are absent in this mode: no autosave, and no recovered draft,
because accepting is one write reached from the queue rather than a surface a half-written edit is
left open on. Neither mode offers a type picker and neither ever will: an id encodes its type for
life (R-ID-3), so a submission filed as a task that should have been an epic is answered by creating
the epic and marking this one a duplicate of it.

**EpicTree (`/p/$projectKey/epics`)** — Three-level tree (epic → story → task)
with lazy expansion, per-node rollups (done/total, points sum, % complete), inline
status change, and drag to re-parent (a re-parent writes the child's `parent`
field). A "flatten" toggle switches to a table of all descendants. The header
creates an epic and every node creates its own child — a story under an epic, a
task under a story — through the shared create link (§8.1).

**MilestoneList / MilestoneDetail** — Milestones sorted by `due`, each with a
progress bar, item count by status, overdue highlight, and a burnup sparkline
(Phase 6). Detail view lists member items with the same table component as
ItemTable, pre-filtered. The header creates a milestone, and each card creates a
story already filed under it (§8.1).

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

**Team projects (`features/settings/TeamProjectsCard.tsx`, story GIT-US-0037).** The
`projects:` list of the active team's `team.yaml` (doc 04 §3.9), managed from Settings. It
carries the `TeamSelector` of GIT-US-0036 rather than a second team-selection mechanism, so the
list it edits is the one the boards, sprints and retros on screen belong to.

Adding a project offers the projects this machine has indexed as candidates and pre-fills the
entry from them — key, name and docs folder from the index, remote URL and branch from the sync
status — because the link between a registered repository and an entry is **the project key
alone** (doc 04 §7.1). A project nobody here cloned is declared by typing its details, which is
the normal way a team owns a repository not everybody checks out. Every row says whether it is
cloned or renders from a committed index snapshot, and a clone whose `project.yaml` declares
another key shows `W-TEAM-KEY-MISMATCH` on the row: that warning is the failure mode this
surface produces, so it belongs next to the entry that caused it.

A duplicate key is refused before the call goes out and again by the core
(`team_project_exists`). A removal that would orphan a `ref:` is refused with the references
named, and the row then offers "Remove anyway", which repeats the call with `force`.

**Public tunnel (`features/settings/TunnelCard.tsx`, story GIT-US-0043).** Companion
mode only, and hidden entirely when the companion answers `supported: false`. One switch
publishes this companion through a Cloudflare quick tunnel and shows the temporary
`https` address to share (doc 07 §4.1 and §5.5, ADR-027).

The card is written as a security surface first and a convenience second, because that is
what it is: what a tunnel publishes is a server with read **and write** access to every
mounted repository, behind one bearer token (doc 07 §5.1.1). Nothing opens on its own —
the switch is the only thing that opens it — and while the tunnel is up the card carries a
standing, unmissable notice that the workspace is on the public internet. Sharing is two
separate controls, deliberately: the prominent one copies the **bare URL**, which is safe
to paste anywhere because whoever opens it still has to enter the token; the link that
carries the token is a second, quieter control with the warning that **the link is a
credential** next to it. That link is assembled in the tab from the token this session
already holds — it is never asked of the API and never logged.

A companion started with `--token none` reports `tokenConfigured: false`, and a refused
enable (`tunnel_requires_token`, HTTP 409) is rendered as an explanation with the fix —
restart without `--token none` — rather than as a failure to retry.

The five states the card shows, from `GET /api/v1/tunnel`:

| State | Badge | What it means |
|---|---|---|
| `off` | Off | No tunnel. This workspace is reachable on this machine only. |
| `starting` | Starting… | The address exists but is not reachable yet. |
| `connected` | Public | At least one edge connection is up; the address answers. |
| `reconnecting` | Reconnecting… | Every edge connection dropped; cloudflared is retrying. |
| `error` | Failed | The tunnel could not be started, or died. The reason is shown. |

**Why the URL appears before it is reachable.** The hostname comes from provisioning: the
broker returns it in the same call that creates the tunnel, before any edge connection
exists and before DNS has propagated, which takes a few seconds. Hiding the address until
it answers would mean hiding it during the only part of the flow the user is waiting
through, and inventing an address later would be worse. So the address is shown while the
state is still `starting`, labelled as propagating rather than presented as a live link,
with the copy actions disabled until the state settles — a link shared during `starting`
fails for the person who opens it. The card polls the status while it comes up, and a
tunnel that never settles says so instead of polling forever.

Nothing here is cached or persisted. A **new hostname is minted on every enable**, so the
card reads the status rather than remembering one, and turning the tunnel off invalidates
every link already shared.

**YouTrack connection (`features/settings/YouTrackCard.tsx`, stories GIT-US-0055 and
GIT-US-0065).** Connecting a project to a YouTrack instance is three facts — an instance, a
credential and a remote project — and the card exists so that the user finds out whether they are
right *before* anything is written: **Test connection** probes the URL and token currently in the
form, saved or not, and names the YouTrack user it resolved.

The credential decides the shape of the screen. The API is write-only about it — a read returns
`hasToken` and `tokenSource`, never the token (doc 07 §5.5, [ADR-032](./adr/ADR-032-local-integration-credential-storage.md))
— so the field is **never** pre-filled, not even with a masked placeholder, which a save would
happily write back as the literal string of asterisks. Instead the card says that a token is stored
and where it came from: the companion's configuration file, its environment, or its command line. A
token that arrived in the environment or on the command line belongs to whoever started the
companion, and the card reports it rather than pretending it can clear it.

The **YouTrack project** field is an autosuggest over the instance's own projects rather than a text
box, and it degrades to a text box when the instance cannot be listed, because a short name typed
from memory is the single most common way this connection is wrong. Three selects carry the rest of
the committed block of doc 03 §6.5: push comments (`manual` / `auto`), knowledge base sync
(`manual` / `on_write`) and sync direction (`push` / `pull` / `both`).

Everything the instance says about itself arrives as a distinct problem code — a rejected token, a
token without the permission, a base URL missing its context path, an unreachable host — and each is
rendered as its own sentence with its own fix, never collapsed into one "failed" line. The companion
answers `502` for all of them on purpose, so that a browser never mistakes YouTrack refusing a token
for its own session expiring.

The card shows whenever `youtrackSupported` is true — that is, in companion mode — and **not** only
when a project is already linked: a card that appeared only once a project was connected could never
connect the first one. That is the opposite gate from the import entry below, and the two are
deliberately different questions (doc 02 §2).

**The field map (`features/settings/YouTrackFieldMap.tsx`).** A table of the git-in-track fields of
doc 03 §6.5 against the YouTrack custom fields that carry them. Neither list is hard-coded in the
browser: the git-in-track side comes from the companion's own `FieldMapKeys` and the YouTrack side is
discovered from the instance, so neither can drift from what the importer will actually read.

The table answers two questions, not one. *Which* YouTrack field carries a git-in-track field — the
field called "State" carries `status` — and *what* one of that field's values means here — the value
"In Progress" is the local status `in_progress`. `GET /api/v1/youtrack/fields` answers each
bundle-backed field with its allowed values and names the three git-in-track fields whose values may
be mapped at all (`valueMappableFields`: `status`, `priority`, `type`), so the value tables appear
for exactly those three and only when the field they point at has a bundle behind it. The local half
of a value mapping is this project's own vocabulary: its workflow statuses, its declared priorities,
the four item types an issue can be imported as.

Three rules make the table honest rather than convenient. **A default is proposed, never applied
silently:** on first open every unmapped row — field or value — is matched case-insensitively
against the instance's real names and marked as a proposal, so what gets saved is what somebody
looked at. **An unknown flag proposes nothing:** a state value carries `isResolved` only when the
instance actually declared it, absent rather than `false`, and an absent flag must not propose a done
status — silence is not a claim that a value closes an issue. A `isResolved: true` is the one
fallback when no name matches. And **a mapping pointing at something that no longer exists is a
warning, not a deletion:** it stays selected and clearing it is an explicit act, because a rename in
YouTrack must not quietly unmap a field here and turn every later import into a silent default. An
archived value is listed rather than hidden, for the same reason: it is no longer offered on new
issues but it is still on the old ones, which are exactly the issues an import reads.

Over the wire an entry is always an object, `{"status": {"field": "State", "values": {"In Progress":
"in_progress"}}}`. A field with no value mapping is written without a `values` key, which is what
keeps a hand-written `project.yaml` as readable as it was found (doc 03 §6.5).

**Semantic search over Pando (`features/settings/PandoSearchCard.tsx`, story GIT-US-0091).** The
card exists to answer one complaint — "semantic search returns nothing" — without reading the
companion's log, so everything on it is a step of that diagnosis: the backend badge (`core` or
`pando`, plus *degraded* when the endpoint did not answer), the MCP and REST URLs, the code project
id, an `allowRemote` switch carrying doc 07 §3.3's warning verbatim, the result of the **live**
reachability probe with whatever error it produced, and **one row per mounted repository of what
Pando is pointed at** — the working tree, the documentation directories, the items, pages and
comments git-in-track's own index found under them, and the state of the repository's code-project
registration as a badge (`off`, `registered`, `indexing`, `unavailable`) with the companion's own
sentence explaining it. That row replaces the exported-corpus report the card used to show: there is
no second copy to date any more (epic GIT-EP-0020, ADR-036), so the diagnosis is *where Pando was
pointed and how much was found there* — a row reporting 0 items and 0 pages is a misconfigured
`KBPath`, and that is what the card makes visible. Underneath it the card states in prose that the
backlog lives under `.pmngr/` inside the knowledge-base directory, so one indexation covers items,
comments and pages, and that the working tree is registered separately as a code project when the
companion starts. **Reindex now** posts `POST /api/v1/search/reindex`, which answers `202` with a job
and then works in the background; the card follows the `search.progress` frames through the
provider's event seam (a `searchProgress` change event) — phases `code` → `kb` →
`completed`/`failed` — and re-reads the settings on a terminal frame, because the counts, the
per-repository errors and the knowledge-base note live on the finished job rather than in the frame.
A second run while one is going is refused with `search_reindex_running` and reported as "a reindex
is already running", not as a failure to retry. Two things it deliberately does not do: it never
renders a token field — neither Pando token is reported or patchable, and the card says instead that
they are set in the configuration file or in `GINTRACK_PANDO_MCP_TOKEN` /
`GINTRACK_PANDO_REST_TOKEN` — and it never calls a reindex that did not happen a completed one:
without a REST URL the job's own `kbNote` says nothing was asked to reindex and that Pando's watcher
still follows the documentation directory, and the card repeats it word for word. A standing
warning says the embedding model is pinned configuration: Pando silently skips chunks whose vector
length differs from the query's, and the model is per Pando **instance**, so changing it degrades
recall invisibly for every consumer of that instance until a full reindex. The card is gated on the
`searchSettings` capability — companion mode — so browser-only mode has no card rather than an empty
one.

The workspace list reads the same settings (one cached query, `features/settings/search-queries.ts`)
and gives each repository row a semantic-search badge — `on` (registered), `indexing`, `off`,
`unavailable` (story GIT-US-0101). For `off` or `unavailable` the row offers **Enable semantic
search**, which calls `reindexSearch(repoId)` — `POST /api/v1/search/reindex` with `{repo}` — so only
that repository is registered with Pando and indexed. The row follows its own job through the same
`searchProgress` events and refetches the settings on the terminal frame. With no Pando configured
the control is a link to the settings card (`/settings#semantic-search`); without the
`searchSettings` capability it is absent.

**Import from YouTrack (`features/youtrack/`, story GIT-US-0059).** The entry is a button in the
backlog toolbar, and it needs **both** capability flags: `youtrackSupported`, because a browser-only
tab has no process to hold a token, and `youtrack`, because importing from an instance nothing is
linked to is meaningless. Without both, the button is absent rather than disabled.

The dialog is one linear flow — **pick**, **preview**, **run**, **summary**, the first two being one
screen — and it is linear because an import writes items into a git repository, and the step that
makes that safe is the one where the user sees what would be written before anything is.

*Pick* is a typeahead over the linked project plus five saved queries, because the alternative is
asking a person to remember a query language to answer "which of my issues do I want here". The
preset chips are the common questions and the text box is the escape hatch; the two compose.
Selection is multiple and additive with a running count always on screen — an import is a batch, and
its size is the thing to know before pressing preview. A result a previous import already created is
marked with the git-in-track id it became and **stays selectable**: that is not an edge case but the
normal second import, and the preview will say `update` rather than `create`.

The options are a subtask-depth stepper from 0 to 5 — 0 means the selected issues and nothing else,
and the recursion is a number rather than a checkbox because "these three issues" and "these three
issues and everything under them" are wildly different amounts of writing — plus three switches:
include linked issues (non-hierarchy relations become `links[]`), include comments (one comment file
each, keeping the original author and time), and include attachments (paths recorded on the item,
files fetched by the job engine). A fifth control, *land in Inbox*, is present and **disabled on
purpose**: it is the shape the Inbox epic gives an import, and a visibly unavailable control says so
far better than a missing one, which reads as an option nobody thought of.

**Preview and run are the same call with the same options object**, which is what stops "what the
preview showed me" and "what the run did" from drifting. Preview is synchronous and writes nothing:
it answers, per issue, what it would become, whether it is a `create` or an `update`, which item an
update would patch, how deep the recursion found it, how many comments it would write, and every
value the field map could not read. Run is never inline: `POST /api/v1/youtrack/import` always
queues and always answers `202` with a job id, because an import is a hundred issues and a hundred
requests against somebody else's rate limit. The progress strip is fed by the `sync.job.*` events —
coalesced server-side to one frame per 500 ms per group, terminal frames never throttled — so the
component adds no throttling of its own and treats a jump in the counts as normal.

The summary therefore says what the queue knows: how many issues were processed, or, when the job
failed outright, the message the engine recorded — already redacted, rendered as plain text, with a
pointer to the queue in Settings, which holds the job, its attempts and its last error. The events
carry counts and never the per-issue outcome, and no second call answers one, so the dialog does not
pretend to have it.

The dialog writes nothing itself. It calls the import operations and lets the vault do the writing,
which is what keeps one implementation of "import an issue" behind REST, MCP and the CLI alike.

**Send to YouTrack on a comment (`features/backlog/ItemDetail.tsx`, story GIT-US-0076).** A comment
thread on an item that carries a YouTrack `external` reference offers a per-comment push, gated on
all three facts at once: `capabilities.features.youtrack`, a linked project, and that reference. The
state of a pushed comment is **derived, never held in the component** — from the comment's own
`external` field plus the `sync.job.*` frames for its path — so a reload shows the truth rather than
a spinner that outlived its page: pending while the job is queued or running, sent with the remote
comment id and a link to it, failed with the error and a retry.

The project setting `push_comments` decides whether the action is there at all. Under `manual` every
comment is pushed by hand; under `auto` the per-comment action collapses into the state badge alone,
because everything is going anyway. The settings card spells out the consequence rather than naming
the mode: `auto` sends **every comment written from then on**, and never sends existing ones
retroactively. Feedback notes are comments (ADR-030), so the feedback panel needs one control and
not a second sink: a "send to YouTrack after saving" checkbox, remembered per project in the
feedback store.

**Knowledge-base sync (`features/kb/KbSyncToolbar.tsx`, story GIT-US-0093).** The KB viewer carries
a toolbar group — *Publish to YouTrack*, *Publish folder…*, *Sync now*, a status badge and *Check the
article* — rendered only when the runtime supports YouTrack and the project is linked. Publishing one
page is a click; publishing a **folder** is a click plus a confirmation that says how many pages it
will touch, because "publish" over a handbook is hundreds of articles and a person is entitled to
know that before it starts. The `## Feedback` block is never part of what is published: it stays in
the repository (ADR-030), and the confirmation says so.

Nothing here happens inline. Both directions queue a job and return, and the job reports itself over
the `sync.job.*` stream the viewer already listens to, so the toasts say *queued* and never
*published* — the second would be a lie the moment an instance is slow. The badge renders the five
states the KB status operation answers (unlinked, in sync, out of date locally, out of date
remotely, conflict) with the article id and a link to the article when known, and it appears per node
in the tree so a folder summarises its children. Reading the *local* state costs no request: it comes
from each page's own `external` entry and the content it would publish, which is what makes asking
about a whole tree affordable. *Check the article* is the one action that actually reads the remote
article, which is why it is a button rather than something the screen does on its own. A conflict —
both sides changed since the last sync — renders an inline notice linking to the generated
`<page>.conflict.md` and stating that the page itself was left untouched.

The toolbar **reads the project's `kb_sync` setting rather than assuming it**. Under `on_write` a
save already enqueues a publish, so a button labelled "Publish to YouTrack" would be describing the
setting's work as its own: it relabels to *Publish now*, and the toolbar says where publishing
actually comes from. It is not disabled — publishing this page this instant is still a thing to
want, most obviously when the last automatic job failed. The direction matters to that reading: an
`on_write` project whose direction is `pull` publishes nothing on save, so the manual wording stays.
The setting itself, and its direction, are edited in the YouTrack settings card above.

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
  mountRepo(input: MountInput): Promise<RepoInfo>;   // `docsFolders` declares nested backlogs
  createProject(input: CreateProjectInput): Promise<ProjectSummary>; // scaffolds .pmngr/
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

  // teams (docs/04 §3, GIT-US-0036)
  listTeams(): Promise<TeamSummary[]>;
  // `team` is the key of a team.yaml or the id of the repository holding it.
  // It may be omitted while the workspace holds a single team.
  getTeam(team?: string): Promise<TeamSummary | null>;
  // The project list of a team (docs/04 §3.9, GIT-US-0037). A registered
  // repository is linked to an entry by project key alone.
  addTeamProject(project: TeamProjectDraft, team?: string): Promise<TeamProjectResult>;
  // `team_project_referenced` unless `force`: a board, sprint or retro action
  // still points at an item of that project.
  removeTeamProject(key: string, opts?: { force?: boolean }, team?: string)
    : Promise<TeamProjectResult>;

  // boards (implemented) / sprints / retros
  // Every team-scoped call takes the active team; see ADR-019.
  listBoards(team?: string): Promise<BoardSummary[]>;
  getBoard(slug: string, team?: string): Promise<BoardView>;
  // CardMove carries `board`, `ref`, `toColumn`, `position` and the two
  // revisions (`rev` for the board, `itemRev` for the item), plus `force`.
  moveCard(move: CardMove): Promise<BoardMoveResult>;
  getSprint(teamId: string, id: string): Promise<Sprint>;
  updateSprint(teamId: string, id: string, patch: SprintPatch, rev: string): Promise<Sprint>;
  listRetros(filter?: RetroFilter, team?: string): Promise<RetroListing>;
  getRetro(id: string, team?: string): Promise<RetroView>;
  createRetro(input: RetroDraft): Promise<RetroResult>;
  updateRetro(id: string, patch: RetroPatch, rev?: string): Promise<RetroResult>;
  // Creates the task in the named project and writes the ref back into the retro.
  promoteRetroAction(input: RetroPromotion): Promise<RetroResult>;

  // MCP write tools (implemented)
  getMcpSettings(): Promise<McpSettings>;
  setMcpWriteTools(allowWrite: boolean): Promise<McpSettings>;

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

  // agent — Pando AG-UI through the companion (GIT-EP-0018, §19)
  // Every call takes `{ repo, signal }`: one `pando agui-serve` runs per
  // repository and the companion routes `repo` onto its `{url, token}`, so the
  // Pando token never reaches the browser.
  getAgentInfo(options?: AgentRequestOptions): Promise<AguiInfo>;
  getAgentHealth(options?: AgentRequestOptions): Promise<AgentHealth>;
  // An async iterable rather than a callback feed, because that is what the
  // SDK's `PandoThread` consumes. The whole transcript is resent every turn.
  runAgent(input: RunAgentInput, options?: AgentRunOptions): AsyncIterable<AguiEvent>;
  listAgentThreads(options?: AgentRequestOptions): Promise<AgentThreadSummary[]>;
  getAgentThreadMessages(threadId: string, options?: AgentRequestOptions): Promise<AguiMessage[]>;
  // Re-attaches to a thread whose run is still live, without starting one.
  streamAgentThread(threadId: string, options?: AgentRunOptions): AsyncIterable<AguiEvent>;
  deleteAgentThread(threadId: string, options?: AgentRequestOptions): Promise<void>;
  cancelAgentRun(threadId: string, options?: AgentRequestOptions): Promise<void>;

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
  fullTextSearch: 'core' | 'bleve' | 'pando';
  mcp: boolean;
  openInEditor: boolean;
  maxBatchWrite: number;
  searchSettings: boolean;   // the runtime exposes GET|PATCH /api/v1/search/settings
  agent: boolean;            // a Pando AG-UI adapter is configured and reachable
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
  → rehypeTaskList                       // data-task-line on every checkbox (§8.2)
  → rehypeResolveAssets                  // repo-relative images/links → assets / routes
  → shiki (lazily imported chunk)        // dual theme, see below
  → rehype-katex (only when math enabled)
  → rehype-sanitize(schema)              // always last
  → hast-util-to-jsx-runtime             // React 18 runtime, custom component map
```

**Sanitisation.** A hardened schema derived from `defaultSchema`:
allow `input[type=checkbox][checked][disabled]` for task lists, plus the
`data-task-line` / `data-task-index` the renderer stamps on them (§8.2); allow
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
- Drafts: unsaved buffers survive a reload, offered back on the next visit. See
  §8.3 for what was actually built.
- Commit-on-save (Phase 4): when enabled, a save also produces a commit using the
  configured template; the editor footer shows the resulting message and a
  "amend last commit" option when the previous commit touched the same file within
  a configurable window (default 5 min).

### 8.1 Creating an item from where the user is

**Status: implemented** (GIT-US-0033). There is exactly one create implementation,
`features/editor/NewItemPage` at `/p/$projectKey/items/new`. It creates any of the
four editable types — epic, story, task, milestone — and its draft goes through
`provider.createItem` → `item.create`, so the core allocates the id and validates
the draft exactly as it does for an agent writing over MCP.

Every list of items reaches that page through one link component,
`features/backlog/NewItemLink`, instead of growing a form of its own. The link
carries what the surface already knows as search parameters, which the route
validates (`validateNewItemSearch`): `type` always, plus `parent` when the context
is an owning item and `milestone` when it is a milestone.

| Where | Control | Opens with |
| ----- | ------- | ---------- |
| ItemTable header | New item | `type=story` |
| EpicTree header | New epic | `type=epic` |
| EpicTree, on an epic | New story | `type=story&parent=<epic>` |
| EpicTree, on a story | New task | `type=task&parent=<story>` |
| MilestoneList header | New milestone | `type=milestone` |
| MilestoneList, on a milestone | New story | `type=story&milestone=<milestone>` |
| ItemDetail, children panel of an epic | New story | `type=story&parent=<epic>` |
| ItemDetail, children panel of a story | New task | `type=task&parent=<story>` |

Saving returns to the new item's detail page, and the relationship is visible
straight away: the parent's children panel and the epic tree both read `parent`
from the index the write refreshed.

### 8.2 Ticking a criterion from the detail view (as built, GIT-US-0010)

Acceptance criteria are the most-clicked thing in a tracker, so the detail view
ticks them without opening the editor. A rendered checkbox carries the source
line it came from, and a click sends that line to the core, which rewrites it.

1. `rehypeTaskList` (§7) stamps `data-task-line` — the 1-based line of the list
   marker inside the item body — on every task-list checkbox. The plugin runs
   before sanitisation, and the attribute is allow-listed in the schema.
2. `MarkdownContent` renders those checkboxes as controls instead of the
   disabled markers GFM produces, but only when the host passes `onToggleTask`.
   Every read-only surface (and a read-only workspace) simply omits it.
3. `ItemDetail` passes one, and the click calls
   `provider.setTaskItem(id, line, checked, rev)` → `item.task.set` →
   `core.SetTaskListItem`.

The rewrite itself is a core function, never a browser one:
`core.SetTaskListItem(body, line, checked)` replaces the single character
between the brackets on that line and returns the body otherwise byte for byte
unchanged — no reflowing, no re-serialising, CRLF and a missing final newline
included. It refuses a line that is not a task-list item, a `- [ ]` inside a
fenced code block among them, with `task_list_mismatch`: the client is looking
at a body that has changed since it was rendered, and the fix is to re-read it.

The write is the ordinary rev-guarded one: the body goes back through
`item.update`, so `updated` is stamped, the canonical serialiser writes the
file, and a losing race raises `stale_revision` and the conflict path of §8,
exactly like an editor save.

Checkboxes inside a comment work the same way. `CommentsPanel` passes an
`onToggleTask` to each comment's renderer, and the click calls
`provider.setCommentTask(id, path, line, checked, rev)` → `comment.task.set`
(`POST /api/v1/items/{id}/comments/tasks`). A comment has no id of its own, so
it is addressed by `path`, `line` counts inside the comment body, and `rev` is
the comment's. The rewrite is the same `core.SetTaskListItem`, written back
through `comment.update`, which stamps the comment's `updated`.

### 8.3 Drafts (as built, GIT-US-0010)

An unsaved edit is kept in `localStorage` under
`gintrack:draft:<workspace>:<projectKey>:<itemId>`, written on every change and
holding the front-matter values, the body and the `rev` the edit started from.
The workspace half is the companion URL this tab talks to, or `browser` in
browser-only mode (ADR-019), so two workspaces on one machine never read each
other's drafts.

On mount the editor reads the draft once. A draft it finds is **offered, never
applied**: a banner says when it was written, warns when the file has changed on
disk since, and gives the user "Restore draft" and "Discard draft". The draft is
cleared on a successful save and when the user takes the disk version out of the
conflict dialog. Drafts older than 30 days, and drafts that do not parse, are
dropped on read.

A draft is per-viewer, derived state: nothing but the editor that wrote it ever
reads it, it never reaches a repository, and losing every draft costs a user
their unsaved typing and nothing else. Storage that throws — a private window, a
sandboxed frame — reads as "no draft" rather than as an error.

The `useBlocker` "your unsaved changes will be lost" dialog stays: it is about
leaving the route, and the draft is what makes the warning survivable.

### 8.4 Deleting an item (as built, GIT-US-0010)

The detail view carries a **Delete** button, disabled in a read-only workspace
and on an already deleted item. It opens a confirmation that first shows what
still points at the item, read through `provider.getItemReferences(id)` →
`item.references` → `core.ItemReferences`:

| Where | Fields searched |
| ----- | --------------- |
| Items of the project | `parent`, `milestone`, every `links[]` entry of every kind, qualified (`KEY/ID`) or bare |
| Boards of every open team | the column orders, and `filters.milestone` |
| Sprints of every open team | `items` and `committed` |
| Retros of every open team | the `task` of a promoted action |

Children are listed twice — among the references and as their own warning — 
because a child is the one reference a delete could leave pointing at nothing.

**The UI always soft-deletes** (ADR-026). The file keeps its id, its history and
its path and gains `deleted: true` (docs/03 §7.1): the id is never reused, a
merge cannot resurrect a stale copy, and every reference above still resolves —
to an item marked deleted, which is a warning the user can act on, rather than
to a dangling id. That is why the reference list is a warning and not a refusal.
A hard delete stays a deliberate act at the CLI and the API
(`DELETE /items/{id}?hard=true`), where the user is asking for the file to go.

### 8.5 Feedback mode (as built, ADR-030)

The item detail view and the knowledge-base viewer both carry a **Feedback**
button. In feedback mode the reader selects text in the rendered description or
page and an overlay opens under the selection: the quote, the source lines it
came from, and a box for the comment or clarification. Selecting text while the
mode is off asks whether to turn it on first. Each note joins the **Feedback**
panel, where it can be edited or removed before it is saved.

- **Lines.** Both screens render with `sourceLines: true`, which runs
  `rehype-source-lines` (§7): every paragraph, heading, list item, quote, code
  block and table row carries `data-line-start` / `data-line-end`, the 1-based
  lines of the body (front matter excluded). A selection resolves to the
  innermost stamped blocks at both of its ends; when neither end is stamped (a
  highlighted code block), the quote is searched for in the source.
- **Drafts.** Notes live in `localStorage` under
  `gintrack:feedback:<item|kb>:<project>:<id-or-path>`, together with whether
  the mode is on, so unsaved feedback on any number of items and pages survives
  a reload and is restored when the screen opens again. Saving, or discarding,
  removes the key. Nothing is written to the repository before **Save
  feedback**.
- **On an item** the notes are saved as **one comment**
  (`provider.addComment`): each note quotes its text, names the description
  lines it sits on, and carries the note below the quote.
- **On a page** the notes are saved through `provider.addPageFeedback` →
  `kb.feedback.add` → `core.AddKbFeedback`, which appends them to the feedback
  block at the end of the file (docs/03 §14.4): a `### <author> feedback: <id>`
  entry per note, the referenced source lines as a quote, then the note. Each
  entry is anchored to a hash of those lines, and every page write through the
  core drops the entries whose lines no longer exist, so an AI reading the page
  never sees feedback about text that has changed. The write is rev-guarded; a
  stale page is reloaded and the notes are kept.
- **Author.** Neither screen sends an author. The companion attributes the write
  to the git identity (`user.name`, `user.email`) of the repository it lands in;
  browser-only mode reads the repository's own `.git/config`, then the author of
  Settings → Sync. Comments show the name, with the email as its tooltip.

### 8.6 One editor, two modes (as built, GIT-US-0060)

`ItemEditorPage` takes a `mode` of `edit` or `accept`, and there is no second
page behind the second mode. Accepting a submission into the backlog *is* the
ordinary editor with a different verb, so the alternative — a duplicated page
shell — would only have guaranteed that the two drift: a field added to the
front matter form would reach the editor and quietly not reach triage.

What the mode changes is exactly what the two flows disagree about:

| | `edit` | `accept` |
|---|---|---|
| Title and primary action | *Edit `<title>`* / Save | *Accept into the backlog* / Accept |
| Initial status | the item's own | the workflow's initial non-triage status |
| What the save writes | a rev-checked patch | the acceptance, then the rest of the form against the revision it produced |
| Cancel goes to | the item | the queue |
| Autosave, drafts, leave-guard | yes | no |

Everything else — the form, the body editor, the validation, the conflict dialog
— is one implementation. The status default is applied as part of *loading* the
item rather than as a later patch, so a project list that arrives after the item
still lands the right status in the form; and an acceptance is always a write,
even when nobody touched the form, because clearing the triage state is the point
of pressing the button.

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
- **The active cycle, as built** (`features/boards/ActiveCycle.tsx`, story GIT-US-0089): inside
  `SprintPanel`, above the columns, so "where does the sprint stand" is answered without leaving
  the board. It shows the derived status and the date range, the progress of the commitment, and
  the burndown. Two rules shape what it may say. **The status is derived, never stored**
  ([ADR-034](./adr/ADR-034-sprint-status-is-derived-from-dates.md)): it comes from the core, computed from the
  dates against the host's day, so the component never recomputes any of it — and a sprint with no
  dates is a *draft*, which is given a planning state rather than a progress bar at zero, because a
  bar at zero over a sprint nobody has scheduled reads as "no work done" instead of "no sprint yet".
  **A chart is never shown without its provenance** (ADR-017): the note above the burndown is
  printed always, and a closed sprint reads its stored snapshot and says so — after the close the
  items have left the scope, so the frozen block is the only truthful answer (doc 04 R-MET-12).
  The panel's chips and the cycle's chips are deliberately different things: one is what somebody
  did, the other is what the dates make of it.
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

### 9.1 Authoring a board (as built, GIT-US-0032)

`BoardList` carries a **New board** control and `BoardView` a **Board settings**
one; both render the same `BoardFormFields`, and `features/boards/board-form.ts`
translates that form into the `board.create` draft and the `board.update` patch.
A read-only workspace, and a workspace with no team repository open, disable the
controls rather than hiding them — **and say why** (story GIT-US-0035): the
disabled control carries the reason, and the board empty state offers a link into
the add-repository wizard instead of asking the user to "mount a team repository"
by some means the UI does not have. `RetroList` does the same: "Start a retro" is
disabled with the same reason rather than being enabled and failing with a raw
`not_found` from the workspace.

- **Creating** asks for the name, the kind, the projects in scope, the filters
  and the columns. The core turns the name into the slug, refuses a slug that is
  already a board (`duplicate_id`) and fills in the default columns — which map
  status *categories*, so they work for a project whose workflow the team has
  never seen (doc 04 R-COL-2). The dialog then navigates to the new board.
- **Editing** patches the same fields plus the scrum backlog column, sending the
  whole form so that a cleared filter means "cleared" rather than "unchanged".
  The card order is never patched here; it moves one card at a time.
- **Cards are a query, and the UI says so.** A board holds no items: the form's
  scope and filter sections explain that widening the scope or relaxing a filter
  is what puts epics, stories and tasks on a board, because there is nothing to
  copy into the board file.
- **Deleting** sits in the same dialog behind a confirmation, states that no item
  is touched, and is refused while a sprint still names the board
  (`sprint_already_active` when it is running, `board_in_use` otherwise).
- **The first sprint of a scrum board.** A scrum board pointing at no sprint used
  to be a dead end: `SprintPanel` renders only once the board has one. It now
  shows a "No sprint yet" card with **Plan the first sprint**, which creates the
  sprint (`NewSprintDialog`, shared with the panel) and points the board at it in
  the same gesture. The new `/sprints` route lists every sprint of the team
  repository, opens one for any board, and points a board at a sprint that
  already exists.
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
- Keyboard: every action reachable without a pointer. Inside a project, Ctrl+Shift+F
  (⌘⇧F on macOS) opens the project search overlay (§3.1, as built). Global shortcuts (⌘K search,
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

> The palette, the component rules and the accessibility contract are specified
> in [13-design-system.md](./13-design-system.md), which is the authority; this
> section is the summary. `npm run styleguide` renders the whole system, and
> `npm run tokens:check` verifies it.

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
- The theme control is a three-way `light`/`dark`/`system` radio group in the
  sidebar footer; the choice is stored under `gintrack:theme` and applied by
  `public/theme-boot.js`, which `index.html` loads before first paint. It is a
  served file and not an inline script because the companion's CSP has no
  `'unsafe-inline'` in `script-src`: an inline bootstrap is dropped without an
  error, and the stored choice then loses to `prefers-color-scheme` on every
  reload. `main.tsx` applies it a second time on mount, so a page served without
  that file still ends up on the chosen theme.
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
4. ~~Multi-team workspaces: the router already namespaces by `teamId`, but the
   settings UI currently assumes one team. Revisit in Phase 3.~~ **Answered by
   GIT-US-0036 (ADR-019).** A workspace holds as many team repositories as the
   user mounts. One of them is *active*: chosen in the team selector, remembered
   in `localStorage` per workspace (`gintrack:active-team:<companion URL or
   `browser`>`), and passed as `team` on every call that reads or writes a
   board, a sprint, a retro or a team knowledge base. The choice is client
   state; neither host holds one, so the companion and browser-only mode behave
   identically. The team is **not** in the URL yet: a shared `/boards/<slug>`
   link resolves against the active team of whoever opens it, and moving it into
   the route is the open question that replaces this one.

---

## 19. The agent chat (as built, GIT-US-0057)

`/agent` is a conversation with a Pando agent that can read this workspace
(epic GIT-EP-0018). It is capability-gated end to end: the sidebar entry is
rendered only when `capabilities.agent` is true, and the route itself always
resolves but renders "the agent is not available here" when it is false — the
branch is on the capability, never on the provider kind, so a companion built
without the AG-UI routes behaves like browser-only mode.

### 19.1 Layout

Three columns — conversations, the conversation, and a right rail — collapsing
to one below `lg`, where the rail disappears and the list becomes a panel behind
a toggle. The rail holds the shared-state panel (§19.8).

```
src/features/agent/
  index.ts              the public surface; nothing outside imports deeper
  client.ts             the AG-UI transport over `DataProvider` (GIT-US-0053)
  threads.ts            thread identity, the tab guard, the `AgentMessage` projection
  store.ts              `useAgentStore` — messages, run status, interrupt, read-only
  hitl.ts               permission/question classification over the SDK's helpers
  tools/registry.ts     the frontend tools declared to the agent, and their runner
  tools/navigation.ts   open_item, open_kb_page, focus_board_card
  tools/backlog.ts      apply_backlog_filter, show_items
  tools/types.ts        the tool contract: schema, validator, executor, context
  ui/AgentPage.tsx      the three-column shell and the route component
  ui/ThreadList.tsx     new / select / delete, titles derived from the first prompt
  ui/MessageList.tsx    the transcript: a polite live region, auto-scrolling when pinned
  ui/MessageBubble.tsx  one message: Markdown, reasoning, tool calls
  ui/ToolCallCard.tsx   a collapsed `<details>` card per tool call
  ui/ReasoningBlock.tsx reasoning, collapsed, monospace, never Markdown
  ui/Composer.tsx       Enter sends, Shift+Enter breaks, Stop cancels the run
  ui/InterruptSlot.tsx  the seam, and the routing from an interrupt to its dialog
  ui/PermissionDialog.tsx approve / deny / always — the security surface (§19.7)
  ui/QuestionDialog.tsx   typed answers, and an explicit cancellation
  ui/StatePanel.tsx     the right rail: plan, context budget, files, sub-agents
  ui/ItemCards.tsx      `show_items` rendered inline in the transcript
  ui/model.ts           what the transcript shows, and what the list shows
  ui/threadMeta.ts      derived thread titles in `localStorage`
```

### 19.2 Agent output is untrusted content

An agent reply goes through the same pipeline as repository Markdown (§7,
`@/markdown` public surface only), which sanitises as its last transform. Raw
HTML in a reply is therefore text, not markup. `externalImages` is off for this
surface — an image URL a model chose is a request a model chose to make — and
`wikilinks` is off, because a reply is not a vault page. Tool arguments and tool
results never touch Markdown at all: they are escaped text inside a `<pre>`,
with results clamped at 4 kB behind a "show more".

User messages are rendered as escaped plain text as well: what someone typed is
what they should see.

### 19.3 Streaming

`renderMarkdown` is a full unified pass, so re-running it per delta would burn
the main thread on a long answer. `MessageBubble` throttles the source it parses
(~120 ms, trailing edge) and renders the unparsed tail — always a suffix, because
text only grows — as plain text beside it, with a caret while the run is live.
Syntax highlighting waits for the end of the stream. The list auto-scrolls only
while the reader is at the bottom, and offers "jump to latest" otherwise.

### 19.4 What is persisted

Transcripts are **not**. Pando owns the history, keyed by thread id, and the
store restores a thread by asking for it. The browser keeps three derived
things, all of them disposable:

| Key | What |
|---|---|
| `gintrack:agent-threads:<repo>` | the thread ids this repository has seen |
| `gintrack:agent-active-thread:<repo>` | which one this browser was last on |
| `gintrack:agent:threads` | `{ [repo]: { id, title, updatedAt }[] }` — titles derived from the first user message |

Losing the whole lot costs a list of labels and a starting point.

### 19.5 One live run per tab

A second POST on a thread that already has a run is refused by Pando with
`session_busy`, so tabs agree among themselves over a `BroadcastChannel`
(`threads.ts`): a tab announces the thread it wants, whoever owns it says so,
and the newcomer falls back to read-only — everything renders, nothing runs, and
the composer says why. It is advisory and interim, until park-on-disconnect
lands on the Pando side.

### 19.6 The interrupt slot

A run parks when the agent calls a tool the browser owns (a permission prompt, a
question, a frontend tool). Until the dialogs land (GIT-US-0061, GIT-US-0064)
the page shows what is pending and a Cancel. Two seams exist so that wave
changes nothing else: `AgentPage` takes a `renderInterrupt` prop of type
`AgentInterruptRenderer` — `({ interrupt, resume, cancel, readOnly,
alwaysAllowed, allowAlways }) => ReactNode` — and `ui/InterruptSlot.tsx` holds
the fallback it replaces. The `result` string that `resume(toolCallId, result)`
takes is built by the SDK's HITL helpers (`approve`, `deny`, `answerQuestion`,
`cancelQuestion`).

An interrupt is routed by tool name (`features/agent/hitl.ts`):

| Pending tool | Who answers |
|---|---|
| `pando_permission_request` | `ui/PermissionDialog.tsx` — a human, always |
| `AskUserQuestion` | `ui/QuestionDialog.tsx` — a human, always |
| anything else | the frontend tool registry, automatically (§19.9) |

`classifyHitl` re-checks the *shape* of the arguments rather than trusting the
SDK's name-only narrowing: a half-streamed payload classifies as `malformed`,
and the store refuses it immediately — a denial for a permission, a
cancellation for a question — instead of rendering a dialog nobody can answer.

### 19.7 Human in the loop is the interim security boundary (GIT-US-0061)

Until Pando ships the adapter-wide `[AGUI] Tools` allow-list and
`Mesnada = false` (PANDO-EP-0002), `HumanInTheLoop = true` with
`AutoApprove = false` plus these dialogs is **the only thing** standing between
a prompt and a destructive call: the agent behind `/agent` is Pando's full
coder agent, with bash, edit and write. The approval card is a product surface
and a security surface, not a debug affordance. When the allow-list lands, it
narrows what can even be requested and HITL becomes defence in depth — it is
superseded, not replaced. See ADR-035 and docs/20; a second layer worth having
today is running `agui-serve` as a low-privilege user in a container.

What follows from that, concretely:

- **Nothing defaults to yes.** Deny is the resting state, and Pando agrees:
  `approvalFromMessage` (`internal/agui/hitl.go:145`) reads anything that is
  not an explicit approval — prose, a malformed answer, a client error, no
  answer at all — as a denial.
- **Dismissal is an explicit refusal, behind one confirmation.** Escape, the
  backdrop and the corner close all hold the dialog open and ask; the answer to
  that question is a denial (or, for a question, `cancelQuestion()`). A stray
  keypress cannot end a turn, and no dialog can close without an answer
  reaching Pando.
- **Arguments are agent output.** They render as escaped text in a `<pre>` —
  never Markdown, never HTML.
- **"Always allow" is a client-side memory.** *Pando itself has no per-thread
  permission policy*: the adapter asks every time. The grant lives in the agent
  store's `alwaysAllowed`, keyed on the underlying tool name, and the store
  auto-approves matching prompts for the rest of that thread. It is in memory
  only — never `localStorage`, never the config file — so it is gone on reload
  and on every thread switch, and it is listed as a revocable badge under
  "Standing approvals" in the state panel. A standing cross-session grant would
  be a security decision needing its own ADR.

### 19.8 The shared-state panel

`ui/StatePanel.tsx` fills the right rail with the AG-UI shared-state document
(`internal/agui/state.go`): the model, the context budget as a meter over
prompt + completion tokens, the todo list, the touched files and the delegated
sub-agents. `STATE_SNAPSHOT` seeds it and `STATE_DELTA` patches it, both inside
the SDK's reducer; the document belongs to the **thread**, not the run, so it is
never cleared on `RUN_STARTED` and survives between turns. Every section is a
native `<details>`, and an empty one is omitted rather than rendered as a hollow
heading.

The store gained `hydrate()` for this wave: a thread restore (`GET
/threads/{id}/messages`) with no stream attached. `AgentPage` calls it on mount
for a thread the adapter still knows, where `reattach()` would additionally
subscribe to a run that is not running.

### 19.9 The frontend tool registry (GIT-US-0064)

`features/agent/tools/` declares the UI actions the agent may ask for. Five
ship: `open_item`, `open_kb_page`, `focus_board_card`, `apply_backlog_filter`
and `show_items`. Each entry carries a name, a description, a JSON-schema
`parameters` object, a zod schema and an executor; `toolDeclarations()` returns
one frozen array, built at module load, that goes out as `RunAgentInput.tools`
on every `send` and `resume` — Pando keys its agent pool by agent name plus a
hash of the declared toolset (`internal/agui/agentpool.go:54-77`), so a list
that moved would rebuild the agent every turn.

Execution rides the interrupt protocol. Pando emits the tool call, then
`RUN_FINISHED{outcome:"interrupt"}`, and keeps the agent suspended; the store
validates the arguments, runs the executor and resumes the same thread with a
trailing `tool` message carrying the JSON result. **Every failure is a result,
never a throw**: an unknown tool name, arguments that fail validation and an
executor that raises all come back as `{error: …}`, because a frontend tool
that threw would leave the run suspended until Pando's ten-minute window
elapsed, which reads to the user as the app having frozen.

Four rules hold the registry in place:

- **No router import.** Navigation arrives as `ToolContext.navigate`, handed in
  by `AgentPage` from `useNavigate()`. Tests pass a spy.
- **No writes, and no data path.** Anything that reads or writes backlog
  content goes through the gintrack MCP server, where the rev protocol and the
  write gate apply. `show_items` resolves ids through `ToolContext.lookup`,
  which the page wires to `queryClient.ensureQueryData` — a cache read that
  becomes a request only for an item nobody has fetched.
- **No argument may decide a URL on its own.** Item ids are matched against the
  shape ids have, KB paths are refused outright if they carry a scheme, a
  backslash, a leading `/` or a `..` segment, and `apply_backlog_filter`
  round-trips through the route's own `parseItemSearch` so the params it writes
  are by construction ones the router accepts. The URL stays the only home of a
  filter.
- **No shadowing.** A frontend tool named `pando_permission_request` or
  `AskUserQuestion` would route an approval through an executor that answers
  it, so `assertNoHitlShadowing` runs at import time and the registry refuses to
  load.

`show_items` is the one tool that renders rather than navigates: its result
carries the resolved cards and the ids it could not resolve, and
`ui/ItemCards.tsx` draws them inline under the tool-call card through
`MessageList`'s `renderToolResult` hook. Reading the cards off the result — not
off a fresh query — is what makes an old turn show what the agent showed then.

### 19.10 The agent store's surface

```
state    threadId, knownThreadIds, threads, messages, stateDoc, runStatus,
         interrupt, error, readOnly, alwaysAllowed, runner
actions  attach({provider, repo, agent?, guard?, tools?}), newThread,
         selectThread, send, resume(toolCallId, result), cancel,
         hydrate, reattach, refreshThreads, deleteThread,
         registerTools(runner | null), allowToolForThread(toolName),
         revokeToolForThread(toolName), dispose
```

`alwaysAllowed` and the `settled` set of already-answered tool calls are both
cleared by `attach`, `newThread`, `selectThread` and `dispose`: a grant and an
auto-answer belong to one conversation and no other.
