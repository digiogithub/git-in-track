# 21 — Semantic Search: Pando indexes the repository

Status: **as built** for the two indexations of `GIT-EP-0020` and the search and settings
surface over them (`GIT-US-0082`, `GIT-US-0091`, `GIT-US-0096`, `GIT-US-0098`, `GIT-US-0099`).
The exported corpus this document once described is retired; §5 records what was removed and
which of its warnings were withdrawn. The decision is [ADR-036](./adr/ADR-036-pando-indexes-the-repository-directly.md).
Phase: **Phase 9 — agent interface and semantic search** (`GIT-M-0013`)
Audience: contributors working on the search features; anyone configuring Pando against a
git-in-track workspace.

git-in-track's own index answers exact questions: an id, a label, a status, a substring.
Semantic search answers the other kind — *"where did we decide how conflicts are merged?"* —
and that needs an embedding index. Pando already has one, so the companion points Pando at the
repository's own files and searches them through it. Nothing is copied anywhere.

---

## 0. The two indexations

Pando has two indexations and they divide this repository cleanly. Neither writes to it.

| | Knowledge base | Code |
|---|---|---|
| Configured by | `[Remembrances] KBPath` + `KBWatch = true` | `code_index_project`, called by the companion at startup |
| Pointed at | the repository's documentation directory, `<repo>/docs` | the repository root, `<repo>` |
| Walks | `filepath.WalkDir` filtered only by extension — **no exclusions at all** (Pando `internal/rag/kb/sync.go`) | the tree minus every dot-directory, `node_modules`, `vendor`, `dist`, `build`, `__pycache__`, `.git` (Pando `internal/rag/code/indexer.go:234-240`) |
| Therefore covers | the backlog under `docs/.pmngr/` — items and comment threads — and the knowledge-base pages | the source, `README`, `CHANGELOG`, and the Markdown that lives outside the knowledge base |
| Searched with | `kb_search_documents` | `code_hybrid_search` (with `include_docs: true`; Pando excludes Markdown by default) |
| A hit carries | a chunk of the document plus its front matter as Pando stored it | a symbol — a heading, for Markdown — a file path and a line, and **no front matter at all** |

**Why the backlog is unreachable from the code side.** The code indexer skips any directory
whose name starts with a dot, and the backlog lives in `docs/.pmngr/`. So the code index cannot
see an item, a comment or a board even in principle, which is exactly the half the knowledge-base
indexation covers. The exclusions are hardcoded and `code_index_project` exposes only a
`languages` filter, so this is a property of Pando, not a setting to be kept right.

**Where they do overlap: `docs/*.md`.** Those files are knowledge-base documents through
`KBPath` *and* indexable Markdown for the code indexer — it has a tree-sitter grammar that turns
each heading into a symbol. One file can therefore come back from both sides, with two scores on
two scales, for one query. See §0.2.

### 0.1 Registration happens when the server starts

`gintrack serve` registers every mounted repository root with Pando as a code project
(`internal/server/search_register.go`), not `gintrack agent init`: `agent init` writes
configuration once, by one person, while a fresh clone becomes searchable by whoever starts the
server. The first index of this repository — 937 indexable files — took **62 s** wall clock, all
of it after `code_index_project` had already returned a job id, so it runs eagerly at startup
rather than lazily on the first search.

- **The project id** is Pando's sanitisation of the repository's absolute path
  (`pando.SanitizeProjectID`), which is the same value `search.pando.projectId` defaults to, so
  the registration and the search can never disagree about which project is being read. A mount
  id alone would not do: it is unique within one workspace, while Pando's project table is shared
  by every workspace on the machine.
- **It is idempotent.** `code_list_projects` is asked first, and a project whose id already
  points at this working tree is left alone — neither duplicated nor reindexed. (Pando's indexer
  also skips a file whose content hash has not changed, so even an explicit reindex is
  incremental, but the promise here is that nothing is asked for at all.) A project id pointed at
  *another* tree is re-registered, because searching it would answer another repository's
  questions.
- **It never blocks startup.** The pass runs in a goroutine after the listener is up, every call
  is bounded by the client's own deadline, and a Pando that is down or slow leaves the companion
  fully functional with no code search. What happened is stated per repository in
  `GET /api/v1/search/settings` → `indexed[].code` (`status` one of `off`, `registered`,
  `indexing`, `unavailable`, plus a `note` in words), which the settings card renders: the reason
  there is no code search is readable in the UI, not only in the log.

### 0.2 Duplicates are merged at presentation

A `docs/` file that both indexes returned is shown **once**
(`internal/server/search_code.go`, `semanticMerge`):

- **The key is the resolved file path**, scoped by the repository the document was resolved in —
  not the path Pando reported. The knowledge-base side reports a path relative to `KBPath`
  (`architecture/overview.md`) and the code side one relative to the repository root
  (`docs/architecture/overview.md`); both are resolved against git-in-track's own index first, so
  they meet on the same key. An item is keyed by its id instead, which is unique workspace-wide.
- **The better score wins**, where "better" is relative to the best hit of its own leg. The raw
  scores are not comparable — a knowledge-base fusion score sits around 0.016 while a code score
  is already normalised to 0..1 against its own top hit — so each leg is divided by its own best
  score before the two are compared or ordered. The surviving row keeps its own raw `score`.
- **The loser fills the winner's gaps.** A code hit has no front matter: it knows a symbol name
  and a path, while the knowledge-base hit knows the title, the item id and the project. Whichever
  won, the row a user sees carries both, and a `file` kind is upgraded to the `page` or `item` the
  other side recognised.
- **The alternative was rejected.** Filtering `docs/` out of the code side would mean a path
  filter on every query — Pando's code exclusions cannot be configured — and the two indexes
  answer different shapes of question over the same file (a paragraph versus a heading symbol),
  which is worth keeping both of.

Every hit reaches the API with `index: "kb" | "code"` alongside `source: "pando"`, and the web
search labels the row with it, so a code hit is never mistaken for a backlog item.

---

## 1. Configuring Pando

`gintrack agent init` writes this into the repository's `.pando.toml`, and `--kb-path`
overrides the directory for a layout it cannot guess (docs/07 §4.18):

```toml
[Remembrances]
Enabled = true
KBPath = "/home/you/src/acme-api/docs"   # the repository's own documentation folder
KBAutoImport = true
KBWatch = true
```

Three things about that block are deliberate.

**`KBPath` is the documentation folder, not the repository root.** The KB walk applies no
exclusions at all, so a root would be indexed whole — `.git`, `node_modules`, `dist` and every
build artifact — and, with the watcher on, would exhaust the host's inotify watches. Pointed at
`docs/` it reaches the backlog under `docs/.pmngr/` and the knowledge-base pages, and nothing
else. See §4.

**`KBWatch = true`, which is Pando's own default.** An edit to an item or a page is reindexed as
it happens. See §5 for the warning this replaces.

**Nothing is copied.** The files Pando indexes are the repository's own, committed files. There
is no second directory to keep current, nothing to prune, and no window in which the index is
one export behind the working tree.

The repository root is registered as a Pando **code** project separately, by the companion, when
`gintrack serve` starts (§0.1). Nothing has to be run by hand for that.

---

## 2. What a hit is, and how it resolves

A Pando hit is a **candidate**, never a record. `internal/server/search_pando.go` parses the path
Pando reports and resolves it against git-in-track's own index; every field the UI shows — title,
status, project, id — is re-read locally.

| Path Pando reports | Resolves to | `kind` |
|---|---|---|
| `.pmngr/<type>/<ID>-<slug>.md` | the item with that id | `item` |
| `.pmngr/comments/<ID>/<file>` | the **item the comment belongs to**, `match: "comment"` | `item` |
| `<path>.md` under the documentation folder | the knowledge-base page | `page` |
| anything else that is still on disk | a plain file result: a path, a snippet, no front matter | `file` |

Paths are parsed **from the right**, so a deployment that reports the path with a prefix — an
absolute `metadata.source_path`, or a `file_path` relative to a different `KBPath` — still
resolves.

- **Comment hits collapse.** Several comments of one item that all match the query become one
  row for the item, carrying the best-scoring fragment and `moreMatches` counting the rest, so a
  thread that answers a query in five places does not fill the result list with one item.
- **A foreign path is not a ghost and not a drop.** A file that is neither a backlog file nor a
  page this index holds — a hand-edited stray, or a document under the documentation folder that
  git-in-track does not own — comes back as a `file` hit. Only a path that is no longer on disk
  is dropped, because a dangling row is worse than a thin result.
- **Drops are counted.** The resolver reports how many candidates it dropped per query, on a
  debug log line with a running total, so "search is missing things" has a number behind it.
- **Every hit names its index.** `index: "kb" | "code"` travels to the API alongside
  `source: "pando"` (§0.2).

---

## 3. Reindexing

`POST /api/v1/search/reindex` is the catch-up pass. It is not how the index normally changes —
the watcher is — but it is what a companion started after a batch of commits landed needs, and
what a Pando whose watcher was off needs.

The job walks two phases and publishes `search.progress` for each (docs/07 §5.6):

| Phase | What runs |
|---|---|
| `code` | `code_index_project` for every mounted repository's working tree, one Pando job id per repository |
| `kb` | Pando's `POST /api/v1/remembrances/kb/reindex`, once, for the knowledge base |
| `completed` / `failed` | the terminal frame; `failed` means at least one repository's code half failed, and the successful halves are still in the job |

The knowledge-base half is honest about doing nothing. With no `search.pando.restUrl` configured
there is no route to call — the reindex lives on `pando serve`'s REST surface only — and `kbNote`
says so, adding that Pando's own watcher still follows the documentation directory. It never
claims a reindex that did not happen.

A second call while one is running is refused with `search_reindex_running` (409) and the running
job is untouched. A companion with no Pando endpoint answers `search_not_configured` (400).

**One repository.** A body of `{"repo":"<mount id>"}` limits the `code` phase to that repository
(GIT-US-0101): it is registered and indexed alone, the other repositories are not touched, and the
`kb` phase runs as usual. This is what the workspace list's **Enable semantic search** button posts
for a repository whose code index is `off` or `unavailable`; afterwards that row reads `indexing`,
or `unavailable` with Pando's reason if it was refused. An unknown id is refused with
`repo_not_registered` (404). Where Pando is not configured at all, the workspace row links to the
settings card instead, and browser-only mode shows no control.

---

## 4. The KB walk has no exclusions — which is why `KBPath` is `docs/`

Pando's knowledge-base sync is a bare `filepath.WalkDir` filtered only by extension
(`internal/rag/kb/sync.go:125`). There is no hidden-directory rule, no `node_modules` rule, no
ignore file. **Pointed at a repository root it would walk the whole working tree**, `.git`
included, and with `KBWatch = true` it would register an inotify watch per directory.

That is a real constraint and it is why `KBPath` names the documentation folder. It is **not** a
reason to keep the index outside the repository: the documentation folder is inside the
repository, is committed, and is exactly the half that answers backlog questions.

**It is also not true of the code indexer.** `internal/rag/code/indexer.go:234-240` skips every
dot-directory, `node_modules`, `vendor`, `dist`, `build`, `__pycache__` and `.git`. That is why
the repository root can be registered as a code project without indexing `node_modules`, and it
is also why the code index cannot see `docs/.pmngr/` — a dot-directory — which is precisely the
half the KB indexation covers. The two exclusion sets are properties of Pando, hardcoded, with
no setting to get wrong.

---

## 5. What was removed, and the warning that was withdrawn

Until `GIT-EP-0020` the companion exported a second copy of every item and knowledge-base page
into `<cacheDir>/pando-kb/<repo id>/`, outside every repository, and configured Pando with
`KBWatch = false`. `internal/pandosync`, its hub subscription, the `search.pando.corpusDir`
configuration key, the `corpusDir`/`corpora`/`documents`/`lastExport` fields of
`GET /api/v1/search/settings` and the corpus directory input in the settings card are all gone.
A configuration that still sets `search.pando.corpusDir` is refused by name, citing this epic,
rather than ignored. **A corpus directory left behind by an older version is not read any more
and is safe to delete by hand.**

Two sentences appeared throughout this repository's documentation, its templates and its code
comments. Both are withdrawn, and a reader who remembers them should know which was wrong.

> ⚠️ **Withdrawn (false):** *"Pando's KB watcher re-writes the documents it processes without
> parsing their front matter, so every incremental edit it handles strips the metadata the
> exporter wrote."*

Verified against Pando at commit `710a39281`, the version installed here: `internal/rag/kb/watcher.go`
contains **no write call of any kind**. It stats, reads, and calls `AddDocument` /
`UpdateDocument` / `DeleteDocument`, which are database operations. The bug behind the warning was
real and is documented in Pando's own `internal/rag/kb/repair.go:22-40`, but it damaged **metadata
in the database, not files on disk**, and it was fixed under PANDO-US-0003/0004: the watcher now
shares `buildDocumentMetadata` with the sync path, self-writes are suppressed for three seconds,
and a startup repair re-parses documents whose stored metadata lost keys their source still
declares. `KBWatch = true` is Pando's own default.

> ⚠️ **Withdrawn (true, but applied to the wrong thing):** *"Pando's directory walk has no
> hidden-directory or `node_modules` exclusion, so `KBPath` must never point at a repository."*

The first half is true of the **KB** walk and remains the reason `KBPath` is the documentation
folder rather than the repository root (§4). The conclusion does not follow: the documentation
folder is inside the repository. And the claim is false of the **code** indexer, which skips
those directories, which is what makes registering the repository root safe.

**The write hazard is real, and it is not the watcher.** `kb_add_document`, `kb_delete_document`
and the memory `remember` / `forget` path mirror a document to disk through a serializer that
emits Pando's typed front-matter keys alone. A call against an existing repository path would
rewrite that file without `id`, `status` or `parent` — and for gintrack a mismatched `id` or
`type` is a hard parse error that removes the item from the index until it is fixed. What keeps
those tools away from repository files is the `[AGUI] Tools` allow-list `gintrack agent init`
writes, which admits only the reading KB tools plus `code_hybrid_search` and `code_find_symbol`.
That is a configuration boundary, not a code boundary: see ADR-036 and docs/20 §6.2.

**Tag filtering inside Pando is lost.** The exporter synthesised a `tags` list; real backlog files
carry `labels`, which is not one of Pando's reserved keys. `labels` reaches a hit's `metadata`
verbatim through `MergeUnknownFrontMatterKeys`, but Pando's own tag filter no longer has anything
to filter on. That is accepted — no derived data is written into source front matter — and it
costs nothing downstream, because a consumer resolves a hit to an id and re-reads every field
from git-in-track's own index anyway.

---

## 6. Searching

Search itself is specified with `GIT-US-0082`. The contract the two indexations impose on it is short:

- A Pando hit is a **candidate**, not a record: resolve it and re-read every field the UI shows
  from git-in-track's own index. §2 is the resolution table.
- A path that is no longer on disk is a stale hit from an index Pando has not caught up with
  yet. Drop it rather than rendering a ghost.
- Both legs run inside one latency budget (`pandoBudget`, 300 ms) and in parallel, because they
  are two calls to the same Pando. The knowledge-base leg is the one a failure degrades over: a
  code leg that failed on its own costs the answer its code hits and nothing else.
- `code_hybrid_search` excludes Markdown unless `include_docs` is set, and the companion always
  sets it: `docs/*.md`, the README and the changelog are what a question about this repository is
  most often answered from.
- The companion reaches all of this through `vault.Workspace`: the host installs a
  `vault.SemanticSearcher` with `SetSemanticSearcher`, and every caller of the core contract —
  the REST endpoint, the MCP `search_semantic` tool — reaches it through the `search.semantic`
  method. A session with no backend installed (every browser-only one) answers `unavailable`
  rather than an empty result.

---

## 7. Operating notes

| Question | Answer |
|---|---|
| Where does Pando keep its index? | In its own database, under Pando's data directory. Nothing of it is in this repository, and nothing of this repository is copied anywhere. |
| Can I delete the old `<cacheDir>/pando-kb/` directory? | Yes. Nothing reads it any more (§5). |
| Is `KBWatch = true` safe? | Yes. The watcher performs no file write at all. The tools that do write are excluded by the `[AGUI] Tools` allow-list (§5, ADR-036). |
| Why is `KBPath` `docs/` and not the repository root? | The KB walk has no exclusions, so a root would be indexed whole. The code indexer, which *is* pointed at the root, does exclude (§4). |
| Does Pando see the backlog? | Yes, through the KB indexation: `docs/.pmngr/` is under `KBPath`. The code indexation cannot see it, because it skips dot-directories. |
| Why did my edit not show up in search? | The watcher reindexes as it happens; if it was off, or the companion missed a batch of commits, run `POST /api/v1/search/reindex` (§3). |
| Why are there no tags in Pando? | Backlog files carry `labels`, not `tags`, and the exporter that synthesised `tags` is gone. Filtering by tag inside Pando is lost on purpose (§5). |
| A search hit is a file with no title — is that a bug? | No. A path that is neither a backlog file nor a knowledge-base page comes back as a plain `file` result (§2). |
| Does the index contain secrets? | It contains exactly what the repository contains. Treat it with the same care. |

---

## 8. Related documents

- [ADR-036](./adr/ADR-036-pando-indexes-the-repository-directly.md) — why the exported corpus was retired
- [Agent interface](./20-agent-interface.md) — `gintrack agent init`, the tool allow-list, the security posture
- [CLI and API](./07-cli-and-api.md) — `search.pando` configuration, the search settings endpoints
- [Research: Pando gap analysis](./research/2026-09-13-pando-gap-search-and-fit.md) — accurate when written, superseded by this document
