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
      specs/                         # ADR-037 (section 21)
        ACME-SP-0003-sso-sessions.md
      comments/
        ACME-US-0042/
          20260901T104512Z-jose.md
          20260901T111003Z-marta.md
      attachments/
        ACME-US-0042/
          sso-sequence.png
          vendor-quote.pdf
      index.json                     # OPTIONAL derived cache, git-ignored by default
      verify.json                    # OPTIONAL derived verification cache (section 21.6), git-ignored
```

Rules:

- **R-LOC-1** `.pmngr/` MUST be a direct child of the documentation folder. A repository MUST NOT
  contain two `.pmngr/` folders that are both configured as project backlogs (a repo may host a
  monorepo with several docs folders, but each is a separate *project* with a distinct key; see
  [§3.5](#35-multiple-projects-in-one-repository)).
- **R-LOC-2** `project.yaml` MUST exist for the folder to be recognised as a project backlog. Its
  presence is the discovery marker used by `gintrack` and by the web app's folder picker.
- **R-LOC-3** The item folders (`epics/`, `stories/`, `tasks/`, `milestones/`, `comments/`, and
  `specs/` once [ADR-037](./adr/ADR-037-specs-with-requirement-blocks.md) is implemented) are
  created lazily. A missing folder is equivalent to an empty one and MUST NOT be an error.
- **R-LOC-4** Item folders MUST be flat except `comments/`, which has exactly one level of
  subfolders keyed by item ID. Nested subfolders under `epics/`, `stories/`, `tasks/`,
  `milestones/` or `specs/` are ignored by the indexer and reported by `gintrack doctor` as `W-LAYOUT-NESTED`.
- **R-LOC-5** `index.json` is derived. The default `.gitignore` snippet emitted by `gintrack init`
  ignores it inside project repositories, together with the verification cache `verify.json`
  ([§21.6](#216-verification-and-coverage)). (In the *team* repository the equivalent snapshots under
  `.pmngr/index/` ARE committed — that is the one deliberate exception, specified in doc 04.)
- **R-LOC-6** Any file under `.pmngr/` that is not `project.yaml`, not under `attachments/`, and
  does not end in `.md` is ignored with warning `W-LAYOUT-STRAY`.

### 2.1 Where a project is looked for

Discovery is **bounded** ([ADR-018](./adr/ADR-018-bounded-project-discovery.md)). A repository is
not searched from top to bottom: an unbounded walk reports every `.pmngr/` a working tree happens
to carry — test fixtures, vendored samples, a second checkout — as a project of the user.

- **R-DISC-1** A `.pmngr/project.yaml` is discovered when it sits in the repository **root**, in
  one of the root's **first-level directories** (`docs/.pmngr/`, `apps/.pmngr/`, `.pmngr/`), or in
  a documentation folder the **registration explicitly declares**, at any depth.
- **R-DISC-2** Declared folders are repository-relative. One that does not exist is not an error;
  one that escapes the repository (`..`, an absolute path) is ignored.
- **R-DISC-3** `.git/`, `node_modules/`, `dist/`, `vendor/` and dot folders other than `.pmngr` are
  never probed, whatever the rule says.
- **R-DISC-4** **Detection is deeper than discovery, and is a different act.** When a repository is
  registered, `gintrack add` and the web wizard look up to four levels down and *offer* every
  backlog they find; the user declares the ones that are theirs. A monorepo
  ([§3.5](#35-multiple-projects-in-one-repository)) therefore declares its projects once:

  ```bash
  gintrack add ~/code/mono --docs apps/api/docs --docs apps/web/docs
  ```

  The declaration is stored in the gintrack configuration (`repos[].docsFolders`, doc 07 §3.2) and,
  in browser-only mode, in the persisted folder record. Creating a project declares its folder.
- **R-DISC-5** Both runtimes apply the same rule: the shared core implements it once
  (`core.DiscoverProjectsWith`) and every host — CLI, companion, WebAssembly worker — feeds it the
  same declaration.

### 2.2 Creating a backlog

Nothing about `.pmngr/` needs a tool: a folder, a `project.yaml` and Markdown files are enough.
When a tool does create one, it writes exactly this and nothing else:

```
<docsFolder>/.pmngr/
  project.yaml              # schema 1, the key, the name, the default workflow of section 6.2
  .gitignore                # `index.json` (R-LOC-5)
  epics/  stories/  tasks/  milestones/  comments/  attachments/
```

A tool creates a backlog at `schema: 1` even when it supports specs; the project moves to
`schema: 2` only when its first spec construct is written ([§21.10](#2110-schema-version-2),
ADR-037).

- **R-NEW-1** The key MUST match the grammar of [§3.3](#33-identifiers) before anything is written.
- **R-NEW-2** A folder that already holds a `project.yaml` MUST be refused, never overwritten.
- **R-NEW-3** The written file MUST validate under [§6.3](#63-validation-rules-for-projectyaml)
  with no findings.

The single implementation is `core.CreateProject` (`internal/core/scaffold.go`), which touches
nothing but the injected file system, so the CLI (`gintrack init`, `gintrack add --key`), the REST
API (`POST /repos/{id}/projects`) and the browser all produce byte-identical files.

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
- **R-FMT-7** A file produced by resolving a merge conflict is a file like any other: the
  conflict resolver merges front matter on parsed values and the body as text, then puts the
  result back through the same parser and emitter the editor writes with, so the resolution is
  byte-identical in shape to a normal save and can never contain a conflict marker
  (docs/06 §5.7, `internal/core/merge.go`).

### 3.2 Canonical key order

Writers MUST emit known keys in this order; unknown/custom keys follow, sorted lexicographically.
Keys whose value is null/empty MUST be omitted rather than written as `null` or `[]`.

```
id, type, title, status, priority, parent, epic, milestone, sprint,
assignees, author, labels, estimate, effort, spent,
created, updated, started, closed, due,
links, blocks, depends_on, external, attachments, custom, inbox, requirements, deleted
```

Comments use the same order with their own keys in the region they belong to:
`type, item, author, author_name, author_email, created, updated, in_reply_to, kind, reactions,
external, attachments`.

Rationale: identity first, then classification, then people, then numbers, then dates, then
relations, then the blocks. Diffs of unrelated changes touch different regions of the block.
`external` sits with the relations because it is one — a relation to an artifact outside this
repository ([§12.5](#125-external-references), [ADR-031](./adr/ADR-031-external-references.md)) —
and `inbox` sits at the end, next to `deleted`, because like `deleted` it is lifecycle state that
only a minority of files carry ([§6.4](#64-the-triage-category-and-the-inbox),
[ADR-033](./adr/ADR-033-inbox-is-a-reserved-triage-status-category.md)). `requirements` (spec files
only, [§21](#21-specs-and-requirement-blocks)) sits just before `deleted` for the same reason, and
because it is the largest block a file can carry.

### 3.3 Identifiers

```
<ID> ::= <KEY> "-" <TYPECODE> "-" <NUMBER>

<KEY>      ::= [A-Z][A-Z0-9]{1,9}          # from project.yaml `key`
<TYPECODE> ::= "EP" | "US" | "T" | "M" | "SP"   # "SP" added by ADR-037
<NUMBER>   ::= [0-9]{4,}                    # zero-padded to at least 4 digits

<REQREF>   ::= <KEY> "-SP-" <NUMBER> ".R" <RNUM>   # a requirement block, ADR-037
<RNUM>     ::= [1-9][0-9]*                  # no padding, no leading zero
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
- **R-ID-5** A requirement is not an item and has no file: it is addressed by the scoped ref
  `<SPEC-ID>.R<n>` (`ACME-SP-0003.R2`), permanent and never reused, allocated `max + 1` within its
  spec ([§21.3](#213-requirement-ids)). `R<n>` has exactly one spelling: `R02` and `r2` are
  `E-ID-GRAMMAR`.

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

Both of those sit three levels down, which the bounded discovery rule
([§2.1](#21-where-a-project-is-looked-for)) does not reach on its own: the registration declares
them once (`gintrack add --docs apps/api/docs --docs apps/web/docs`, or the checkboxes of the
add-repository wizard) and they are found on every scan afterwards. Two projects in first-level
siblings — `api/.pmngr/` and `web/.pmngr/` — need no declaration at all.

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
| `type` | `epic`, `story`, `task`, `milestone`, `comment`, `spec` ([§21](#21-specs-and-requirement-blocks)) (the `board`, `sprint` and `retro` types exist only in the team repo, and are specified in [doc 04](./04-team-repository.md) §§5, 8 and 9; all three round-trip through the same byte-stable emitter as an item, so an edit to one field is a one-line diff). A sprint's stored `state` is `planned`, `active` or `closed`; the `draft`/`upcoming`/`current`/`completed` status a reader sees is derived from its dates and is never a stored value ([ADR-034](./adr/ADR-034-sprint-status-is-derived-from-dates.md)) |
| `priority` | `critical`, `high`, `medium`, `low` |
| `status` | any `id` declared in `project.yaml:workflow.statuses` |
| relation kind | `blocks`, `blocked_by`, `relates_to`, `duplicates`, `duplicated_by`; added by ADR-037: `implements`, `implemented_by`, `modifies`, `modified_by`, `supersedes`, `superseded_by` ([§12.1](#121-the-links-field)) |
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
   The bump edits that one value in place and leaves every other byte of `project.yaml` as
   written; only a shape it cannot splice (a flow-style `id_allocation`, say) falls back to
   re-encoding the YAML node tree, which keeps comments and key order but not spacing.

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
  `doctor` prints the exact `team.yaml` edit needed. Since GIT-US-0037 that edit is also a
  supported write: remove the old entry and add the new one from Settings, from
  `team.project.remove`/`team.project.add`, or over `/api/v1/teams/{key}/projects`
  ([doc 04 §3.9](./04-team-repository.md)). The two acts stay separate — the product never
  renames a project key inside `team.yaml`, because a rename that is not the whole change set
  leaves every `ref:` dangling.

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
- **R-REV-3** Writes are conditional. `PATCH /api/v1/items/ACME-US-0042` with
  `If-Match: sha256:9f2b…` (or the MCP `update_item{rev: …}`) MUST fail with the
  `stale_revision` problem — `412 Precondition Failed` over HTTP — if the on-disk `rev` differs.
  The failure carries `currentRev` and `conflicts[]` so a client or agent can retry a merge
  without a second round trip.
- **R-REV-3a** The conflict report is one shape on every surface. `currentRev` is the revision the
  file holds now. `conflicts[]` lists the fields the refused write **would still have changed**,
  judged against that current content: `{ "field": "status", "current": "in_progress",
  "proposed": "in_review" }`. It is not a diff against the caller's base version — the base is a
  hash, not a document, so no reader holds it — and an empty list therefore means the write had
  already been made by whoever won the race, so the caller has nothing left to do. The list is
  empty only when every proposed field is already on disk: a write the store would refuse anyway
  (a blank title, an unknown field to unset) names every field it carries instead, so a refused
  change never reads as a saved one. The body, `custom`, `external` and `inbox` are reported as
  the bare field name, never quoted back.
- **R-REV-3b** Omitting the revision is not the same as waiving the check. A surface that serves
  unattended writers MUST refuse a write that carries no revision (`precondition_required`); the
  waiver is spelled explicitly as `If-Match: *` over HTTP and `rev: "*"` over MCP, and is
  documented as unsafe. Creating something that does not exist yet needs no revision.
- **R-REV-3c** A write that changes several fields, or fields *and* status, is one conditional
  write against one revision. A surface MUST NOT split it into two writes, because the second
  would have to quote a revision the caller never saw and would accept a stale caller silently.
- **R-REV-4** `rev` is *not* a version number and MUST NOT be persisted, compared for ordering, or
  used as a cache key across machines beyond its purpose (it is a pure function of content, so it is
  in fact a perfectly good cross-machine cache key — but nothing may assume monotonicity).
- **R-REV-5** Relationship to git: for a file staged unmodified, git's blob OID would serve the same
  purpose, but working-tree files are frequently dirty and `gintrack` must work on an uncommitted
  tree. `rev` is therefore computed from the working tree, independently of git.
- **R-REV-6** `rev` covers the whole file, front matter and body. A body-only edit changes `rev`.
  Comment files have their own `rev`; the parent item's `rev` does not change when a comment is
  added. A comment is a new file and can overwrite nothing, so the revision quoted when adding one
  is the *item's*: it does not protect the thread, it proves the writer has seen the item it is
  commenting on. It is required of agents (docs/08) and optional for a human in the UI.

This section is the single definition of the locking contract; `07-cli-and-api.md` section 5.3 and
`08-mcp-server.md` section 3.5 describe how each surface spells it, and neither adds a rule.

---

## 6. `project.yaml`

The only non-Markdown file in `.pmngr/`. Plain YAML, no front matter.

### 6.1 Fields

| Key | Type | Req. | Default | Notes |
|---|---|---|---|---|
| `schema` | integer | yes | `1` | Data-model version. Unknown/higher → refuse to write, allow read-only. `2` once the project holds a spec construct ([§21.10](#2110-schema-version-2), ADR-037). |
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
| `integrations` | mapping | no | — | External trackers this backlog mirrors ([§6.5](#65-integrations)). Credentials never appear here. |
| `specs` | mapping | no | `{lint: {severity: warning}}` | Spec settings; today only `lint`, the requirement-grammar severity `off\|warning\|error` ([§21.9](#219-grammar-lint-and-specslint), ADR-037). |

`docs` sub-keys: `path` (relative to repo root, informational — the real path is where the file
was found), `wikilinks` (bool, default `true`), `mermaid` (bool, default `true`), `math` (bool,
default `false`), `footnotes` (bool, default `true`), `callouts` (bool, default `true`),
`attachments_dir` (default `.pmngr/attachments`).

`workflow` sub-keys:

- `statuses`: ordered list of `{id, name, category, wip?, color?, terminal?}`.
  - `id`: `[a-z][a-z0-9_]{0,31}`, unique.
  - `category`: `todo | in_progress | done | cancelled | triage` — the *coarse* bucket used by boards
    (a board column maps `categories:` instead of `statuses:` when it must work for a project
    whose workflow the team has never seen — doc 04 R-COL-2),
    metrics, and agents that do not know a project's custom workflow. This field is what makes
    heterogeneous projects comparable on a team board. `triage` is the reserved inbox category
    ([§6.4](#64-the-triage-category-and-the-inbox)); a project that declares no status in it simply
    has no inbox.
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
    - { id: triage,      name: Triage,      category: triage }
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

integrations:
  youtrack:
    url: https://yt.example.com/youtrack
    project: ACME
    field_map:
      status: State
      priority: Priority
    push_comments: manual
    kb_sync: manual
    kb_sync_direction: push

specs:                    # ADR-037 (section 21.9)
  lint:
    severity: warning     # off | warning | error
```

### 6.3 Validation rules for `project.yaml`

- `E-PROJ-MISSING` — `.pmngr/` exists but `project.yaml` does not.
- `E-PROJ-KEY` — `key` absent or not matching `[A-Z][A-Z0-9]{1,9}`.
- `E-PROJ-SCHEMA` — `schema` missing, or greater than the supported version (read-only fallback;
  enforced on writes from the release that introduces specs, R-SCHEMA-2-4).
- `E-PROJ-STATUS-DUP` — duplicate status `id`.
- `E-PROJ-STATUS-CATEGORY` — a status has an unknown `category`.
- `E-PROJ-INITIAL` — `workflow.initial` names a status that does not exist.
- `E-PROJ-TRANSITION-TARGET` — a transition names an unknown status.
- `W-PROJ-NO-DONE` — no status has category `done`; metrics will be meaningless.
- `W-PROJ-LABEL-DUP` — duplicate label name (case-insensitive).
- `W-PROJ-LABEL-KEYS` — a label entry has keys other than `name`, `color` and `description`.
  The usual cause is a flow-style entry such as `{ name: core, description: Parser, index }`:
  unquoted commas end a plain scalar inside `{ }`, so the description is cut short and the rest
  becomes extra keys. Quote the description.
- `W-PROJ-COUNTER-STALE` — a counter is lower than the maximum scanned ID (informational; the scan
  wins and the counter is rewritten on the next allocation).
- `E-PROJ-INTEGRATION` — an `integrations.<system>` block is present but unusable: a `url` that is
  not an absolute `http`/`https` URL, an empty `project`, an unknown mode, or a `field_map` key that
  is not a git-in-track field ([§6.5](#65-integrations)).
- `E-PROJ-SPECS` — `specs.lint` (scalar shorthand), `specs.lint.severity` or a `specs.lint.rules`
  value is not `off`/`warning`/`error`, or `rules` names an unknown lint rule ([§21.9](#219-grammar-lint-and-specslint)).

### 6.4 The `triage` category and the inbox

Incoming work that nobody has reviewed yet has to get an id, a file, a history and comments without
polluting the backlog, the boards, the sprints or the metrics. It does that by being an **ordinary
item in a status whose category is `triage`**, plus an `inbox:` front-matter block that records how
it arrived and what the triager decided ([ADR-033](./adr/ADR-033-inbox-is-a-reserved-triage-status-category.md)).

There is no inbox item type, no folder under `.pmngr/` and no stored `is_inbox` boolean: **the
category is the truth and the block is the metadata.**

```yaml
status: triage
inbox:
  status: snoozed
  snoozed_until: 2026-10-01
  source: web
  received: 2026-09-10T07:59:12Z
```

| Key | Type | Req. | Notes |
|---|---|---|---|
| `status` | `pending` \| `accepted` \| `rejected` \| `snoozed` \| `duplicate` | no | absent reads as `pending` |
| `snoozed_until` | date | conditional | required for, and only allowed with, `status: snoozed` |
| `duplicate_of` | item ID | conditional | required for `status: duplicate` |
| `source` | string | no | free text: `web`, `mcp`, `youtrack`, a form name. Never an enumeration |
| `received` | timestamp | no | when the submission arrived, which is not when the file was created |

- **R-INBOX-1** An item is in the inbox when its `status` resolves to category `triage`, and at no
  other time. The `inbox:` block on an item outside that category is preserved and reported as
  `W-INBOX-CATEGORY`: it is history of how the item arrived, and it is ignored by the inbox.
- **R-INBOX-2** A default query **excludes** triage items. `Filter.Inbox` is a tri-state —
  `exclude` (the zero value), `only`, `include` — so every filter written before the inbox existed
  keeps its meaning. Board views, sprint views, sprint candidates and sprint metrics exclude them
  unconditionally.
- **R-INBOX-3** A sprint file that names a triage reference reports it as **unresolved**, never as
  work: it contributes no points and is never counted as done. The exclusion cannot be smuggled in
  by hand-editing a sprint.
- **R-INBOX-4** Snooze expiry is a **query-time comparison**, never a scheduler and never a
  background job. A `snoozed` item whose `snoozed_until` is at or before the caller-supplied
  `SnoozeAsOf` matches a query for `pending`. `internal/core` reads no clock — the caller passes the
  instant, so results are reproducible and the package still compiles to WebAssembly.
- **R-INBOX-5** Unknown keys **inside** the block are preserved on rewrite and re-emitted after the
  known ones, sorted lexicographically, exactly as unknown top-level keys are (R-FMT-6).
- **R-INBOX-6** Diagnostics: `E-INBOX-STATUS` (unknown triage state), `E-INBOX-SNOOZE`
  (`snoozed_until` missing on a snoozed item, or present on any other), `E-INBOX-DUPLICATE`
  (`duplicate_of` is not an item id, points at the item itself, or is missing on a `duplicate`),
  `W-INBOX-CATEGORY` (block outside the triage category) and `W-INBOX-DUP-DEAD` (`duplicate_of`
  resolves to nothing — a warning, because the target may arrive in a later merge).
- **R-INBOX-7** A project scaffolded by `gintrack` declares `{id: triage, name: Triage, category:
  triage}`, and it is neither the initial status nor the target of any declared transition, so
  nothing ordinary lands there by accident.

---


### 6.5 `integrations`

`integrations.<system>` records **where this backlog's items also live**, so that a
clone knows what it is mirroring without being told. Only `youtrack` exists today
([ADR-032](./adr/ADR-032-local-integration-credential-storage.md), GIT-EP-0011); the
key is the same `system` an item's `external:` entry carries ([§12.5](#125-external-references)),
which is what ties a connection to the references it produced.

```yaml
integrations:
  youtrack:
    url: https://yt.example.com/youtrack   # instance URL, context path included
    project: ACME                          # YouTrack project short name
    project_id: 0-17                       # its internal entity id, what writes address
    field_map:                             # git-in-track field -> YouTrack custom field
      status: State
      priority: Priority
    push_comments: manual                  # manual | auto
    kb_sync: manual                        # manual | on_write
    kb_sync_direction: push                # push | pull | both
    comment_template: "\n\n---\n_{{.Author}} · git-in-track {{.ItemID}}_"
    land_in_inbox: false                   # imported issues arrive in triage, not the backlog
```

| Key | Type | Req. | Default | Notes |
|---|---|---|---|---|
| `url` | absolute URL | yes | — | `http` or `https`, context path included; no query, no fragment. Trailing `/` is trimmed. |
| `project` | string | yes | — | YouTrack project short name: the `ACME` of `ACME-42`. |
| `project_id` | entity id | no | — | The same project's internal id, as in `0-17`. YouTrack addresses a project by this id on every write — creating an article with the short name is refused with `Invalid structure of entity id` — so the settings picker records it when a project is chosen from the instance. Absent is legal: the companion resolves the id from the short name when a job needs one. Changing `project` without giving a new id drops it. |
| `field_map` | mapping | no | `{}` | Keys from `status`, `priority`, `type`, `assignee`, `estimate`, `milestone`. An entry is either a YouTrack custom-field name or a `{field, values}` block; see below. |
| `push_comments` | `manual` \| `auto` | no | `manual` | When a comment written here is pushed to the linked issue. |
| `kb_sync` | `manual` \| `on_write` | no | `manual` | When a knowledge-base page is synchronized with a YouTrack article. |
| `kb_sync_direction` | `push` \| `pull` \| `both` | no | `push` | Which way that synchronization flows. |
| `comment_template` | `text/template` | no | see R-INT-6 | The attribution line appended to a comment pushed upstream. |
| `land_in_inbox` | boolean | no | `false` | Imported issues arrive in the project's triage queue instead of its backlog (R-INT-7, [§6.4](#64-the-triage-category-and-the-inbox), GIT-EP-0012). |

A settings save rewrites only the lines of the `integrations.youtrack` block and leaves every
other byte of `project.yaml` as written: comments, alignment, quoting and flow mappings elsewhere
in the file are untouched. Only a shape it cannot splice (a flow-style `integrations`, say) falls
back to re-encoding the YAML node tree, which keeps comments and key order but not spacing.

`field_map` answers two different questions, and an entry says which one it is answering. Written as
a plain string it renames a field: `status: State` tells the importer which custom field to read a
status out of. Written as a block it also translates the values inside that field:

```yaml
field_map:
  status:
    field: State
    values:
      In Progress: in_progress
      Fixed: done
  priority: Priority
```

Both forms are read, both are valid, and the flat one is written back flat, so a file only grows the
nesting it asked for. The keys are names on both sides — a YouTrack field name and a YouTrack value
name — never ids, which are local to an instance, and never localized names, which change with the
reader's language.

Six keys name a field: `status` → `State`, `priority` → `Priority`, `type` → `Type`, `assignee` →
`Assignee`, `estimate` → `Estimation`, `milestone` → `Fix versions`. Only three of them accept a
`values` block — `status`, `priority` and `type` — because those are the fields whose vocabularies
differ between the two systems; an estimate and an assignee are converted rather than looked up.
[§12.6](#126-what-a-youtrack-issue-becomes) documents what the importer does with a field nobody
mapped, defaults included.

Earlier builds also accepted `labels`, `due` and `sprint`. They were stored, validated and then read
by nothing, which is the worst outcome a configuration file can produce: the mapping is recorded and
never honoured. They are now **refused at load time**, each with the reason it was never a mapping in
the first place — labels travel as YouTrack tags rather than through a custom field, and neither a
due date nor a sprint is read from one.

- **R-INT-1 No credential is ever written here.** `project.yaml` is a committed file:
  the permanent token lives on the machine running the companion, in its `0600`
  configuration file, keyed by project key, overridable by `GINTRACK_YOUTRACK_TOKEN`
  (doc 07 §3.2, ADR-032). A token found in a `project.yaml` is a leaked token, not a
  configuration.
- **R-INT-2 The block is written surgically.** A writer edits the YAML node tree in
  place — the same mechanism the id allocator rewrites its counters with — so comments,
  key order and every section no Go struct models survive. Re-serializing `project.yaml`
  from a decoded struct would silently delete them, and is never done.
- **R-INT-3 An unusable block is a load error, not a silent default.** A URL that is not
  absolute, an empty project short name, an unknown mode or an unknown `field_map` key is
  refused with `E-PROJ-INTEGRATION` naming the offending key, because a typo in a field map
  is otherwise invisible until a sync writes the wrong field.
- **R-INT-4 A block alone connects nothing.** Reaching the instance also needs a token on
  this machine, and browser-only mode has neither the token nor the network reach: it hides
  the feature entirely (doc 07, `features.youtrack`).
- **R-INT-5 An unknown `field_map` key is refused, an unused one is not.** The nine keys above are
  the whole vocabulary and a tenth is `E-PROJ-INTEGRATION` (R-INT-3), because the alternative —
  ignoring what looks like a typo — is a field map that silently does nothing. A key that is legal
  but not yet consumed is accepted in silence, which is the cost of keeping the vocabulary stable
  while the importer grows into it.
- **R-INT-6 The attribution line is a team decision, so it is committed.** `comment_template` is a
  Go `text/template` rendered against `.Author`, `.AuthorName`, `.ItemID`, `.IssueID` and
  `.CommentRef`, appended to every comment this project pushes upstream; empty means the shipped
  default, `\n\n---\n_{{.Author}} · git-in-track {{.ItemID}}_`. It lives here rather than in the
  machine-local file because a clone must sign what it publishes the same way the original does. The
  item id is emitted bare on purpose: YouTrack auto-links anything shaped like one of *its* issue
  ids, a git-in-track id is not one, and a Markdown link around it would be a dead link.
- **R-INT-7 `land_in_inbox` decides where an import lands, and refuses rather than guesses.** With
  it `false` — the default, and what every import did before the key existed — an imported issue is
  written with the workflow's initial status. With it `true`, the issue is written with the
  project's first `triage` status and an `inbox:` block of `status: pending`, `source: youtrack`,
  so a large import is a queue to review rather than a backlog somebody has to un-commit. It
  decides arrival only: an issue a later import *updates* keeps the status it has, because landing
  is a decision about where work appears the first time and not about every sync ([§6.4](#64-the-triage-category-and-the-inbox), ADR-033). A project that
  declares no triage status and sets the option to `true` is a configuration mistake and is
  **refused**: landing a thousand issues in the backlog instead would be exactly the outcome the
  option exists to prevent. The decision is one helper — `core.InboxLandingStatus` — which every
  entry point that can file work shares, so an import, an agent's `create_inbox_item` and a web
  submission cannot disagree about where a submission belongs. The key is read and never written:
  a connection saved from the settings screen edits only the keys that screen owns (R-INT-2), so a
  team that set this by hand keeps it.


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
| `external` | list of external references | no | the same artifact in another system, [§12.5](#125-external-references) |
| `attachments` | list of strings | no | filenames under `attachments/<ID>/` |
| `custom` | mapping | no | declared custom fields |
| `inbox` | mapping | no | triage metadata, only on items in the `triage` category, [§6.4](#64-the-triage-category-and-the-inbox) |
| `deleted` | bool | no | soft delete, default `false` |

An epic MUST NOT have `parent`. Stories point *up* to their epic; epics do not list their children
(that would duplicate state and create merge conflicts on every story creation).

**Deleting is soft by default.** `deleted: true` keeps the file, its id, its path and its
history: the id is never reused (R-ID-3), a merge cannot resurrect a stale copy, and every
`parent`, `milestone` and `links[]` entry that named the item still resolves — to an item
marked deleted rather than to nothing. A deleted item is excluded from lists, boards, search
and metrics unless a caller asks for it (`includeDeleted`). Removing the file is a hard
delete, and it is deliberate: `gintrack item delete --hard` and
`DELETE /api/v1/items/{id}?hard=true`. The web app only ever soft-deletes, after showing what
still points at the item ([ADR-026](./adr/ADR-026-the-web-app-soft-deletes-and-warns-first.md),
[doc 05 §8.4](./05-web-app.md)).

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
| `sprint` | sprint ID | no | `<TEAMKEY>-S-<NNNN>`, resolved in the team repo; soft reference. The sprint's own `start` and `end` are optional — both or neither — and its `draft`/`upcoming`/`current`/`completed` status is derived from them at read time and never stored; a closed sprint additionally carries a frozen `snapshot` block. See [doc 04](./04-team-repository.md) §8.2 and [ADR-034](./adr/ADR-034-sprint-status-is-derived-from-dates.md) |
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
| `labels`, `author`, `created`, `updated`, `links`, `external`, `attachments`, `custom`, `deleted` | as elsewhere | | |

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
| `author_name` | string | no | the writer's git `user.name`; the handle is derived from it when no handle is given (ADR-030) |
| `author_email` | string | no | the writer's git `user.email` (ADR-030) |
| `created` | timestamp | yes | MUST match the filename timestamp |
| `updated` | timestamp | no | present only if edited |
| `in_reply_to` | comment ref | no | `<ITEM-ID>#<file-stem>` |
| `kind` | `comment` \| `status_change` \| `system` | no | default `comment` |
| `reactions` | mapping emoji → list of handles | no | |
| `external` | list of external references | no | the comment this one mirrors in another system, [§12.5](#125-external-references) |
| `attachments` | list of strings | no | resolved under `attachments/<ITEM-ID>/` |

`kind: system` marks machine-written entries (e.g. an agent recording an automated check). Systems
SHOULD write few, high-value system comments; the git log is the audit trail, not the comment stream.

- **R-CMT-4** A human write that names nobody is attributed to the git identity of the repository
  the item lives in (`user.name`, `user.email`, or the configured `git.authorName`/`authorEmail`
  overrides): `author` becomes the handle of `user.name`, and `author_name`/`author_email` carry the
  identity itself. Only when no identity resolves does the handle fall back to `unknown`.
- **R-CMT-5** `external` on a comment is the same list, with the same set semantics, as on an item
  ([§12.5](#125-external-references)); what differs is what it is *for*. On an item it answers "is
  this issue already imported?"; on a comment it answers "is this remark already upstream?", one
  remark at a time, which is what makes pushing a thread idempotent. A comment that carries no
  entry for the system is **created** remotely and the id that comes back is written into the file;
  a comment that already carries one is **edited** in place. That is not an optimisation — the job
  engine re-delivers a job after a retry, after a journal replay and when somebody clicks retry in
  the dead-letter list, and without the reference each delivery would leave another copy of the same
  remark on the issue.
- **R-CMT-6 A local delete never deletes remotely, and there is deliberately no way to make it.**
  Removing a comment file, or removing its `external` entry, unlinks the record and stops there: no
  job is queued, and none exists to queue. A repository is not the authority on a conversation that
  other people are also having in the tracker, and a mistaken `rm` — or a branch that never had the
  file — must not erase a thread. The asymmetry is the point, not an omission: writes propagate
  outward, deletions do not ([ADR-031](./adr/ADR-031-external-references.md)). A comment that is
  deleted upstream is likewise left alone locally. A comment whose `external` entry was removed and
  which is then pushed again produces a **second** remote comment, because as far as both sides can
  tell it is a new one.

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
| `implements` | `implemented_by` | story/task → requirement or spec: the work realises it (ADR-037) |
| `implemented_by` | `implements` | |
| `modifies` | `modified_by` | story/task → requirement or spec: the work changes an existing requirement (ADR-037) |
| `modified_by` | `modifies` | |
| `supersedes` | `superseded_by` | requirement → requirement, spec → spec: the source replaces the target (ADR-037) |
| `superseded_by` | `supersedes` | |

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
- **R-LINK-6** *(ADR-037.)* `target` MAY also be a requirement ref, qualified or not:
  `<link-target> ::= [<KEY> "/"] (<ID> | <REQREF>)`. A missing spec or block is `W-REF-DANGLING`.
  `implements`/`modifies` (and inverses) need a spec or requirement target, `supersedes`/
  `superseded_by` need a target of the source's own kind (requirement ↔ requirement, spec ↔ spec);
  any other combination is `E-LINK-TARGET-TYPE`. Requirement-level relations are stored in
  `requirements.R<n>.links` ([§21.4](#214-the-requirements-map)), same `{kind, target}` shape,
  restricted to `supersedes`, `superseded_by` and `relates_to` (R-REQ-13). Using any of the new
  kinds or a spec/requirement target requires `schema: 2` ([§21.10](#2110-schema-version-2)).

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

Source code points at *requirements* (not items) with `// Implements: ACME-SP-0003.R2` and
`// Verifies: ACME-SP-0003.R2` comment markers, defined by ADR-037 and specified in
[§21.7](#217-in-code-markers). They are read by the native trace engine, never by `internal/core`.

### 12.4 Cross-project references

A project repo MAY reference another project's item (`WEB/WEB-US-0031`). Resolution requires the
team repo (to map `WEB` → repo/docs path) and, for a live title/status, either a local clone or the
committed snapshot `.pmngr/index/WEB.json` in the team repo (doc 04, §6). Without either, the
reference renders as inert text with the ID. This is by design: **backlogs never leave their project
repository**.

### 12.5 External references

`external` is the first-class record of the same artifact in another system: the YouTrack issue an
item was imported from, the comment a mirrored thread came from, the wiki page a knowledge-base
article was copied out of. It is a **list**, because one item may be linked into more than one
system, and it is a first-class front-matter key rather than a `custom:` entry or an `x-` key
because importers depend on it being typed, validated and indexed
([ADR-031](./adr/ADR-031-external-references.md)).

```yaml
external:
  - { system: youtrack, id: PRJ-42, url: https://youtrack.example.com/issue/PRJ-42, key: PRJ, synced_at: 2026-09-02T10:29:00Z }
  - { system: plane, id: 9f2b1c7d }
```

| Key | Type | Req. | Notes |
|---|---|---|---|
| `system` | short token | yes | `[a-z0-9][a-z0-9._-]{0,31}`, stored lower-case. **Not an enumeration** |
| `id` | string (1..200) | yes | the identifier the external system uses |
| `url` | string | no | an absolute `http`/`https` address a human can open |
| `key` | string | no | the external project/space key, when the system has one |
| `synced_at` | timestamp | no | when this reference was last reconciled |

- **R-EXT-1** `system` and `id` are both required. An entry missing either is `E-EXT-FIELDS`.
- **R-EXT-2** The pair **(`system`, `id`)** is the identity of an entry and the idempotency key of
  every importer. It is compared with `system` lower-cased and `id` case-sensitive. A parser that
  reads the same pair twice in one file keeps the first entry.
- **R-EXT-3** `system` is never validated against a list of known systems. A file naming a system
  this version has never heard of is valid and MUST round-trip untouched (R-EVO-5). Only the shape
  of the token is checked, so that a system name can be a path segment or a map key unescaped.
- **R-EXT-4** A `url` that is not `http`/`https` is `W-EXT-URL`, a warning: the reference is still
  usable, it just cannot be opened.
- **R-EXT-5** Writes use set semantics keyed on (`system`, `id`), like `labels` and `links`:
  `addExternal` / `removeExternal`. Re-adding a pair that is already present **updates** `url`,
  `key` and `synced_at` in place and never appends a second entry; fields the writer omits keep the
  value somebody else recorded. A `removeExternal` entry with an empty `id` unlinks every reference
  of that system. Two writers pushing different systems into the same item therefore never clobber
  each other.
- **R-EXT-6** The index keeps a lookup from (`system`, `id`) to item id, so an importer's
  "have I already got this one?" check is a single map read and not a scan of the backlog. Two
  items claiming the same pair is `W-EXT-DUP`; the first file in path order wins the lookup.
- **R-EXT-7** Knowledge-base pages carry the same key in their (otherwise free-form) front matter.
  Comments carry it too, which is what lets a mirrored discussion be reconciled comment by comment.

### 12.6 What a YouTrack issue becomes

`external` answers "have I seen this issue before?"; this section answers "what does it turn into
the first time?". The translation lives in `internal/youtrack/mapping`, it is a pure function of the
payload and the field map, and every surface that imports — the REST route, the background job, the
MCP tool, the CLI — calls the same one, so an issue cannot become one thing in the web app and
another on the command line.

The table is written down here because it is a *data-model* decision rather than an implementation
detail: it says which of this document's fields a foreign tracker is allowed to fill, and, just as
importantly, which ones it is not.

| YouTrack | git-in-track | Notes |
|---|---|---|
| `summary` | `title` | Trimmed. An empty summary is a warning and an item with no title |
| `description` | body | Normalised, never sanitised — see below |
| `reporter` | `author` | The login, or the full name when the instance sent no login |
| `tags[]` | `labels[]` | Trimmed and deduplicated, in the order YouTrack returned them |
| `idReadable` | `external[]` | `{system: youtrack, id, url, synced_at}` — the idempotency key (§12.5). An imported item records no `key`; only a published page does (§14.6) |
| Type field | `type` | Default `Type`; a value out of a *version* bundle is a `milestone` whatever it is called |
| State field | `status` | Default `State` |
| Priority field | `priority` | Default `Priority` |
| Estimation field | `estimate` | Default `Estimation`; a period divided by one working day |
| Assignee field | `assignees[]` | Default `Assignee`; multi-valued, the login is the handle |
| Milestone field | `milestone` | Default `Fix versions`; several versions keeps the first and warns |
| `Subtask` link, inward | `parent` | More than one parent keeps the first and warns |
| `Subtask` link, outward | children | The other half of the same hierarchy |
| Other link types | `links[]` | `Depend`, `Duplicate` and `Relates`; see below |
| `attachments[]` | `attachments[]` | Recorded as bare filenames, like every other writer (§13.4); the bytes arrive later — see R-YT-7 |
| `comments[]` | comment files | One file per comment (§11), keeping the original author and time |

Nothing fills `sprint`, `due`, `effort` or `spent`. Those are git-in-track's own planning fields;
an import that guessed at them would overwrite a decision this team made with one YouTrack never
took, and a re-import would do it again every time.

**The six field names are configurable, the three value maps are not.** `integrations.youtrack.field_map`
([§6.5](#65-integrations)) renames the custom fields a mapper reads; the *values* inside those
fields are translated by the built-in tables below, which a project overrides in code rather than in
`project.yaml`. That asymmetry is deliberate: a renamed field is a one-line configuration, whereas a
state bundle with twenty entries is a decision nobody wants to express in YAML.

**Types** — anything not listed becomes a `task`:

| YouTrack `Type` | Item type |
|---|---|
| `Epic` | `epic` |
| `User Story`, `Story`, `Feature` | `story` |
| `Task`, `Bug`, `Usability Problem`, `Performance Problem`, `Cosmetics`, `Exception` | `task` |
| `Milestone`, `Version`, or any value from a version bundle | `milestone` |

**States** — anything not listed leaves `status` **unset**, so the project's own default status
applies rather than a status this project may not even declare:

| YouTrack `State` | Status |
|---|---|
| `Submitted`, `To be discussed` | `backlog` |
| `Open`, `Reopened` | `todo` |
| `In Progress` | `in_progress` |
| `To Verify`, `In Review` | `in_review` |
| `Fixed`, `Verified`, `Done` | `done` |
| `Can't Reproduce`, `Duplicate`, `Won't fix`, `Obsolete`, `Incomplete` | `cancelled` |

**Priorities** — anything not listed becomes `medium`:

| YouTrack `Priority` | Priority |
|---|---|
| `Show-stopper`, `Critical`, `Blocker` | `critical` |
| `Major`, `High` | `high` |
| `Normal`, `Medium` | `medium` |
| `Minor`, `Low` | `low` |

**Link types.** `Relates` becomes `relates_to` in both directions. `Depend` is directed: its outward
half ("is required for") is `blocks` and its inward half is `blocked_by`. `Duplicate` likewise gives
`duplicates` outward and `duplicated_by` inward. A link type outside those three is a warning and no
link — the five kinds of §12.1 are the whole vocabulary, and inventing a sixth to hold a YouTrack
type would break every consumer of `links`.

- **R-YT-1** All three lookups are case-insensitive on the trimmed value, so `In Progress`,
  `in progress` and `  IN PROGRESS ` are one key.
- **R-YT-2** **A value nobody understands is a warning, never an error and never a silent drop.** An
  import of two hundred issues must finish and then say what it could not read; failing the batch on
  one unknown enum value would make the feature unusable against any real instance.
- **R-YT-3** An `Estimation` period is read from `minutes` when YouTrack sent it and from its
  presentation (`1w 2d 3h`) otherwise, using a stock working week — 8 hours a day, 5 days a week —
  and divided by one working day to give one story point, rounded to two decimals. A presentation
  that does not parse cleanly yields **no** estimate and a warning: an estimate of `0` is a
  statement, and guessing one is worse than leaving the field unset.
- **R-YT-4** `parent`, `milestone` and `links[].target` are **not** written by the mapper. It returns
  the YouTrack identifiers, and the importer resolves each one through the (`system`, `id`) index of
  R-EXT-6 before writing anything; a target it cannot resolve is a warning and an omitted relation,
  never an id of a foreign tracker sitting in a field this document says holds an item id.
- **R-YT-5** A re-import **patches**, it does not replace. Only the fields YouTrack actually carried
  are written, the item keeps the id it was allocated, `external` is merged rather than overwritten
  (R-EXT-5), and a comment that already carries its YouTrack reference is not written twice.
- **R-YT-6** Descriptions and comment bodies are **untrusted third-party Markdown**. The importer
  normalises them structurally — attachment embeds become the `.pmngr/attachments/` paths the files
  are downloaded to, and the YouTrack-only `{color:…}` and `{width=…}` extensions are dropped with a
  warning, both exempt inside code fences and code spans — and does nothing else. It does not
  escape, sanitise or rewrap; every renderer sanitises, and every agent treats the text as data
  rather than as instructions (§17.5, docs/02 §10.5).
- **R-YT-7** An imported `attachments[]` entry is a **bare filename**, exactly as §13.4 specifies
  for every other writer: the folder is `.pmngr/attachments/<ITEM-ID>/` by convention, derived from
  the item's own id, and repeating it inside each entry would only create a second place for it to
  be wrong. Whatever the tracker calls a file, only the base name is recorded, so an entry can never
  resolve outside the item's folder. Earlier builds recorded the full vault-relative path and this
  rule documented the divergence; the divergence is now resolved in favour of the model, because
  nothing ever read those paths — the download job builds the folder from the item id. What remains
  true is the timing: only the background import job downloads the binaries, and the synchronous
  `youtrack.import.run` records the names and leaves the files to the job, so an item imported over
  MCP or over the CLI can legitimately list a file that is not on disk yet (`W-ATT-MISSING` until
  the job runs).

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
  > **They are visible to semantic search.** Pando indexes these files in place (docs/21,
  > ADR-036) and keeps only a fixed reserved list of front-matter keys for itself; every other
  > key, `x-` ones included, reaches a hit's `metadata` verbatim through
  > `MergeUnknownFrontMatterKeys`. So an `x-` key is searchable and readable by anything that
  > can query Pando. That is useful — it is how `labels` survives into a hit — and it is also a
  > reason not to put anything confidential in one.

### 13.3 Defaults

`project.yaml:defaults.<type>` supplies `status`, `priority`, `assignees`, `labels` when an item is
created through the UI/CLI/MCP. Defaults are materialised into the file at creation time — they are
**not** applied at read time, so a file always says exactly what it means.

### 13.4 Attachments

- Path: `<docs>/.pmngr/attachments/<ITEM-ID>/<filename>`.
- Front matter `attachments: [sso-sequence.png]` lists *filenames*, resolved relative to that folder.
  Every writer obeys this, importers included: the YouTrack import records bare filenames too
  (R-YT-7), reduced to their base name so an entry can never point outside the folder.
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
| `[[ACME-SP-0003.R2]]` | A requirement block: ref, title and status; anchor `#acme-sp-0003-r2` (§21.3) |
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

### 14.4 Feedback notes on pages

A reviewer can attach notes to lines of a KB page from the web app (docs/05). The notes are written
into the page itself, as one block at the very end of the file, so a person or an agent reading the
page reads the feedback next to the text it is about (ADR-030):

````markdown
<!-- gintrack:feedback:begin -->

---

## Feedback

<!-- gintrack:feedback:note id="fb-1c0e5b2a" anchor="sha256:4e1b9c0d7a3f2e61" lines="3-4" author="Jose F. Rives Lirola" email="jose@digio.es" created="2026-09-11T10:00:00Z" -->
### Jose F. Rives Lirola feedback: fb-1c0e5b2a

> Lines 3–4:
>
> Run the migration first.
> Then restart the workers.

Selected: “the migration”

Which migration? There are two in this release.

<!-- gintrack:feedback:end -->
````

- **R-FB-1** `lines` are 1-based lines of the page **body** — the Markdown after the front matter,
  with the blank lines around it trimmed, exactly as the `body` of a KB page read returns it.
- **R-FB-2** `anchor` is `sha256:` plus the first 16 hex digits of the SHA-256 of the referenced
  lines, each trimmed, joined with `\n`, the whole trimmed. `id` is `fb-` plus 8 hex digits, unique
  within the page.
- **R-FB-3** A note is anchored to the page content only — never to the block itself — and a note
  with no text, or one that refers only to blank lines, is refused.
- **R-FB-4** Every write of a page through the core prunes the block: a note whose anchor matches no
  run of the same number of lines anywhere in the content is removed, a note whose text moved has its
  `lines` rewritten to the matching run closest to where it was, and the block is removed when no
  note is left. A page without a block is never touched. Feedback therefore never outlives the text
  it was about.
- **R-FB-5** The block is recognised only when its begin marker is outside a code fence and its end
  marker is the last non-blank line of the file; the markers are HTML comments, so a renderer shows
  only the `## Feedback` section. Free text in a note cannot open or close an HTML comment (`<!--`
  and `-->` are escaped), so a note cannot forge a marker.

Feedback on a backlog item is not written into the item: it is posted as an ordinary comment
(§11), with the quoted text and the note in its body.

### 14.5 Publishing a page as a YouTrack article

A knowledge-base page and a YouTrack article are the same document written twice, and the transform
between them lives in `internal/youtrack/mapping` beside the issue mapper. It matters to this
document rather than to the API reference because it decides what of a page's *stored form* crosses
the boundary and what stays here — and everything that stays here is something a reader of the
Markdown can see and a merge can conflict on.

Five rules decide it, once, so that publishing and pulling cannot drift apart:

- **R-KB-1 The title lives in exactly one place.** YouTrack keeps it in the article's `summary`, and
  the article body never carries it as an H1. Going up, a leading H1 is removed; coming down, the
  summary is written to the page's `title` front-matter key and nothing is prepended to the body. A
  round trip therefore cannot end with the title twice, which is what happens to every naive copy.
  A page with neither a title nor a leading H1 produces an empty summary and a warning saying that
  YouTrack will not create an article without one; an H1 that *disagrees* with the title publishes
  the title, drops the H1 and warns.
- **R-KB-2 The `## Feedback` block never leaves the repository.** It is local review commentary
  (§14.4, ADR-030), it is stripped from every outgoing payload, and it is put back unchanged on the
  way down. Because R-FB-4 rewrites the block on every write, its absence upstream is never evidence
  of a remote change — which is exactly the trap a byte comparison falls into.
- **R-KB-3 Front matter is stripped going up and rebuilt coming down.** What YouTrack stores is
  Markdown only. The page's front matter is the local side's business and is carried over key for
  key, with `title` refreshed from the summary and this system's `external` entry refreshed in
  place; every other key, including another system's `external` entry, survives untouched.
- **R-KB-4 A wikilink becomes an article link only when its target is already published.** Anything
  else degrades to the text the link displayed, with a warning. A published article must not carry a
  link that resolves nowhere, and a `[[…]]` means nothing outside this vault.
- **R-KB-5 An attachment is addressed by file name.** YouTrack resolves an image or a link target
  against the article's own attachments rather than against a URL, so local references become bare
  file names on the way up and are put back to the paths the page used on the way down. A base name
  that two different local references share is ambiguous and is left alone, so a pull never moves a
  file.

The YouTrack Markdown extensions `{color:red}…{color}` and `{width=300px}` are **passed through**
here, and preserved with a warning coming back, so that a round trip is byte-stable. That is the
opposite of what an issue description gets (R-YT-6, which strips both), and the reason is the
lifecycle rather than the syntax: an issue description is imported once and then belongs to us, while
a page is round-tripped and still belongs to both sides.

### 14.6 Deciding who changed

Publishing and pulling are the same problem in two directions — *who edited since we last agreed?* —
and both answer it the same way, from the page's own `external` entry:

- `key` holds the **fingerprint of the content that was last synchronized**, and `synced_at` when
  that was. This is the one place the model uses `key` for something other than §12.5's external
  project key; a page has no project key to record, and a fingerprint that travels beside the
  article id is a fingerprint that cannot be separated from it.
- The fingerprint is taken over what actually crosses the boundary — front matter stripped, feedback
  block stripped, title in the summary — never over the file's bytes. Comparing bytes would report a
  remote edit every time somebody adds a local note (R-FB-4, R-KB-2), and "out of date" would become
  permanent.
- Both sides are compared against the recorded fingerprint rather than against each other. That is
  what makes *both changed* distinguishable from *one changed* at all.

| State | Meaning |
|---|---|
| `unlinked` | the page carries no `external` entry for this system |
| `in_sync` | content and summary match the article |
| `local_ahead` | the page moved since the recorded fingerprint |
| `remote_ahead` | the article moved. Only ever reported when the remote was actually read |
| `conflict` | both moved, or the two differ with no evidence of which one did |

- **R-KB-6** A page published before the fingerprint existed, or linked by hand, has no `key` to
  pivot on and falls back to comparing `updated` timestamps — which is all there is, and is weaker.
- **R-KB-7 Last writer wins per direction; both-changed writes a file and merges nothing.** A
  `conflict` leaves the page **exactly** as it is and writes the incoming content to
  `<page>.conflict.md` beside it, carrying a `conflict_of` front-matter key naming the original. A
  three-way merge of two documents nobody can diff meaningfully is worse than two files a person can
  read side by side, and a silent merge is the worst answer of all. The `youtrack.kb.conflict` event
  announces it (doc 07 §5.6); resolving it is a human editing two files and deleting one.
- **R-KB-8 Asking for status is cheap unless you ask for the remote.** A documentation tree is
  hundreds of pages, so the remote side is read only when the caller opts in; without it the answer
  comes from the page's own `external` entry and the content the page would publish, and no request
  leaves the process.

Publishing and pulling are always **queued**, never performed in the call that asked for them: a
handbook is hundreds of articles, and the retry ladder, the shared rate limit and the journal all
live in the companion's job engine (docs/02 §3.2). Asking for status is the exception — it is a read,
and it answers inline. All three are core-API methods (`youtrack.kb.status`, `.publish`, `.pull`) and
are exposed as the MCP tools `publish_kb_page_to_youtrack` and `sync_kb_page_from_youtrack`; doc 07
§5.5 is the normative reference for their HTTP surface. There is **no web-app screen** for either
direction yet: the only thing the UI shows of them is their jobs passing through the sync queue.

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
- **R-IDX-4** *(ADR-037.)* Spec items appear in `items[]` like any other type, and each
  requirement block adds one row to a separate `requirements[]` array: `ref`, `spec`, `title`,
  `status`, `category`, `rev` (block rev), `trace` and `verified` as stored, and the computed
  inverse links. The title comes from the block heading: the one exception to R-IDX-2, because a
  requirement has no front matter of its own. Coverage state (`untested`/`passing`/`failing`/`suspect`) is **not** part of this
  file; it needs the verification cache and git history (§21.6), which the index does not read.

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
| `E-EXT-FIELDS` | E | An `external` entry is missing `system` or `id`, or `system` is not a short token ([§12.5](#125-external-references)) |
| `E-INBOX-STATUS` | E | `inbox.status` is not one of the five triage states ([§6.4](#64-the-triage-category-and-the-inbox)) |
| `E-INBOX-SNOOZE` | E | `inbox.snoozed_until` missing on a snoozed item, or present on any other |
| `E-INBOX-DUPLICATE` | E | `inbox.duplicate_of` is not an item id, names the item itself, or is missing on a `duplicate` |
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
| `W-EXT-URL` | W | An `external` entry has a `url` that is not `http`/`https` |
| `W-EXT-DUP` | W | The same `(system, id)` pair appears twice, in one file or across two |
| `W-INBOX-CATEGORY` | W | An `inbox` block on an item whose status is not in the `triage` category |
| `W-INBOX-DUP-DEAD` | W | `inbox.duplicate_of` points at an unknown item |

Added by [ADR-037](./adr/ADR-037-specs-with-requirement-blocks.md), not yet emitted
([§21](#21-specs-and-requirement-blocks)):

| Code | Sev | Condition |
|---|---|---|
| `E-REQ-FOREIGN` | E | A requirement heading in a spec names another spec's ref |
| `E-REQ-DUPLICATE` | E | Two blocks in one spec claim the same `R<n>` |
| `E-REQ-STATUS` | E | `requirements.R<n>.status` is a status of the `triage` category (an unknown status is `E-STATUS-UNKNOWN`) |
| `E-REQ-FIELD` | E | A `requirements:` key is not `R<n>`, or `trace`/`verified`/`links` has the wrong shape (bad trace ref, `verified.rev` not a rev, `commit` not hex, `at` not a timestamp, a `links` kind other than `supersedes`/`superseded_by`/`relates_to`) |
| `E-SCHEMA-FEATURE` | E | A spec construct in a `schema: 1` project ([§21.10](#2110-schema-version-2)); `doctor --fix` raises `schema` to 2 |
| `E-LINK-TARGET-TYPE` | E | A link kind used with a target of the wrong type (R-LINK-6) |
| `E-DELTA-OP` | E | A `## Spec Delta` heading has an unknown operation or is malformed |
| `E-DELTA-TARGET` | E | `ADDED` names a requirement, or `MODIFIED`/`REMOVED` names a spec |
| `E-DELTA-REASON` | E | `REMOVED` without a `Reason:` line |
| `E-PROJ-SPECS` | E | Invalid `specs.lint` configuration ([§6.3](#63-validation-rules-for-projectyaml)) |
| `W-REQ-SEPARATOR` | W | Requirement heading uses ` - `, ` -- ` or ` – ` instead of ` — ` |
| `W-REQ-HEADING` | W | A level-3 heading under `## Requirements` that is not a requirement heading |
| `W-REQ-NO-ENTRY` | W | A block with no `requirements:` entry, or an entry with no `status` |
| `W-REQ-ORPHAN-ENTRY` | W | A `requirements:` key with no block |
| `W-DELTA-DANGLING` | W | A Spec Delta targets an unknown spec or block |
| `LINT-REQ-*` | W, configurable | Requirement grammar ([§21.9](#219-grammar-lint-and-specslint)); `off`, `warning` or `error` under `specs.lint` |

`W-MARKER-SYNTAX`, `W-MARKER-DANGLING` and `W-TRACE-BROKEN` (a `trace:` path or symbol that no
longer exists) are emitted by the native trace engine, which reads source code; `internal/core`
cannot, and does not.

The `E-TEAM-*` / `W-TEAM-*` codes belong to `team.yaml` and are catalogued in
[`04-team-repository.md`](./04-team-repository.md) §3.5. They share this catalog's namespace and
the same severity rules: `internal/core` emits both from one `Diagnostic` type.

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
docs/.pmngr/specs/*.md                        # specs and their requirement blocks (ADR-037)
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
  spec.schema.json           # ADR-037
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
    "id":        { "type": "string", "pattern": "^[A-Z][A-Z0-9]{1,9}-(EP|US|T|M|SP)-[0-9]{4,}$" },
    "qualifiedId": { "type": "string", "pattern": "^([A-Z][A-Z0-9]{1,9}/)?[A-Z][A-Z0-9]{1,9}-(EP|US|T|M|SP)-[0-9]{4,}$" },
    "reqRef":    { "type": "string", "pattern": "^([A-Z][A-Z0-9]{1,9}/)?[A-Z][A-Z0-9]{1,9}-SP-[0-9]{4,}\\.R[1-9][0-9]*$" },
    "linkTarget": { "anyOf": [ { "$ref": "#/$defs/qualifiedId" }, { "$ref": "#/$defs/reqRef" } ] },
    "rev":       { "type": "string", "pattern": "^sha256:[0-9a-f]{16}$" },
    "traceRef":  { "type": "string", "pattern": "^(?!/)(?!.*(^|/)\\.\\.(/|$))[^#]+(#.+)?$" },
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
        "kind":   { "enum": ["blocks", "blocked_by", "relates_to", "duplicates", "duplicated_by",
                             "implements", "implemented_by", "modifies", "modified_by",
                             "supersedes", "superseded_by"] },
        "target": { "$ref": "common.defs.json#/$defs/linkTarget" },
        "note":   { "type": "string", "maxLength": 200 }
      }
    },
    "external": {
      "type": "object",
      "required": ["system", "id"],
      "additionalProperties": false,
      "properties": {
        "system":    { "type": "string", "pattern": "^[a-z0-9][a-z0-9._-]{0,31}$" },
        "id":        { "type": "string", "minLength": 1, "maxLength": 200 },
        "url":       { "type": "string", "pattern": "^https?://" },
        "key":       { "type": "string", "maxLength": 64 },
        "synced_at": { "$ref": "common.defs.json#/$defs/timestamp" }
      }
    },
    "inbox": {
      "type": "object",
      "additionalProperties": true,
      "properties": {
        "status":        { "enum": ["pending", "accepted", "rejected", "snoozed", "duplicate"] },
        "snoozed_until": { "$ref": "common.defs.json#/$defs/date" },
        "duplicate_of":  { "$ref": "common.defs.json#/$defs/id" },
        "source":        { "type": "string", "maxLength": 64 },
        "received":      { "$ref": "common.defs.json#/$defs/timestamp" }
      }
    },
    "requirement": {
      "type": "object",
      "additionalProperties": true,
      "properties": {
        "status": { "$ref": "common.defs.json#/$defs/statusId" },
        "trace": {
          "type": "object", "additionalProperties": true,
          "properties": {
            "code":  { "type": "array", "items": { "$ref": "common.defs.json#/$defs/traceRef" } },
            "tests": { "type": "array", "items": { "$ref": "common.defs.json#/$defs/traceRef" } }
          }
        },
        "verified": {
          "type": "object", "additionalProperties": true,
          "required": ["rev", "commit", "at", "by"],
          "properties": {
            "rev":    { "$ref": "common.defs.json#/$defs/rev" },
            "commit": { "type": "string", "pattern": "^([0-9a-f]{40}|[0-9a-f]{64})$" },
            "at":     { "$ref": "common.defs.json#/$defs/timestamp" },
            "by":     { "$ref": "common.defs.json#/$defs/handle" }
          }
        },
        "links": { "type": "array", "items": { "$ref": "common.defs.json#/$defs/link" } }
      }
    }
  }
}
```

`reqRef`, `linkTarget`, `rev`, `traceRef` and `requirement` are added by
[ADR-037](./adr/ADR-037-specs-with-requirement-blocks.md), together with `SP` in `id` and the six
new link kinds. `requirement` is open like `inbox`, for the same reason: unknown keys inside an
entry are preserved (R-FMT-6). Which link kinds may sit in a requirement's `links`, and which target
type each kind accepts, depend on the source and cannot be expressed here; the Go validator reports
them as `E-LINK-TARGET-TYPE`.

`external` and `inbox` are the only two `$defs` whose value objects are not closed the same way:
`external` is `additionalProperties: false` because the shape is fixed, while `inbox` is open
because unknown keys inside the block are preserved on rewrite exactly as unknown top-level keys
are (R-FMT-6, R-EVO-5).

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
    "external":  { "type": "array", "items": { "$ref": "common.defs.json#/$defs/external" } },
    "attachments": { "type": "array", "items": { "type": "string" } },
    "custom":    { "type": "object" },
    "inbox":     { "$ref": "common.defs.json#/$defs/inbox" },
    "deleted":   { "type": "boolean" }
  },
  "patternProperties": { "^x-": true },
  "additionalProperties": false
}
```

Outline of `comment.schema.json`. It is the smallest of the item schemas and the only one whose
`external` list is not about an item at all — it addresses one remark inside a thread (R-CMT-5), so
its `id` is the external system's *comment* identifier, not the issue's:

```jsonc
{
  "$id": "https://git-in-track.dev/schema/comment.schema.json",
  "type": "object",
  "required": ["type", "item", "author", "created"],
  "properties": {
    "type":         { "const": "comment" },
    "item":         { "$ref": "common.defs.json#/$defs/id" },
    "author":       { "$ref": "common.defs.json#/$defs/handle" },
    "author_name":  { "type": "string" },
    "author_email": { "type": "string" },
    "created":      { "$ref": "common.defs.json#/$defs/timestamp" },
    "updated":      { "$ref": "common.defs.json#/$defs/timestamp" },
    "in_reply_to":  { "type": "string" },
    "kind":         { "enum": ["comment", "status_change", "system"] },
    "reactions":    { "type": "object", "additionalProperties": {
                        "type": "array", "items": { "$ref": "common.defs.json#/$defs/handle" } } },
    "external":     { "type": "array", "items": { "$ref": "common.defs.json#/$defs/external" } },
    "attachments":  { "type": "array", "items": { "type": "string" } }
  },
  "patternProperties": { "^x-": true },
  "additionalProperties": false
}
```

`external` reuses the shared `$def` unchanged. Nothing in the schema distinguishes a comment
reference from an item reference, and nothing should: the shape is identical, and which artifact an
entry names is decided by the file it sits in.

Outline of `spec.schema.json` (ADR-037). It has no planning fields — a spec is a living
description, not scheduled work — and it is the only schema with `requirements`:

```jsonc
{
  "$id": "https://git-in-track.dev/schema/spec.schema.json",
  "type": "object",
  "required": ["id", "type", "title", "status", "created", "updated"],
  "properties": {
    "id":        { "pattern": "^[A-Z][A-Z0-9]{1,9}-SP-[0-9]{4,}$" },
    "type":      { "const": "spec" },
    "title":     { "type": "string", "minLength": 1, "maxLength": 200 },
    "status":    { "$ref": "common.defs.json#/$defs/statusId" },
    "priority":  { "$ref": "common.defs.json#/$defs/priority" },
    "assignees": { "type": "array", "items": { "$ref": "common.defs.json#/$defs/handle" } },
    "author":    { "$ref": "common.defs.json#/$defs/handle" },
    "labels":    { "type": "array", "items": { "$ref": "common.defs.json#/$defs/label" } },
    "created":   { "$ref": "common.defs.json#/$defs/timestamp" },
    "updated":   { "$ref": "common.defs.json#/$defs/timestamp" },
    "started":   { "$ref": "common.defs.json#/$defs/timestamp" },
    "closed":    { "$ref": "common.defs.json#/$defs/timestamp" },
    "links":     { "type": "array", "items": { "$ref": "common.defs.json#/$defs/link" } },
    "external":  { "type": "array", "items": { "$ref": "common.defs.json#/$defs/external" } },
    "attachments": { "type": "array", "items": { "type": "string" } },
    "custom":    { "type": "object" },
    "requirements": {
      "type": "object",
      "propertyNames": { "pattern": "^R[1-9][0-9]*$" },
      "additionalProperties": { "$ref": "common.defs.json#/$defs/requirement" }
    },
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
  Writes are refused by the vault from the release that introduces specs onward; binaries up to
  2.0.1 only report `E-PROJ-SCHEMA` ([§21.10](#2110-schema-version-2), R-SCHEMA-2-4).
- **R-EVO-3** Additive changes that a previous version ignores or round-trips (a new optional
  field, R-EVO-5) do not bump `schema`. Renames, removals, changes in file layout, **and additive
  changes that a previous version's validator rejects** (a new value in a closed enum such as the
  link kinds, a wider ID or link-target grammar) do: the older client must see one unknown schema
  and fall back read-only (R-EVO-2) rather than fail file by file.
- **R-EVO-4** `gintrack migrate` performs a version bump in one commit, with a dry-run mode and a
  printed diff summary.
- **R-EVO-5** Unknown keys are always preserved on rewrite, which makes forward-compatible round
  trips safe for tools built by others.
- **R-EVO-6** *(ADR-037.)* The spec layer of
  [ADR-037](./adr/ADR-037-specs-with-requirement-blocks.md) bumps `schema` to **2** under R-EVO-3:
  its new link kinds and requirement-ref targets would otherwise be rejected by older binaries as
  `E-ENUM` / `E-ID-GRAMMAR`, story by story. The bump is per project and on first use — the first
  write that introduces a spec construct also sets `schema: 2` — and needs no content migration
  ([§21.10](#2110-schema-version-2)). Projects without specs stay at `schema: 1`.

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
| Phase 6 | Metrics derived from `category`, dates and estimates as they stood at each point in the **git history of the item files** — burndown, cumulative flow, cycle time, lead time, throughput. No new field, no stored time series: see [ADR-017](./adr/ADR-017-metrics-history-from-git-not-a-stored-time-series.md) and [doc 04 §12](./04-team-repository.md). |
| Phase 11 | Specs and requirement blocks, requirement IDs and revs, the `requirements:` map, the spec link kinds, markers, Spec Delta and `specs.lint` — [§21](#21-specs-and-requirement-blocks), [ADR-037](./adr/ADR-037-specs-with-requirement-blocks.md). |

---

## 21. Specs and requirement blocks

> **Status: accepted** by [ADR-037](./adr/ADR-037-specs-with-requirement-blocks.md) (2026-09-24),
> not yet implemented: the implementation story is `GIT-US-0105`. This section is the normative
> format; the ADR records the reasoning, the consequences and the alternatives rejected. Using specs raises the project to `schema: 2` ([§21.10](#2110-schema-version-2)).

**Path:** `.pmngr/specs/<KEY>-SP-<NNNN>-<slug>.md`

A spec describes one **capability**. Its requirements are **blocks in its body**, not files, but
each requirement behaves as a separate unit everywhere: its own ID, status, rev, trace,
verification stamp, row in lists, search and coverage, MCP address and search anchor.

### 21.1 Front matter and body

| Field | Type | Req. | Notes |
|---|---|---|---|
| `id` | ID (`SP`) | yes | allocated per §4.1; `id_allocation.counters.spec` is its hint |
| `type` | `spec` | yes | |
| `title` | string (1..200) | yes | the capability, e.g. `Item ID allocation` |
| `status` | status id | yes | the project workflow, like every item |
| `priority`, `assignees` (owners), `author`, `labels` | as elsewhere | no | |
| `created`, `updated`, `started`, `closed` | timestamps | as elsewhere | |
| `links`, `external`, `attachments`, `custom`, `deleted` | as elsewhere | no | |
| `requirements` | mapping `R<n>` → entry | no | [§21.4](#214-the-requirements-map) |

A spec has **no** `parent`, `epic`, `milestone`, `sprint`, `estimate`, `effort`, `spent`, `due` or
`inbox`: it is a living description, not scheduled work. Planning happens on the stories that
`implements` or `modifies` it. A spec is **not an inbox target**: it never takes a
`triage`-category status, and the inbox tools never create or triage one.

Body conventions:

```
## Purpose        what the capability is for
## Scope          what it covers and what it does not
## Glossary       optional; terms the requirements use
## Requirements   the requirement blocks, in any order
## Notes          optional
```

Complete example:

```markdown
---
id: ACME-SP-0003
type: spec
title: Item ID allocation
status: done
assignees: [jose]
author: jose
labels: [backend]
created: 2026-09-24T12:00:00Z
updated: 2026-10-01T09:12:00Z
requirements:
  R1:
    status: done
  R2:
    status: in_progress
    trace:
      code: [internal/core/allocator.go#NextID]
      tests: [internal/core/allocator_test.go#TestNextID/stale_counter]
    verified: {rev: "sha256:4e1b9c0d7a3f2e61", commit: 9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21, at: 2026-10-01T09:12:00Z, by: claude}
---

## Purpose

Give every item a short, permanent, human-speakable id without a coordination service.

## Requirements

### ACME-SP-0003.R1 — IDs are never reused

The allocator SHALL NOT assign a number that any existing, deleted or reserved item of the same
type holds.

#### Scenario: a deleted item keeps its number
- **WHEN** `ACME-T-0107` is marked `deleted: true`
- **THEN** no later task is allocated `ACME-T-0107`

### ACME-SP-0003.R2 — Allocate the next ID by index scan

WHEN an item of type T is created, the allocator SHALL assign `max(existing numbers of T) + 1`.

#### Scenario: a stale counter hint is ignored
- **WHEN** `id_allocation.counters.task` is 12 and the scan finds 108
- **THEN** the next task is `ACME-T-0109`
```

### 21.2 Requirement blocks

```
<req-heading> ::= "### " <REQREF> " — " <title>        # U+2014 EM DASH; title 1..200 chars
```

- **R-REQ-1** A requirement heading is a level-3 ATX heading at column 0, outside a fenced code
  block. Its ref MUST name the spec it is in (`E-REQ-FOREIGN`). ` – `, ` - ` and ` -- ` are accepted
  on read as `W-REQ-SEPARATOR`; writers emit the em dash and never rewrite a body only to fix it.
  Any other level-3 heading under `## Requirements` is `W-REQ-HEADING`.
- **R-REQ-2** The first paragraph after the heading is the **statement**: an EARS pattern
  (`The <system> SHALL …`, `WHEN …`, `WHILE …`, `IF … THEN …`, `WHERE …`) or a `SHALL` sentence.
  It is followed by zero or more `#### Scenario: <name>` sub-blocks listing `**GIVEN**` (optional),
  `**WHEN**`, `**AND**` and `**THEN**` steps. The grammar is linted ([§21.9](#219-grammar-lint-and-specslint)),
  never a parse gate.
- **R-REQ-3 Block extent.** On the canonical body (R-REV-1 applied, front matter removed), a block
  starts at the first byte of its heading line and ends just before the next line, outside a fenced
  code block, that is an ATX heading of level 1–3, or at the end of the body. `####` and deeper
  headings belong to the block; setext headings are not boundaries.

### 21.3 Requirement IDs

- **R-REQ-4** A requirement is addressed as `<SPEC-ID>.R<n>` (R-ID-5). The number is permanent:
  never renumbered, never reused, never reassigned, including after removal. Gaps are normal.
- **R-REQ-5 Allocation.** The next number is `max + 1` over every block heading **and** every
  `requirements:` key of that spec **and** every ref to that spec in the project index (link
  targets, Spec Delta headings). Two blocks with one number are `E-REQ-DUPLICATE`.
- **R-REQ-6 Removal and moves.** Removing a requirement moves its status to a `cancelled`-category
  status and keeps block and number. Moving one to another spec allocates a new ref there, records
  `supersedes` from the new to the old ref in the new entry's `links`, and removes the old one.
- **R-REQ-7 Anchor.** A block's anchor is its ref lower-cased with `.` replaced by `-`
  (`#acme-sp-0003-r2`). Search hits, the web app and wikilinks ([§14.1](#141-syntax)) resolve to it.

### 21.4 The `requirements:` map

| Key | Type | Notes |
|---|---|---|
| `status` | status id | project workflow; a `triage`-category status is `E-REQ-STATUS`; absent reads as `workflow.initial` (`W-REQ-NO-ENTRY`) |
| `trace.code` | list of `<path>[#<symbol>]` | code that realises the requirement and cannot carry a marker |
| `trace.tests` | list of `<path>[#<symbol>]` | tests that verify it when a marker is impractical |
| `verified` | mapping | the durable verification stamp, written only when implementing work reaches `done` or by `gintrack spec verify --commit` (R-REQ-11a); ordinary runs go to the local cache (R-REQ-11) |
| `verified.rev` | block rev | the text that was verified ([§21.5](#215-block-rev-and-requirement-rev)) |
| `verified.commit` | full hex commit id | the commit at which every linked test passed (the git commit id in a Jujutsu repository) |
| `verified.at` | timestamp | UTC RFC 3339, R-TIME-1; when the recorded run happened, not when the stamp was written |
| `verified.by` | handle | who ran the recorded verification |
| `links` | list of `{kind, target}` | same shape as item `links`; only `supersedes`, `superseded_by`, `relates_to` (R-REQ-13) |

- **R-REQ-8** Trace paths are relative to the **repository root**, `/`-separated, with no `..`. A
  path without `#symbol` means the whole file. A symbol is the language's identifier path
  (`NextID`, `Store.Put`, `TestNextID/stale_counter`; for TS/JS the function name or the
  `describe > it` path).
- **R-REQ-9** A key with no block is `W-REQ-ORPHAN-ENTRY` (and keeps its number reserved); a block
  with no key is `W-REQ-NO-ENTRY`. Keys are emitted in numeric order (`R2` before `R10`); inside an
  entry the order is `status, trace, verified, links`, then unknown keys sorted. Unknown keys at any
  level of an entry are preserved (R-FMT-6).
- **R-REQ-10** `status` says where the requirement is in its life. Whether it is tested and holding
  is the computed coverage state of [§21.6](#216-verification-and-coverage), which is never stored.
- **R-REQ-13 Requirement links.** `requirements.R<n>.links` holds relations whose source is that
  requirement, as `{kind, target}` entries shaped like item links (§12.1). `supersedes` and
  `superseded_by` MUST target a requirement ref (optionally `<KEY>/`-qualified); `relates_to` MAY
  target a requirement ref, a spec or any item. Any other kind is `E-REQ-FIELD`; a wrong target
  type is `E-LINK-TARGET-TYPE`. Links are stored on one side only (R-LINK-1).

### 21.5 Block rev and requirement rev

Two hashes per requirement, both `"sha256:" + lowercase_hex(sha256(x))[0:16]` like R-REV-1:

- **R-REQ-REV-1 Block rev** — `x` is the block extent (R-REQ-3) with trailing blank lines removed
  and exactly one `\n` appended. It covers the heading, statement and scenarios and nothing else.
  It is what `verified.rev` records and what suspect compares. Editing another block, front matter,
  or the blank lines between blocks does not change it.
- **R-REQ-REV-2 Requirement rev** — `x` is the same bytes followed by the canonical JSON (UTF-8,
  sorted keys, no insignificant whitespace) of the requirement's map entry, `{}` when absent. It is
  the **write token** of a single requirement: every requirement read returns it, every requirement
  write quotes it, and a mismatch fails with `stale_revision` exactly as §5 specifies, `conflicts[]`
  naming `text`, `title`, `status`, `trace`, `verified` or `links`. A write to one requirement never
  invalidates another requirement's rev.
- **R-REQ-REV-3** The file `rev` (§5) is unchanged; spec-level writes quote it. Neither requirement
  hash is ever stored, except the block rev as the value of `verified.rev`.

### 21.6 Verification and coverage

- **R-REQ-11 Verification cache.** Every `gintrack spec verify` run and MCP `verify_requirement`
  call records its results in a local, derived **verification cache** and writes nothing into the
  spec. One entry per requirement per run holds `ref`, `rev` (the block rev tested), `commit` (full
  hex id of `HEAD`), `tests` (the test ids run: `Verifies:` markers ∪ `trace.tests`), `result`
  (`pass`|`fail`), `at` (UTC RFC 3339) and `by`. A run is `pass` only if every linked test passed
  and the traced files are unchanged in the working tree relative to `commit`; a run on a dirty
  tree is not recorded. The cache lives beside the index cache — `<docs>/.pmngr/verify.json`
  natively, git-ignored (R-LOC-5); the same IndexedDB database as the WASM index in the browser,
  where it is normally empty because tests cannot run there. It is never committed, never the
  source of truth, and may be deleted at any time (R-IDX-1).
- **R-REQ-11a Durable stamp.** `verified` is the only state written back into a spec by running
  something, and only (a) when a story or task that `implements` or `modifies` the requirement
  moves to a `done`-category status — the same write that applies its Spec Delta (§21.8) copies
  `rev`, `commit`, `at`, `by` from the most recent `pass` cache entry whose `rev` equals the
  requirement's current (post-apply) block rev; with no such entry no stamp is written, the move
  is not refused, and the result lists the unstamped requirements — or (b) by
  `gintrack spec verify --commit`, intended for CI on `main`, which stamps every requirement whose
  run passed (one write per spec, quoting requirement revs; the caller commits). A `fail` never
  overwrites a stamp. A hand-written stamp means what its author says.
- **R-REQ-12 Coverage.** The coverage state — `untested`, `passing`, `failing`, `suspect` — is
  **computed, never stored**, from the cache first and the stamp as the durable baseline. The
  evidence is the most recent cache entry whose `rev` equals the current block rev and whose `at`
  is later than `verified.at`, else the stamp. A `fail` entry → `failing`; `pass` evidence →
  `passing`, or `suspect` if a traced file or symbol changed between its `commit` and `HEAD`,
  directly or transitively (Pando); a stamp whose `rev` differs from the current block rev, with
  no newer matching entry → `suspect`; neither → `untested`. **An empty cache falls back to the
  stamp**, so each requirement shows its last durable state. No file carries a `suspect`,
  `coverage` or test-result key; the marker scan is a derived cache too.

### 21.7 In-code markers

```
<marker-line> ::= <ws>* <opener> <ws>* ("Implements" | "Verifies") ":" <ws>+ <mref>
                  (<ws>* "," <ws>* <mref>)* <ws>* [","] <ws>* [<closer>] <ws>* EOL
<mref>        ::= [<KEY> "/"] <REQREF>
<opener>      ::= "//" | "#" | "--" | "/*" | "/**" | "*" | "<!--" | ""
<closer>      ::= "*/" | "-->"
```

| Comment syntax | File types |
|---|---|
| `//`, `/* */` (`*` or nothing on a continuation line) | `.go .ts .tsx .js .jsx .mjs .cjs .java .kt .swift .c .h .cc .cpp .hpp .cs .rs .scala .dart .php .css .scss .vue .svelte` |
| `#` | `.py .rb .sh .bash .zsh .pl .r .yaml .yml .toml .tf`, `Makefile`, `Dockerfile` |
| `--` | `.sql .lua` |
| `<!-- -->` (nothing on a continuation line) | `.md .html .htm .xml .svg .vue .svelte` |

- **R-MARK-1 Match rule.** A line is a marker only if the whole line matches `<marker-line>` for a
  comment syntax of its file type: the opener is the first non-whitespace text (a trailing comment
  after code is not a marker; the empty opener is valid only inside an already open block
  comment), the keyword is case-sensitive and directly followed by `:`, and after it come one or
  more complete, comma-separated requirement refs followed by nothing but whitespace, one optional
  trailing comma and the comment closer. A line whose opener and keyword match but whose ref list
  does not is `W-MARKER-SYNTAX` and contributes nothing. File types outside the table, everything
  under `.pmngr/`, and fenced code blocks in Markdown are not scanned. A marker inside a string
  literal is not a marker (the Go scanner uses the Go lexer; other languages the line rule).
- **R-MARK-2** A marker in the comment block directly before a declaration attaches to it; inside a
  declaration's body, to the enclosing declaration; before the first declaration, or in a file type
  without declarations, to the whole file.
- **R-MARK-3** `Implements:` adds code to the requirement's trace, `Verifies:` adds tests. Markers
  and `trace:` entries are **unioned**; neither overrides the other.
- **R-MARK-4** Markers name requirements only; a bare spec ID makes the line malformed
  (`W-MARKER-SYNTAX`), an unknown ref is `W-MARKER-DANGLING`. Markers are scanned by the native
  trace engine, never by `internal/core`. Adding a file type or comment syntax to the table is an
  additive scanner change, not a data-model change.

### 21.8 `## Spec Delta`

A story or task proposes changes to a living spec in a `## Spec Delta` section of its own body. The
spec changes only when the story reaches a `done`-category status.

```
<delta-heading> ::= "### " ("ADDED" <ws> <SPEC-ID> | ("MODIFIED" | "REMOVED") <ws> <REQREF>) " — " <title>
```

- **R-DELTA-1** `ADDED <SPEC-ID>` carries a complete new block without a number; an optional
  `Supersedes: <REQREF>` line directly under the heading makes it a move (R-REQ-6). On apply the
  number is allocated, the block is appended to the spec, and the heading in the story is rewritten
  to `### ADDED <REQREF> — …`.
- **R-DELTA-2** `MODIFIED <REQREF>` carries the complete replacement text; applying it changes the
  block rev, so an existing stamp becomes suspect until re-verified.
- **R-DELTA-3** `REMOVED <REQREF>` carries a mandatory `Reason:` line; applying it moves the
  requirement to the first `cancelled`-category status and keeps the block.
- **R-DELTA-4** Until applied, each `MODIFIED`/`REMOVED` target is indexed as a `modifies` relation
  of the story; after applying, the story carries `implements`/`modifies` links in front matter.
- **R-DELTA-5** Diagnostics: `E-DELTA-OP`, `E-DELTA-TARGET`, `E-DELTA-REASON`, `W-DELTA-DANGLING`
  ([§16](#16-validation-rules-consolidated)); added and replacement blocks are linted like any block.

### 21.9 Grammar lint and `specs.lint`

```yaml
specs:
  lint:
    severity: warning        # off | warning (default) | error
    rules:                   # optional per-rule override, same three values
      LINT-REQ-VAGUE: error
      LINT-REQ-SCENARIO: off
    vague_words: [fast, quickly, user-friendly, easy, as appropriate, as needed, etc, robust]
```

| Rule | Checks |
|---|---|
| `LINT-REQ-STATEMENT` | the statement is an EARS pattern or contains `SHALL` |
| `LINT-REQ-SCENARIO` | at least one `#### Scenario:` |
| `LINT-REQ-WHEN-THEN` | each scenario has a `**WHEN**` and then a `**THEN**` |
| `LINT-REQ-VAGUE` | a word of `vague_words` (whole word, case-insensitive) in the statement or a scenario |
| `LINT-REQ-MULTI` | more than one `SHALL` in the statement |

- **R-LINT-1** Findings carry the requirement ref, the rule and a line. At `off` the rule does not
  run and reports nothing; at `warning` findings never block anything; at `error` they are validation errors of the spec (writes through the API and MCP
  are refused, `gintrack doctor` exits non-zero).
- **R-LINT-2** `vague_words`, when present, replaces the built-in list. A per-rule value wins over
  `severity` in both directions. `specs.lint: <value>` (a scalar) is shorthand for
  `specs.lint: {severity: <value>}`. A value other than `off`, `warning`, `error`, or an unknown
  rule, is `E-PROJ-SPECS`.
- **R-LINT-3** The linter lives in `internal/core` and runs in the browser through WASM, so
  authoring and lint work in browser-only mode; impact and coverage, which need git and test
  results, answer `unavailable` there.

### 21.10 Schema version 2

- **R-SCHEMA-2-1** A project that contains a **spec construct** MUST declare `schema: 2`. Spec
  constructs are: a file under `specs/`; a link, item-level or in `requirements.R<n>.links`, of
  kind `implements`, `implemented_by`, `modifies`, `modified_by`, `supersedes` or
  `superseded_by`; a link target that is an `SP` ID or a requirement ref. `## Spec Delta`
  sections, requirement wikilinks, in-code markers and the `specs:` key are not spec constructs:
  older binaries read them as prose, a broken wikilink, a comment, or a preserved unknown key.
- **R-SCHEMA-2-2** A binary that supports ADR-037 reads and writes `schema: 1` and `schema: 2`,
  and still creates new projects at `schema: 1` (§2.2). In a `schema: 1` project each spec
  construct is `E-SCHEMA-FEATURE` on its file; the project stays writable and the construct is
  still indexed.
- **R-SCHEMA-2-3 Upgrade on first use.** The first write through the vault (web app, CLI, MCP or
  WASM) that introduces a spec construct into a `schema: 1` project also rewrites that line of
  `project.yaml` to `schema: 2` in the same write (committed together under commit-on-save), quoting
  the file `rev` of `project.yaml` as usual, and reports `schemaUpgraded: 2` in its result. Nothing
  is upgraded on open, on read, or on a write without a spec construct. `gintrack doctor --fix`
  performs the same one-line edit for constructs written by hand. No other file changes, so no
  `gintrack migrate` step is needed (R-EVO-4), and no `gintrack spec init` step is required;
  there is no downgrade. A reviewer who sees the one-line `schema` change in a spec PR traces it
  to the `schemaUpgraded: 2` the author's tool reported.
- **R-SCHEMA-2-4 Older binaries.** A binary whose supported schema is `1` reports `E-PROJ-SCHEMA`
  on a `schema: 2` project and opens it read-only (R-EVO-2), ignoring `specs/`. Released binaries
  up to 2.0.1 emit the diagnostic but do not gate writes on it and may still write. The write gate
  — every vault write refused on a missing or newer `schema` — ships in the same release that
  introduces specs (`GIT-US-0105`), with no 2.0.2 backport, and that release's upgrade note says
  every writing binary, web build and CI job must be upgraded before a project's first spec
  construct.
