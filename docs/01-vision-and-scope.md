# git-in-track — Vision and Scope

Status: draft (Phase 0 planning)
Audience: contributors, early adopters, and anyone evaluating the project
Related: [02-architecture.md](02-architecture.md), [Architecture Decision Records](adr/README.md)

---

## 1. Problem statement

Software teams keep their source code in git and almost everything else somewhere
else. The backlog lives in Jira or Linear, the specifications live in Confluence or
Notion, the decisions live in Slack threads, and the sprint board lives in a SaaS
tool with its own account model, its own export format, and its own outage schedule.

This split creates four concrete, recurring problems:

1. **The backlog and the code drift apart.** A user story is written against the
   state of a branch, the branch is rewritten, and nothing links the two. There is
   no `git blame` for requirements, no diff for acceptance criteria, no way to
   check out the backlog exactly as it was at release `v1.4.0`.
2. **Context lives outside the repository.** A developer (or an AI agent) working
   in a checkout has the code but not the reasoning. Retrieving the reasoning means
   a browser tab, an API token, and a rate limit.
3. **Project data is hostage to a vendor.** Exports are lossy, schemas are opaque,
   and migration is a project of its own. Teams that must self-host for regulatory
   reasons are pushed toward heavyweight installations.
4. **AI agents are second-class citizens.** Current tools expose REST APIs designed
   for browsers and integrations, not for agents that need compact, deterministic,
   diffable state. An agent that can already read and write files in a repository
   should not need a network round trip and an OAuth dance to read a task
   description.

Meanwhile, developers already have a tool that solves versioning, distribution,
access control, offline work, merging, and audit for text: **git**. And they
already have a format that humans, editors, and language models all read fluently:
**Markdown**.

**git-in-track** is the project management tool that follows from taking those two
facts seriously. Every epic, story, task, milestone, comment, board, sprint,
retrospective, and knowledge-base page is a Markdown file with YAML front matter,
stored in a git repository, editable with any text editor, reviewable in a pull
request, and readable by an AI agent with nothing but filesystem access.

---

## 2. What git-in-track is

- A **local-first web application** (React + Vite + TypeScript) that opens folders
  on your machine and presents the Markdown files inside them as a project
  knowledge base, a backlog, and a set of agile boards.
- A **shared Go core** that parses, validates, indexes, and queries those files —
  compiled once natively for the CLI and once to WebAssembly for the browser, so
  the two modes never disagree about what a file means.
- An optional **companion CLI** (`gintrack`) that serves the same web app from
  `http://127.0.0.1:7317`, watches the filesystem for changes, indexes faster, and
  performs git operations natively.
- An **MCP server** (`gintrack mcp`) that exposes the same model to AI agents as a
  small set of well-shaped tools.

There is no server to deploy, no database to back up, and no account to create.
Sync is `git fetch` / `git rebase` / `git push`.

---

## 3. Target users

### 3.1 Development teams that already live in git

Teams of 2–30 people who write code, review pull requests, and would rather manage
their backlog with the same muscle memory. They value: text files, diffs, code
review, offline work, and not paying per seat for a tool their VCS host almost
replaces.

### 3.2 AI-agent-assisted teams

Teams where a meaningful share of the work is executed by coding agents (Claude
Code, Cursor, Aider, in-house agents). For these teams, the backlog is not just a
plan — it is the **input format** for the agents. They need:

- Task descriptions and acceptance criteria that an agent can read without an API.
- A write path an agent can use to report progress and open follow-up work.
- A change history in which agent-authored edits are as reviewable as human ones,
  because they are commits.

### 3.3 Teams with data-residency or self-hosting constraints

Consultancies, regulated industries, and public-sector teams that cannot put
project data in a third-party SaaS. If they can host a git remote — GitHub
Enterprise, GitLab, Gitea, a bare repo over SSH — they can run git-in-track.

### 3.4 Explicit non-audience

git-in-track is **not** aimed at large non-technical organisations that need
per-field permissions, mandatory workflow gates, timesheets, or portfolio
management across hundreds of teams. Those needs are legitimate; they are better
served by a database-backed product with a central authority. See
[Non-goals](#6-non-goals).

---

## 4. Guiding principles

These four principles are load-bearing. When a design question is ambiguous,
resolve it in this order.

### 4.1 Git is the source of truth

The repository content *is* the state. There is no shadow database that can become
authoritative, no server-side field that is not in a file, and no operation that
cannot be reproduced by editing a file and committing it. Caches and indexes are
derived artifacts and must be reconstructable from a clean checkout.

Consequences we accept: no server-side atomic transactions, no push notifications
without a git remote round trip, and conflicts that occasionally require a human.
Consequences we gain: `git log` is the audit trail, `git checkout v1.4.0` restores
the backlog of that release, branches let you plan a refactor without disturbing
`main`, and pull requests review the plan as well as the code.

### 4.2 Files are the API

Any capability offered by the UI must be expressible as an ordinary file edit. The
file layout and front-matter schema are a public, versioned contract — documented,
stable, and designed for humans to hand-edit in Vim as much as for the app to
write. A user who deletes the app must lose nothing but convenience.

This is what makes agent integration cheap: the MCP server is a convenience layer
over the same files, not a privileged gateway.

### 4.3 Local-first

The application works with no network. Opening a project, browsing the knowledge
base, editing stories, and moving cards on a board are local filesystem
operations. Network access is required only for git remote operations, and those
are always explicit or explicitly configured. Nothing is uploaded anywhere by
default; there is no telemetry.

### 4.4 Agent-friendly by construction

The data model is designed so that an LLM can consume it cheaply and write it
correctly:

- Front matter is small, flat, and typed; bodies are conventional Markdown with
  predictable section headings (`## Description`, `## Acceptance Criteria`,
  `## Notes`).
- Read APIs return front matter only unless the body is explicitly requested, so an
  agent can scan a 2,000-item backlog without burning its context window.
- Every read carries a `rev` (content hash) and every write can require it, giving
  agents optimistic concurrency without a lock server.
- Repository conventions for agents live in `AGENTS.md`, versioned with the code.

---

## 5. Goals

| # | Goal | How we will know |
|---|------|------------------|
| G1 | Manage a full backlog (epics, stories, tasks, milestones, comments) as Markdown files in the project repo | A team runs a two-week sprint without leaving the repo |
| G2 | Render a project's documentation folder as a first-class knowledge base | GFM tables, task lists, footnotes, callouts, wikilinks, and Mermaid all render |
| G3 | Work in a browser with zero installation | Chromium browser + File System Access API, no CLI, full CRUD (Phase 1) |
| G4 | Offer a strictly better experience when the CLI is installed | Auto-detected companion adds fsnotify live updates, native indexing, native git (Phase 2) |
| G5 | Aggregate multiple projects onto one team board | Kanban and Scrum boards in the team repo referencing items across project repos (Phase 3) |
| G6 | Use git as the only sync mechanism | Commit-on-save, explicit fetch/rebase/push, conflict UI (Phase 4) |
| G7 | Make agents first-class participants | `gintrack mcp` with list/search/get/create/update/comment/move tools (Phase 5) |
| G8 | Support the full agile loop | Sprints, retrospectives, improvement actions, burndown and cumulative flow (Phase 6) |
| G9 | Never lock in data | Every artifact is a readable Markdown file; the schema is documented in this repo |
| G10 | Be installable in one step | Single static binary per platform via GoReleaser; archives + checksums on the GitHub Release |

---

## 6. Non-goals

Stating these plainly saves review cycles later.

- **No central server, no hosted service.** git-in-track will not ship a
  multi-tenant backend, a user database, or a SaaS offering. Sync is git.
- **No identity or permission system of our own.** Access control is whatever the
  git remote enforces. If you can push to the repo, you can change the backlog.
  There are no per-field permissions and no approval workflows enforced by the app
  (branch protection is the mechanism for that).
- **No real-time multiplayer editing.** No CRDTs, no operational transformation,
  no live cursors. Two people editing the same story concurrently is a git
  conflict, presented in a conflict UI.
- **No issue tracker replacement for open-source triage.** Public bug intake from
  anonymous reporters needs an authenticated web form; use GitHub/GitLab issues.
  git-in-track manages the team's internal plan.
- **No time tracking, invoicing, or resource management.** `effort` in hours exists
  as an estimate field; there is no timer, no timesheet, and no billing.
- **No plugin marketplace in v1.** Extension points may come later; v1 ships a
  focused product.
- **No mobile app.** The web UI should be usable on a tablet; native mobile is out
  of scope.
- **No automatic migration from Jira/Linear in v1.** Importers are welcome as
  community contributions against the documented file format.
- **No CRDT-based offline merge for structured fields.** We rely on git's text
  merge plus a conflict UI, and on a file layout designed to minimise conflicts
  (one item per file, comments as separate files).

---

## 7. Personas

### 7.1 Dana — senior developer

**Context.** Works in a monorepo, lives in the terminal and the editor, reviews
five PRs a day. Considers context-switching to a browser tab a tax.

**Needs.** To read the acceptance criteria of the story she is implementing without
leaving her checkout; to update status as part of the same commit as the code; to
see what changed in a story since she last looked.

**With git-in-track.** The story is `docs/.pmngr/stories/ACME-US-0042-login-with-sso.md`
in the repo she already has open. She edits it in her editor when convenient and in
the web UI when she wants the board view. `git log -p` on that file is the story's
history. Her status change and her code land in the same pull request.

**Failure mode we must avoid.** A UI that rewrites files with different formatting
than she typed, producing noisy diffs. Round-tripping must be stable: key order,
quoting style, and body content are preserved.

### 7.2 Marco — product manager / scrum master

**Context.** Runs two squads across three repositories. Facilitates planning,
standups, and retrospectives. Comfortable with Markdown; not comfortable being
told to use the command line.

**Needs.** A board that spans all three projects; sprint scope and a burndown;
retrospectives that produce actions he can actually track; a way to write a story
with a rich description and acceptance criteria without learning YAML.

**With git-in-track.** He opens the team repository and the three project
repositories in the web app (or runs `gintrack serve` from a desktop shortcut). The
team board in `.pmngr/boards/delivery.md` references items from all three projects.
He drags a card; the app rewrites the `status` field in the project repo and the
`order:` list in the board file. At sprint end he creates a retro from a template,
and the actions he checks become tasks in the relevant project repos.

**Failure mode we must avoid.** Requiring him to understand rebases. Sync must be
one button with a comprehensible result, and conflicts must be explained in terms
of stories, not hunks.

### 7.3 Atlas — AI coding agent

**Context.** Runs in a sandbox with the project repository checked out and an MCP
client. Has a bounded context window and is charged per token. Is asked to "pick up
the next ready task and implement it."

**Needs.** To enumerate candidate tasks cheaply (front matter only, filtered,
paginated); to read one task's body in full; to write progress without clobbering a
concurrent human edit; to open follow-up work items that a human will review.

**With git-in-track.** Calls `search_items(type=task, status=todo, assignee=@me, limit=20)`
and gets a compact JSON array of front matter plus `rev`. Calls `get_item(id, body=true)`
for the chosen one. Implements the change. Calls `update_item(id, rev, status=in_review)`;
if the `rev` no longer matches, the call fails and it re-reads instead of overwriting.
Calls `add_comment(item, text)` to record what it did. Every one of those writes is a
file change in the working tree, visible in `git status`, reviewable in the PR.

**Failure mode we must avoid.** An API that forces the agent to fetch full bodies to
find a task, or that silently accepts a stale write.

---

## 8. Key user journeys

### 8.1 Open a project folder (first run, browser-only)

1. Dana opens the web app in a Chromium-based browser.
2. She clicks "Open project" and picks the repository folder with the File System
   Access API. The browser prompts for read/write permission; the handle is
   persisted in IndexedDB so the next visit is one click.
3. The app looks for a documentation folder. If `project.yaml` is not found under a
   `.pmngr/` directory, it offers to initialise one: she picks the docs folder
   (`docs/`), a project key (`ACME`), and a status workflow (default:
   `backlog → todo → in_progress → in_review → done`, plus `cancelled`).
4. The WASM core scans the folder in a Web Worker, parses front matter, and builds
   the in-memory index; progress is streamed to the UI. The index is cached in
   IndexedDB keyed by the folder handle and file mtimes.
5. She lands on the project overview: knowledge base tree on the left, backlog
   counts by status, recent activity from the index.

Acceptance: on a repository with 1,000 items and 500 KB pages, this completes in
under 3 seconds on a mid-range laptop, and under 1 second on subsequent visits from
cache.

### 8.2 Create a user story

1. Marco clicks "New story" from the epic `ACME-EP-0007`.
2. The app allocates the next free ID for the project (`ACME-US-0043`) by scanning
   the index, not by trusting a counter file, so two people creating stories
   offline produce at worst a reconcilable duplicate rather than a corrupted
   sequence.
3. He fills in title, assignees, labels, priority, estimate; writes a description
   and acceptance criteria as a task list in the CodeMirror editor.
4. On save the app writes `docs/.pmngr/stories/ACME-US-0043-password-reset.md` with
   YAML front matter and the Markdown body.
5. If commit-on-save is enabled, the app commits with the configured template
   (e.g. `docs(backlog): create ACME-US-0043 Password reset`). Otherwise the file
   sits in the working tree until he syncs.

### 8.3 Team board across projects

1. Marco opens the team repository. `team.yaml` lists three project repositories
   with their remote URL, default branch, docs folder, and project key.
2. For each project he has cloned locally, the app resolves items live from the
   local working tree.
3. For projects he has **not** cloned, cards render as **remote references**: title
   and status come from the index snapshot committed in the team repo at
   `.pmngr/index/<projectKey>.json`, with a link to the file on the remote host.
   These cards are read-only and visually marked as such.
4. He drags `ACME-US-0043` from *In progress* to *In review*. The app updates the
   `status` front-matter field in the project repo and the `order:` list for the
   target column in `.pmngr/boards/delivery.md` in the team repo — two files in two
   repositories, each committed in its own repo.
5. WIP limits are evaluated from the board definition; exceeding one warns but does
   not block (git-in-track advises, it does not gate).

### 8.4 An agent picks up a task via MCP

1. Atlas is started with the project repo checked out and `gintrack mcp` configured
   as an MCP server (stdio), or pointed at the companion's streamable HTTP endpoint.
2. It calls `search_items` with `type=task`, `status=todo`, and a label filter,
   receiving compact front matter plus `rev` for each hit, paginated.
3. It calls `get_item(id, body=true)` for the selected task and reads the acceptance
   criteria.
4. It sets `status=in_progress` with `update_item(id, rev, ...)`, implements the
   change in the codebase, and appends a comment describing the approach at
   `.pmngr/comments/ACME-TSK-0117/20260903T101500Z-atlas.md`.
5. It moves the task to `in_review` and opens a pull request. The reviewer sees the
   code change, the status change, and the agent's comment in a single diff.
6. Conventions the agent follows — which fields it may set, when to create versus
   comment, how to name commits — live in `AGENTS.md` at the repo root.

### 8.5 Retrospective and improvement actions

1. At sprint end, Marco creates a retro from the sprint: `.pmngr/retros/2026-w36.md`
   with `sprint`, `date`, and `participants[]`.
2. The team fills `## Went well`, `## To improve`, and `## Actions` as task lists.
   Everyone can edit the file directly, or contribute through the UI.
3. Selected improvements are recorded in `actions[]` in the front matter.
4. Each action can be promoted to a task in a project repo; the created task links
   back to the retro through `links[]` (`relates_to`), so the next retro can show
   whether the previous actions were completed.
5. Sprint metrics (burndown from `estimate` and status transitions in git history,
   cumulative flow from index snapshots) are computed from the repository, not from
   an event log we maintain.

---

## 9. Competitive positioning

| Tool | Where it wins | Where git-in-track differs |
|------|---------------|----------------------------|
| **Jira** | Deep configurability, enterprise governance, huge integration ecosystem, per-field permissions | git-in-track has no central server, no permission model of its own, and far fewer features — in exchange for plain files, git history, offline work, zero admin, and no per-seat cost. Jira's data lives in Jira; ours lives in your repo. |
| **Linear** | Exceptional UX and speed, opinionated agile workflow, excellent triage | Linear is a hosted, closed system with an API-only data path. git-in-track cannot match its polish at v1, but its state is diffable and reviewable and works with no network. We aim to be "Linear-shaped where it costs nothing to be." |
| **GitHub Projects / Issues** | Already in the host, free, tight PR integration, good API | Issues live in the host's database, not the repo: no `git checkout` of the backlog, no offline edit, no portability across GitLab/Gitea, and cross-host aggregation is hard. git-in-track works identically on any git remote and aggregates repos across hosts on one board. |
| **Obsidian** | Superb local-first Markdown knowledge base, plugin ecosystem, wikilinks and graph | Obsidian is a note vault with community plugins bolted on for tasks; it has no shared team model, no cross-repo boards, and no agent API. git-in-track adopts the vault philosophy but ships a first-class project model, git sync, and MCP as core features. Vaults remain readable in Obsidian. |
| **Logseq** | Outliner model, block references, local files, good for personal planning | Block-level granularity is powerful for thinking and hostile to git diffs and agents. git-in-track chooses file-level granularity (one item, one file) precisely so diffs, merges, reviews, and agent writes stay legible. |
| **Taskwarrior / git-bug / plain Markdown TODOs** | Extremely simple, fully local, git-friendly | These lack a team model, a knowledge base, boards, or a UI a PM will use. git-in-track keeps the file-based ethos and adds the parts that make it usable by a whole team. |

**The one-sentence position:** *git-in-track is what you get when you take Obsidian's
local-first Markdown vault, give it Linear's project model, and make git the only
sync mechanism — so that humans and AI agents read and write the same files.*

**Honest limitations to state up front.** Browser-only mode needs Chromium
(File System Access API); other browsers get a read-only fallback via
`<input webkitdirectory>`. In-browser git may need a CORS proxy for some hosts.
Concurrency is optimistic and conflicts are real. Large repositories index more
slowly in WASM than natively. None of these are hidden; all are documented.

---

## 10. Success criteria

### 10.1 Product

- **P1.** A team can run a complete sprint — plan, board, standup, review, retro —
  using only git-in-track and their git remote.
- **P2.** A developer can do everything from the editor and the terminal without the
  UI, and the UI does not corrupt or reformat what they wrote.
- **P3.** An AI agent can find, read, update, and comment on work items through MCP
  without ever fetching a full body it does not need.
- **P4.** Deleting the app leaves a repository that is still perfectly readable in
  GitHub's Markdown preview and in Obsidian.

### 10.2 Technical

- Index 10,000 items in **under 2 s natively** and **under 8 s in WASM** (see
  [02-architecture.md](02-architecture.md#9-performance-targets) for the full table).
- One shared Go core: zero model, parsing, or validation logic duplicated in
  TypeScript.
- Single static binary per platform; `gintrack serve` from download to working UI in
  under 60 seconds.
- CI green on every PR: `go vet`, `go test`, `golangci-lint`, web lint/typecheck/test,
  and Playwright end-to-end tests.

### 10.3 Adoption (12 months after 1.0)

- The project dogfoods itself: git-in-track's own backlog lives in `docs/.pmngr/`.
- At least three teams outside the founding organisation run it for real work.
- At least one third-party importer or MCP client integration built against the
  documented file format by someone who never spoke to the maintainers — the proof
  that "files are the API" is true.

### 10.4 Anti-goals as guardrails

We will consider the project to have failed its own principles if any of these
become true:

- A feature requires running a server that stores state outside the repository.
- The UI writes a file that a human cannot reasonably hand-edit.
- The file format changes without a documented migration and a version bump.
- An agent must scrape HTML or reverse-engineer an internal endpoint to participate.

---

## 11. Scope by phase

The roadmap phases are defined in the architecture brief and referenced throughout
the documentation:

| Phase | Scope | Vision goals covered |
|-------|-------|----------------------|
| **Phase 0 — Foundations** | Repo scaffold, core model, front-matter parser, ID allocation, CI | G9 |
| **Phase 1 — Browser-only MVP** | Open folders, KB viewer (Markdown + Mermaid), backlog CRUD in `.pmngr`, `project.yaml`, single project | G1, G2, G3 |
| **Phase 2 — Companion CLI** | `gintrack serve` + `go:embed`, fsnotify watcher, native index, REST/WS API, auto-upgrade from the web app | G4 |
| **Phase 3 — Team repo** | `team.yaml`, multi-project, Kanban/Scrum boards, sprints, remote references, index snapshots | G5 |
| **Phase 4 — Git sync** | isomorphic-git in browser, go-git in CLI, commit-on-save, conflict handling, credentials | G6 |
| **Phase 5 — MCP + agents** | `gintrack mcp`, `AGENTS.md` conventions, read API optimised for agents | G7 |
| **Phase 6 — Agile loop and 1.0** | Retrospectives, improvement actions, burndown and cumulative flow, polish, releases | G8, G10 |

Anything not listed above is out of scope until it has an ADR.

---

## 12. Open questions

These are tracked as decisions still to be made; each will become an ADR when
resolved.

1. **Licensing.** MIT is the placeholder in the brief; the owner must confirm before
   the first public release.
2. **Front-matter schema versioning.** Do we stamp a `schema:` version into
   `project.yaml` only, or into every item file? Leaning towards `project.yaml` only,
   with migrations applied per project.
3. **Non-Chromium browsers.** Is read-only fallback via `<input webkitdirectory>`
   enough for Phase 1, or do we need an OPFS-based import/export path?
4. **CORS proxy for in-browser git.** Do we document third-party proxies only, or
   ship `gintrack proxy` as a self-hostable option?
5. **Search backend.** In-memory inverted index everywhere, or bleve natively once
   corpora exceed a threshold? Decide with real measurements in Phase 2.
6. **Index snapshot freshness.** How and when does the team repo's
   `.pmngr/index/<projectKey>.json` get refreshed — CI job in the project repo, or a
   `gintrack snapshot` command run by whoever has the clone?
