# ADR-036 — Pando indexes the repository's own files; the exported corpus is retired

- **Status:** Accepted
- **Date:** 2026-09-16
- **Phase:** 9 (Agent interface and semantic search)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-002](ADR-002-git-as-only-sync.md), [ADR-010](ADR-010-mcp-agent-surface.md), [ADR-035](ADR-035-agent-interface-over-ag-ui.md)
- **Supersedes:** nothing. The corpus exporter shipped under `GIT-EP-0019` / `GIT-US-0073` without an ADR, so the original reasoning is stated here in §Context rather than linked.
- **Implements:** `GIT-EP-0020` — Pando indexes the repository directly; retire the exported corpus
- **Research:** [Pando gap analysis: search and fit](../research/2026-09-13-pando-gap-search-and-fit.md) (accurate when written, conclusion superseded by this ADR)

## Context

### What was decided before, and why

Phase 9 gave git-in-track semantic search by pointing a local Pando at a **corpus**: the
companion (`internal/pandosync`) wrote a second Markdown copy of every backlog item and every
knowledge-base page into `<cacheDir>/pando-kb/<repo id>/<PROJECT>/{items,kb}/`, outside every
repository, and the generated `.pando.toml` set `[Remembrances] KBPath` to that directory with
`KBAutoImport = true` and `KBWatch = false`. The exporter followed the companion's event hub, an
overflow triggered a full re-export, and `New()` refused outright to write a corpus root that
contained a `.git` directory (`TestNewRefusesACorpusInsideARepository`).

That design rested on four arguments, all of which were believed at the time:

1. **The watcher destroys front matter.** The 2026-09-13 research note recorded, as finding A5
   and rated critical, that Pando's KB watcher does not parse front matter, so every incremental
   edit it handles strips the `tags` the initial sync stored — with 523 of 539 documents in
   Pando's own knowledge base already measured as having lost theirs. `KBWatch = false` followed
   directly, and so did the decision to write `tags` and `aliases` ourselves into an export we
   controlled.
2. **The KB walk has no exclusions.** Pando's sync is a bare `filepath.WalkDir` filtered only by
   extension. Pointed at a repository root it walks `.git`, `node_modules`, `dist` and every
   build artifact, and with a watcher on it exhausts the host's inotify watches. This was read as
   "`KBPath` must never point at a repository", full stop.
3. **Pando's tools write back.** `kb_add_document` mirrors a document to disk, and the research
   note also found a mirror ⇄ watcher feedback loop with no suppression (A7). An index directory
   that Pando could write into and that was *also* the repository was an obvious hazard.
4. **Derived data does not belong in git.** Rendered front matter, a synthesised `tags` list and
   an identity line are derived; keeping them outside every repository guaranteed that nothing
   derived could ever be committed.

### The evidence that overturned it

The premise in (1) is **false for the version installed here**, and it was the load-bearing one.

Verified against the Pando tree at `/www/MCP/Pando/pando`, commit `710a39281`:

- `internal/rag/kb/watcher.go` contains **no write call of any kind**. It stats, reads, and calls
  `AddDocument` / `UpdateDocument` / `DeleteDocument`, which are database operations. There is no
  `WriteFile`, no `os.Create`, no filesystem mirror on that path.
- The bug behind the warning was real, and Pando documents it itself in
  `internal/rag/kb/repair.go:22-40` — but it lost **metadata in the database, not bytes on disk**.
  The observation that 523 of 539 documents had lost their tags was an observation about Pando's
  database, not about anybody's files.
- It was fixed under `PANDO-US-0003` / `PANDO-US-0004`: the watcher now shares
  `buildDocumentMetadata` with the sync path, self-writes are suppressed for three seconds
  (closing A7), and a startup repair re-parses documents whose stored metadata lost keys their
  source still declares.
- `KBWatch = true` is Pando's own default (`internal/config/config.go`).

(2) is **true, but was applied to the wrong walk.** The exclusion-free walk is the *knowledge
base* walk (`internal/rag/kb/sync.go:125`), and it remains the reason `KBPath` names the
documentation folder rather than the repository root. It is not a reason to keep the indexed
files outside the repository — the documentation folder is inside it. And it is not true of the
**code** indexer (`internal/rag/code/indexer.go:234-240`), which skips every dot-directory,
`node_modules`, `vendor`, `dist`, `build`, `__pycache__` and `.git`. Those two exclusion sets
happen to partition this repository exactly: the code index cannot see the backlog, because
`docs/.pmngr/` is a dot-directory, and the KB index covers precisely that half.

(3) is **true and remains true**, and it is the one hazard this ADR does not remove. See
*Consequences*.

(4) turned out to be an argument for nothing. Everything in the corpus already travelled with the
clone. The corpus held 413 documents — the 353 files under `docs/.pmngr/` plus the 60 `.md` pages
under `docs/`, exactly — and added only rendered front matter and an identity line. Nothing ever
read it back: the only coupling was that the search resolver parsed a hit's path to recover an
item id and then re-read every field from git-in-track's own index.

### What the maintainer actually wants

Project-management Markdown and the knowledge base live **inside** the project's git repository,
so that a clone carries the knowledge and it does not disappear with one person's machine
(ADR-001, ADR-002). That is already true of the sources. A duplicate of them, held outside every
repository, kept current by an event subscription that can drop, is the opposite of that goal
wearing its clothes.

## Decision

**Pando indexes the repository's own committed files. The exported corpus is retired.**

Concretely:

- `internal/pandosync` and its hub subscription are deleted. So are the
  `search.pando.corpusDir` configuration key — a configuration that still sets it is **refused by
  name**, with a message citing `GIT-EP-0020`, rather than ignored — the `corpusDir`, `corpora`,
  `documents` and `lastExport` fields of `GET|PATCH /api/v1/search/settings`, and the corpus
  directory input in the web settings card.
- `gintrack agent init` generates `[Remembrances] KBPath = <repo>/<docs folder>`,
  `KBAutoImport = true` and **`KBWatch = true`**, Pando's own default.
- The **repository root** is registered with Pando as a code project by `gintrack serve`, not by
  `agent init`: a fresh clone becomes searchable by whoever starts the server rather than by one
  person remembering a command. The project id is Pando's sanitisation of the repository's
  absolute path, which is the same value `search.pando.projectId` defaults to, so the
  registration and the search can never disagree about which project is being read.
- `GET /api/v1/search/settings` reports `indexed[]` — one row per mounted repository with its
  `root`, its documentation directories, the `items` / `pages` / `comments` git-in-track's own
  index found under them, and the state of the code registration — in place of a report about a
  directory that no longer exists.
- A search hit resolves from a **real repository path**: `.pmngr/<type>/<ID>-<slug>.md` is an
  item, `.pmngr/comments/<ID>/<file>` resolves to the item the comment belongs to, a `.md` under
  the documentation folder is a page, and anything else still on disk comes back as a plain
  `file` result. Every hit names the index it came from, `kb` or `code`, and the two are merged
  by resolved path with each leg normalised by its own top score.
- **The `[AGUI] Tools` allow-list is the whole mitigation for Pando's writing tools.** The
  generated list admits `gintrack_*`, `kb_search_documents`, `kb_get_document`,
  `kb_related_documents`, `code_hybrid_search` and `code_find_symbol`, and nothing that writes.
- **No derived data is written into item or knowledge-base front matter.**

Measured on this repository: the first full code index covered 937 files in **62.4 s**, all of it
after `code_index_project` had already returned a job id, which is why the registration runs in a
goroutine after the listener is up and never blocks startup.

## Consequences

### Easier

- **One copy of the truth.** What Pando indexes is what is committed. There is no window in which
  the index is one export behind the working tree, no pruning, no overflow-triggered re-export,
  and no "is my corpus current?" question — which is why the settings endpoint can answer with
  what git-in-track's own index found under `KBPath` instead.
- **A clone is searchable by starting the server.** Nothing has to be exported first, and nothing
  has to be re-exported after a `git pull`.
- **An edit is reindexed as it happens**, because the watcher is on and safe.
- **Comments are searchable.** They are files under `docs/.pmngr/comments/`, so the KB indexation
  reaches them; a comment hit resolves to its parent item.
- **A whole package, its tests, a hub subscription, an event vocabulary, a configuration key, a
  REST field group and a UI input are gone.** The failure modes they carried went with them.

### Harder, and what we now live with

- **Tag filtering inside Pando is lost.** The exporter synthesised a `tags` list; real backlog
  files carry `labels`, which is not one of Pando's reserved front-matter keys. `labels` still
  reaches a hit's `metadata` verbatim through `MergeUnknownFrontMatterKeys`, but Pando's own tag
  filter has nothing to filter on any more. This is accepted rather than closed, because closing
  it would mean writing derived keys into source files.
- **The allow-list is a configuration boundary, not a code boundary.** `kb_add_document`,
  `kb_delete_document` and the memory `remember` / `forget` path mirror a document to disk
  through `SerializeFrontMatter`, which emits Pando's typed struct alone.
  `resolveMirrorDocumentPath` only stops a path escaping the base; it does not stop an overwrite.
  So a call against an existing repository path would rewrite that file with Pando's keys only —
  and for gintrack a missing or mismatched `id` or `type` is a **hard parse error** that removes
  the item from the index until a human fixes it. What prevents this is the `[AGUI] Tools`
  allow-list in the generated `.pando.toml`, and nothing else. **The hazard therefore remains
  live for any repository pointed at Pando with a configuration that did not come from
  `gintrack agent init`** — a hand-written `.pando.toml`, a `[AGUI] Tools` entry added later, a
  Pando TUI session in the same working directory, or the MCP gateway turned on, which puts the
  tools behind `mcp_call_tool` where the allow-list cannot see them. Anyone editing the generated
  file has to know that; docs/20 §6.2 says so where an operator will read it.
- **`KBPath` still cannot be the repository root.** The KB walk has no exclusions at all, so a
  root would be indexed whole and, with the watcher on, would exhaust the host's inotify watches.
  The documentation folder is the answer, and `--kb-path` exists for layouts this cannot guess.
  A repository whose backlog does not live under a documentation folder is not covered by the
  default.
- **We depend on a specific Pando version's behaviour.** The watcher is safe at commit
  `710a39281`. A future Pando that reintroduces a filesystem write on the watch path would write
  into the repository, not into a throwaway cache directory. The evidence is pinned in this ADR
  and in docs/21 §5 so the next person can re-check it rather than re-derive it.
- **Pando indexes more than the backlog.** The repository root as a code project means source,
  README and CHANGELOG are in the index. That is the point, but it also means a secret committed
  anywhere in the tree is reachable by a search — the same exposure the repository already has,
  now with a second reader.
- **A corpus directory left by an older version is orphaned.** Nothing reads
  `<cacheDir>/pando-kb/` any more. It is safe to delete by hand, and the CHANGELOG says so;
  nothing deletes it for the user, because a companion that removes directories it no longer
  understands is worse than one that leaves them.

## Alternatives considered

**Move the corpus inside the repository and commit it.** Rejected. It keeps every cost of the
exporter — a second copy, a sync model, a prune, an overflow path — and adds a new one: a
committed duplicate of every item, doubling every diff and every merge conflict. It would also
re-create the feedback loop the exporter was free of, since `internal/server/watch.go` publishes
`item.changed` / `file.changed` for any repository write and an exporter writing into the
repository would re-trigger itself.

**Keep the exporter behind a flag.** Rejected. A flag would keep the whole package, its tests,
its hub subscription and its configuration key alive to serve a path that is wrong on the
evidence, and would keep the false warning in the documentation as a supported option. Two
supported topologies also means two sets of path-resolution rules in the search resolver, which
is the one place a mistake shows up as a wrong answer rather than an error.

**Write derived `tags` / `aliases` into the source front matter.** Rejected, and this is the
maintainer's decision rather than a technical one. It would recover Pando-side tag filtering, at
the price of putting generated data into hand-edited files — noise in every diff, a class of
merge conflict that carries no meaning, and a standing invitation to add the next derived key.
The feature it buys is redundant: a hit already carries every non-reserved front-matter key in
`metadata` plus an absolute `source_path`, and git-in-track re-reads its own index for each hit
regardless.

**Fix Pando's mirror-write serializer instead of restricting the allow-list.** Rejected as scope.
The correct fix — `kb_add_document` against an existing file should merge unknown front-matter
keys rather than emit Pando's typed struct alone — belongs in Pando, and no story is opened
against it here. The allow-list is the whole mitigation, with the residual hazard stated above.
It is also the faster boundary: the allow-list ships in a file `gintrack agent init` already
writes, while a serializer fix would gate this epic on another project's release.

**Push documents to Pando with `kb_add_document` instead of letting it pull.** Rejected for the
same reason the exporter is retired: it is a second copy with a sync model, it needs the writing
tools the allow-list exists to exclude, and it puts the documents in Pando's typed shape rather
than the repository's.

**Point `KBPath` at the repository root and accept the walk.** Rejected on the evidence of the
KB walk having no exclusions at all: `.git` and `node_modules` would be indexed and watched. The
documentation folder gives the same coverage of what matters — backlog and knowledge base — with
none of that, and the code indexation covers the rest of the tree with exclusions of its own.
