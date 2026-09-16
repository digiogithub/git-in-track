# 11 — Roadmap

The development plan for **git-in-track**: the seven phases from the architecture brief,
expanded into milestones with goals, deliverables and exit criteria, then broken down into
epics and user stories with estimates, dependencies, risks and a working model for a mixed
team of humans and AI agents.

This document is the **sequencing** authority. The backlog under `docs/.pmngr/` is the
**status** authority: same IDs, same titles, kept in sync (see
[10-development-guidelines.md](./10-development-guidelines.md) §11). We manage this project
with the tool we are building, from the first story onwards.

- Project key: `GIT`
- Epics: `GIT-EP-0001` … `GIT-EP-0007` (one per phase); `GIT-EP-0011` … `GIT-EP-0019` for
  the post-1.0 phases 7–9 (§7)
- Milestones: `GIT-M-0001` … `GIT-M-0007`; `GIT-M-0011` … `GIT-M-0013` (§7)
- Stories: `GIT-US-0001` … `GIT-US-0030`; `GIT-US-0044` … `GIT-US-0094` (§7)
- Total estimated: **171 story points** for phases 0–6, **330** for phases 7–9

---

## 1. Milestones at a glance

| Milestone     | Phase | Epic          | Theme                          | Version  | Points |
| ------------- | ----- | ------------- | ------------------------------ | -------- | ------ |
| `GIT-M-0001` | 0     | `GIT-EP-0001` | Foundations                    | `v0.1.0` | 21     |
| `GIT-M-0002` | 1     | `GIT-EP-0002` | Browser-only MVP               | `v0.2.0` | 37     |
| `GIT-M-0003` | 2     | `GIT-EP-0003` | Companion CLI                  | `v0.3.0` | 24     |
| `GIT-M-0004` | 3     | `GIT-EP-0004` | Team repository and boards     | `v0.4.0` | 26     |
| `GIT-M-0005` | 4     | `GIT-EP-0005` | Git sync                       | `v0.5.0` | 26     |
| `GIT-M-0006` | 5     | `GIT-EP-0006` | MCP server and agent workflows | `v0.6.0` | 16     |
| `GIT-M-0007` | 6     | `GIT-EP-0007` | Retros, metrics and 1.0        | `v1.0.0` | 21     |

Estimates use a modified Fibonacci scale (1, 2, 3, 5, 8, 13). A story larger than 8 points
is split before it is started. No calendar dates are committed here: this is an
open-source project with variable capacity, so the plan is ordered, not scheduled.

---

## 2. Milestones in detail

### Milestone 1 — Foundations (`GIT-M-0001`, Phase 0, `v0.1.0`)

**Goal.** Establish the repository, the shared Go core and the pipeline, so that every
later phase is built on a model and a build that already work. Nothing user-facing ships;
everything user-facing depends on this.

**Deliverables**

- Monorepo scaffold exactly as specified in
  [02-architecture.md](./02-architecture.md): `cmd/gintrack`, `internal/core`,
  `internal/server`, `internal/watcher`, `internal/gitops`, `internal/mcp`, `wasm/`, `web/`,
  `docs/`, `Makefile`, `go.mod`, `.goreleaser.yaml`.
- `internal/core`: item model, YAML front-matter parser and serialiser (round-trip safe),
  schema validation, configurable status workflows from `project.yaml`, ID allocator,
  in-memory index and a first query API, implementing
  [03-data-model.md](./03-data-model.md).
- `core.FS` abstraction with a native implementation, so the core never touches `os`
  directly.
- WASM build target producing `web/public/core.wasm` plus the JS glue contract.
- `ci.yml` and `release.yml` as specified in
  [09-ci-cd-and-releases.md](./09-ci-cd-and-releases.md), plus `.golangci.yaml`,
  ESLint/Prettier/tsconfig, and the Vite + React + Tailwind + shadcn skeleton.
- Fixture repositories `testdata/fixtures/project-basic` and `team-basic`.
- This documentation set and the dogfooded backlog under `docs/.pmngr/`.

**Exit criteria**

- `make build` produces a `gintrack` binary on Linux, macOS and Windows.
- `make test` is green; `internal/core` coverage ≥ 85 %.
- Parser round-trips every fixture file byte-for-byte except for intentional
  normalisation, verified by golden tests.
- The same core compiles to WASM and answers a smoke query from a browser page.
- A tagged `v0.1.0` produces all six archives plus `checksums.txt` through GoReleaser.

**Explicitly out of scope.** Any UI beyond a smoke page, any git operation, any server.

---

### Milestone 2 — Browser-only MVP (`GIT-M-0002`, Phase 1, `v0.2.0`)

**Goal.** A useful product with no installation: open a local project folder in a Chromium
browser, read the knowledge base, and manage a single project's backlog.

**Deliverables**

- Folder picking through the File System Access API, with persisted handles and permission
  re-prompting.
- WASM core hosted in a Web Worker behind a typed adapter interface; index cached in
  IndexedDB and invalidated by content hash.
- Knowledge-base viewer: GFM, task lists, footnotes, callouts, wikilinks `[[Page]]`,
  Mermaid diagrams, optional math; an outline and a backlinks panel from the link graph.
- Backlog views for a single project: list, filters (type, status, label, assignee,
  milestone, priority), full-text search, item detail.
- Create and edit epics, stories, tasks and milestones in a CodeMirror 6 editor with front
  matter edited through a form and the body as Markdown; writes land in the working tree.
- Read-only fallback via `<input webkitdirectory>` for Firefox and Safari, with a clear
  explanation of the limitation.

**Exit criteria**

- A new user opens `testdata/fixtures/project-basic` and creates, edits and reads a story
  without touching a terminal, in under two minutes.
- A 5,000-file vault indexes in under 3 seconds on a mid-range laptop and re-indexes a
  single changed file in under 100 ms.
- The UI thread is never blocked for more than 50 ms during indexing (measured).
- Every write produces a file that the Go native parser reads back identically.
- Firefox and Safari load the vault read-only and say so.

---

### Milestone 3 — Companion CLI (`GIT-M-0003`, Phase 2, `v0.3.0`)

**Goal.** Remove the browser's limits for users who install the binary: native speed, real
file watching, and the same UI served locally.

**Deliverables**

- `gintrack serve` binding `127.0.0.1:7317` and serving the embedded `web/dist` through
  `go:embed`.
- `internal/watcher` over fsnotify with debouncing, recursive watching, ignore rules, and a
  polling fallback for platforms and network drives where events are unreliable.
- REST API for projects, items, KB pages and search, plus a WebSocket channel streaming
  index and file change events; per-run auth token and strict origin checks.
- Native indexer sharing the same `internal/core` code path as the WASM build.
- Companion auto-detection in the web app: `GET /api/health`, automatic upgrade from the
  WASM adapter to the REST adapter with no reload and no loss of state.
- CLI utility commands: `gintrack version`, `init`, `validate`, `index`, `open`.

**Exit criteria**

- Editing a file in an external editor updates the open UI in under 300 ms.
- A 50,000-file vault indexes natively in under 5 seconds.
- The app behaves identically in both modes for every Phase 1 flow (one shared E2E suite,
  run twice).
- The server refuses to bind a non-loopback address without `--allow-remote`, and rejects
  requests with a foreign `Origin`.

---

### Milestone 4 — Team repository and boards (`GIT-M-0004`, Phase 3, `v0.4.0`)

**Goal.** Make the tool work for a team rather than one person: many projects, shared
boards, sprints, and visibility into projects that are not cloned locally.

**Deliverables**

- `team.yaml` model: metadata, members, and the list of project repositories (remote URL,
  default branch, docs folder path, project key).
- Multi-project workspace: open several project repos plus one team repo; unified search
  and cross-project item resolution by `ref: <projectKey>/<itemId>`.
- Kanban boards (`.pmngr/boards/*.md`): columns with mapped statuses, WIP limits, filters,
  card ordering persisted in `order:`, drag and drop with dnd-kit.
- Scrum boards and sprints (`.pmngr/sprints/*.md`): sprint definition, goal, item
  references, board views scoped to the active sprint.
- Remote references: cards for projects that are not cloned render from
  `.pmngr/index/<projectKey>.json`, clearly marked read-only with a link to the remote
  file.
- Index snapshot generation and refresh (`gintrack snapshot`), committed to the team repo.

**Exit criteria**

- A board in `testdata/fixtures/team-basic` shows cards from two projects, one cloned and
  one remote-only.
- Dragging a card between columns rewrites exactly one item file's `status` and the board's
  `order:` list, and nothing else.
- Two people moving different cards produce a mergeable YAML diff (verified by a scripted
  concurrent-edit test).
- WIP limits are enforced visually and cannot be silently exceeded.

---

### Milestone 5 — Git sync (`GIT-M-0005`, Phase 4, `v0.5.0`)

**Goal.** Close the loop: git becomes the sync mechanism from inside the product, in both
operating modes.

**Deliverables**

- `internal/gitops`: go-git wrapper with an optional system-`git` backend, selectable by
  configuration.
- `isomorphic-git` in browser-only mode over File System Access handles, including the
  documented CORS-proxy requirement for hosts that do not send permissive headers.
- Commit on save, off by default, with a configurable message template
  (`{{action}} {{id}}: {{title}}`) and batching of rapid edits.
- Explicit sync: fetch → rebase (or merge, configurable) → push, with a clear status
  indicator and a dry-run preview.
- Conflict handling: detection, a three-way text conflict UI for Markdown bodies, a
  field-level merge helper for YAML front matter, and an always-available "keep mine / keep
  theirs / edit" escape hatch.
- Credentials: native mode delegates to the user's credential helper and SSH agent; browser
  mode holds a per-session token in memory only, never persisted.

**Exit criteria**

- Two clones of the same project, edited concurrently, are reconciled through the UI
  without touching a terminal, including one real conflict.
- No credential is ever written to disk or to `localStorage` by git-in-track (verified by
  test).
- Commit on save produces one commit per logical edit, not one per keystroke.
- A push failure leaves the working tree in a state the user can recover from, with an
  actionable message.

---

### Milestone 6 — MCP server and agent workflows (`GIT-M-0006`, Phase 5, `v0.6.0`)

**Goal.** Make AI agents first-class collaborators, reading and writing the same files
through a well-specified interface rather than by guessing at Markdown.

**Deliverables**

- `gintrack mcp` over stdio, plus streamable HTTP on the companion server.
- Tools: `list_items`, `search_items`, `get_item`, `create_epic`, `create_story`,
  `create_task`, `update_item`, `add_comment`, `move_on_board`, `list_kb_pages`,
  `get_kb_page`, `search_kb`.
- Agent-optimised responses: compact JSON, front matter only unless the body is requested,
  cursor pagination, and a `rev` content hash on every item.
- Optimistic locking: writes carry the `rev` they are based on and are rejected with a
  structured conflict when stale.
- `AGENTS.md` conventions: how an agent picks a story, what it must not touch, how it
  reports progress in comments, and how its PRs are attributed and reviewed.

**Exit criteria**

- An agent connected over MCP picks a `todo` story, moves it to `in_progress`, opens a PR
  and comments on the story — with no human file edits in the loop.
- Two agents writing the same item concurrently produce exactly one success and one
  `rev` conflict, never a lost update.
- The tool schemas are stable and documented; a schema change bumps the MINOR version.

---

### Milestone 7 — Retrospectives, metrics and 1.0 (`GIT-M-0007`, Phase 6, `v1.0.0`)

**Goal.** Complete the agile loop, prove the model with numbers, and ship a release people
can install without reading the repository.

**Deliverables**

- Retrospectives (`.pmngr/retros/*.md`): went well / to improve / actions, participants,
  linked sprint; selected improvements recorded in `actions[]`.
- Promotion of a retro action into a task in a project repository, with a back-link.
- Metrics computed from git history and the index: burndown per sprint, cumulative flow by
  status, cycle and lead time, throughput — all derived, nothing stored redundantly.
- Polish pass: empty states, keyboard shortcuts, command palette, onboarding, accessibility
  audit, dark mode, performance budget.
- Distribution: Homebrew tap, Scoop bucket, Docker image on GHCR, documented `go install`.
- 1.0 release: frozen data model with `schemaVersion: 1`, a migration path from 0.x, a
  written compatibility promise, and complete user documentation.

**Exit criteria**

- A full sprint is planned, run, closed and retrospected entirely inside the tool, by the
  project's own team, using `docs/.pmngr/`.
- Burndown and cumulative flow match a hand-computed reference for a fixture sprint.
- All accessibility checks pass at WCAG 2.1 AA for the primary flows.
- `brew install`, `scoop install`, `docker run` and `go install` each produce a working
  `gintrack`, verified on a clean machine.

---

## 3. Epics and user stories

Every story below exists as a file in `docs/.pmngr/stories/` with the same ID and title.
"SP" is the story-point estimate.

### `GIT-EP-0001` — Foundations (Phase 0, milestone `GIT-M-0001`, 21 SP)

Repository scaffold, the shared Go core, and the pipeline everything else stands on.

| ID            | Title                                            | SP | Priority | Depends on |
| ------------- | ------------------------------------------------ | -- | -------- | ---------- |
| `GIT-US-0001` | Scaffold the monorepo and build toolchain        | 3  | critical | —          |
| `GIT-US-0002` | Parse Markdown front matter into the core model  | 5  | critical | US-0001    |
| `GIT-US-0003` | Validate items against the project workflow      | 3  | high     | US-0002    |
| `GIT-US-0004` | Allocate collision-free item IDs                 | 5  | high     | US-0002    |
| `GIT-US-0005` | Set up the CI pipeline and the WASM build        | 5  | critical | US-0001    |

### `GIT-EP-0002` — Browser-only MVP (Phase 1, milestone `GIT-M-0002`, 37 SP)

Open a folder, read the knowledge base, manage one project's backlog — no install.

| ID            | Title                                                     | SP | Priority | Depends on       |
| ------------- | --------------------------------------------------------- | -- | -------- | ---------------- |
| `GIT-US-0006` | Open a local project folder in the browser                | 5  | critical | US-0005          |
| `GIT-US-0007` | Index a vault in a Web Worker and cache it in IndexedDB   | 8  | critical | US-0006, US-0002 |
| `GIT-US-0008` | Render knowledge base pages with extended Markdown        | 8  | critical | US-0006          |
| `GIT-US-0009` | Browse and filter the backlog                             | 5  | high     | US-0007          |
| `GIT-US-0010` | Create and edit items in the Markdown editor              | 8  | critical | US-0009, US-0004 |
| `GIT-US-0011` | Provide a read-only fallback for non-Chromium browsers    | 3  | medium   | US-0008          |

### `GIT-EP-0003` — Companion CLI (Phase 2, milestone `GIT-M-0003`, 24 SP)

Native speed, real file watching, the same UI served from `127.0.0.1`.

| ID            | Title                                                    | SP | Priority | Depends on       |
| ------------- | -------------------------------------------------------- | -- | -------- | ---------------- |
| `GIT-US-0012` | Serve the embedded web app with `gintrack serve`         | 5  | critical | US-0010          |
| `GIT-US-0013` | Watch the file system and stream changes over WebSocket  | 8  | critical | US-0012          |
| `GIT-US-0014` | Expose the REST API for items and knowledge base pages   | 8  | critical | US-0012          |
| `GIT-US-0015` | Auto-detect the companion and upgrade the web app        | 3  | high     | US-0014          |

### `GIT-EP-0004` — Team repository and boards (Phase 3, milestone `GIT-M-0004`, 26 SP)

Many projects, shared boards, sprints, and remote references.

| ID            | Title                                              | SP | Priority | Depends on       |
| ------------- | -------------------------------------------------- | -- | -------- | ---------------- |
| `GIT-US-0016` | Load a team repository and its projects            | 5  | critical | US-0015          |
| `GIT-US-0017` | Run a Kanban board with drag and drop              | 8  | critical | US-0016          |
| `GIT-US-0018` | Plan and run sprints on a Scrum board              | 8  | high     | US-0017          |
| `GIT-US-0019` | Show remote references from index snapshots        | 5  | high     | US-0016          |

### `GIT-EP-0005` — Git sync (Phase 4, milestone `GIT-M-0005`, 26 SP)

Git as the only sync mechanism, driven from the product.

| ID            | Title                                                | SP | Priority | Depends on       |
| ------------- | ---------------------------------------------------- | -- | -------- | ---------------- |
| `GIT-US-0020` | Commit on save with a configurable message template  | 5  | high     | US-0014          |
| `GIT-US-0021` | Sync a repository with fetch, rebase and push        | 8  | critical | US-0020          |
| `GIT-US-0022` | Resolve text conflicts in the UI                     | 8  | high     | US-0021          |
| `GIT-US-0023` | Handle git credentials safely in both modes          | 5  | critical | US-0021          |

### `GIT-EP-0006` — MCP server and agent workflows (Phase 5, milestone `GIT-M-0006`, 16 SP)

Agents as first-class collaborators over a specified interface.

| ID            | Title                                                    | SP | Priority | Depends on       |
| ------------- | -------------------------------------------------------- | -- | -------- | ---------------- |
| `GIT-US-0024` | Expose backlog tools over an MCP server on stdio         | 8  | critical | US-0014          |
| `GIT-US-0025` | Guard agent writes with rev-based optimistic locking     | 5  | critical | US-0024          |
| `GIT-US-0026` | Document AGENTS.md conventions for agent contributors    | 3  | high     | US-0025          |

### `GIT-EP-0007` — Retrospectives, metrics and 1.0 (Phase 6, milestone `GIT-M-0007`, 21 SP)

Close the agile loop, prove it with numbers, ship 1.0.

| ID            | Title                                                | SP | Priority | Depends on       |
| ------------- | ---------------------------------------------------- | -- | -------- | ---------------- |
| `GIT-US-0027` | Capture retrospectives and improvement actions       | 5  | high     | US-0018          |
| `GIT-US-0028` | Show burndown and cumulative flow metrics            | 8  | medium   | US-0018, US-0021 |
| `GIT-US-0029` | Publish Homebrew, Scoop and Docker distributions     | 5  | medium   | US-0005          |
| `GIT-US-0030` | Ship the 1.0 release                                 | 3  | high     | all              |

---

## 4. Dependencies

### Epic dependency graph

```mermaid
graph TD
  EP1["GIT-EP-0001<br/>Foundations<br/>21 SP"]
  EP2["GIT-EP-0002<br/>Browser-only MVP<br/>37 SP"]
  EP3["GIT-EP-0003<br/>Companion CLI<br/>24 SP"]
  EP4["GIT-EP-0004<br/>Team repo and boards<br/>26 SP"]
  EP5["GIT-EP-0005<br/>Git sync<br/>26 SP"]
  EP6["GIT-EP-0006<br/>MCP and agents<br/>16 SP"]
  EP7["GIT-EP-0007<br/>Retros, metrics, 1.0<br/>21 SP"]

  EP1 --> EP2
  EP2 --> EP3
  EP3 --> EP4
  EP3 --> EP5
  EP3 --> EP6
  EP4 --> EP7
  EP5 --> EP7
  EP6 --> EP7
  EP1 -. "release pipeline reused" .-> EP7
```

Epics 4, 5 and 6 all depend only on the companion CLI, so once Phase 2 lands they can be
worked in parallel by separate streams. Phase 3 (boards) is sequenced first because it is
the strongest product differentiator; Phase 5 (MCP) can be pulled forward if agent capacity
is available, since it depends on the REST layer rather than on boards.

### Story-level sequencing

```mermaid
gantt
  title git-in-track — indicative sequencing (relative, not calendar-committed)
  dateFormat YYYY-MM-DD
  axisFormat %b

  section Phase 0 — Foundations
  US-0001 scaffold            :done,    a1, 2026-09-08, 5d
  US-0005 CI and WASM build   :active,  a2, after a1, 7d
  US-0002 front-matter parser :         a3, after a1, 10d
  US-0003 validation          :         a4, after a3, 5d
  US-0004 ID allocation       :         a5, after a3, 7d

  section Phase 1 — Browser MVP
  US-0006 open folder         :         b1, after a2, 8d
  US-0007 worker index        :         b2, after b1, 12d
  US-0008 KB renderer         :         b3, after b1, 12d
  US-0009 backlog views       :         b4, after b2, 8d
  US-0010 editor and writes   :         b5, after b4, 12d
  US-0011 read-only fallback  :         b6, after b3, 5d

  section Phase 2 — Companion CLI
  US-0012 serve and embed     :         c1, after b5, 8d
  US-0013 watcher and WS      :         c2, after c1, 12d
  US-0014 REST API            :         c3, after c1, 12d
  US-0015 auto-upgrade        :         c4, after c3, 5d

  section Phase 3 — Team boards
  US-0016 team repo           :         d1, after c4, 8d
  US-0017 kanban board        :         d2, after d1, 12d
  US-0018 sprints and scrum   :         d3, after d2, 12d
  US-0019 remote references   :         d4, after d1, 8d

  section Phase 4 — Git sync
  US-0020 commit on save      :         e1, after c3, 8d
  US-0021 fetch rebase push   :         e2, after e1, 12d
  US-0022 conflict UI         :         e3, after e2, 12d
  US-0023 credentials         :         e4, after e2, 8d

  section Phase 5 — MCP
  US-0024 MCP stdio server    :         f1, after c3, 12d
  US-0025 rev locking         :         f2, after f1, 8d
  US-0026 AGENTS.md           :         f3, after f2, 5d

  section Phase 6 — 1.0
  US-0027 retrospectives      :         g1, after d3, 8d
  US-0028 metrics             :         g2, after g1, 12d
  US-0029 distribution        :         g3, after e4, 8d
  US-0030 1.0 release         :         g4, after g2, 5d
```

Durations are relative sizing hints for ordering only. They are not commitments and no
milestone carries a date until the team has measured its own velocity over two sprints.

---

## 5. Risk register

Risks are scored *likelihood × impact* on a 1–3 scale; the product is the priority. Each
risk names an owner phase, a mitigation and, where the mitigation can fail, a fallback.

### R1 — File System Access API support is Chromium-only (score 6: L3 × I2)

Firefox and Safari do not implement `showDirectoryPicker()`. Roughly a third of desktop
users cannot use browser-only **write** mode at all, and Safari users on macOS are exactly
the audience most likely to try the no-install path first.

*Mitigation.* Ship the read-only fallback (`GIT-US-0011`) from Phase 1 with an honest,
non-nagging explanation and a one-click path to the companion binary. Design the core
adapter so browser-only mode is a plug-in strategy, not a fork: the companion (Phase 2) is
the supported answer for those browsers, and it is a single download. Track the Origin
Private File System and the state of the Web Applications WG proposals; adopt if they
land.

*Fallback.* Position the companion as the default install and the browser as the preview,
inverting the marketing rather than the architecture.

### R2 — WASM performance and payload size (score 6: L2 × I3)

A Go binary compiled to WASM is large (several MB even with `-ldflags "-s -w"`), and
`GOOS=js` has no worker-thread parallelism and a garbage collector that can pause. Indexing
a large vault could be slow enough to make browser-only mode feel broken, and the download
alone could deter first use.

*Mitigation.* Run the core in a dedicated Web Worker so pauses never freeze the UI
(`GIT-US-0007`); stream and cache the index in IndexedDB keyed by content hash so a second
visit is near-instant; index incrementally per file rather than in one pass; serve
`core.wasm` compressed (brotli) and lazily, after first paint. Set an explicit performance
budget in Phase 1 (5,000 files < 3 s, `core.wasm` < 6 MB compressed) and measure it in CI
with a benchmark job that fails on regression.

*Fallback.* If the budget cannot be met, move heavy queries to a TypeScript index built on
top of a thin WASM parser, keeping only parsing (the correctness-critical part) in shared
Go. Evaluate TinyGo for the WASM target, accepting its reflection limits.

### R3 — CORS blocks git operations from the browser (score 6: L3 × I2)

`isomorphic-git` speaks the git smart-HTTP protocol over `fetch`, and almost no git host
sends the CORS headers a browser requires. GitHub does not. Self-hosted Gitea and GitLab
usually do not either. Without a proxy, Phase 4 simply does not work in browser-only mode.

*Mitigation.* Document this as a first-class limitation, not a footnote (it is already
called out in the brief). Support a configurable CORS proxy URL, ship instructions for
self-hosting `cors-proxy` in one command, and never send credentials to a proxy the user
did not explicitly configure. When the companion is present, route git through it, which
removes the problem entirely.

*Fallback.* Make browser-only mode read/write on disk but sync-free: the user syncs with
their own git client. This is an acceptable product, since the working tree is the source
of truth in every mode.

### R4 — ID collisions across concurrent branches (score 6: L3 × I2)

IDs are per-project sequential (`GIT-US-0042`). Two people, or two agents, creating a story
on separate branches will both allocate `0042` and the collision only surfaces at merge —
when both files already exist and are referenced.

*Mitigation.* Allocate against the index of the **whole repository**, not the working
branch, and detect duplicates at load time with a hard validation error naming both files
(`GIT-US-0004`). Provide `gintrack renumber <id>` that rewrites the file and every inbound
reference atomically. Offer an optional `idStrategy: sequential | ulid` in `project.yaml`
for teams with heavy parallelism; ULIDs never collide at the cost of readability. Add a CI
check to the project template that fails a PR introducing a duplicate ID.

*Fallback.* Default new high-parallelism projects to a hybrid: readable sequential ID plus
a short random suffix on collision (`GIT-US-0042b`).

### R5 — Merge conflicts inside YAML front matter (score 6: L3 × I2)

Git merges Markdown files line by line. Two people editing different fields of the same
item, or reordering a board's `order:` list, produce conflicts that are ugly to resolve by
hand and easy to resolve *wrongly* — a bad merge can silently drop an assignee or a label.

*Mitigation.* Keep front matter canonical: fixed key order, stable formatting, one item per
line, no inline flow collections, written by the serialiser and never by hand-editing in
the UI. This makes most concurrent edits touch different lines and merge cleanly. Provide a
field-level three-way merge UI for front matter (`GIT-US-0022`) that works on parsed values
rather than text. Store card order in the board file as one ref per line so reordering
produces a readable diff. Ship a `.gitattributes` with a custom merge driver hint, and
document `merge=union` for append-only lists.

*Fallback.* Detect the conflict, refuse to guess, and present "keep mine / keep theirs /
edit" per field. Never auto-resolve front matter silently.

### R6 — File watching on Windows (and network drives) (score 4: L2 × I2)

`fsnotify` on Windows uses `ReadDirectoryChangesW`, which drops events under load, does not
follow directory renames well, and behaves unpredictably on SMB shares, WSL-mounted paths
and OneDrive-synced folders. A missed event means the UI silently shows stale data — the
worst kind of bug for a tool whose whole promise is that the files are the truth.

*Mitigation.* Debounce and coalesce events, then **verify** rather than trust: after any
event burst, re-stat the affected subtree and compare content hashes, so a missed event
costs a delay, not a wrong answer. Add a periodic low-cost reconciliation sweep (default 30
s) that catches anything the watcher lost. Ship `--watch-mode=events|poll|hybrid` with
`hybrid` as the default on Windows and on any path detected as a network or synced folder.
Run the Phase 2 integration suite on `windows-latest` in CI from the day the watcher lands.

*Fallback.* Polling with an adaptive interval; measurably slower but correct, and
acceptable for vaults of the size this tool targets.

### R7 — Data model churn breaking users' repositories (score 4: L2 × I2)

The on-disk format *is* the product's API. A rename in Phase 4 invalidates vaults created
in Phase 1, and unlike a database there is no central place to migrate.

*Mitigation.* `schemaVersion` in `project.yaml` and `team.yaml` from `v0.1.0`. Every
breaking change ships `gintrack migrate` and a changelog note (see
[09-ci-cd-and-releases.md](./09-ci-cd-and-releases.md) §7). Parsers are permissive on read
and strict on write: unknown front-matter fields are preserved on round-trip, never
dropped. The model is frozen at 1.0 with a written compatibility promise.

### R8 — Scope creep toward "another Jira" (score 6: L3 × I2)

Every project-management feature suggests three more. The differentiator is git-native
plain files, not feature parity.

*Mitigation.* This roadmap is the contract. Anything not in it enters the backlog as
`status: backlog` with no milestone and is reviewed at phase boundaries. The test for
inclusion is: *does this work when the only thing you have is a git repository and a text
editor?* Custom fields, permissions, time tracking, notifications and reporting beyond §2's
metrics are explicitly deferred past 1.0.

### R9 — Single-maintainer bus factor and review latency (score 4: L2 × I2)

An open-source project where one person reviews everything stalls whenever that person is
busy, and mixed human/agent throughput makes it worse: agents can produce PRs faster than
humans can review them.

*Mitigation.* Keep PRs small (§6 of the guidelines) so review is cheap. Automate everything
mechanical — formatting, lint, coverage, conventional-commit checks — so review is about
design, not style. Grow to at least two maintainers before 1.0. Cap agent work in progress
(see §6 below) so the review queue cannot be flooded.

### Risk summary

| ID | Risk                                  | Score | Phase most at risk |
| -- | ------------------------------------- | ----- | ------------------ |
| R1 | File System Access API support        | 6     | 1                  |
| R2 | WASM performance and payload size     | 6     | 1                  |
| R3 | CORS for browser git                  | 6     | 4                  |
| R4 | ID collisions                         | 6     | 0, 5               |
| R5 | YAML front-matter merge conflicts     | 6     | 3, 4               |
| R8 | Scope creep                           | 6     | all                |
| R6 | Windows file watching                 | 4     | 2                  |
| R7 | Data model churn                      | 4     | 0–5                |
| R9 | Bus factor and review latency         | 4     | all                |

Risks are reviewed at every phase boundary and in every retrospective from Phase 6 onwards;
new risks are appended here rather than tracked elsewhere.

---

## 6. Working model: humans and agents

git-in-track is built by a small mixed team, and the way that team works is itself a test
of the product. If picking up a story through MCP is awkward for our own agents, it will be
awkward for our users' agents.

### Roles

- **Maintainers (human, 1–3).** Own the roadmap and the data model, review every PR,
  cut releases, and are the only ones who merge. They write the stories, or at least
  approve them, because a badly specified story is what turns an agent into a liability.
- **Contributors (human).** Pick a story, implement it, open a PR. Community contributors
  are pointed at stories labelled `good-first-issue`, which are deliberately kept stocked in
  every phase.
- **Agents (AI).** Pick stories that are well-bounded and heavily test-covered: parser
  edge cases, table-driven tests, golden-file coverage, component extraction, documentation,
  fixture generation, mechanical refactors, dependency bumps. From Phase 5 they do this
  through the MCP server against `docs/.pmngr/`.
- **Reviewers.** Any maintainer, plus contributors with context on the area. **Every agent
  PR is reviewed by a human before merge, without exception** — this is a hard rule, not a
  default.

### How an agent picks up work (from Phase 5)

1. `list_items` with `type: story`, `status: todo`, ordered by priority, filtered to the
   current milestone and to labels the agent is trusted with (`agent-ok`).
2. `get_item` for the full body: description, acceptance criteria, links.
3. `update_item` sets `status: in_progress` and adds itself to `assignees[]`, carrying the
   `rev` it read. A stale `rev` means someone else took it — pick the next story, do not
   retry blindly.
4. Branch as `<type>/<scope>-<slug>` (guidelines §8), implement, keep commits conventional.
5. `add_comment` on the story with the branch name and a short plan, so humans can see what
   is in flight without reading the diff.
6. Open a PR whose title is a Conventional Commit and whose body references
   `Refs: GIT-US-XXXX`.
7. On merge, `update_item` sets `status: done`, ticks the acceptance criteria, sets
   `updated`, and links the PR.

The same seven steps are what a human does; the agent just does them over MCP instead of in
the UI. That symmetry is the point.

### Rules that keep this safe

- **WIP limits apply to agents too.** At most two agent stories `in_progress` at a time, so
  the human review queue stays drainable. This is enforced by the board's WIP limit, not by
  convention.
- **Stories are the unit of trust.** An agent may only act within the story it claimed.
  Anything it notices outside that scope becomes a new story in `backlog`, never a drive-by
  change in the PR.
- **Some areas are human-only until 1.0**: the data model in `internal/core`, the security
  surface (`internal/server` auth, credential handling, path validation), the release
  pipeline, and this roadmap. Agents may propose changes there as stories; they may not
  implement them unsupervised.
- **Definition of Done is not negotiable** (guidelines §10). "The tests pass" is not done.
- **Attribution is explicit.** Agent-authored commits carry a `Co-Authored-By:` trailer
  naming the agent, so `git log` tells the truth about how the project was built.
- **Comments are the audit trail.** Every non-trivial decision an agent makes is recorded
  as a comment on the story in `.pmngr/comments/`, which is version-controlled like
  everything else.

### Cadence

Two-week iterations, tracked on the project's own board once Phase 3 lands (until then, on
a plain list view). Each iteration: a short planning pass on the next stories, continuous
delivery to `main`, and a retrospective recorded in the team repository from Phase 6. The
first retrospective the tool ever stores will be our own — and if writing it is unpleasant,
that is a bug report.

---

## 7. Post-1.0: Phases 7–9 — integrations, triage and agents

Three phases planned in September 2026, after the 1.0 line and the workspace and Jujutsu
epics (`GIT-EP-0008` … `GIT-EP-0010`). They turn git-in-track from a self-contained tool into
one that talks to the systems around it: JetBrains YouTrack for issues and knowledge base,
Plane-style triage and cycles for the way work flows, and Pando for a conversational agent
and semantic search. Sections 1–6 above are unchanged; this section follows the same shape.

The analysis behind the plan came from four code reviews: `youtrack-cli` for the YouTrack
REST surface, Plane (`apps/api` intake and cycle models, importer contract), Pando
(`internal/agui`, `internal/rag`, the TypeScript SDK) and git-in-track itself. The decisions
they produced are recorded in the epics and stories, and the ones that change the data model
or a documented promise get an ADR in their story.

### 7.1 Decisions taken up front

- **`external` is a first-class front-matter field** on items, comments and KB pages:
  `external: [{system, id, url, key, synced_at}]`. It is the idempotency key for every
  import and sync, and it is the shape a second system (Plane, Jira) would reuse. ADR-031.
- **The YouTrack token lives on the machine running the companion**, in the `0600` config
  file keyed by project, with a `GINTRACK_YOUTRACK_TOKEN` override. It is never written to a
  repository, never echoed by an API and never logged. This revises the "no credentials
  stored" promise in `docs/10-development-guidelines.md`; ADR-032 records why.
- **Per-project integration settings are committed** in `project.yaml` under
  `integrations.youtrack` (instance URL, project key, field map, push and sync modes), so a
  clone knows where its items came from without knowing the token.
- **Sync semantics are deliberately one-directional per flow** in Phase 7: import issues
  (re-import updates in place), push comments and feedback, publish and pull KB pages. No
  bidirectional status sync; conflicts on KB pages produce a conflict page, not a merge.
- **Background work gets a real engine** (`internal/syncengine`): persistent queue, worker
  pool, batches, shared rate limiter, retry with backoff, progress on the WebSocket hub, JSON
  journal in the cache directory. No embedded database.
- **Everything external is companion-only.** Browser-only mode cannot reach YouTrack (the
  CORS proxy is a git proxy, ADR-025) or Pando, so each feature sits behind a capability
  (`features.youtrack`, `features.agent`, `fullTextSearch: 'pando'`) and hides when absent.
- **Cycles extend sprints**; no new entity. **Inbox items are ordinary items** in a
  reserved `triage` status category with an `inbox` front-matter block.
- **The agent interface speaks AG-UI directly** with `@pando-ai/sdk/agui` and the shadcn
  design system, through a companion proxy that injects the Pando token and routes each
  repository to its own `pando agui-serve`. Pando does not implement the CopilotKit runtime,
  so CopilotKit would have meant a Node sidecar; rejected. `@ag-ui/client` was rejected too:
  its schemas drop Pando's `outcome: interrupt` and reasoning events (2026-09-13 gap analysis
  in `docs/research/`, backlog in the `PANDO` project).
- **Semantic search is an optional native accelerator** behind the existing `core/search`
  contract, exactly as `docs/02-architecture.md` §8 prescribes for bleve. Pando indexes the
  repository's own files in place — the documentation folder as its knowledge base, the
  working tree as a code project (`GIT-EP-0020`, ADR-036) — and the companion queries it over
  MCP; the core matcher stays the fallback.

### 7.2 Milestones at a glance

| Milestone     | Phase | Epics                          | Theme                                   | Points |
| ------------- | ----- | ------------------------------ | --------------------------------------- | ------ |
| `GIT-M-0011` | 7     | `GIT-EP-0011` … `GIT-EP-0015` | YouTrack integration and sync engine    | 188    |
| `GIT-M-0012` | 8     | `GIT-EP-0016`, `GIT-EP-0017`  | Inbox and cycles                        | 69     |
| `GIT-M-0013` | 9     | `GIT-EP-0018`, `GIT-EP-0019`  | Agentic interface and semantic search   | 73     |

The milestones carry planning due dates in the backlog (mid November 2026, mid December
2026, end of January 2027). They are estimates for a single maintainer plus agents, not
commitments; the order is what matters.

### 7.3 Milestones in detail

#### Milestone 11 — YouTrack integration (`GIT-M-0011`, Phase 7)

**Goal.** Link a project to a YouTrack project and move work in both directions without
leaving git-in-track, on top of a sync engine every later integration reuses.

**Deliverables**

- `internal/youtrack`: a native REST client with rate limiting, retries and stable paging.
- The `external` field, the local token store and the `integrations.youtrack` block.
- Settings card with connection test and an autosuggest project picker (a generic
  `Combobox` extracted from `ItemPicker`).
- "Import from YouTrack" in the backlog: query presets, autosuggest, subtasks, links,
  comments, attachments, preview, idempotent re-import, optional landing in the inbox.
- "Send to YouTrack" on comments and feedback notes; automatic push per project.
- KB publish and pull against YouTrack articles, folder trees, conflict pages.
- `internal/syncengine` with its REST API, WebSocket events and settings card.
- MCP tools and `gintrack youtrack …` commands for every flow.

**Exit criteria**

- A YouTrack epic with subtasks and comments imports into a fixture project, re-imports
  without duplicates, and a comment written locally appears on the issue.
- A KB folder publishes as an article tree and a remote edit pulls back without touching the
  local feedback block.
- The engine survives a restart mid-batch and resumes from its journal; `go test -race`
  passes with the fake clock tests.
- The token appears in no repository file, API response, event payload or log line.

#### Milestone 12 — Inbox and cycles (`GIT-M-0012`, Phase 8)

**Goal.** Give incoming work a waiting room and give sprints the date discipline and
history Plane's cycles have.

**Deliverables**

- `triage` status category, `inbox` block, index exclusion from backlog, boards and metrics.
- Inbox route with accept (opens the edit form), reject, snooze until, duplicate of.
- Entry points: quick create, MCP `create_inbox_item`, YouTrack import landing, CLI.
- Sprints: optional dates make a draft, derived status, no overlap per board, a progress
  snapshot frozen on close, transfer of incomplete items, active cycle view.

**Exit criteria**

- An item created into the inbox is invisible to every board, sprint and metric until
  accepted, proven by tests.
- Closing a sprint writes a snapshot that the burndown reads back identically to the live
  computation for a fixture sprint.

#### Milestone 13 — Agentic interface and semantic search (`GIT-M-0013`, Phase 9)

**Goal.** Talk to the backlog, and let the agent find things the substring matcher cannot.

**Deliverables**

- Companion proxy to Pando AG-UI, `@pando-ai/sdk/agui` store, chat route, tool-call cards,
  human-in-the-loop dialogs, shared-state panel, frontend tools that drive the UI.
- Pando configuration template, `backlog-assistant` persona and routing skill;
  `docs/20-agent-interface.md`.
- MCP client to `pando mcp-server`, `pando` search backend, semantic results in the search
  UI, `search_semantic` MCP tool, Pando settings card. The corpus exporter this phase first
  shipped was retired inside the phase by `GIT-EP-0020` (ADR-036): Pando indexes the
  repository's own files instead.

**Exit criteria**

- "Which stories touch the watcher?" answered in the chat with item cards, using
  git-in-track's MCP tools and Pando search, with a permission prompt rendered and answered
  in the browser.
- Search falls back to the core matcher, with a visible notice, when Pando is down.

### 7.4 Epics and user stories

Each story has 3–6 tasks in `docs/.pmngr/tasks/`, ordered core → server → web → MCP/CLI →
docs, most of them labelled `agent-ok`. Counts are shown per story.

#### `GIT-EP-0011` — YouTrack connection, credentials and project link (milestone `GIT-M-0011`, 45 SP)

| ID            | Title                                                                    | SP | Priority | Tasks |
| ------------- | ------------------------------------------------------------------------ | -- | -------- | ----- |
| `GIT-US-0044` | First-class `external` reference on items, comments and KB pages         | 8  | high     | 5     |
| `GIT-US-0046` | Native YouTrack REST client in `internal/youtrack`                       | 13 | high     | 6     |
| `GIT-US-0048` | Store the YouTrack token locally and the project link in `project.yaml`  | 8  | high     | 5     |
| `GIT-US-0052` | REST endpoints for YouTrack settings and connection test                 | 5  | high     | 5     |
| `GIT-US-0055` | Settings card to connect a project to YouTrack, with project autosuggest | 8  | medium   | 4     |
| `GIT-US-0058` | CLI: `gintrack youtrack connect` and `status`                            | 3  | medium   | 4     |

#### `GIT-EP-0012` — Import YouTrack issues into the backlog (milestone `GIT-M-0011`, 49 SP)

| ID            | Title                                                             | SP | Priority | Tasks |
| ------------- | ----------------------------------------------------------------- | -- | -------- | ----- |
| `GIT-US-0045` | YouTrack issue to item mapping layer                              | 8  | high     | 6     |
| `GIT-US-0047` | Vault operations `youtrack.import.preview` and `youtrack.import.run` | 8 | high  | 5     |
| `GIT-US-0050` | Import job kind in the sync engine                                | 5  | high     | 4     |
| `GIT-US-0054` | YouTrack issue search and autosuggest endpoint                    | 5  | high     | 4     |
| `GIT-US-0059` | Backlog "Import from YouTrack" dialog                             | 13 | high     | 5     |
| `GIT-US-0062` | MCP tool and CLI command for YouTrack import                      | 5  | medium   | 4     |
| `GIT-US-0065` | Field map configuration in the YouTrack settings card             | 5  | medium   | 4     |

#### `GIT-EP-0013` — Push comments and feedback to YouTrack (milestone `GIT-M-0011`, 21 SP)

| ID            | Title                                                          | SP | Priority | Tasks |
| ------------- | -------------------------------------------------------------- | -- | -------- | ----- |
| `GIT-US-0068` | External references on comments and the comment push job kind  | 8  | medium   | 4     |
| `GIT-US-0072` | Automatic push seams on comment writes                         | 5  | medium   | 4     |
| `GIT-US-0076` | "Send to YouTrack" action on comments and feedback notes       | 5  | medium   | 4     |
| `GIT-US-0079` | MCP tool and CLI command for pushing comments                  | 3  | medium   | 4     |

#### `GIT-EP-0014` — Knowledge base sync with YouTrack articles (milestone `GIT-M-0011`, 29 SP)

| ID            | Title                                                          | SP | Priority | Tasks |
| ------------- | -------------------------------------------------------------- | -- | -------- | ----- |
| `GIT-US-0083` | KB page and YouTrack article content transform                 | 8  | medium   | 5     |
| `GIT-US-0087` | KB publish and pull job kinds with conflict handling           | 8  | medium   | 4     |
| `GIT-US-0090` | Vault and REST operations for KB sync status, publish and pull | 5  | medium   | 4     |
| `GIT-US-0093` | KB view sync toolbar, status badge and project settings        | 5  | medium   | 4     |
| `GIT-US-0094` | MCP tools and CLI commands for KB sync                         | 3  | medium   | 4     |

#### `GIT-EP-0015` — Sync engine: queue, workers, batches and settings (milestone `GIT-M-0011`, 44 SP)

| ID            | Title                                                             | SP | Priority | Tasks |
| ------------- | ----------------------------------------------------------------- | -- | -------- | ----- |
| `GIT-US-0063` | Core sync engine: queue, worker pool and keyed batching           | 13 | high     | 5     |
| `GIT-US-0067` | Retry policy, backoff and failure handling in the sync engine     | 5  | high     | 3     |
| `GIT-US-0070` | JSON job journal in the cache directory with replay and pruning   | 5  | medium   | 4     |
| `GIT-US-0074` | Sync job progress events on the WebSocket hub                     | 5  | high     | 4     |
| `GIT-US-0078` | REST API for sync jobs and engine settings                        | 5  | medium   | 4     |
| `GIT-US-0081` | Sync engine settings card with a live queue table                 | 8  | medium   | 4     |
| `GIT-US-0084` | Wire the sync engine into the server lifecycle and `gintrack serve` | 3 | high    | 4     |

#### `GIT-EP-0016` — Inbox: triage incoming work before it enters the backlog (milestone `GIT-M-0012`, 32 SP)

| ID            | Title                                                                          | SP | Priority | Tasks |
| ------------- | ------------------------------------------------------------------------------ | -- | -------- | ----- |
| `GIT-US-0051` | Inbox data model: reserved triage category and the inbox front-matter block    | 8  | high     | 5     |
| `GIT-US-0056` | Inbox operations: create, list and triage over the vault, REST and MCP         | 8  | high     | 4     |
| `GIT-US-0060` | Inbox web route: two-pane triage queue with accept, reject, snooze and duplicate | 8 | medium | 4     |
| `GIT-US-0066` | Inbox entry points: quick create, agent submissions, YouTrack landing and CLI  | 5  | medium   | 4     |
| `GIT-US-0071` | Prove and document that triage work never reaches boards, sprints or metrics   | 3  | medium   | 3     |

#### `GIT-EP-0017` — Cycles: date-driven sprints with snapshots and transfer (milestone `GIT-M-0012`, 37 SP)

| ID            | Title                                                                         | SP | Priority | Tasks |
| ------------- | ----------------------------------------------------------------------------- | -- | -------- | ----- |
| `GIT-US-0075` | Sprint dates become optional and status becomes derived from them             | 8  | high     | 5     |
| `GIT-US-0080` | Closing a sprint freezes a progress snapshot into its file                    | 8  | high     | 5     |
| `GIT-US-0085` | Transfer incomplete items to another sprint or the backlog in one operation   | 8  | high     | 4     |
| `GIT-US-0089` | Active cycle view, close dialog and status-grouped sprint list in the web app | 8  | medium   | 4     |
| `GIT-US-0092` | Sprint CLI commands and the cycles documentation pass                         | 5  | medium   | 4     |

#### `GIT-EP-0018` — Conversational agent interface over Pando AG-UI (milestone `GIT-M-0013`, 39 SP)

| ID            | Title                                                                | SP | Priority | Tasks |
| ------------- | -------------------------------------------------------------------- | -- | -------- | ----- |
| `GIT-US-0049` | Companion agent proxy to Pando AG-UI                                 | 8  | high     | 5     |
| `GIT-US-0053` | AG-UI client layer and agent store in the web app                    | 8  | high     | 5     |
| `GIT-US-0057` | Chat route and message UI in the shadcn design system                | 8  | high     | 5     |
| `GIT-US-0061` | Human in the loop dialogs and shared-state panel                     | 5  | medium   | 4     |
| `GIT-US-0064` | Frontend tools registry so the agent can drive the UI                | 5  | medium   | 5     |
| `GIT-US-0069` | Pando side configuration, persona and agent interface documentation  | 5  | medium   | 5     |

#### `GIT-EP-0019` — Semantic search with Pando (milestone `GIT-M-0013`, 34 SP)

| ID            | Title                                                             | SP | Priority | Tasks |
| ------------- | ----------------------------------------------------------------- | -- | -------- | ----- |
| `GIT-US-0073` | Corpus exporter that keeps a Pando-indexable copy of the backlog  | 8  | high     | 5     |
| `GIT-US-0077` | Companion MCP client to the Pando search tools                    | 5  | high     | 4     |
| `GIT-US-0082` | Pando search backend behind the core search contract              | 8  | high     | 5     |
| `GIT-US-0086` | Semantic results in the search UI                                 | 5  | medium   | 4     |
| `GIT-US-0088` | Semantic search as an agent tool and a routing skill              | 5  | medium   | 4     |
| `GIT-US-0091` | Pando settings card with index status and reindex                 | 3  | medium   | 4     |

> `GIT-US-0073`'s corpus exporter was **reversed** by `GIT-EP-0020` — *Pando indexes the
> repository directly; retire the exported corpus* — inside the same phase and milestone. See
> [ADR-036](./adr/ADR-036-pando-indexes-the-repository-directly.md).

### 7.5 Dependencies

```mermaid
graph TD
  EP11["GIT-EP-0011<br/>YouTrack connection<br/>45 SP"]
  EP15["GIT-EP-0015<br/>Sync engine<br/>44 SP"]
  EP12["GIT-EP-0012<br/>Import issues<br/>49 SP"]
  EP13["GIT-EP-0013<br/>Push comments<br/>21 SP"]
  EP14["GIT-EP-0014<br/>KB sync<br/>29 SP"]
  EP16["GIT-EP-0016<br/>Inbox<br/>32 SP"]
  EP17["GIT-EP-0017<br/>Cycles<br/>37 SP"]
  EP18["GIT-EP-0018<br/>Agent interface<br/>39 SP"]
  EP19["GIT-EP-0019<br/>Semantic search<br/>34 SP"]

  EP11 --> EP12
  EP11 --> EP13
  EP11 --> EP14
  EP15 --> EP12
  EP15 --> EP13
  EP15 --> EP14
  EP12 -. "land in inbox" .-> EP16
  EP19 --> EP18
  EP11 -. "capability pattern" .-> EP18
```

Inside Phase 7, `GIT-US-0044` (the `external` field) and `GIT-US-0063` (the engine core) are
the two roots: everything else in the milestone reads one of them. `GIT-EP-0011` and
`GIT-EP-0015` can be worked in parallel; the three flow epics start once both have landed
their first stories. Phase 8 has no dependency on Phase 7 except the optional "land in inbox"
import option, so it can be pulled forward if YouTrack work stalls. In Phase 9 the search
epic is sequenced first: the agent's value depends on the search tools it can call.

### 7.6 Risks specific to these phases

- **R10 — Credential storage changes the security story.** A stored YouTrack token makes
  the companion config file a secret. Mitigation: `0600`, never echoed, env override for CI,
  ADR-032, and a standing warning in the tunnel settings card while a token is configured,
  since a public URL then exposes a companion that can write to YouTrack.
- **R11 — YouTrack instances differ.** Custom field names, link types and Markdown dialect
  vary per instance. Mitigation: field discovery endpoint, per-project field map, golden
  tests from real exports, warnings instead of failures on unknown values.
- **R12 — The sync engine becomes a second git.** A queue that persists state next to the
  repository is a step away from "git is the only sync" (ADR-002). Mitigation: the journal
  holds only job bookkeeping, never content; every effect is a normal commit.
- **R13 — Pando coupling.** Pando is per-project, has no REST search API and a single
  `KBPath`, and its knowledge-base walk applies no exclusions, so `KBPath` is the
  repository's documentation folder rather than its root. Mitigation: loopback only, a
  bearer token on the MCP transport, feature flags, core fallback, and a short list of Pando
  changes we would upstream (REST search, per-agent tool allow-list — the KB reindex trigger
  and the tool allow-list have since landed).
- **R14 — Pando's writing tools reach repository files.** Since `GIT-EP-0020` Pando indexes
  the repository itself, and `kb_add_document`, `kb_delete_document` and the memory
  `remember`/`forget` path mirror a document to disk with Pando's typed front-matter keys
  alone — which would strip an item's `id` and drop it from the index. Mitigation: the
  `[AGUI] Tools` allow-list `gintrack agent init` writes admits none of them. That is a
  configuration boundary, not a code boundary, so the risk is accepted and stated in
  ADR-036 and docs/20 §6.2; git is the recovery path.
