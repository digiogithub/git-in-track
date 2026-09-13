# ADR-031 — `external` is a first-class front-matter field

- **Status**: Accepted
- **Phase**: 7
- **Related**: [ADR-001](ADR-001-markdown-yaml-storage.md),
  [ADR-008](ADR-008-id-scheme.md),
  [ADR-033](ADR-033-inbox-is-a-reserved-triage-status-category.md)

## Context

Phase 7 connects git-in-track to external trackers, YouTrack first. Every direction
of that traffic needs the same thing: given an artifact in the other system, find
the file in this repository that already represents it — or find out that there is
none. An import that cannot answer that question either duplicates work on every
run or matches on the title, which is guesswork dressed up as a heuristic.

Item ids are ours and permanent (ADR-008). They deliberately say nothing about the
outside world, and the external system's identifiers are not ours to mint, so the
mapping has to be *recorded*, in the files, where the rest of the state lives
(ADR-001). Three places were available:

1. `custom:` — the declared custom-field mapping. Typed only by what `project.yaml`
   declares, and per project.
2. An `x-external` key — the escape hatch reserved for third-party tools, preserved
   verbatim and never interpreted.
3. A new top-level key of the data model.

The mapping is also not one-to-one forever. A story imported from YouTrack may
later be pushed to a second tracker, and a comment thread is reconciled comment by
comment, so both items **and** comments need the record. Knowledge-base pages have
the same problem when an article is copied out of a wiki.

Finally, the check has to be cheap. An importer walking a thousand issues performs
a thousand lookups; a linear scan of the corpus per lookup turns a two-second
import into a two-minute one.

## Decision

`external` is a first-class front-matter key on items, comments and knowledge-base
pages: a **list** of `{system, id, url?, key?, synced_at?}`.

- The pair **(`system`, `id`)** is the identity of an entry and the idempotency key
  every importer matches on. `system` is a short lowercase token, `id` is whatever
  the external system hands out and keeps its case.
- `system` is **not an enumeration**. The parser accepts any token matching
  `[a-z0-9][a-z0-9._-]{0,31}` and round-trips it untouched, so a file written by a
  tool we have never heard of survives a pass through an older binary.
- Writes are set operations keyed on the pair — `addExternal` / `removeExternal` —
  the same discipline `labels` and `links` already use. Re-adding an existing pair
  merges into it rather than appending; omitted fields keep the value somebody else
  recorded.
- The index maintains a map from (`system`, `id`) to item id, so the idempotence
  check is one map read.
- The key sits between `links` and `attachments` in the canonical key order
  (docs/03 §3.2): it is a relation, just one that leaves the repository.

Validation lives in `internal/core`, which compiles to WebAssembly, so nothing about
any tracker's HTTP API is allowed anywhere near it. The field is inert data as far
as the core is concerned; the syncing is somebody else's package.

## Consequences

### Easier

- An importer is idempotent by construction: look the pair up, update what you find,
  create only when there is nothing. Re-running an import is safe.
- A file says where it came from. A human reading the Markdown, `grep`, and a merge
  all see the same record; nothing is hidden in a cache.
- Two systems can claim the same item at once without a schema change, and two
  writers pushing different systems never clobber each other.
- The link is bidirectional in practice: `url` gives a human a way back out.

### Harder — the price we are paying

- **A fifth list field on the item.** Every surface that projects, serialises,
  diffs, merges or renders an item now has one more field to carry, and every one
  that forgets silently drops data. The core's set semantics reduce that to one
  implementation, but MCP, the HTTP API and the web app each still have to expose it.
- **`synced_at` is a lie waiting to happen.** It records when *we* last reconciled,
  not when the other system last changed. Nothing keeps it honest and nothing
  invalidates it; a stale timestamp reads exactly like a fresh one.
- **The uniqueness of a pair is advisory.** Two files can claim the same
  (`system`, `id`) — a merge of two branches that both imported the same issue does
  exactly that. The index reports `W-EXT-DUP` and picks the first file in path order,
  which means the *lookup* is deterministic but the *resolution* is arbitrary: an
  importer will then update one of the two and leave the other diverging silently.
  There is no automatic repair; `gintrack doctor` can only point at it.
- **An open `system` vocabulary means no cross-checking.** `youtrack`, `you-track`
  and `yt` are three different systems as far as the model is concerned, and nothing
  will ever tell you that you typoed one. That is the deliberate cost of forward
  compatibility.
- **It invites a synchronisation feature we have not designed.** The field describes
  a correspondence, not a contract: nothing here says what happens when both sides
  changed. Users will reasonably read `external` as "this is kept in sync", and
  until an ADR says how conflicts are resolved, it is not.
- **Comments carry it too**, which means the comment file format grew a key that
  most comments will never have, and the comment round-trip tests grew with it.

## Alternatives considered

- **`custom:` entry.** Rejected: custom fields are declared per project in
  `project.yaml`, so an importer would have to mutate the project configuration
  before it could write an item, and a project that forgot the declaration would
  produce `E-CF-*` errors on perfectly good imported data. Custom fields are also
  typed as scalars, not as a list of structured entries.
- **`x-external` key.** Rejected: `x-` keys are preserved verbatim and never
  interpreted (R-CF-4). Nothing would validate them, nothing could index them, and
  the O(1) lookup — the whole point — would be impossible without special-casing one
  `x-` key, at which point it is a first-class field with a worse name.
- **A `links` entry with a new `kind`.** Rejected: `links` targets are item ids
  within the reference grammar (R-LINK-2), and every consumer of `links` — the graph,
  the inverse-relation computation, the dangling-reference check — assumes it can
  resolve a target to an item. A link whose target is a URL would have to be excluded
  from all of them, which is a special case pretending to be a reuse.
- **A single `external` object instead of a list.** Rejected: it forecloses the
  second system, and widening a scalar into a list later is a breaking format change
  (R-EVO-3) that would cost a `schema` bump.
- **A separate mapping file, e.g. `.pmngr/external.yaml`.** Rejected: one file that
  every import writes to is a merge conflict on every concurrent import, and it
  separates the record from the thing it describes — exactly the split the data model
  exists to avoid.
- **Deriving the mapping from the item body or a commit trailer.** Rejected: both are
  free text. Parsing them is a heuristic, and neither survives an edit.
