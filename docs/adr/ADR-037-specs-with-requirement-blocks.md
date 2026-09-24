# ADR-037 — Specs are items; requirements are addressable blocks inside them

- **Status:** Accepted — 2026-09-24, decided by the human maintainer after two review rounds (see
  "Decisions from review"). Core implemented by `GIT-US-0105` (the `spec` type, requirement
  blocks and revs, the `requirements:` map, schema 2 and the write gate); the link kinds and
  requirement-ref link targets of §5 by `GIT-US-0106`; lint, Spec Delta, verification and
  markers follow in their own stories (docs/03 §21 banner).
- **Date:** 2026-09-24
- **Phase:** 11 (Spec-driven development)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-003](ADR-003-shared-go-core-wasm.md),
  [ADR-008](ADR-008-id-scheme.md), [ADR-012](ADR-012-comments-as-separate-files.md),
  [ADR-017](ADR-017-metrics-history-from-git-not-a-stored-time-series.md),
  [ADR-033](ADR-033-inbox-is-a-reserved-triage-status-category.md),
  [ADR-036](ADR-036-pando-indexes-the-repository-directly.md)
- **Extends:** ADR-008 — adds the type code `SP` and the requirement-ref grammar. ADR-008's
  decision (per-project, per-type counters by index scan; immutable IDs) is unchanged and
  applies to specs as written.
- **Known discrepancy, out of scope:** ADR-008 lists the type codes `TSK` and `MS`, while the code
  and docs/03 §3.3 use `T` and `M`. This ADR follows the code and does not amend ADR-008.
- **Implements:** `GIT-US-0104` (epic `GIT-EP-0022`, milestone `GIT-M-0015`)
- **Research:** [Spec-driven development overview](../research/2026-09-24-spec-driven-development-overview.md),
  validated through `GIT-T-0238`; §7 "Decisions (2026-09-24)" is the input to this ADR.

## Context

Agents that work from Markdown specs share three blind spots whatever tool they use: they cannot
tell which requirements a change touches (**impact**), whether each requirement is tested and
passing (**coverage**), or whether it still holds after the change (**validity**). The common root
cause is that none of those tools gives a requirement a stable identity, a place in an index, or a
link to the code and tests that realise it. Drift is then found by asking an LLM to re-read code
and spec, which is expensive, non-deterministic and not incremental.

git-in-track already has most of the machinery: permanent IDs allocated by scan (ADR-008), typed
links, `rev` content hashes and the stale-revision protocol (docs/03 §5), an MCP surface with
projection and pagination, and Pando for semantic search and code-symbol impact (ADR-036). What is
missing is the spec layer, and the reviewer of `GIT-T-0238` fixed its shape:

1. Requirements are **blocks inside one `spec` file**, not files of their own, yet each behaves as
   a separate unit everywhere (status, rev, trace, verification, list row, MCP address, search hit).
2. Code and tests point at requirements with **in-code markers**, plus an optional `trace:` entry.
3. Requirements reuse the **project workflow**; there is no per-type workflow.
4. Grammar lint **warns by default** and a project can raise it to an error.
5. The type is `spec` / `SP`, the new link kinds are `implements`, `modifies` and `supersedes`
   with their inverses, and link targets accept requirement refs.

Data-model paths, ID formats and front-matter fields are a human-only area until 1.0 (AGENTS.md),
so the format is written down and approved here before any code changes it.

Forces that shape the details below:

- **Merge-friendliness** (docs/03 §1): a one-field change must be a one-line diff, and two people
  editing two different requirements of one spec must not invalidate each other's writes.
- **Human-editable without the tool**: a person with `vim` must be able to add a requirement.
- **The WASM core** (ADR-003): parsing, IDs and lint must compile to `GOOS=js`; nothing that
  walks source code, runs tests or reads git history may enter `internal/core`.
- **Files are the only truth**: everything computed — suspect, test results, the marker scan —
  must be rebuildable from the repository plus a test run, and must never be written back as if
  it were a fact.

## Decision

### 1. The `spec` item type

- New item type **`spec`**, type code **`SP`**, folder **`<docs>/.pmngr/specs/`**, file name
  `<KEY>-SP-<NNNN>-<slug>.md` exactly like every other item (docs/03 §3.4). IDs are allocated per
  project and per type by index scan (ADR-008, docs/03 §4.1); `id_allocation.counters` gains the
  key `spec`.
- A spec describes one **capability**. Its body holds, by convention, `## Purpose`, `## Scope`
  and `## Glossary`, followed by `## Requirements` containing the requirement blocks, and an
  optional `## Notes`. Only the requirement blocks are interpreted structurally.
- Front matter is the common item set — `id`, `type`, `title`, `status`, `priority`,
  `assignees` (owners), `author`, `labels`, `created`, `updated`, `started`, `closed`, `links`,
  `external`, `attachments`, `custom`, `deleted` — plus the new **`requirements`** map (§4). A spec
  is a living description, not scheduled work: `parent`, `epic`, `milestone`, `sprint`,
  `estimate`, `effort`, `spent`, `due` and `inbox` are **not** spec fields. Planning happens on
  the stories that implement or modify it. A spec is **not an inbox target**: it is never created
  in, nor moved to, a `triage`-category status, and `create_inbox_item`/`triage_inbox_item` never
  produce one (decided in review, see "Decisions from review").
- `requirements` is placed in the canonical key order after `inbox` and before `deleted`
  (docs/03 §3.2): it is the largest block and only spec files carry it, so it sits at the end
  where unrelated edits of the identity/classification region do not touch it.

### 2. Requirement blocks

A requirement is a level-3 ATX heading in the spec body followed by its content:

```markdown
### GIT-SP-0003.R2 — Allocate the next ID by index scan

WHEN an item of type T is created, the allocator SHALL assign
`max(existing numbers of T) + 1`, counting deleted items and reserved ranges.

#### Scenario: a hand-created high number is respected
- **WHEN** the project holds `GIT-T-0100` and a hand-written `GIT-T-0500`
- **THEN** the next task is `GIT-T-0501`

#### Scenario: a stale counter hint is ignored
- **WHEN** `id_allocation.counters.task` is 12 and the scan finds 108
- **THEN** the next task is `GIT-T-0109`
```

**Heading grammar**

```
<req-heading> ::= "### " <REQREF> " " <sep> " " <title>
<sep>         ::= "—"                       # U+2014 EM DASH, canonical
<title>       ::= 1..200 characters, no line break
```

- The heading MUST start at column 0, outside a fenced code block, and its `<REQREF>` MUST name
  the spec it is in. A ref naming another spec is `E-REQ-FOREIGN`.
- ` – ` (en dash) and ` - ` / ` -- ` are accepted on read and reported as `W-REQ-SEPARATOR`;
  every tool that writes a block emits the em dash. The body is never rewritten just to fix it
  (R-FMT-6).
- A level-3 heading inside `## Requirements` that does not match the grammar is
  `W-REQ-HEADING` — it is prose, not a requirement, and it ends the previous block.

**Statement.** The first paragraph after the heading is the **statement**: one EARS pattern —
ubiquitous (`The <system> SHALL …`), event-driven (`WHEN <trigger>, the <system> SHALL …`),
state-driven (`WHILE <state>, …`), unwanted behaviour (`IF <condition>, THEN the <system> SHALL …`),
optional feature (`WHERE <feature>, …`) — or a plain `SHALL` sentence. The keywords are uppercase.

**Scenarios.** Zero or more `#### Scenario: <name>` sub-blocks, each a list of `**WHEN**`,
`**AND**` and `**THEN**` steps (a `**GIVEN**` step is allowed before the first `**WHEN**`). The
grammar is **linted, never parsed as a gate** (§10): a block that breaks it is still a requirement
with an ID, a rev and a status.

**Block extent (exact).** On the canonical body — the file canonicalised per R-REV-1 (BOM
removed, CRLF → LF), front matter and its closing `---\n` removed — a block starts at the first
byte of its heading line and ends immediately before the first later line, outside a fenced code
block, that is an ATX heading of level 1, 2 or 3 (`#`, `##` or `###` followed by a space or the
end of the line), or at the end of the body. Level-4 and deeper headings belong to the block.
Setext headings are not block boundaries.

### 3. Requirement IDs

```
<SPEC-ID> ::= <KEY> "-SP-" <NUMBER>                # ADR-008 grammar with TYPECODE "SP"
<REQREF>  ::= <SPEC-ID> ".R" <RNUM>                # GIT-SP-0003.R2
<RNUM>    ::= [1-9][0-9]*                          # no padding, no leading zero
<RKEY>    ::= "R" <RNUM>                           # the key inside `requirements:`
```

- **Scoped and permanent.** A requirement is `<SPEC-ID>.R<n>`. The number is never renumbered,
  never reused and never reassigned, even after the requirement is removed or superseded. Gaps
  are normal. `R02` and `r2` are `E-ID-GRAMMAR`: unlike `NNNN`, `R<n>` has exactly one spelling.
- **Allocation is `max + 1` within the file**, where max is taken over the numbers of every block
  heading **and** every key of the `requirements:` map in that spec, **and** every ref to that
  spec found anywhere in the project index (link targets, Spec Delta headings). The map and the
  inbound refs keep a number reserved after somebody deletes a block by hand.
- **Removing** a requirement is not deleting it: its block and its ID stay, and its status moves
  to a `cancelled`-category status. A hand deletion is legal Markdown but leaves every reference
  to the number dangling (`W-REF-DANGLING`).
- **Moving** a requirement to another spec allocates a **new** ref in the destination and records
  `supersedes` from the new requirement to the old one (§5); the old one is removed as above.
  Refs never travel between files.
- Duplicate headings for one ref in a file are `E-REQ-DUPLICATE`. Two branches that each add
  `R5` to one spec usually conflict textually in git (both append to the same region); when they
  do not, the post-merge duplicate is caught by `E-REQ-DUPLICATE` exactly as ADR-008 catches
  item collisions, and `yaml.v3` refuses the duplicated map key as `E-FM-YAML`.
- **Anchor.** The HTML/Markdown anchor of a block is its ref lower-cased with `.` replaced by `-`:
  `#git-sp-0003-r2`. Search hits (Pando), the web app and wikilinks resolve to it.

### 4. The `requirements:` front-matter map

Per-requirement metadata lives in the spec's front matter, keyed by `R<n>`; prose stays in the
body. Every key is optional.

```yaml
requirements:
  R1:
    status: done
  R2:
    status: in_progress
    trace:
      code: [internal/core/allocator.go#NextID]
      tests: [internal/core/allocator_test.go#TestNextID/stale_counter]
    verified: {rev: "sha256:4e1b9c0d7a3f2e61", commit: 9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21, at: 2026-10-01T09:12:00Z, by: claude}
  R3:
    status: cancelled
  R4:
    status: todo
    links:
      - { kind: supersedes, target: GIT-SP-0001.R7 }
```

| Key | Type | Notes |
|---|---|---|
| `status` | status id | Any status of `project.yaml:workflow.statuses` **except one in the `triage` category** (`E-REQ-STATUS`). Unknown → `E-STATUS-UNKNOWN`. Absent → read as `workflow.initial`, reported `W-REQ-NO-ENTRY`; writers always materialise it (docs/03 §13.3) |
| `trace.code` | list of trace refs | Code that realises the requirement and cannot carry a marker (generated code, config, SQL) |
| `trace.tests` | list of trace refs | Tests that verify it, when a `Verifies:` marker is impractical |
| `verified` | mapping | The **durable** verification stamp — the only machine-written state (§7). Written only when a story or task that implements or modifies the requirement moves to a `done`-category status, or by `gintrack spec verify --commit`; ordinary `verify` runs go to the local verification cache instead |
| `verified.rev` | block rev | The block `rev` (§6) of the text that was verified |
| `verified.commit` | commit id | Full hex git commit id (40 or 64 hex) at which the linked tests passed. In a Jujutsu repository, the git commit id of that change (ADR-021) |
| `verified.at` | timestamp | UTC RFC 3339 timestamp (R-TIME-1, confirmed in review) of the passing run the stamp records, not of the write that stored it |
| `verified.by` | handle | Person or agent that ran the verification recorded by the stamp |
| `links` | list of relations | Requirement-level relations, same `{kind, target}` shape as an item's `links`; exactly `supersedes`, `superseded_by` and `relates_to` are allowed (§5). Approved in review |

```
<trace-ref> ::= <repo-path> [ "#" <symbol> ]
<repo-path> ::= repository-relative, "/"-separated, no "..", no leading "/"
<symbol>    ::= the language's identifier path: Func, Type.Method, TestX, TestX/sub_case,
                or for TS/JS the function name or the `describe > it` title path
```

- Paths are relative to the **repository root**, not to the docs folder: code lives outside it.
- A path without `#symbol` means the whole file.
- **Status reuses the project workflow** — no per-type workflow, no new vocabulary. The status
  says where the requirement is in its life (`todo`, `in_progress`, `done`, `cancelled`); whether
  it is **tested and holding** is a separate, computed coverage state (§7).
- A map key with no block is `W-REQ-ORPHAN-ENTRY`; a block with no map entry is `W-REQ-NO-ENTRY`.
  Both are warnings because either half may arrive in a later merge; the orphan key keeps its
  number reserved.
- Unknown keys inside an entry, and inside `trace` and `verified`, are preserved on rewrite and
  re-emitted after the known ones, sorted (R-FMT-6, R-EVO-5).
- Emission order inside an entry: `status, trace, verified, links`; inside `trace`: `code, tests`;
  inside `verified`: `rev, commit, at, by`. Map keys are emitted in numeric order (`R2` before
  `R10`), not lexicographic order.

### 5. Link kinds and the link-target grammar

Three new pairs join the five kinds of docs/03 §12.1. As before, a link is stored on one side only
and the index computes the inverse (R-LINK-1).

| Kind | Inverse | Source → target | Semantics |
|---|---|---|---|
| `implements` | `implemented_by` | story or task → requirement (or whole spec) | the work realises the requirement |
| `modifies` | `modified_by` | story or task → requirement (or whole spec) | the work changes an existing requirement — the delta (§9) |
| `supersedes` | `superseded_by` | requirement → requirement; spec → spec | the source replaces the target, which is removed |

The link-target grammar of R-LINK-2 is extended to accept requirement refs:

```
<link-target> ::= [ <KEY> "/" ] ( <ID> | <REQREF> )
```

- A `<REQREF>` target resolves to a block; a dangling spec **or** a dangling block is
  `W-REF-DANGLING` (a warning, as for every other target).
- `implements`, `modifies` and their inverses require a spec or requirement target; `supersedes`
  and `superseded_by` require a target of the same kind as the source (requirement → requirement,
  spec → spec). Any other combination is `E-LINK-TARGET-TYPE`. This is statically visible from
  the ref itself, so it is an error rather than a warning.
- **Requirement-level links (approved).** Relations whose source is a requirement live in
  `requirements.R<n>.links`, a list of `{kind, target}` entries with exactly the shape of an
  item's `links` (docs/03 §12.1); item-level ones live in the item's `links`. The allowed kinds
  in `requirements.R<n>.links` are exactly three:
  - `supersedes` and `superseded_by` — target MUST be a requirement ref (`<REQREF>`, optionally
    `<KEY>/`-qualified);
  - `relates_to` — target MAY be a requirement ref, a spec ID or any item ID.

  Any other kind there — including `implements`, `modifies` and their inverses, whose source is
  always the work item, and `blocks`/`duplicates`, which have no meaning between requirements —
  is `E-REQ-FIELD`. An allowed kind with a target of the wrong type is `E-LINK-TARGET-TYPE`. As
  for items, a link is stored on one side only and the index computes the inverse (R-LINK-1), so
  a move normally records only `supersedes` on the new requirement.
- A story's or task's `links` MAY target requirements.
- `relates_to`, `blocks`, `blocked_by`, `duplicates` and `duplicated_by` MAY target a spec or a
  requirement from an item; they keep their existing meaning.
- **Wikilinks** use the same grammar: `[[GIT-SP-0003.R2]]` renders the ref, the requirement title
  and its status; `[[GIT-SP-0003]]` renders the spec. R-WIKI-1 ("a target matching the ID
  grammar is resolved as an item") covers requirement refs too.

### 6. Block `rev`

Two content hashes are computed per requirement. Neither is ever stored except as the value of
`verified.rev`.

- **Block rev** — `"sha256:" + lowercase_hex(sha256(B))[0:16]`, where `B` is the block extent of
  §2 with trailing blank lines removed and exactly one `\n` appended. It covers the heading
  (therefore the title) and the whole text, scenarios included, and nothing else: blank lines
  between blocks, edits to other blocks and every front-matter change leave it unchanged. It is
  the fingerprint of **what the requirement says**: `verified.rev` records it and suspect
  compares against it.
- **Requirement rev** — the same function over `B` followed by the canonical JSON encoding
  (UTF-8, keys sorted, no insignificant whitespace) of the requirement's map entry, `{}` when
  absent. It is the **write token**: every requirement read returns it and every requirement
  write (vault `UpdateRequirement`, MCP `update_requirement`) quotes it, under exactly the
  protocol of docs/03 §5 (R-REV-3 … R-REV-3c: `stale_revision` with `currentRev` and per-field
  `conflicts[]`, fields being `text`, `title`, `status`, `trace`, `verified`, `links`). A write to
  one requirement never invalidates the rev of another requirement of the same spec.
- The file `rev` (R-REV-1) is unchanged and still covers the whole file; spec-level writes
  (title, labels, purpose) quote it as for any item.

The two are separate on purpose: if the write token were the verification fingerprint, writing
`verified` would change the value it just recorded, and so would a status change — every stamp
would be suspect the moment it was written.

### 7. Verification stamp, coverage state, and what is never stored

Verification has two layers: a **local verification cache** that every run writes, and the
**durable stamp** `verified` in the spec, written only at two well-defined moments (decided in
the second review round).

**The verification cache (derived, local, never the source of truth).**

- Every `gintrack spec verify` run and every MCP `verify_requirement` call **records its result in
  the cache and writes nothing into the spec**. One entry per requirement per run holds: `ref`,
  `rev` (the block rev of §6 that was tested), `commit` (full hex id of `HEAD` when the tests ran),
  `tests` (the test ids that ran for it: the union of `Verifies:` markers and `trace.tests`),
  `result` (`pass` or `fail`), `at` (UTC RFC 3339) and `by` (the handle that ran it, needed to
  write a stamp later).
- A run is recorded as `pass` only if every linked test passed **and** the traced files are
  unchanged in the working tree relative to `commit`; otherwise it is `fail` (a test failed) or
  not recorded at all (a dirty tree gives no evidence about `commit`).
- It lives **next to the on-disk index cache**: natively in `<docs>/.pmngr/verify.json` beside
  `index.json`, git-ignored by the same `gintrack init` `.gitignore` snippet (R-LOC-5); in the
  browser in the same IndexedDB database as the WASM index. Browser-only mode cannot run tests, so
  its cache is normally empty. The cache is per machine, never committed, never synced, and can be
  deleted at any time; deleting it loses only evidence that was never promoted to a stamp. Nothing
  in it may be required to read or write the repository (R-IDX-1).

**The durable stamp (`verified`, in the spec's front matter).** It is the **only state a tool
writes back** into a spec as a result of running something, and it is written only:

1. **When a story or task that implements or modifies the requirement moves to a `done`-category
   status.** The same vault write that moves the item (and that applies its Spec Delta, §9) writes
   `verified` for each requirement the item `implements` or `modifies`, copying `rev`, `commit`,
   `at` and `by` from the most recent cache entry with `result: pass` whose `rev` equals the
   requirement's current block rev (after the delta is applied). If there is no such entry — an
   empty cache, a browser session, or text that changed since the last run — no stamp is written
   for that requirement and the status move is **not** refused; the result lists the requirements
   left unstamped.
2. **By `gintrack spec verify --commit`**, intended for CI on `main`: it runs the linked tests,
   records the cache entries, and writes a stamp for every requirement whose run is `pass`, one
   write per spec quoting its requirement revs (§6). Committing the result is the caller's step
   (the CI job commits it). A `fail` never overwrites an existing stamp.

A human may hand-write `verified`; it then means what the human says.

**Coverage state** — `untested`, `passing`, `failing`, `suspect` — is **computed, never stored**,
from the cache first and the stamp as the durable baseline:

- The **evidence** for a requirement is the most recent cache entry whose `rev` equals the current
  block rev and whose `at` is later than `verified.at`; when there is none, it is the stamp.
- Evidence from a cache entry with `result: fail` → `failing`. Evidence with `pass` (a cache entry
  or the stamp) → `passing`, unless a traced file or symbol changed between the evidence's
  `commit` and `HEAD` (directly, or transitively through Pando) → `suspect`.
- A stamp whose `rev` differs from the current block rev, with no newer matching cache entry →
  `suspect`. No stamp and no matching cache entry → `untested`.
- **An empty or deleted cache falls back to the stamp**: every requirement shows its last durable
  state, exactly as a fresh clone or CI on `main` sees it. The cache can only make the answer more
  recent on this machine, never contradict what the repository records.

No key named `suspect`, `coverage` or `tested` exists in any file, and a writer MUST NOT invent
one. The **marker scan** is a derived cache of the same kind, outside the source of truth. Pando
never writes to `specs/` (ADR-036).

### 8. In-code markers

The scanner is **line-based and opt-in by file type**: a line is a marker only if the whole line
matches the grammar below for a comment syntax that the file's type uses. Everything else is
ignored, so prose that merely mentions `Implements:` never becomes a trace.

```
<marker-line> ::= <ws>* <opener> <ws>* <kw> ":" <ws>+ <mref> ( <ws>* "," <ws>* <mref> )*
                  <ws>* [ "," ] <ws>* [ <closer> ] <ws>* EOL
<kw>          ::= "Implements" | "Verifies"                  # case-sensitive, exact
<mref>        ::= [ <KEY> "/" ] <REQREF>                      # REQREF of §3, whole token
<opener>      ::= "//" | "#" | "--" | "/*" | "/**" | "*" | "<!--" | ""
<closer>      ::= "*/" | "-->"
```

| Comment syntax | Openers accepted | File types (by extension or name) |
|---|---|---|
| `//` and `/* */` | `//`, `/*`, `/**`; `*` or `""` on a continuation line of an open `/* … */` | `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`, `.java`, `.kt`, `.swift`, `.c`, `.h`, `.cc`, `.cpp`, `.hpp`, `.cs`, `.rs`, `.scala`, `.dart`, `.php`, `.css`, `.scss` |
| `#` | `#` | `.py`, `.rb`, `.sh`, `.bash`, `.zsh`, `.pl`, `.r`, `.yaml`, `.yml`, `.toml`, `.tf`, `Makefile`, `Dockerfile` |
| `--` | `--` | `.sql`, `.lua` |
| `<!-- -->` | `<!--`; `""` on a continuation line of an open `<!-- … -->` | `.md`, `.html`, `.htm`, `.xml`, `.svg`, `.vue`, `.svelte` |

Examples: `// Implements: GIT-SP-0003.R2`, `# Verifies: GIT-SP-0003.R2, GIT-SP-0003.R4`,
` * Implements: WEB/WEB-SP-0001.R1`, `-- Implements: GIT-SP-0007.R1`,
`<!-- Verifies: GIT-SP-0003.R2 -->`. `.vue`/`.svelte`/`.php` files also accept the `//` and
`/* */` row.

**Match rule (exact).**

- The opener MUST be the first non-whitespace text on the line (a full-line comment). A trailing
  comment after code (`x := 1 // Implements: …`) is **not** a marker. The empty opener `""` is
  accepted only on a line inside a block comment (`/* … */`, `<!-- … -->`) that is already open.
- The keyword follows the opener after optional whitespace, is case-sensitive and is immediately
  followed by `:`. `implements:`, `Implements :` and `Implemented:` are not markers.
- After the colon come **one or more** refs separated by commas. Each ref MUST be a complete
  `<REQREF>` of §3 (`R` number unpadded), optionally qualified with `<KEY>/`. After the last ref
  only whitespace, one optional trailing comma and the comment's closer (`*/`, `-->`) may follow.
  Any other trailing text — prose, a second keyword, a bare spec ID — means the line is **not** a
  marker.
- A line whose opener and `<kw>:` match but whose ref list does not is reported as
  `W-MARKER-SYNTAX` (so a typo is visible) and contributes nothing.
- Only file types in the table are scanned; any other file, anything under `.pmngr/`, and in
  Markdown anything inside a fenced code block, is ignored. A `#` line in a Markdown file is a
  heading, not a comment, which is why Markdown accepts only `<!-- -->`.
- A marker inside a string literal is not a marker. The Go scanner uses the Go lexer; other
  languages use this line rule, whose full-line and whole-ref requirements keep false positives
  to string or heredoc lines that happen to look exactly like a comment marker.

**Attachment and meaning.**

- A marker in the comment block immediately preceding a declaration attaches to that
  declaration; a marker inside a declaration's body attaches to the enclosing declaration; a
  marker before the first declaration of a file — and every marker in a file type with no
  declarations (Markdown, HTML, YAML, SQL) — attaches to the whole file.
- `Implements:` adds code to the requirement's trace, `Verifies:` adds tests. Markers and
  `trace:` entries are **unioned**; duplicates collapse. Neither overrides the other.
- A marker names requirements only: a well-formed marker whose ref is unknown in the index is
  `W-MARKER-DANGLING`; a spec ID without `.R<n>` makes the line malformed (`W-MARKER-SYNTAX`).
  Markers are read by the native trace engine, never by `internal/core`.
- Adding a row to the table (another extension or comment syntax, e.g. `;`) is an additive
  change to the scanner, not to the data model.

### 9. `## Spec Delta` in a story or task

A story proposes changes to living specs in its own body; the spec changes only when the story
reaches a `done`-category status (OpenSpec's change-proposal model).

```markdown
## Spec Delta

### ADDED GIT-SP-0003 — Reject a malformed reserved range
The allocator SHALL refuse a `reserved` range whose start is greater than its end.

#### Scenario: inverted range
- **WHEN** `reserved.task` is `[[249, 200]]`
- **THEN** project validation reports an error naming the range

### MODIFIED GIT-SP-0003.R2 — Allocate the next ID by index scan
<the complete replacement block text: statement and scenarios>

### REMOVED GIT-SP-0003.R4 — Counters are authoritative
Reason: superseded by R2; counters are hints only.
```

```
<delta-heading> ::= "### " <op> " " <target> " " <sep> " " <title>
<op>            ::= "ADDED" | "MODIFIED" | "REMOVED"
<target>        ::= <SPEC-ID>                   # ADDED: no number yet
                  | <REQREF>                    # MODIFIED, REMOVED
```

- **ADDED** carries a complete new block without a number. A `Supersedes: <REQREF>` line directly
  under the heading turns it into a move (§3). On apply the vault allocates `R<n>`, appends the
  block to the spec, and rewrites the delta heading to `### ADDED <REQREF> — …` so the story
  records what it created.
- **MODIFIED** carries the complete replacement text; on apply it replaces the block (changing its
  block rev, so an existing stamp becomes suspect until re-verified).
- **REMOVED** carries a mandatory `Reason:` line; on apply the requirement's status becomes the
  first `cancelled`-category status and the block stays.
- Operations inside the delta use the same block-extent rule (§2); `####` scenario headings stay
  inside their operation.
- Until applied, a MODIFIED or REMOVED target is indexed as a `modifies` relation of the story, so
  impact and coverage see pending changes. After applying, the story holds `implements` or
  `modifies` links in its front matter.
- Diagnostics: `E-DELTA-OP` (unknown operation or malformed heading), `E-DELTA-TARGET` (ADDED
  naming a requirement, MODIFIED/REMOVED naming a spec), `W-DELTA-DANGLING` (unknown spec or
  block — a warning, it may arrive in a later merge), `E-DELTA-REASON` (REMOVED without `Reason:`).
  The added and replacement blocks are linted like any block (§10).

### 10. Grammar lint and its `project.yaml` key

```yaml
specs:
  lint:
    severity: warning        # off | warning (default) | error
    rules:                   # optional per-rule override, same three values
      LINT-REQ-VAGUE: error
      LINT-REQ-SCENARIO: off
    vague_words: [fast, quickly, user-friendly, easy, as appropriate, as needed, etc, robust]
```

`specs.lint: off` (or `warning`, `error`) — a scalar in place of the mapping — is shorthand for
`specs.lint: {severity: <value>}`; writers preserve whichever form they read.

| Rule | Checks |
|---|---|
| `LINT-REQ-STATEMENT` | the statement is one EARS pattern or contains `SHALL` |
| `LINT-REQ-SCENARIO` | the block has at least one `#### Scenario:` |
| `LINT-REQ-WHEN-THEN` | every scenario has at least one `**WHEN**` and one `**THEN**`, in that order |
| `LINT-REQ-VAGUE` | the statement or a scenario contains a word of `vague_words` (whole word, case-insensitive) |
| `LINT-REQ-MULTI` | the statement contains more than one `SHALL` (one requirement per block) |

- The key is **`specs.lint`** in `project.yaml`. `severity` sets every rule; `rules` overrides one.
  Default: `warning`, so a spec that breaks the grammar is still valid, indexable and writable.
- At `off`, the rule does not run and produces no finding anywhere (API, MCP, `doctor`, web
  editor). A per-rule value wins over the global one in both directions: `severity: off` with
  `rules: {LINT-REQ-VAGUE: warning}` runs only that rule.
- At `error`, a finding is a validation error like any `E-*` code: it blocks writes to that spec
  through the API and MCP and makes `gintrack doctor` exit non-zero.
- `vague_words` replaces the built-in list when present.
- Values other than `off`, `warning` and `error`, or unknown rule IDs, are `E-PROJ-SPECS`. Lint codes are prefixed
  `LINT-`, not `E-`/`W-`, because their severity is the project's choice.

### 11. Schema version 2

The spec layer **bumps `project.yaml:schema` from `1` to `2`** (decided in review).

**Why, although the change is additive.** By the letter of R-EVO-3 nothing here removes or
renames anything. But an older binary does not ignore the new constructs, it rejects them one at a
time: the six new link kinds are `E-ENUM`, a `GIT-SP-0003.R2` target or an `SP` ID is
`E-ID-GRAMMAR`, and it therefore refuses to write any story that carries one while writing the
rest of the project normally — a piecemeal failure with cryptic codes. With `schema: 2` the same
binary reports one clear diagnostic, `E-PROJ-SCHEMA` ("schema 2 is newer than the supported
version 1"), and falls back to read-only for the whole project (R-EVO-2). R-EVO-3 is amended
accordingly (docs/03 §19): an additive change that a previous version's validator **rejects**
(a new value in a closed enum, a wider ID or link-target grammar) bumps `schema`; one it merely
ignores or round-trips (a new optional key, R-EVO-5) does not.

**When schema 2 is required.** A project needs `schema: 2` as soon as it contains any
**spec construct**: a file under `specs/`, a link (item-level or requirement-level) of kind
`implements`, `implemented_by`, `modifies`, `modified_by`, `supersedes` or `superseded_by`, or a
link target that is an `SP` ID or a requirement ref. `## Spec Delta` sections, requirement
wikilinks, in-code markers and the `specs:` key of `project.yaml` are not spec constructs on their
own: an older binary reads them as prose, as a broken wikilink, as a comment, or as an unknown
key it preserves. A project with no spec construct stays at `schema: 1`, and a binary that
supports schema 2 still **creates new projects at `schema: 1`** (docs/03 §2.2): a team pays the
upgrade only when it starts using specs.

**What a supporting binary does.**

- It reads and writes both `schema: 1` and `schema: 2` projects; `core.SupportedSchema` becomes
  `2`.
- In a `schema: 1` project, every spec construct is `E-SCHEMA-FEATURE` ("requires schema 2") on
  the file that holds it — the project stays writable, the construct is still indexed so nothing
  is hidden, and `gintrack doctor --fix` performs the upgrade.
- **Upgrade on first use, automatically.** The first write through the vault (web app, CLI, MCP,
  WASM alike) that would introduce a spec construct into a `schema: 1` project — creating the
  first spec, adding the first `implements` link, applying a Spec Delta — also rewrites
  `schema: 1` to `schema: 2` in `project.yaml`, as part of the same write (one extra one-line diff,
  committed together under commit-on-save), and says so in its result (`schemaUpgraded: 2`). The
  `project.yaml` edit quotes its file `rev` like any write; on `stale_revision` the whole write is
  retried once, per docs/03 §5. Nothing is upgraded on open, on read or on a write that introduces
  no spec construct.
- Hand edits are covered by the same rule: a person who adds a spec file with `vim` gets
  `E-SCHEMA-FEATURE` until they change the one line or run `gintrack doctor --fix`.
- The 1 → 2 migration transforms no file other than that one line, so it needs no
  `gintrack migrate` (R-EVO-4); when `gintrack migrate` exists, `--to 2` performs the same edit.
  There is no downgrade: a project that has specs and returns to `schema: 1` reports
  `E-SCHEMA-FEATURE` on every spec construct.

**What an older binary does.** Any binary whose `SupportedSchema` is `1` (every release up to and
including 2.0.1) reports `E-PROJ-SCHEMA` on a `schema: 2` project and is expected to open it
read-only (R-EVO-2); it ignores `specs/` entirely. Mixed teams upgrade every binary, the web app
build and CI before the first spec is created.

**The write gate ships with specs (decided in the second review round).** Up to and including
2.0.1, `E-PROJ-SCHEMA` is emitted as a diagnostic but `internal/vault` does not refuse writes on
an unknown or newer `schema`. The gate — every vault write refused on a project whose `schema` is
missing or newer than `SupportedSchema`, with the project opened read-only (R-EVO-2) — ships in
**the same release that introduces specs**, owned by the implementation story `GIT-US-0105`.
There is **no 2.0.2 patch** backporting it. Consequently:

- binaries from that release onward enforce R-EVO-2 for any future schema;
- released binaries up to 2.0.1 report `E-PROJ-SCHEMA` on a `schema: 2` project but **may still
  write** to it, including rewriting files whose spec constructs they do not understand;
- the release's upgrade note states this explicitly: every binary, web app build and CI job that
  writes to a project must be upgraded before that project's first spec construct is created.

A repository without spec constructs is byte-for-byte unaffected.

## Decisions from review

### First round (2026-09-24)

The human reviewer settled every point the first draft left open.

1. **`schema` is bumped to 2** (§11): the upgrade happens automatically on the first write that
   introduces a spec construct (or via `gintrack doctor --fix`); older binaries report
   `E-PROJ-SCHEMA` and fall back to read-only instead of failing piecemeal with `E-ENUM` /
   `E-ID-GRAMMAR`. R-EVO-3 and R-EVO-6 are amended; the "no schema bump" position is withdrawn.
2. **`requirements.R<n>.links` is approved** (§4, §5): same `{kind, target}` shape as item
   links; exactly `supersedes`, `superseded_by` and `relates_to` are allowed.
3. **A spec has no planning fields** (`parent`, `epic`, `milestone`, `sprint`, `estimate`,
   `effort`, `spent`, `due`, `inbox`) **and is not an inbox target** (§1). Decided.
4. **Lint severity gains `off`** (§10): globally (`specs.lint.severity`, or the scalar shorthand
   `specs.lint: off|warning|error`) and per rule (`specs.lint.rules`).
5. **Marker comment syntaxes** are `//`, `#`, `/* */`, `--` (SQL, Lua) and `<!-- -->` (HTML,
   Markdown), with the exact full-line, whole-ref match rule of §8.
6. **`verified.at` stays a UTC RFC 3339 timestamp** (R-TIME-1), as drafted.

### Second round (2026-09-24)

The human maintainer reviewed the revised draft, settled the remaining points and **accepted**
the ADR.

1. **Older binaries: the write gate ships with specs** (§11). The read-only gate on an unknown or
   newer `schema` ships in the same release that introduces specs, owned by `GIT-US-0105`, and
   the upgrade note documents it. There is no 2.0.2 patch; binaries up to 2.0.1 report
   `E-PROJ-SCHEMA` but may still write.
2. **The schema upgrade stays implicit** (§11): it happens on the first write that adds a spec
   construct, reported as `schemaUpgraded: 2`; no `gintrack spec init` step is required. The
   one-line `schema` change in a spec PR is an accepted cost (Consequences).
3. **Verification results accumulate in a local cache; the stamp is written rarely** (§4, §7).
   `verify` runs record `{ref, rev, commit, tests, result, at, by}` in a rebuildable, git-ignored
   cache next to the index cache. The `verified` stamp is written into the spec only when a story
   or task that implements or modifies the requirement moves to `done`, or by
   `gintrack spec verify --commit` (CI on `main`). Coverage uses the cache first and the stamp as
   the durable baseline; an empty cache falls back to the stamp. This replaces the first draft's
   "every `verify` writes the stamp".
4. **Accepted as drafted:** two hashes per requirement (§6); allocating ADDED numbers when the
   story is done (§9); and the minor costs listed under Consequences — the reformatter risk,
   front-matter growth and the `LINT-*` naming.

## Consequences

### Easier

- A requirement has a permanent, speakable, greppable ID (`GIT-SP-0003.R2`) that survives
  retitling and reordering, and can be written in a commit message, a code comment or an agent
  prompt — the precondition for every impact and coverage query in Phase 11.
- One spec reads top to bottom as a document. Purpose, glossary and requirements are together, so a
  human reviewer and a Markdown viewer on GitHub see a coherent capability rather than a folder of
  fragments.
- Per-requirement concurrency: two agents editing two requirements of one spec never refuse each
  other, because each quotes its own requirement rev.
- Drift detection is deterministic and incremental: a block rev and a commit id are enough to say
  "suspect" without an LLM.
- Nothing new to learn for status: requirements move through the same statuses as everything else.

### Harder — the price we are paying

- **Older binaries lose write access to a project that uses specs.** The first spec construct
  raises the project to `schema: 2`, and from then on a pre-ADR-037 `gintrack` opens the whole
  project read-only (R-EVO-2) — not only the spec files. A team must upgrade every binary, the
  web app build and CI before its first spec. The write gate of §11 ships only in the release
  that introduces specs (no 2.0.2 backport), so binaries up to 2.0.1 merely report
  `E-PROJ-SCHEMA` and may still write to a `schema: 2` project; the upgrade note is the only
  protection against them.
- **The upgrade is implicit.** Creating the first spec also edits `project.yaml`; a reviewer sees
  a one-line `schema` change in a PR that is otherwise about a spec, and must know why. Every
  tool reports `schemaUpgraded: 2` in the result of the write that made the change, so the
  author can say so in the PR and a reviewer can trace the line to its cause.
- **Two hashes per requirement.** Contributors and agents must learn which one is quoted on a write
  (requirement rev) and which one is stamped (block rev). Getting it wrong is either a spurious
  `stale_revision` or a stamp that is always suspect.
- **Numbers can still be reused by a determined hand.** If someone deletes a block *and* its map
  entry *and* every inbound reference, `max + 1` will hand the number out again. The rule forbids
  it; nothing on disk can prove it happened. Git history can, and `doctor` does not read history.
- **A spec file is a merge hotspot.** Concurrent additions to one capability append to the same
  region and conflict textually in git more often than one-file-per-item ever does. Allocating
  ADDED numbers only when a story is done shortens the window; it does not close it.
- **Front matter grows with the spec.** A spec with 40 requirements carries a 40-entry map, and a
  front-matter-only index read (docs/03 §17.1) is no longer a few hundred bytes for these files.
- **The body is now structurally interpreted.** Until now only `## Acceptance Criteria` was
  (R-STORY-3). A requirement's identity lives in a heading, so a careless Markdown reformatter
  that changes heading levels or the em dash silently turns requirements into prose.
- **Lint codes break the E/W naming convention.** A `LINT-*` code does not reveal its severity by
  reading it; tooling must consult `project.yaml`.
- **Markers are only as good as the scanner.** A line heuristic outside Go can misattribute a
  marker to the wrong symbol; a marker in a file type or comment syntax outside the §8 table, or
  written as a trailing comment after code, is invisible by design.
- **Stamps are still commits into specs, but rarely.** A `verify` run writes only the local cache;
  the spec's front matter changes when implementing work reaches `done` (inside a commit that
  already touches the spec's lifecycle) or when CI on `main` runs `spec verify --commit`. The
  noise is one stamp commit per CI verification rather than one per local run. The price is that
  evidence lives in two places: a machine with a fresh cache and a teammate with an empty one can
  show different coverage for the same requirement until a stamp records it, and a passing run
  that is never promoted is lost with the cache.
- **Some `done` moves leave a requirement unstamped.** A story closed from the browser, or whose
  Spec Delta changed the text after the last local run, finds no matching cache entry; the
  requirement stays `suspect` or `untested` until CI on `main` stamps it.

## Alternatives considered

- **One file per requirement** (`requirements/GIT-RQ-0042-….md`, Doorstop-style). Maximally
  merge-friendly and trivially addressable with the existing item machinery. Rejected by the
  reviewer: a capability shatters into dozens of files nobody reads as a document, the spec's
  purpose and glossary have no natural home, and moving a sentence between requirements becomes a
  multi-file change. Blocks give the same addressability at the cost of the block-parsing rules
  above.
- **Global requirement IDs** (`GIT-RQ-0042`, a sixth type code with its own project-wide counter).
  Shorter refs and allocation identical to every other type. Rejected: the ref no longer says which
  spec it belongs to, so every marker and link needs a lookup to be meaningful; a project-wide
  counter conflicts across unrelated specs; and a requirement block carrying a global ID would
  still need a spec to live in, i.e. two identities for one thing.
- **Title- or slug-based requirement identity** (OpenSpec's `### Requirement: <name>`). Human
  friendly and zero allocation. Rejected: a retitle breaks every marker, link and stamp — the exact
  instability this ADR exists to remove.
- **A per-type workflow for requirements** (`draft → approved → implemented → verified`). Closer
  to classic requirements tooling. Rejected by the reviewer: a second status vocabulary every
  surface must render and every board must map, when "verified" is a computed coverage state
  rather than a lifecycle step, and the project workflow already expresses the rest.
- **One rev per requirement, doubling as the stamp.** Simpler to explain. Rejected: writing the
  stamp, or changing the status, would change the fingerprint it records, so every requirement
  would be suspect immediately after verification (§6).
- **Storing coverage state or test results in the spec** (`tested: true`, `last_result: pass`).
  Rejected: derived, machine-specific and stale on the next commit; it contradicts R-IDX-1 and
  would turn every test run into a commit.
- **Writing the stamp on every `verify` run** (the first draft). Keeps all evidence in git.
  Rejected in the second review round: every local run by every person and agent became a
  front-matter commit on a shared file, a merge hotspot on top of the one a spec already is (§7
  keeps runs in a local cache and stamps only at `done` or from CI on `main`).
- **Keeping verification evidence only in the cache**, with no stamp at all. No commits, no
  noise. Rejected: a fresh clone, a teammate and CI would all see every requirement as
  `untested`, and coverage would depend on which machine asks.
- **Metadata inline in the block** (a YAML fence or an HTML comment under each heading). Keeps a
  requirement's metadata next to its prose. Rejected: it mixes machine state into prose diffs,
  defeats the one-line-diff goal for a status change, and has no precedent in the model — every
  other structured field lives in front matter.
- **Keeping `schema: 1`**, because the change is additive by the letter of R-EVO-3. Rejected in
  review: older binaries would fail per file with `E-ENUM` / `E-ID-GRAMMAR` and keep writing the
  rest of the project, instead of reporting one unknown schema and falling back to read-only.
- **Bumping every project to `schema: 2` when the binary is upgraded** (or creating new projects
  at 2). Rejected: it would lock older teammates out of projects that never use specs. The bump
  happens on first use instead (§11).
- **Recognising markers anywhere on a line, in any file.** Rejected: prose, string literals and
  Markdown headings that mention `Implements:` would become traces. The full-line, whole-ref,
  per-file-type rule of §8 trades a few missed markers for almost no false ones.
- **Lint as a hard gate by default.** Rejected by the reviewer: imported and hand-written specs
  would be unwritable until rewritten; the default must let people and agents write first and
  fix the grammar later.
