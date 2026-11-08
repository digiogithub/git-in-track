# ADR-044 — Memory is an item type, and a project may be knowledge-only

- **Status:** Proposed
- **Date:** 2026-11-08
- **Phase:** Post-1.0 (gintrack hub proof of concept)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-008](ADR-008-id-scheme.md), [ADR-012](ADR-012-comments-as-separate-files.md), [ADR-018](ADR-018-bounded-project-discovery.md), [ADR-026](ADR-026-the-web-app-soft-deletes-and-warns-first.md), [ADR-033](ADR-033-inbox-is-a-reserved-triage-status-category.md), [ADR-037](ADR-037-specs-with-requirement-blocks.md), [ADR-043](ADR-043-gintrack-hub-optional-self-hosted-multi-user-server.md)

## Context

The hub of ADR-043 keeps a shared memory of facts: short, explicit statements
such as "customer X is invoiced quarterly", each with a scope, an author and a
date. Agents propose them and people review them. The same facts travel over
`alchemai.memory.v2`, whose records carry a `key`, a `local_id`
(`scope + "/" + key`), a `content_hash` (sha256 of
kind␟scope␟key␟path␟content), a monotonic `version`, `valid_to` and
`superseded_by`.

Two things in today's model stand in the way:

- **There is nowhere to put a fact.** A KB page has no status, no permanent id
  and no review workflow. A story has all three, but it is work, not
  knowledge, and every board, sprint and metric would have to learn to ignore
  it.
- **A repository without a backlog is not a project.** Discovery needs
  `.pmngr/project.yaml` (`internal/core/index.go`, ADR-018), and the project
  key is what ids and cross-project wikilinks are built from. A team that only
  wants a knowledge base would have to declare statuses it never uses. The
  team repository already escapes this as a pseudo-project with no backlog
  (`ProjectRef.Team`, `internal/core/index.go:81`;
  `TeamRef.KBScope`, `internal/core/teamdiscover.go:114`), but only for itself.

## Decision

### 1. `memory` is an item type with the id code `MEM`

- **Ids** are `<KEY>-MEM-<NNNN>`, for example `ACME-MEM-0042`. `M` is already
  milestone (`internal/core/ids.go:27`). The id grammar becomes
  `<KEY>-<EP|US|T|M|SP|MEM>-<NNNN>`.
- **Allocation** uses the existing allocator: the scan wins, reserved ranges
  count, and `Reconcile` runs after a merge. Ids are permanent and never
  reused (ADR-008).
- **Location follows scope**, and scope is **derived from the location, never
  stored**, so the field and the folder can never disagree:

  | Scope | Folder |
  |---|---|
  | project (`project/<key>`) | `<docs>/.pmngr/memory/` of the project repository |
  | organisation (`org/<team>`) | `<team>/.pmngr/memory/` of the team repository |

  The hub has no personal scope. Personal memory stays local in Pando, and
  `user/*` is `scope_unknown` (ADR-043).
- **File name**: `<ID>-<slug(key)>.md`.

### 2. Front matter

```yaml
id: ACME-MEM-0042
type: memory
title: "Customer X is invoiced quarterly"    # the fact, one line
key: customer-x-invoicing     # stable slug, unique per scope; the upsert key
status: active                # proposed | active | superseded | forgotten | cancelled
author: ana                   # the authenticated principal, never free text
created: 2026-11-08
updated: 2026-11-08
version: 17                   # monotonic per project, allocated like an id
content_hash: 9f2c…           # sha256 as alchemai.memory.v2 defines it
local_id: project/acme/customer-x-invoicing
valid_from: 2026-11-08T10:00:00Z
valid_to: 2027-06-30T00:00:00Z            # optional; set on forget
superseded_by: ACME-MEM-0051              # optional; computed, see §4
labels: [billing]
source: {kind: agent, ref: "memsync/<instance>"}   # agent|human|conversation|document|external
links:
  - {kind: supersedes, target: ACME-MEM-0017}
inbox: {status: pending, source: memsync, received: 2026-11-08T10:00:00Z}  # ADR-033, while proposed
```

- The body is the fact in prose, with an optional `## Evidence` section.
- **`version` lives in the file, not only in an index.** It is the cursor of
  the protocol's changes feed. If it lived only in a derived index, rebuilding
  that index would renumber every record and break every client's cursor. It
  is allocated under the project lock as max(scan) + 1. Every accepted
  content change, acceptance or forget bumps it.
- **`content_hash` is recomputed by the server** on every write and compared
  with the live file of the same `local_id`. An equal hash is a duplicate and
  writes nothing.
- `rev` is unchanged: the content hash of the file, never stored. The
  protocol's `base_version` and MCP's `rev` are two spellings of the same
  optimistic lock.

### 3. Lifecycle: proposals are triage

Memory has a **fixed lifecycle owned by `internal/core`**, not the workflow of
`project.yaml`:

| Status | Category | Meaning |
|---|---|---|
| `proposed` | `triage` (ADR-033) | Waiting for review. Not returned by search or by the changes feed to anyone but reviewers |
| `active` | `done` | Accepted, visible and injected into agent context |
| `superseded` | `done` | Replaced by a newer memory through `supersedes` |
| `forgotten` | `cancelled` | Withdrawn. The file stays and the feed emits a tombstone |
| `cancelled` | `cancelled` | A proposal that was rejected. It was never visible, so there is no tombstone |

- Because `proposed` is in the triage category, **the existing inbox lists
  proposals** and `triage_inbox_item` accepts or rejects them. Accepting moves
  `proposed` → `active` and bumps `version`. The `inbox:` block stays behind as
  the record of arrival, exactly as ADR-033 says.
- A grant decides the path in. `propose` writes `proposed`; `write` writes
  `active` directly. Agents default to `propose`.
- **Forgetting never deletes.** `forgotten` sets `valid_to` and bumps
  `version`, and git keeps the history. The web app's soft-delete rule
  (ADR-026) applies.

### 4. `supersedes` is a link kind

- `links` gains `supersedes` and its inverse `superseded_by`.
- Writing `supersedes: B` on memory A moves B to `superseded` and fills B's
  `superseded_by` in the same commit.
- `superseded_by` in front matter is the computed mirror, never hand-written,
  in the same spirit as ADR-037's `implemented_by`.

### 5. A project may be knowledge-only

`project.yaml` gains an optional `kind`:

```yaml
key: ACME
name: Acme knowledge
kind: knowledge     # backlog (default) | knowledge
```

- Discovery is unchanged: a knowledge project is found exactly where a backlog
  is.
- Validation skips statuses and workflow for it.
- The indexer treats it as it treats the team pseudo-project: pages, memory
  and comments, no backlog.
- It keeps its `ProjectKey`, so memory ids and cross-project wikilinks work.
- Item tools other than memory answer `not_a_backlog`, and the web app hides
  backlog, boards, sprints and specs for it.
- The team pseudo-project becomes the same mechanism, which removes a special
  case.
- The team key grammar (`[A-Z][A-Z0-9-]{1,15}`,
  `internal/core/teamscaffold.go:122`) is wider than the project key grammar
  (`[A-Z][A-Z0-9]{1,9}`, `internal/core/ids.go:38`), and `KBScope` casts one to
  the other (`teamdiscover.go:116`). Organisation memory ids need a valid
  project key, so a team that holds memory must have a key that satisfies the
  project grammar, and `team init` must say so.

## Consequences

### Easier

- Memory reuses allocation, `rev`, three-way merge, comments (ADR-012), the
  inbox and the web app, with no parallel system.
- A fact has a permanent id that a story, a page or another fact can link to,
  and its whole history is `git log` of one file.
- The protocol's records map one to one onto files, so the hub needs no store
  of its own.

### Harder — the price we are paying

- **A data-model change.** The id grammar, a new item type, a new link kind,
  new front-matter fields and a new `project.yaml` key. Older `gintrack`
  versions will report `MEM` ids and `kind: knowledge` as invalid. The release
  that ships this must say so, and validation must degrade to read-only rather
  than refuse.
- **Every surface that enumerates types** has to learn about `memory`:
  `ItemType`, `TypeCodeFor`, `ItemDirName`, the JSON schema, query filters,
  `web/` types, and the default exclusions of boards, sprints and metrics.
- **A fixed lifecycle beside the configurable one.** Memory statuses are not
  editable in `project.yaml`, and the docs must say so plainly.
- **`version` is another allocated number.** Two offline writers can collide.
  Like ids, the scan wins and `Reconcile` repairs, but a protocol client may
  briefly see a version twice. The changes feed must be idempotent, which
  memsync already is.

### Documents to change when this is accepted (not changed by this ADR)

- **docs/03-data-model.md**:
  - the id grammar (§ ids, R-ID-1);
  - the `memory` type, folders and front matter;
  - the memory lifecycle and its categories;
  - the `supersedes`/`superseded_by` link kinds;
  - `project.yaml` `kind`;
  - the team-key constraint for organisation memory.
- **docs/04-team-repository.md**: `.pmngr/memory/` in the team repository.
- **docs/08-mcp-server.md**: memory tools and their `rev` semantics.
- **AGENTS.md**: the front-matter field list, the `type` enum, the link kinds
  and the id rule.
- `internal/core/schema` (the JSON schema) and the golden files under
  `internal/core/testdata/`.

## Alternatives considered

### Memory as knowledge-base pages under `memory/`

Rejected. Pages have no status, no permanent id and no review queue. The
proposal queue would need a UI and a workflow of its own, which is the
duplication the inbox already solved.

### Memory as stories with a label

Rejected. Every board, sprint, metric and WIP limit would count facts as work,
and filtering them out everywhere is a standing bug.

### A store outside git (SQLite) exported to Markdown

Rejected in ADR-043: git is the source of truth, and a derived index must be
rebuildable from the files.

### Keep `version` only in the derived index

Rejected. Rebuilding the index would renumber records and break every
protocol client's changes cursor.

### Store the scope in front matter

Rejected. The folder already says it, and two sources of the same fact drift.

### Personal memory in a per-user repository on the hub

Rejected by the maintainer for the PoC. Personal memory stays local in Pando.
That also avoids putting data subject to erasure into git history.
