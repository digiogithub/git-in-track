# 03 — Data Model: the `.pmngr/` backlog folder

**Status:** planning specification (normative for Phase 0 and Phase 1).
**Applies to:** project repositories only. The team repository data model is specified in
[`04-team-repository.md`](./04-team-repository.md).
**Implementation home:** `internal/core/` (model, front-matter parser, validation, ID allocation,
indexer). JSON Schemas live in `internal/core/schema/` and are embedded with `go:embed`.

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, **MAY** are to be interpreted as
described in RFC 2119.

---

## 1. Scope and design goals

`git-in-track` stores an entire project backlog as plain files inside the project's own git
repository. There is no database of record: the files *are* the database. Everything else
(`index.json`, the WASM index in IndexedDB, the CLI's in-memory index) is a derived cache that can
be rebuilt from the files at any time by deleting it and re-scanning.

Design goals, in priority order:

1. **Diff-friendly.** A one-field change must produce a one-line diff. This drives the choice of
   YAML front matter with stable key ordering and one item per file.
2. **Merge-friendly.** Concurrent edits by different people must conflict as rarely as possible, and
   when they do conflict, the conflict must be resolvable by a human reading the hunk.
3. **Human-editable without the tool.** A developer with `vim` and no `gintrack` binary must be able
   to create a valid story. Therefore: few required fields, forgiving defaults, no generated
   identifiers that a human cannot type.
4. **Agent-readable at low token cost.** An AI agent must be able to answer "what is in progress?"
   by reading a few kilobytes, not the whole backlog. See [§17](#17-agent-optimized-reading).
5. **No lock-in.** Every file renders acceptably in GitHub's Markdown viewer, Obsidian, and any
   static site generator that understands front matter.

Non-goals: real-time collaborative editing (git is the sync layer), server-side access control
(git access is the access model), and cross-project referential integrity enforced at write time
(references to other projects are soft, see [§12.4](#124-cross-project-references)).

---

## 2. Location and folder layout

The user marks exactly one folder of the project repository as the **documentation folder**
(commonly `docs/`, but any path is valid, including the repository root). That folder is the
project's knowledge base (KB). The backlog lives in a `.pmngr/` subfolder of it.

```
<repo-root>/
  docs/                              # documentation folder (KB root), configurable
    index.md                         # KB pages: free Markdown, any structure
    architecture/
      overview.md
      adr/0001-use-git-as-storage.md
    .pmngr/                          # backlog root  <-- specified by this document
      project.yaml                   # project configuration (the only non-Markdown item)
      epics/
        ACME-EP-0001-single-sign-on.md
        ACME-EP-0002-billing-v2.md
      stories/
        ACME-US-0042-login-with-sso.md
        ACME-US-0043-logout-everywhere.md
      tasks/
        ACME-T-0107-add-oidc-discovery-client.md
        ACME-T-0108-wire-callback-route.md
      milestones/
        ACME-M-0003-public-beta.md
      comments/
        ACME-US-0042/
          20260901T104512Z-jose.md
          20260901T111003Z-marta.md
      attachments/
        ACME-US-0042/
          sso-sequence.png
          vendor-quote.pdf
      index.json                     # OPTIONAL derived cache, git-ignored by default
```

Rules:

- **R-LOC-1** `.pmngr/` MUST be a direct child of the documentation folder. A repository MUST NOT
  contain two `.pmngr/` folders that are both configured as project backlogs (a repo may host a
  monorepo with several docs folders, but each is a separate *project* with a distinct key; see
  [§3.5](#35-multiple-projects-in-one-repository)).
- **R-LOC-2** `project.yaml` MUST exist for the folder to be recognised as a project backlog. Its
  presence is the discovery marker used by `gintrack` and by the web app's folder picker.
- **R-LOC-3** The five item folders (`epics/`, `stories/`, `tasks/`, `milestones/`, `comments/`) are
  created lazily. A missing folder is equivalent to an empty one and MUST NOT be an error.
- **R-LOC-4** Item folders MUST be flat except `comments/`, which has exactly one level of
  subfolders keyed by item ID. Nested subfolders under `epics/`, `stories/`, `tasks/`, or
  `milestones/` are ignored by the indexer and reported by `gintrack doctor` as `W-LAYOUT-NESTED`.
- **R-LOC-5** `index.json` is derived. The default `.gitignore` snippet emitted by `gintrack init`
  ignores it inside project repositories. (In the *team* repository the equivalent snapshots under
  `.pmngr/index/` ARE committed — that is the one deliberate exception, specified in doc 04.)
- **R-LOC-6** Any file under `.pmngr/` that is not `project.yaml`, not under `attachments/`, and
  does not end in `.md` is ignored with warning `W-LAYOUT-STRAY`.

---

## 3. Common conventions

### 3.1 File format

Every item is a UTF-8 Markdown file that begins with a YAML front-matter block delimited by `---`:

```markdown
---
id: ACME-US-0042
type: story
title: Login with SSO
status: in_progress
---

## Description

Body in Markdown.
```

- **R-FMT-1** The file MUST begin with the exact bytes `---\n` (no BOM, no leading blank line).
  A UTF-8 BOM, if present, MUST be stripped by the parser and MUST be rewritten without the BOM.
- **R-FMT-2** The front matter MUST be terminated by a line containing exactly `---`. The remainder
  of the file is the **body**.
- **R-FMT-3** Front matter MUST be a YAML mapping (YAML 1.2 core schema, as implemented by
  `gopkg.in/yaml.v3`). Anchors, aliases, and multiple documents are rejected (`E-FM-YAML`).
- **R-FMT-4** Line endings are LF on write. CRLF on read is accepted and normalised.
- **R-FMT-5** Files are written with a single trailing newline and no trailing whitespace on any
  line. This makes `rev` stable across editors.
- **R-FMT-6** When the tool rewrites a file it MUST preserve the body byte-for-byte unless the body
  itself was edited, and it MUST re-emit front matter in the canonical key order given in
  [§3.2](#32-canonical-key-order). Unknown keys are preserved (see `x-` rules in [§13.2](#132-custom-fields)).

### 3.2 Canonical key order

Writers MUST emit known keys in this order; unknown/custom keys follow, sorted lexicographically.
Keys whose value is null/empty MUST be omitted rather than written as `null` or `[]`.

```
id, type, title, status, priority, parent, epic, milestone, sprint,
assignees, author, labels, estimate, effort, spent,
created, updated, started, closed, due,
links, blocks, depends_on, attachments, custom, deleted
```

Rationale: identity first, then classification, then people, then numbers, then dates, then
relations. Diffs of unrelated changes touch different regions of the block.

### 3.3 Identifiers

```
<ID> ::= <KEY> "-" <TYPECODE> "-" <NUMBER>

<KEY>      ::= [A-Z][A-Z0-9]{1,9}          # from project.yaml `key`
<TYPECODE> ::= "EP" | "US" | "T" | "M"
<NUMBER>   ::= [0-9]{4,}                    # zero-padded to at least 4 digits
```

Examples: `ACME-EP-0001`, `ACME-US-0042`, `ACME-T-0107`, `ACME-M-0003`, and after 9999 items
`ACME-T-10234` (five digits — padding is a *minimum*, not a maximum).

- **R-ID-1** IDs are case-sensitive and MUST be uppercase for `KEY` and `TYPECODE`.
- **R-ID-2** An ID is immutable for the life of the item, except through `gintrack doctor --renumber`
  ([§4.4](#44-renumbering)), which leaves a redirect record.
- **R-ID-3** The numeric part carries no meaning beyond ordering of creation. Nothing may assume
  contiguity: gaps are normal (deleted items, renumbering, aborted creations).
- **R-ID-4** Comments have no ID of their own; they are addressed as
  `<ITEM-ID>#<comment-file-stem>` (see [§11](#11-comments)).

### 3.4 File naming and slugs

```
<filename> ::= <ID> "-" <slug> ".md"
```

The slug is derived from the title:

1. Unicode NFKD normalise, strip combining marks (`é` → `e`, `ñ` → `n`).
2. Lowercase (Unicode simple case folding).
3. Replace every run of characters outside `[a-z0-9]` with a single `-`.
4. Trim leading/trailing `-`; collapse repeated `-`.
5. Truncate to 60 bytes on a `-` boundary; if the result is empty, use `item`.

Examples:

| Title | Slug |
|---|---|
| `Login with SSO` | `login-with-sso` |
| `Añadir métricas de latencia (p95)` | `anadir-metricas-de-latencia-p95` |
| `Fix: 500 on /api/v2/users?filter=…` | `fix-500-on-api-v2-users-filter` |
| `🚀` | `item` |

- **R-SLUG-1** The slug is cosmetic. Lookup is always by the `id` front-matter field, then by the ID
  prefix of the filename. A mismatch between filename slug and title is warning `W-SLUG-STALE`,
  never an error.
- **R-SLUG-2** Renaming an item title SHOULD rename the file (`git mv`) so browsing stays pleasant;
  the CLI does this by default and the web app offers it as a checkbox. Tools MUST tolerate a
  refusal to rename.
- **R-SLUG-3** The ID prefix of the filename MUST match the `id` field. Mismatch is error
  `E-ID-FILENAME`.
- **R-SLUG-4** Two files with the same `id` anywhere under `.pmngr/` is error `E-ID-DUPLICATE`
  (this is the post-merge collision case, see [§4.3](#43-collisions-after-a-merge)).

### 3.5 Multiple projects in one repository

A monorepo MAY host several projects: `apps/web/docs/.pmngr/` with `key: WEB` and
`apps/api/docs/.pmngr/` with `key: API`. Each has its own `project.yaml` and its own ID space. The
team repository lists them as separate project entries that happen to share `repo` and differ in
`docs_path` (doc 04, §3.3).

### 3.6 Dates and times

- **R-TIME-1** All timestamps are ISO 8601 in **UTC** with a `Z` suffix and second precision:
  `2026-09-01T10:45:12Z`. No local offsets, no fractional seconds. Rationale: stable sorting,
  stable diffs, no timezone politics in a shared repo.
- **R-TIME-2** Date-only fields (`due`, `start`, `end` in sprints/milestones) use `YYYY-MM-DD` and
  are interpreted as the whole day in the project's `timezone` (`project.yaml`, default `UTC`) for
  presentation and burndown bucketing only.
- **R-TIME-3** `created` MUST NOT change after creation. `updated` is set on every write that
  changes front matter or body; it MUST NOT be updated by tools that merely re-format or re-index.
- **R-TIME-4** YAML would otherwise parse unquoted timestamps into its own timestamp type; writers
  MUST emit them unquoted in the canonical ISO form and readers MUST accept both the string and the
  YAML-timestamp forms, normalising to the string form on the next write.

### 3.7 People

A person is referenced by **handle**: a short, stable, lowercase token (`jose`, `marta`, `bot-ci`).
Handles are declared in the team repository's `team.yaml` (doc 04, §3.2) and MAY be mirrored in
`project.yaml` under `people:` for projects used standalone.

- **R-PEOPLE-1** `author` is a single handle. `assignees` is a list of handles (0..n; the model
  supports multiple assignees, the default board UI shows the first plus a counter).
- **R-PEOPLE-2** An unknown handle is warning `W-PERSON-UNKNOWN`, never an error: a project repo may
  be read without its team repo present.
- **R-PEOPLE-3** Resolution order for a handle: `project.yaml:people` → `team.yaml:members` → the
  handle rendered literally.
- **R-PEOPLE-4** Email addresses MUST NOT be used as handles in item front matter. Emails live in
  `team.yaml` for git-identity mapping.

### 3.8 Enumerations

| Field | Allowed values |
|---|---|
| `type` | `epic`, `story`, `task`, `milestone`, `comment` (board/sprint/retro types exist only in the team repo) |
| `priority` | `critical`, `high`, `medium`, `low` |
| `status` | any `id` declared in `project.yaml:workflow.statuses` |
| relation kind | `blocks`, `blocked_by`, `relates_to`, `duplicates`, `duplicated_by` |
| `estimate` | number (story points), see [§8.3](#83-estimates-and-effort) |

---

## 4. ID allocation and collision handling

This is the single most important operational decision in the data model, because two people who
are offline can both create "the next story".

### 4.1 The allocation algorithm

**Counters in `project.yaml` are NOT authoritative.** They are a hint that lets a client allocate
without a full scan when it already trusts its index.

Allocation of a new ID for type `T`:

1. Build/refresh the index of `.pmngr/` (front matter only — cheap, see [§17](#17-agent-optimized-reading)).
2. `max_seen = max(numeric part of every existing ID of type T)`, including items marked
   `deleted: true` and including IDs found only in `project.yaml:id_allocation.reserved`.
3. `hint = project.yaml:id_allocation.counters[T]` (0 if absent).
4. `next = max(max_seen, hint) + 1`.
5. Write the new file. In the *same* commit, optionally bump
   `id_allocation.counters[T] = next` (enabled by `id_allocation.write_counters`, default `true`).

Consequences:

- If the counter is stale, wrong, or deleted, allocation still works — the scan wins.
- If someone hand-creates `ACME-T-0500` in an otherwise 100-task project, the next allocation is
  `0501`. Gaps are cheap; reuse is forbidden.
- A client with a warm index allocates in O(1); a cold client pays one directory scan.

### 4.2 Why not a lock, a UUID, or a central service?

| Option | Why rejected |
|---|---|
| Central ID service | Contradicts "git is the only sync mechanism". Requires a server, availability, auth. |
| UUID / ULID IDs | Unspeakable. Humans must be able to say "ACME-US-42" in standup and type it in a commit message. |
| Lock file in git | Locks need a serialisation point; git has none. A lock would be committed and pushed, i.e. a race in itself. |
| Per-user ID ranges (`jose: 1000-1999`) | Works, but leaks org structure into IDs, wastes ranges, and breaks when someone joins. Available as an opt-in (`id_allocation.strategy: ranges`) for very large teams. |
| Hash-of-title IDs | Not sequential, not memorable, changes on retitle. |

The accepted trade-off is: **collisions are possible but rare, cheap to detect, and mechanically
repairable.** In exchange, IDs stay short, sequential, human-speakable, and require no
infrastructure.

### 4.3 Collisions after a merge

Two people branch from the same commit, each creates a story, each gets `ACME-US-0043`. On merge,
git reports **no conflict** (different filenames: `...-0043-login-with-sso.md` and
`...-0043-reset-password.md`) and both files land in the tree. This is the failure mode the model
must handle.

Detection:

- The indexer raises `E-ID-DUPLICATE` listing every path that claims the ID.
- `gintrack doctor` exits non-zero. CI SHOULD run `gintrack doctor --strict` on pull requests so a
  duplicate is caught before it reaches the default branch (see doc on CI; Phase 0 deliverable).
- The web app shows a repository-level banner and refuses to open either item for editing until
  resolved, to avoid writing into an ambiguous ID.

Repair — `gintrack doctor --renumber`:

```
$ gintrack doctor --renumber --docs docs
git-in-track doctor — project ACME (docs/.pmngr)

E-ID-DUPLICATE  ACME-US-0043 claimed by 2 files
  keep    stories/ACME-US-0043-login-with-sso.md      created 2026-09-01T09:12:00Z  (older)
  renum   stories/ACME-US-0043-reset-password.md      created 2026-09-01T09:41:33Z  ->  ACME-US-0044

Rewrites:
  stories/ACME-US-0043-reset-password.md  ->  stories/ACME-US-0044-reset-password.md
  front matter id: ACME-US-0043 -> ACME-US-0044
  1 inbound reference updated:
    tasks/ACME-T-0107-add-oidc-discovery-client.md   parent: ACME-US-0043 -> ACME-US-0044
  2 comment folders moved:
    comments/ACME-US-0043/ -> comments/ACME-US-0044/   (2 files, item: field rewritten)
  redirect recorded in .pmngr/project.yaml: id_allocation.redirects

Apply? [y/N]
```

Tie-break rule for "who keeps the ID" (`R-RENUM-1`): the item with the earlier `created` wins; on a
tie, the lexicographically smaller file path wins. The rule must be deterministic so that two people
running `doctor` independently produce the same result and their fixes merge cleanly.

Redirects (`R-RENUM-2`): `project.yaml:id_allocation.redirects` maps old ID → new ID forever (or
until pruned by hand). The index resolves redirects so that stale links in KB pages, commit
messages, comments, and team-repo boards keep working, rendering as
`ACME-US-0044 (was ACME-US-0043)`.

Out-of-repo references (`R-RENUM-3`) cannot be rewritten — commit messages, chat, PR titles, an
external tracker. This is the residual cost of the strategy and is why renumbering must be rare;
the redirect table exists precisely to soften it.

### 4.4 Renumbering

`gintrack doctor --renumber` also handles two other cases:

- `--renumber --compact` (explicitly opt-in, discouraged): closes gaps. Rewrites history-visible IDs
  en masse. Intended only for a project that has never been shared.
- `--renumber --rekey NEW` : changes the project key (`ACME` → `ACME2`) across every file, comment
  folder, attachment folder, and index. Requires the team repo to be updated in the same change set;
  `doctor` prints the exact `team.yaml` edit needed.

Both operations produce one commit and MUST NOT be mixed with content edits.

### 4.5 Reducing the collision window in practice

- The web app and CLI re-index (`git fetch` + scan when a remote is configured) immediately before
  allocating, if the operation is online. Cost: one fetch; benefit: near-zero collisions for teams
  that stay roughly in sync.
- `id_allocation.reserved` lets a person pre-allocate a block while working offline for a long time:
  `reserved: {task: [200, 249]}` means "nobody else takes 200–249", enforced only by convention and
  by other clients' scan step (reserved ranges participate in `max_seen`).
- `id_allocation.ranges` assigns a permanent block per person, keyed by handle and then by item
  type (`jose: {task: [[1000, 1999]]}`). It is read only when `strategy: ranges`: the allocator then
  takes the first free number inside the acting user's block and fails with "id range exhausted"
  when the block is full. Under either strategy, a block that belongs to somebody else participates
  in `max_seen` and is never allocated from.
- Agents (MCP) are instructed to create items one at a time and re-read the index between creations
  (doc 05, agent conventions).

---

## 5. The `rev` content hash (optimistic concurrency)

`rev` is **never stored in the file**. It is computed by readers and returned by every API and MCP
tool, and required on every write that intends to update an existing item.

Definition (`R-REV-1`):

```
canonical_bytes = file bytes
                  with UTF-8 BOM removed
                  with CRLF -> LF
                  with exactly one trailing LF
rev = "sha256:" + lowercase_hex(sha256(canonical_bytes))[0:16]
```

Example: `rev: "sha256:9f2b1c7d0a4e5b31"`.

- **R-REV-2** 64 bits of hash is sufficient: the population is "versions of one file that two clients
  hold at the same moment", not an adversarial corpus.
- **R-REV-3** Writes are conditional. `PUT /api/items/ACME-US-0042` with `If-Match: sha256:9f2b…`
  (or MCP `update_story{expected_rev: …}`) MUST fail with `409 rev_mismatch` if the on-disk `rev`
  differs. The error payload includes the current `rev` and the current front matter so a client or
  agent can retry a merge without a second round trip.
- **R-REV-4** `rev` is *not* a version number and MUST NOT be persisted, compared for ordering, or
  used as a cache key across machines beyond its purpose (it is a pure function of content, so it is
  in fact a perfectly good cross-machine cache key — but nothing may assume monotonicity).
- **R-REV-5** Relationship to git: for a file staged unmodified, git's blob OID would serve the same
  purpose, but working-tree files are frequently dirty and `gintrack` must work on an uncommitted
  tree. `rev` is therefore computed from the working tree, independently of git.
- **R-REV-6** `rev` covers the whole file, front matter and body. A body-only edit changes `rev`.
  Comment files have their own `rev`; the parent item's `rev` does not change when a comment is
  added.

---

## 6. `project.yaml`

The only non-Markdown file in `.pmngr/`. Plain YAML, no front matter.

### 6.1 Fields

| Key | Type | Req. | Default | Notes |
|---|---|---|---|---|
| `schema` | integer | yes | `1` | Data-model version. Unknown/higher → refuse to write, allow read-only. |
| `key` | string `[A-Z][A-Z0-9]{1,9}` | yes | — | ID prefix. Immutable in practice (see `--rekey`). |
| `name` | string | yes | — | Human name, e.g. `ACME Platform`. |
| `description` | string | no | — | One paragraph; shown in project pickers. |
| `timezone` | IANA tz | no | `UTC` | Presentation of date-only fields only. |
| `docs` | mapping | no | see below | KB rendering settings. |
| `workflow` | mapping | yes | see below | Status definitions and transitions. |
| `id_allocation` | mapping | no | see below | Counters, strategy, reserved ranges, redirects. |
| `labels` | list of mappings | no | `[]` | Label catalog. |
| `priorities` | list of strings | no | the four defaults | Reordering allowed, renaming not. |
| `estimation` | mapping | no | `{scale: fibonacci}` | Story-point scale and hour tracking. |
| `defaults` | mapping | no | `{}` | Default assignees, priority, status, labels per type. |
| `custom_fields` | list of mappings | no | `[]` | Declared extra front-matter fields ([§13.2](#132-custom-fields)). |
| `people` | list of mappings | no | `[]` | Optional local mirror of team members. |
| `team` | mapping | no | — | Back-pointer to the team repo (`repo`, `key`). |
| `links` | mapping | no | — | Host info for building blob URLs (`host: github\|gitlab\|gitea\|bitbucket`, `web_url`). |

`docs` sub-keys: `path` (relative to repo root, informational — the real path is where the file
was found), `wikilinks` (bool, default `true`), `mermaid` (bool, default `true`), `math` (bool,
default `false`), `footnotes` (bool, default `true`), `callouts` (bool, default `true`),
`attachments_dir` (default `.pmngr/attachments`).

`workflow` sub-keys:

- `statuses`: ordered list of `{id, name, category, wip?, color?, terminal?}`.
  - `id`: `[a-z][a-z0-9_]{0,31}`, unique.
  - `category`: `todo | in_progress | done | cancelled` — the *coarse* bucket used by boards,
    metrics, and agents that do not know a project's custom workflow. This field is what makes
    heterogeneous projects comparable on a team board.
  - `terminal`: bool; items in a terminal status are excluded from "open work" queries.
- `initial`: status id used when creating an item (default: first status).
- `transitions`: optional mapping `from → [to…]`. Absent or `null` means "any transition allowed".
  Violations are **warnings** (`W-WORKFLOW-TRANSITION`) in files, and **errors** in the API/MCP layer
  unless `--force`. Rationale: a git repo may receive a file from anywhere; refusing to parse it
  would be worse than flagging it.

### 6.2 Complete example

```yaml
# docs/.pmngr/project.yaml
schema: 1
key: ACME
name: ACME Platform
description: Customer-facing platform: web app, public API, billing.
timezone: Europe/Madrid

docs:
  path: docs
  wikilinks: true
  mermaid: true
  math: false
  footnotes: true
  callouts: true
  attachments_dir: .pmngr/attachments

workflow:
  initial: backlog
  statuses:
    - { id: backlog,     name: Backlog,     category: todo }
    - { id: todo,        name: To Do,       category: todo }
    - { id: in_progress, name: In Progress, category: in_progress, wip: 3 }
    - { id: in_review,   name: In Review,   category: in_progress, wip: 4 }
    - { id: done,        name: Done,        category: done,      terminal: true }
    - { id: cancelled,   name: Cancelled,   category: cancelled, terminal: true }
  transitions:
    backlog:     [todo, cancelled]
    todo:        [in_progress, backlog, cancelled]
    in_progress: [in_review, todo, cancelled]
    in_review:   [done, in_progress, cancelled]
    done:        [in_progress]
    cancelled:   [backlog]

id_allocation:
  strategy: scan          # scan | ranges
  write_counters: true
  counters:               # hints only, NOT authoritative
    epic: 2
    story: 43
    task: 108
    milestone: 3
  reserved:               # optional offline pre-allocation
    task: [[200, 249]]
  ranges:                 # only read when strategy is `ranges` (section 4.2)
    jose:  { task: [[1000, 1999]] }
    marta: { task: [[2000, 2999]] }
  redirects:              # written by `gintrack doctor --renumber`
    ACME-US-0043: ACME-US-0044

priorities: [critical, high, medium, low]

estimation:
  scale: fibonacci        # fibonacci | linear | tshirt | none
  values: [1, 2, 3, 5, 8, 13, 21]
  track_hours: true       # enables `effort` and `spent`

labels:
  - { name: backend,      color: "#2563eb", description: Server-side work }
  - { name: frontend,     color: "#7c3aed", description: Web app work }
  - { name: security,     color: "#dc2626" }
  - { name: tech-debt,    color: "#a16207" }
  - { name: needs-design, color: "#0891b2", description: Blocked on design input }

defaults:
  story:
    status: backlog
    priority: medium
    assignees: [jose]
    labels: [frontend]
  task:
    status: todo
    priority: medium
  epic:
    status: backlog

custom_fields:
  - { key: risk,        type: enum,   values: [low, medium, high], applies_to: [epic, story] }
  - { key: customer,    type: string, applies_to: [story] }
  - { key: compliance,  type: bool,   applies_to: [story, task], default: false }

people:
  - { handle: jose,  name: Jose Ruiz,     email: jose@digio.es }
  - { handle: marta, name: Marta Alonso,  email: marta@example.com }
  - { handle: bot-ci, name: CI Bot,       email: ci@example.com, kind: bot }

team:
  repo: https://github.com/acme/acme-team.git
  key: ACME-TEAM

links:
  host: github
  web_url: https://github.com/acme/platform
```

### 6.3 Validation rules for `project.yaml`

- `E-PROJ-MISSING` — `.pmngr/` exists but `project.yaml` does not.
- `E-PROJ-KEY` — `key` absent or not matching `[A-Z][A-Z0-9]{1,9}`.
- `E-PROJ-SCHEMA` — `schema` missing, or greater than the supported version (read-only fallback).
- `E-PROJ-STATUS-DUP` — duplicate status `id`.
- `E-PROJ-STATUS-CATEGORY` — a status has an unknown `category`.
- `E-PROJ-INITIAL` — `workflow.initial` names a status that does not exist.
- `E-PROJ-TRANSITION-TARGET` — a transition names an unknown status.
- `W-PROJ-NO-DONE` — no status has category `done`; metrics will be meaningless.
- `W-PROJ-LABEL-DUP` — duplicate label name (case-insensitive).
- `W-PROJ-COUNTER-STALE` — a counter is lower than the maximum scanned ID (informational; the scan
  wins and the counter is rewritten on the next allocation).

---

## 7. Epics

**Path:** `.pmngr/epics/<KEY>-EP-<NNNN>-<slug>.md`

An epic is a container for stories. It has no independent workload; its progress is derived from
its children.

### 7.1 Front matter

| Field | Type | Req. | Notes |
|---|---|---|---|
| `id` | ID (`EP`) | yes | |
| `type` | `epic` | yes | |
| `title` | string (1..200) | yes | |
| `status` | status id | yes | |
| `priority` | enum | no | default from `defaults.epic.priority` |
| `milestone` | milestone ID | no | |
| `assignees` | list of handles | no | epic owner(s) |
| `author` | handle | yes on create | |
| `labels` | list of label names | no | |
| `estimate` | number | no | rolled up from stories if absent |
| `created` | timestamp | yes | |
| `updated` | timestamp | yes | |
| `started` / `closed` | timestamp | no | set when leaving/entering a terminal category |
| `due` | date | no | |
| `links` | list of relations | no | [§12](#12-links-and-relations) |
| `attachments` | list of strings | no | filenames under `attachments/<ID>/` |
| `custom` | mapping | no | declared custom fields |
| `deleted` | bool | no | soft delete, default `false` |

An epic MUST NOT have `parent`. Stories point *up* to their epic; epics do not list their children
(that would duplicate state and create merge conflicts on every story creation).

### 7.2 Body conventions

```
## Description        (required by convention, not by the validator)
## Goals              (optional, bullet list of outcomes)
## Out of Scope       (optional)
## Notes              (optional, free)
```

### 7.3 Complete example

```markdown
---
id: ACME-EP-0001
type: epic
title: Single Sign-On
status: in_progress
priority: high
milestone: ACME-M-0003
assignees: [jose]
author: jose
labels: [security, backend]
created: 2026-07-14T08:02:11Z
updated: 2026-09-01T10:12:44Z
started: 2026-08-04T07:31:00Z
due: 2026-10-31
links:
  - { kind: relates_to, target: ACME-EP-0002, note: shares the tenant model }
custom:
  risk: high
---

## Description

Let ACME customers authenticate with their corporate identity provider instead of
ACME-local passwords. Covers OIDC and SAML 2.0 for the web app; the public API keeps
API keys and is explicitly out of scope.

See the design notes in [[architecture/sso-overview]] and ADR [[adr/0007-oidc-over-saml]].

## Goals

- Enterprise tenants can enable SSO without contacting support.
- Session revocation propagates within 60 seconds.
- No password material stored for SSO-only tenants.

## Out of Scope

- SCIM user provisioning (tracked separately in [[ACME-EP-0002]]).
- Machine-to-machine auth for the public API.

## Notes

Vendor evaluation summary is attached: `sso-vendor-comparison.pdf`.
```

---

## 8. User stories

**Path:** `.pmngr/stories/<KEY>-US-<NNNN>-<slug>.md`

### 8.1 Front matter

Everything an epic has, plus:

| Field | Type | Req. | Notes |
|---|---|---|---|
| `type` | `story` | yes | |
| `parent` | epic ID | no | the owning epic; `null` means an orphan story (valid) |
| `milestone` | milestone ID | no | overrides the epic's milestone for planning |
| `sprint` | sprint ID | no | `<TEAMKEY>-S-<NNNN>`, resolved in the team repo; soft reference |
| `estimate` | number | no | story points; MUST be a member of `estimation.values` when the scale is `fibonacci` or `linear` |
| `effort` | number | no | planned hours (requires `estimation.track_hours`) |
| `spent` | number | no | consumed hours |

- **R-STORY-1** `parent` MUST reference an existing epic of the same project, or be absent.
  A dangling parent is `W-REF-DANGLING` (warning: the epic may arrive in a later merge).
- **R-STORY-2** A story MUST NOT be its own ancestor; cycles across `parent` are `E-REF-CYCLE`.
- **R-STORY-3** Acceptance criteria are expressed as GFM task-list items under
  `## Acceptance Criteria`. The indexer parses them into `{text, done}` pairs and exposes
  `ac_total` / `ac_done` counters. This is the only body content the indexer interprets structurally.

### 8.2 Body conventions

```
## Description            free text; the "As a … I want … so that …" form is recommended, not required
## Acceptance Criteria    GFM task list; each item is one verifiable condition
## Technical Notes        optional
## Notes                  optional
```

### 8.3 Estimates and effort

| Concept | Field | Unit | Rolls up to |
|---|---|---|---|
| Story points | `estimate` | scale-dependent | epic, milestone, sprint |
| Planned hours | `effort` | hours (decimal) | story (from its tasks), then epic |
| Consumed hours | `spent` | hours (decimal) | same |

Roll-up rule (`R-EST-1`): a container's own explicit value wins; if absent, the sum of children's
values is used and the UI marks it as *derived*. Never write a derived value into the file — that
would turn every child edit into a parent edit and a merge conflict magnet.

### 8.4 Complete example

```markdown
---
id: ACME-US-0042
type: story
title: Login with SSO
status: in_progress
priority: high
parent: ACME-EP-0001
milestone: ACME-M-0003
sprint: ACME-TEAM-S-0007
assignees: [marta, jose]
author: jose
labels: [frontend, security]
estimate: 8
effort: 20
spent: 11.5
created: 2026-08-19T09:04:02Z
updated: 2026-09-01T10:45:12Z
started: 2026-08-28T08:10:00Z
due: 2026-09-15
links:
  - { kind: blocked_by, target: ACME-T-0107 }
  - { kind: relates_to, target: ACME-US-0043 }
attachments: [sso-sequence.png]
custom:
  risk: medium
  customer: northwind
  compliance: true
---

## Description

As an employee of a tenant with SSO enabled,
I want to sign in with my corporate identity provider,
so that I do not need a separate ACME password.

Sequence diagram: `![SSO sequence](../attachments/ACME-US-0042/sso-sequence.png)`.
Protocol decision: [[adr/0007-oidc-over-saml]].

## Acceptance Criteria

- [x] The login page shows a "Sign in with your company account" button when the tenant has SSO enabled.
- [x] The OIDC authorization-code flow with PKCE completes and creates a session cookie.
- [ ] A user whose IdP account is disabled cannot obtain a session (verified against the staging IdP).
- [ ] `email` and `name` claims populate the ACME profile on first login.
- [ ] Failure states render an actionable message and are logged with a correlation id.

## Technical Notes

Discovery document is cached for 10 minutes; see [[ACME-T-0107]] for the client.
Nonce and state are stored in a signed, `SameSite=Lax`, 10-minute cookie.

## Notes

Northwind is the pilot tenant; their IdP is Entra ID.
```

---

## 9. Tasks

**Path:** `.pmngr/tasks/<KEY>-T-<NNNN>-<slug>.md`

A task is the unit of execution: something one person can finish in a day or two.

### 9.1 Front matter

Same as a story, with:

| Field | Type | Req. | Notes |
|---|---|---|---|
| `type` | `task` | yes | |
| `parent` | story ID (or epic ID) | no | a task MAY hang directly from an epic or from nothing |
| `estimate` | number | no | discouraged for tasks; prefer `effort` in hours |
| `effort` / `spent` | number | no | hours |

- **R-TASK-1** `parent` MUST be a story or an epic of the same project. Task→task nesting is not
  supported (`E-REF-PARENT-TYPE`); use `links: depends_on` instead.
- **R-TASK-2** A task in a `done` category whose parent story is not done is perfectly normal and
  MUST NOT be flagged.

### 9.2 Complete example

```markdown
---
id: ACME-T-0107
type: task
title: Add OIDC discovery client
status: in_review
priority: high
parent: ACME-US-0042
assignees: [jose]
author: marta
labels: [backend, security]
effort: 6
spent: 5
created: 2026-08-27T11:20:40Z
updated: 2026-09-02T16:03:19Z
started: 2026-08-29T07:55:00Z
links:
  - { kind: blocks, target: ACME-US-0042 }
custom:
  compliance: true
---

## Description

Implement `internal/auth/oidc.Discover(issuer)` returning the parsed
`.well-known/openid-configuration` document with a 10-minute TTL cache and a
bounded retry (3 attempts, exponential backoff, 2s cap).

## Acceptance Criteria

- [x] Discovery document parsed; unknown fields ignored.
- [x] JWKS fetched lazily and cached, with `kid` based rotation.
- [ ] Unit tests cover: happy path, HTTP 500, malformed JSON, expired cache.
- [ ] `go test ./internal/auth/...` passes with `-race`.

## Notes

PR: https://github.com/acme/platform/pull/812
```

---

## 10. Milestones

**Path:** `.pmngr/milestones/<KEY>-M-<NNNN>-<slug>.md`

A milestone is a dated target ("Public Beta", "GDPR audit"). Unlike a sprint (which lives in the
team repo and is a team-level time box), a milestone is project-scoped and lives with the backlog.

### 10.1 Front matter

| Field | Type | Req. | Notes |
|---|---|---|---|
| `id` | ID (`M`) | yes | |
| `type` | `milestone` | yes | |
| `title` | string | yes | |
| `status` | status id | yes | typically constrained to `todo`/`in_progress`/`done`/`cancelled` categories |
| `start` | date | no | |
| `due` | date | no | the target date |
| `closed` | timestamp | no | when it was actually reached |
| `owner` | handle | no | single accountable person |
| `labels`, `author`, `created`, `updated`, `links`, `attachments`, `custom`, `deleted` | as elsewhere | | |

Membership is expressed by the items (`milestone: ACME-M-0003`), never by a list inside the
milestone. Same anti-conflict rationale as epics.

### 10.2 Complete example

```markdown
---
id: ACME-M-0003
type: milestone
title: Public Beta
status: in_progress
start: 2026-09-01
due: 2026-11-15
owner: jose
author: jose
labels: [release]
created: 2026-06-30T10:00:00Z
updated: 2026-09-01T08:00:00Z
links:
  - { kind: relates_to, target: ACME-EP-0001 }
---

## Description

First release open to non-invited customers. Feature-complete SSO, billing v2 in
read-only mode, and a documented public API.

## Exit Criteria

- [ ] SSO available to all tenants ([[ACME-EP-0001]]).
- [ ] p95 login latency under 800 ms in staging under 200 rps.
- [ ] Runbook published: [[operations/runbook-beta]].
- [ ] Zero open `critical` defects.

## Notes

Marketing freeze starts two weeks before the due date.
```

---

## 11. Comments

**Path:** `.pmngr/comments/<ITEM-ID>/<TIMESTAMP>-<author>.md`

One file per comment. This is the single most important merge-friendliness decision after
one-file-per-item: appending to a shared comments file would conflict on every concurrent reply,
while a new file per comment never conflicts.

### 11.1 Naming

```
<TIMESTAMP> ::= YYYYMMDD "T" HHMMSS "Z"      # UTC, compact ISO 8601 basic format
<author>    ::= handle (lowercase, [a-z0-9-]+)
```

Example: `.pmngr/comments/ACME-US-0042/20260901T104512Z-jose.md`

- **R-CMT-1** If a file with that exact name exists (same author, same second), append `-2`, `-3`, …
  before `.md`.
- **R-CMT-2** The folder name MUST be a valid item ID of this project. A comments folder for an ID
  that does not exist is `W-CMT-ORPHAN` (the item may be deleted or arriving later).
- **R-CMT-3** Comment files sort chronologically by filename. Readers MUST sort by the `created`
  field and use the filename only as a tie-break.

### 11.2 Front matter

| Field | Type | Req. | Notes |
|---|---|---|---|
| `type` | `comment` | yes | |
| `item` | item ID | yes | MUST equal the folder name |
| `author` | handle | yes | |
| `created` | timestamp | yes | MUST match the filename timestamp |
| `updated` | timestamp | no | present only if edited |
| `in_reply_to` | comment ref | no | `<ITEM-ID>#<file-stem>` |
| `kind` | `comment` \| `status_change` \| `system` | no | default `comment` |
| `reactions` | mapping emoji → list of handles | no | |
| `attachments` | list of strings | no | resolved under `attachments/<ITEM-ID>/` |

`kind: system` marks machine-written entries (e.g. an agent recording an automated check). Systems
SHOULD write few, high-value system comments; the git log is the audit trail, not the comment stream.

### 11.3 Complete example

```markdown
---
type: comment
item: ACME-US-0042
author: marta
created: 2026-09-01T10:45:12Z
in_reply_to: ACME-US-0042#20260901T093300Z-jose
reactions:
  "+1": [jose]
attachments: [idp-error.png]
---

Entra ID returns `AADSTS50011` when the redirect URI has a trailing slash. I normalised
the registered URI in staging; we should assert on it at startup so it fails loudly
instead of at the first login attempt.

Follow-up task: [[ACME-T-0108]].
```

---

## 12. Links and relations

### 12.1 The `links` field

```yaml
links:
  - { kind: blocks,       target: ACME-US-0042 }
  - { kind: blocked_by,   target: ACME-T-0107, note: waiting for the discovery client }
  - { kind: relates_to,   target: WEB-US-0031 }        # cross-project, soft
  - { kind: duplicates,   target: ACME-US-0009 }
```

| Kind | Inverse | Semantics |
|---|---|---|
| `blocks` | `blocked_by` | target cannot progress until source is done |
| `blocked_by` | `blocks` | |
| `relates_to` | `relates_to` | symmetric, no scheduling meaning |
| `duplicates` | `duplicated_by` | source SHOULD be closed as `cancelled` |
| `duplicated_by` | `duplicates` | |

- **R-LINK-1** Links are stored on **one side only** by whoever creates them. The indexer computes
  the inverse in memory. Writing both sides is allowed but produces redundant, conflict-prone edits;
  the tool does not do it.
- **R-LINK-2** `target` is an item ID, optionally qualified for another project as
  `<PROJECTKEY>/<ITEM-ID>` (e.g. `WEB/WEB-US-0031`). The bare form implies the current project.
- **R-LINK-3** A dangling target is `W-REF-DANGLING`, never an error.
- **R-LINK-4** `blocks`/`blocked_by` cycles are `W-REF-CYCLE-BLOCK` (a warning, because a legitimate
  mutual dependency sometimes exists mid-refactor). `parent` cycles are errors.
- **R-LINK-5** `parent` and `milestone` are *not* links; they are dedicated fields because they are
  hierarchical and are indexed differently.

### 12.2 Convenience aliases

`blocks: [ACME-US-0042]` and `depends_on: [ACME-T-0107]` are accepted shorthands on read and are
normalised into `links` entries (`blocks`, `blocked_by` respectively) on the next write. They exist
so a human can hand-write the common case quickly.

### 12.3 References from code and commits

The recommended commit-message convention (Phase 0 doc):

```
feat(auth): cache OIDC discovery document

Refs: ACME-T-0107
Closes: ACME-T-0106
```

`gintrack` does not parse git history in Phase 1–3. Phase 6 may add a "mentions" panel built from
`git log --grep`. Nothing in the data model depends on it.

### 12.4 Cross-project references

A project repo MAY reference another project's item (`WEB/WEB-US-0031`). Resolution requires the
team repo (to map `WEB` → repo/docs path) and, for a live title/status, either a local clone or the
committed snapshot `.pmngr/index/WEB.json` in the team repo (doc 04, §6). Without either, the
reference renders as inert text with the ID. This is by design: **backlogs never leave their project
repository**.

---

## 13. Labels, custom fields, defaults, attachments

### 13.1 Labels

- Declared in `project.yaml:labels`. Applied via `labels: [backend, security]`.
- **R-LBL-1** A label used on an item but absent from the catalog is `W-LABEL-UNDECLARED`, not an
  error; `gintrack doctor --fix-labels` appends undeclared labels to the catalog with a default
  colour.
- **R-LBL-2** Label names: `[a-z0-9][a-z0-9._-]{0,31}`, compared case-insensitively, stored
  lowercase.

### 13.2 Custom fields

Declared in `project.yaml:custom_fields`, stored under the `custom:` mapping in item front matter.

```yaml
custom_fields:
  - { key: risk,       type: enum,   values: [low, medium, high], applies_to: [epic, story] }
  - { key: customer,   type: string, applies_to: [story] }
  - { key: compliance, type: bool,   applies_to: [story, task], default: false }
  - { key: reviewers,  type: list,   items: person, applies_to: [story, task] }
  - { key: target_qps, type: number, applies_to: [story] }
  - { key: review_by,  type: date,   applies_to: [story] }
```

Types: `string`, `text` (multi-line), `number`, `bool`, `date`, `timestamp`, `enum`, `person`,
`list` (with `items`), `url`.

- **R-CF-1** Values live under `custom:` — never at the top level — so that adding a custom field can
  never collide with a future core field.
- **R-CF-2** An undeclared key under `custom:` is `W-CF-UNDECLARED` and is preserved on rewrite.
- **R-CF-3** A declared field with a wrong type is `E-CF-TYPE`.
- **R-CF-4** Top-level keys prefixed `x-` are reserved for third-party tools; `gintrack` preserves
  them verbatim and never validates them.

### 13.3 Defaults

`project.yaml:defaults.<type>` supplies `status`, `priority`, `assignees`, `labels` when an item is
created through the UI/CLI/MCP. Defaults are materialised into the file at creation time — they are
**not** applied at read time, so a file always says exactly what it means.

### 13.4 Attachments

- Path: `<docs>/.pmngr/attachments/<ITEM-ID>/<filename>`.
- Front matter `attachments: [sso-sequence.png]` lists *filenames*, resolved relative to that folder.
- **R-ATT-1** Filenames are sanitised to `[A-Za-z0-9._-]+`; spaces become `-`.
- **R-ATT-2** Large binaries are the user's problem (git LFS is out of scope); the UI warns above
  1 MiB and refuses above 10 MiB by default (`attachments_max_bytes` is not configurable in Phase 1).
- **R-ATT-3** An attachment file present on disk but absent from the front-matter list is
  `W-ATT-UNLISTED`; a listed file that does not exist is `W-ATT-MISSING`.
- **R-ATT-4** In the body, reference attachments with normal Markdown relative links so they render
  on GitHub too: `![SSO sequence](../attachments/ACME-US-0042/sso-sequence.png)` from a file in
  `.pmngr/stories/`.

---

## 14. KB ↔ backlog cross-referencing (wikilinks)

The documentation folder is an Obsidian-like vault. `[[…]]` wikilinks work in three directions.

### 14.1 Syntax

| Form | Meaning |
|---|---|
| `[[architecture/sso-overview]]` | KB page by path relative to the docs folder, `.md` omitted |
| `[[sso-overview]]` | KB page by unique basename; ambiguous basename → `W-LINK-AMBIGUOUS` |
| `[[ACME-US-0042]]` | Backlog item by ID; renders as `ACME-US-0042 — Login with SSO` with a status pill |
| `[[ACME-US-0042\|the SSO story]]` | Same, with custom link text |
| `[[ACME-US-0042#20260901T104512Z-jose]]` | A specific comment |
| `[[WEB/WEB-US-0031]]` | Cross-project item (soft; see §12.4) |
| `[[architecture/sso-overview#Session revocation]]` | Heading anchor inside a KB page |

- **R-WIKI-1** A target matching the ID grammar is resolved as an item; otherwise as a KB page.
  Therefore KB page paths MUST NOT look like item IDs.
- **R-WIKI-2** Unresolved wikilinks render as "broken link" styling and are listed by
  `gintrack doctor` as `W-LINK-BROKEN`. They are never errors — a link may point at a page someone is
  still writing.
- **R-WIKI-3** Redirects from `--renumber` are applied when resolving item wikilinks.
- **R-WIKI-4** Wikilinks can be disabled per project (`docs.wikilinks: false`), in which case
  `[[…]]` renders literally.

### 14.2 The link graph

The indexer builds a bidirectional graph over: KB page → KB page, KB page → item, item body → item,
item body → KB page, and `links`/`parent`/`milestone` relations. This powers:

- **Backlinks panel** on every KB page and every item ("referenced by 3 stories, 1 ADR").
- **Documentation coverage**: epics with no KB page linking to them (`W-KB-UNDOCUMENTED`, opt-in).
- **Agent navigation**: `search_kb` / `get_item` return `backlinks[]` so an agent can walk context
  without a full-text scan.

### 14.3 Recommended cross-reference conventions

- An epic SHOULD link to its design page: `[[architecture/sso-overview]]` in `## Description`.
- An ADR SHOULD link the epic or story that motivated it.
- A story's `## Technical Notes` SHOULD link the tasks that implement it, and vice versa (the
  `parent` field already carries the hierarchy; wikilinks carry the *reading path*).

---

## 15. The derived index (`index.json`)

Both the CLI and the WASM core produce the same in-memory index; the CLI can serialise it to
`.pmngr/index.json` (git-ignored) and the browser caches it in IndexedDB keyed by directory handle.
The **team repository** commits a reduced form of this document per project — that reduced schema is
normative and specified in doc 04 §6; what follows is the local, richer form.

```jsonc
{
  "schema": 1,
  "project": { "key": "ACME", "name": "ACME Platform", "docs_path": "docs" },
  "generated": "2026-09-03T07:11:02Z",
  "generator": "gintrack/0.4.1",
  "source": { "head": "9c1f0a2e…", "dirty": true },
  "counts": { "epic": 2, "story": 43, "task": 108, "milestone": 3, "comment": 214 },
  "max_ids": { "epic": 2, "story": 43, "task": 108, "milestone": 3 },
  "items": [
    {
      "id": "ACME-US-0042",
      "type": "story",
      "title": "Login with SSO",
      "status": "in_progress",
      "category": "in_progress",
      "priority": "high",
      "parent": "ACME-EP-0001",
      "milestone": "ACME-M-0003",
      "sprint": "ACME-TEAM-S-0007",
      "assignees": ["marta", "jose"],
      "labels": ["frontend", "security"],
      "estimate": 8,
      "updated": "2026-09-01T10:45:12Z",
      "due": "2026-09-15",
      "path": "docs/.pmngr/stories/ACME-US-0042-login-with-sso.md",
      "rev": "sha256:9f2b1c7d0a4e5b31",
      "ac": { "total": 5, "done": 2 },
      "comments": 3,
      "links": [{ "kind": "blocked_by", "target": "ACME-T-0107" }]
    }
  ],
  "diagnostics": [
    { "code": "W-SLUG-STALE", "path": "docs/.pmngr/tasks/ACME-T-0091-old-title.md" }
  ]
}
```

- **R-IDX-1** The index MUST be reconstructible from files alone. Nothing may live only in the index.
- **R-IDX-2** `items[]` contains front-matter-derived data only — never body text. Body search is a
  separate structure (bleve index natively; a small inverted index in WASM).
- **R-IDX-3** Staleness is detected by (path, size, mtime) triples natively and by File System Access
  `getFile().lastModified` in the browser; on mismatch the file is re-parsed.

---

## 16. Validation rules (consolidated)

Severity: **E** = error (blocks writes to the affected item; `doctor` exits non-zero),
**W** = warning (reported, never blocks).

| Code | Sev | Condition |
|---|---|---|
| `E-FM-MISSING` | E | File under an item folder has no front matter |
| `E-FM-YAML` | E | Front matter is not a valid YAML mapping |
| `E-FM-TYPE` | E | `type` missing or not valid for the folder it lives in |
| `E-ID-MISSING` | E | `id` absent |
| `E-ID-GRAMMAR` | E | `id` does not match the ID grammar |
| `E-ID-KEY` | E | `id` prefix ≠ `project.yaml:key` |
| `E-ID-TYPECODE` | E | Type code does not match `type` (e.g. `US` in `tasks/`) |
| `E-ID-FILENAME` | E | Filename ID prefix ≠ `id` field |
| `E-ID-DUPLICATE` | E | Two files claim the same `id` |
| `E-TITLE` | E | `title` missing or empty or > 200 chars |
| `E-STATUS-UNKNOWN` | E | `status` not declared in the workflow |
| `E-DATE-FORMAT` | E | A timestamp/date field is not ISO 8601 as specified |
| `E-DATE-ORDER` | E | `closed` < `started` < `created` violated |
| `E-REF-PARENT-TYPE` | E | `parent` points at a type that cannot be a parent |
| `E-REF-CYCLE` | E | `parent` chain contains a cycle |
| `E-CF-TYPE` | E | Custom field value has the wrong declared type |
| `E-CMT-ITEM-MISMATCH` | E | Comment `item` ≠ containing folder name |
| `E-ENUM` | E | `priority` or a custom enum has a value outside its allowed set |
| `W-SLUG-STALE` | W | Filename slug ≠ slug(title) |
| `W-REF-DANGLING` | W | `parent`/`milestone`/`links.target` points at an unknown ID |
| `W-REF-CYCLE-BLOCK` | W | Cycle in `blocks`/`blocked_by` |
| `W-WORKFLOW-TRANSITION` | W | Current status unreachable per declared transitions (informational on files) |
| `W-PERSON-UNKNOWN` | W | Handle not found in `people`/`team.yaml` |
| `W-LABEL-UNDECLARED` | W | Label not in the catalog |
| `W-CF-UNDECLARED` | W | Key under `custom:` not declared |
| `W-ATT-MISSING` / `W-ATT-UNLISTED` | W | Attachment bookkeeping |
| `W-CMT-ORPHAN` | W | Comments folder for a non-existent item |
| `W-LINK-BROKEN` / `W-LINK-AMBIGUOUS` | W | Wikilink resolution |
| `W-LAYOUT-NESTED` / `W-LAYOUT-STRAY` | W | Files where the layout does not expect them |
| `W-ESTIMATE-SCALE` | W | `estimate` not in `estimation.values` |
| `W-PROJ-COUNTER-STALE` | W | Counter below scanned max |

`gintrack doctor` flags: `--strict` (warnings become non-zero exit, for CI), `--fix` (safe
autofixes: slugs, key order, timestamp normalisation, label catalog), `--renumber` (§4.3),
`--json` (machine-readable diagnostics for agents and CI annotations).

---

## 17. Agent-optimized reading

An agent (via MCP, or reading the folder directly) must be able to work without loading the backlog
into its context. The rules below are the contract that doc 05 (MCP) implements.

### 17.1 Read order

1. **`project.yaml` (≈2 KB).** Gives the key, the workflow with `category` mapping, labels, and
   custom fields. Always read this first; everything else is meaningless without the status
   vocabulary.
2. **`index.json` if present and fresh** (compare `generated` to the newest mtime under `.pmngr/`,
   or trust it when the companion CLI is running and reports it as live). One read replaces hundreds.
3. **Front matter only, in bulk.** If no index exists, read the first ~40 lines of each file rather
   than whole files. Front matter is bounded; bodies are not.
4. **Bodies on demand.** Only for the handful of items the task actually concerns.
5. **Comments last.** `comments/<ID>/` is read only when the task needs discussion history.

### 17.2 Glob patterns worth knowing

```
docs/.pmngr/project.yaml                     # vocabulary
docs/.pmngr/index.json                        # snapshot, if present
docs/.pmngr/stories/*.md                      # all stories
docs/.pmngr/stories/ACME-US-004*.md           # ID range
docs/.pmngr/{stories,tasks}/*.md              # work items only, no epics/milestones
docs/.pmngr/comments/ACME-US-0042/*.md        # one item's discussion
docs/.pmngr/attachments/ACME-US-0042/*        # its binaries (do not read; list only)
```

Since the filename carries the ID *and* a human-readable slug, `ls docs/.pmngr/stories/` is already
a low-cost table of contents — roughly 45 bytes per story, versus ~2 KB to read each file.

### 17.3 Token budget guidance

| Operation | Naive cost | Recommended path | Approx. cost |
|---|---|---|---|
| "What is in progress?" | read 150 files (~300 KB) | `index.json`, filter `category=in_progress` | 1 read, ~2 KB of output |
| "What is in progress?" without an index | same | list filenames, then front matter of candidates | ~150 × 300 B |
| "Summarise story X" | read folder | `get_item(ACME-US-0042, body=true)` | 1 file |
| "Find security work" | grep everything | `search(labels=[security])` over the index | 1 query |
| "Next free task ID" | read all tasks | `index.max_ids.task + 1`, then confirm by listing `tasks/` | 1–2 ops |

### 17.4 MCP surface implied by this model

`list_items(project, type?, status?, category?, assignee?, label?, parent?, milestone?, sprint?,
updated_since?, limit, cursor)` → compact rows identical to `index.items[]`.
`get_item(id, body=false)` → front matter + `rev` (+ body when asked).
`create_item(type, title, …)` → allocates per §4.1 and returns the new ID and `rev`.
`update_item(id, expected_rev, patch)` → 409 on `rev` mismatch, per §5.
`add_comment(item, body)` → writes a new comment file.
`search_kb(query)` / `get_kb_page(path)` → the documentation folder.
`doctor(json=true)` → diagnostics.

### 17.5 Writing rules for agents

- **A-1** Always send `expected_rev` on update. Never blind-write.
- **A-2** Create items one at a time; re-read `max_ids` between creations (§4.5).
- **A-3** Never write derived values (roll-ups, backlinks, inverse relations) into files.
- **A-4** Preserve unknown front-matter keys and `x-` keys verbatim.
- **A-5** Set `updated`; never touch `created`.
- **A-6** Prefer adding a comment over editing someone else's body.
- **A-7** When a write would create a `W-*` condition (unknown label, dangling parent), proceed but
  report it; when it would create an `E-*` condition, refuse and explain.

### 17.6 `AGENTS.md`

`gintrack init` writes an `AGENTS.md` at the repository root summarising §17.1–§17.5 for the
project's concrete paths and key, so an agent that reads only `AGENTS.md` still behaves correctly.
Full conventions are specified in the Phase 5 document.

---

## 18. Appendix A — JSON Schema

Full JSON Schemas (Draft 2020-12) are **not** inlined here. They live in the repository at:

```
internal/core/schema/
  project.schema.json
  epic.schema.json
  story.schema.json
  task.schema.json
  milestone.schema.json
  comment.schema.json
  index.schema.json          # the local derived index (§15)
  common.defs.json           # shared $defs: id, handle, timestamp, date, link, label
```

They are embedded via `go:embed` and are the single source of truth for validation in the CLI, in
WASM, and for the JSON Schema published for editor autocompletion (`$schema` comment in
`project.yaml`, YAML Language Server directive). Generation of the TypeScript types for the web app
is driven from the same files.

Outline of the shared definitions:

```jsonc
// common.defs.json ($defs, abbreviated)
{
  "$id": "https://git-in-track.dev/schema/common.defs.json",
  "$defs": {
    "id":        { "type": "string", "pattern": "^[A-Z][A-Z0-9]{1,9}-(EP|US|T|M)-[0-9]{4,}$" },
    "qualifiedId": { "type": "string", "pattern": "^([A-Z][A-Z0-9]{1,9}/)?[A-Z][A-Z0-9]{1,9}-(EP|US|T|M)-[0-9]{4,}$" },
    "handle":    { "type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,31}$" },
    "statusId":  { "type": "string", "pattern": "^[a-z][a-z0-9_]{0,31}$" },
    "label":     { "type": "string", "pattern": "^[a-z0-9][a-z0-9._-]{0,31}$" },
    "timestamp": { "type": "string", "pattern": "^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z$" },
    "date":      { "type": "string", "format": "date" },
    "priority":  { "enum": ["critical", "high", "medium", "low"] },
    "link": {
      "type": "object",
      "required": ["kind", "target"],
      "additionalProperties": false,
      "properties": {
        "kind":   { "enum": ["blocks", "blocked_by", "relates_to", "duplicates", "duplicated_by"] },
        "target": { "$ref": "common.defs.json#/$defs/qualifiedId" },
        "note":   { "type": "string", "maxLength": 200 }
      }
    }
  }
}
```

Outline of `story.schema.json` (the other item schemas differ only in `type`, allowed parent, and a
couple of fields):

```jsonc
{
  "$id": "https://git-in-track.dev/schema/story.schema.json",
  "type": "object",
  "required": ["id", "type", "title", "status", "created", "updated"],
  "properties": {
    "id":        { "pattern": "^[A-Z][A-Z0-9]{1,9}-US-[0-9]{4,}$" },
    "type":      { "const": "story" },
    "title":     { "type": "string", "minLength": 1, "maxLength": 200 },
    "status":    { "$ref": "common.defs.json#/$defs/statusId" },
    "priority":  { "$ref": "common.defs.json#/$defs/priority" },
    "parent":    { "pattern": "^[A-Z][A-Z0-9]{1,9}-EP-[0-9]{4,}$" },
    "milestone": { "pattern": "^[A-Z][A-Z0-9]{1,9}-M-[0-9]{4,}$" },
    "sprint":    { "type": "string" },
    "assignees": { "type": "array", "items": { "$ref": "common.defs.json#/$defs/handle" } },
    "author":    { "$ref": "common.defs.json#/$defs/handle" },
    "labels":    { "type": "array", "items": { "$ref": "common.defs.json#/$defs/label" } },
    "estimate":  { "type": "number", "minimum": 0 },
    "effort":    { "type": "number", "minimum": 0 },
    "spent":     { "type": "number", "minimum": 0 },
    "created":   { "$ref": "common.defs.json#/$defs/timestamp" },
    "updated":   { "$ref": "common.defs.json#/$defs/timestamp" },
    "started":   { "$ref": "common.defs.json#/$defs/timestamp" },
    "closed":    { "$ref": "common.defs.json#/$defs/timestamp" },
    "due":       { "$ref": "common.defs.json#/$defs/date" },
    "links":     { "type": "array", "items": { "$ref": "common.defs.json#/$defs/link" } },
    "attachments": { "type": "array", "items": { "type": "string" } },
    "custom":    { "type": "object" },
    "deleted":   { "type": "boolean" }
  },
  "patternProperties": { "^x-": true },
  "additionalProperties": false
}
```

Note that `status` values, label membership, and custom-field types cannot be expressed in a static
schema (they depend on `project.yaml`); those checks are performed by the Go validator after schema
validation, and produce the `E-STATUS-UNKNOWN`, `W-LABEL-UNDECLARED`, and `E-CF-TYPE` diagnostics.

---

## 19. Appendix B — schema evolution

- **R-EVO-1** `project.yaml:schema` is the version of the whole `.pmngr/` layout. Items do not carry
  their own version.
- **R-EVO-2** A client reading a higher `schema` opens the project **read-only** and says why.
- **R-EVO-3** Additive changes (new optional field) do not bump `schema`. Renames, removals, and
  changes in file layout do.
- **R-EVO-4** `gintrack migrate` performs a version bump in one commit, with a dry-run mode and a
  printed diff summary.
- **R-EVO-5** Unknown keys are always preserved on rewrite, which makes forward-compatible round
  trips safe for tools built by others.

---

## 20. Phase mapping

| Phase | What this document delivers |
|---|---|
| Phase 0 | Model types, front-matter parser, slug/ID grammar, `rev`, JSON Schemas, validator skeleton |
| Phase 1 | `project.yaml`, epics/stories/tasks/milestones/comments CRUD in the browser; wikilinks; attachments |
| Phase 2 | Native indexer, `index.json`, fsnotify-driven incremental re-index, `gintrack doctor` |
| Phase 3 | Cross-project references, project keys consumed by the team repo (doc 04) |
| Phase 4 | `rev`-based conflict detection surfaced in the sync UI; `--renumber` after merges |
| Phase 5 | MCP surface of §17.4, `AGENTS.md` generation |
| Phase 6 | Metrics derived from `category`, dates, and estimates (burndown, cumulative flow) |
