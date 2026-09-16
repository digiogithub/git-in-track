---
id: GIT-EP-0020
type: epic
title: Pando indexes the repository directly; retire the exported corpus
status: done
priority: high
milestone: GIT-M-0013
author: mcp
labels: [core, server, web, cli, docs]
created: 2026-09-16T12:45:38Z
updated: 2026-09-16T15:56:12Z
started: 2026-09-16T15:07:24Z
closed: 2026-09-16T15:56:12Z
---

## Description

Phase 9 shipped a corpus exporter that writes a second copy of every item and KB page to `<cacheDir>/pando-kb/<repo id>/`, outside every repository, and configures Pando with `KBWatch = false`. Both decisions rest on a premise that is false, and on a goal the maintainer never held.

**The false premise.** git-in-track's own docs state that Pando's KB watcher "re-writes the documents it processes without parsing their front matter, so every incremental edit it handles strips the metadata the exporter wrote" (`docs/20-agent-interface.md:264`, `docs/21-semantic-search.md:131`, `internal/pandosync/doc.go:9`, the generated `.pando.toml` comment). Verified against Pando at commit `710a39281`, the version installed here: `internal/rag/kb/watcher.go` contains no write call of any kind — it stats, reads, and calls `AddDocument`/`UpdateDocument`/`DeleteDocument`, which are database operations. The bug was real, is documented in Pando's own `internal/rag/kb/repair.go:22-40`, damaged **metadata in the database rather than files on disk**, and was fixed under PANDO-US-0003/0004: the watcher now shares `buildDocumentMetadata` with the sync path, self-writes are suppressed for 3 s, and a startup repair re-parses documents whose stored metadata lost keys their source still declares. `KBWatch = true` is Pando's own default.

**The goal.** Project-management Markdown and the knowledge base belong inside the project's git repository, so that a clone carries the knowledge and it does not disappear with one person's machine. That is already true of the sources: KB pages are the 60 `.md` under `docs/` outside `.pmngr/`, backlog items are the 353 files under `docs/.pmngr/`, all committed. 353 + 60 = 413, exactly the size of the exported corpus. The corpus is a 1:1 duplicate of content that already travels with the clone, adding only rendered front matter and an identity line, and nothing ever reads it back — the only coupling is that `internal/server/search_pando.go:222-257` parses a hit's path to recover an item id and then re-reads every field from git-in-track's own index.

So Pando indexes the repository's own files, and the corpus goes away.

**The two indexations.** Pando has two, and they divide the work cleanly:

- **KB indexation** (`[Remembrances] KBPath`, watcher, the `kb_*` tools) walks its directory with no exclusions at all — bare `filepath.WalkDir` filtered only by extension (`internal/rag/kb/sync.go:125`). Pointed at `docs/`, it reaches the backlog under `docs/.pmngr/` and the KB pages, and nothing else.
- **Code indexation** (`code_index_project`, the `code_*` tools) walks a project root but skips hidden directories, `node_modules`, `vendor`, `dist`, `build`, `__pycache__` and `.git` (`internal/rag/code/indexer.go:234-240`), never writes outside its own tests, and does index Markdown — it has a tree-sitter grammar that turns each heading into a symbol. Its exclusions are hardcoded rather than configurable; `code_index_project` exposes only a `languages` filter.

They do not fight: the code indexer *cannot* see `docs/.pmngr/` because it is a dot-directory, which is exactly the half the KB indexation covers. So `KBPath` is the repository's `docs/`, and the repository root is registered as a code project for everything else — README, CHANGELOG, source, and the Markdown that lives outside the KB.

**Front-matter coexistence.** No namespacing work is needed. gintrack preserves keys it does not know: on items they land in `Item.Extra` and are re-emitted sorted after the canonical keys (`internal/core/yamlemit.go:196-206`, pinned by `internal/core/frontmatter_test.go:178`), and `docs/03-data-model.md:1518` (R-CF-4) already reserves the `x-` prefix for third-party tools. Pando keeps a fixed reserved list (`internal/rag/kb/frontmatter.go:304-321`); of the ten keys the exporter emits only `tags` and `aliases` are on it, and they mean the same thing on both sides. The other eight reach `metadata` verbatim through `MergeUnknownFrontMatterKeys`. Note `updated` is not Pando's `updated_at`.

The collision that does matter runs the other way: if Pando wrote a key that *is* in gintrack's `canonicalKeyOrder` — `id`, `type`, `title`, `status`, `parent` — gintrack would parse it as that field, and a mismatched `id` or `type` is a hard parse error that removes the item from the index until fixed. The write hazard is not the watcher: `kb_add_document`, `kb_delete_document` and the memory `remember`/`forget` path mirror documents to disk through `SerializeFrontMatter`, which emits only Pando's typed struct, so a call against an existing repository path would rewrite the file with Pando's keys alone. `resolveMirrorDocumentPath` only stops a path escaping the base; it does not stop an overwrite. The lever is the `[AGUI] Tools` allow-list, already delivered by PANDO-EP-0002.

**Tags.** Real backlog files carry `labels`, not the `tags` the exporter synthesised, so filtering by tag inside Pando is lost. That is accepted: a hit already carries every non-reserved front-matter key in `metadata` plus an absolute `source_path`, and git-in-track re-reads its own index for each hit regardless. No derived data is written into source files to close the gap.

## Acceptance Criteria

- [ ] `internal/pandosync` is gone, along with its hub subscription, the `search.pando.corpusDir` config key, the REST `corpusDir` field and the corpus directory input in the web settings card.
- [ ] A Pando search hit resolves to an item id from a real repository path — `docs/.pmngr/<type>/<ID>-<slug>.md` and `docs/<path>.md` — rather than the retired `<PROJECT>/items/<ID>.md` layout.
- [ ] `gintrack agent init` generates `KBPath` pointing at the repository's documentation directory, `KBWatch = true`, and an `[AGUI] Tools` allow-list that excludes every KB tool that writes.
- [ ] The repository root is registered with Pando as a code project, so Markdown outside the KB and the source itself are searchable read-only.
- [ ] Every document, comment and template that states the watcher destroys front matter, or that the corpus must live outside a repository, is corrected rather than deleted — the reversal and its evidence are recorded.
- [ ] An ADR records the decision. There is no ADR for the original choice, so it supersedes nothing and must state the original reasoning and why it was wrong.
- [ ] No derived data is written into item or KB front matter.

## Notes

Evidence for the watcher reversal: Pando tree `/www/MCP/Pando/pando` at commit `710a39281`; `grep -c "WriteFile\|os.Create\|WriteDocumentToFilesystem" internal/rag/kb/watcher.go` returns 0. Mirror writes come only from `internal/llm/tools/remembrances_kb.go:200,214,482`, `internal/llm/tools/remembrances_memory.go:280`, `internal/rag/kb/memory.go` (six sites) and `internal/api/handlers_remembrances_kb.go:134`.

The hardest blocker in the current code is `refuseRepository` in `internal/pandosync/exporter.go`: `New()` errors if the corpus root contains `.git`, pinned by `TestNewRefusesACorpusInsideARepository` (`internal/pandosync/exporter_test.go:620`). Retiring the exporter removes it rather than inverting it.

Retiring the exporter also removes a feedback loop that an in-repo corpus would create: `internal/server/watch.go:231` publishes `item.changed`/`file.changed` for any repository write, the exporter consumes that hub, and an exporter that wrote into the repository would re-trigger itself.

`POST /api/v1/remembrances/kb/reindex` exists on the Pando side (`internal/api/handlers_remembrances_kb.go:153`, delivered under PANDO-US-0001) and refuses a concurrent run with 409. There is no MCP tool for a KB reindex; `code_reindex_file` is code-index only.

Still open on the Pando side and not a dependency of this epic: PANDO-US-0031, the MCP gateway deadlock that keeps GIT-T-0118 in review.

Maintainer decisions, 2026-09-16: retire the exporter rather than move it into the repository or keep it behind a flag; accept the loss of tag filtering rather than write derived keys into source files; exclude the writing KB tools from the allow-list rather than fix Pando's serializer.
