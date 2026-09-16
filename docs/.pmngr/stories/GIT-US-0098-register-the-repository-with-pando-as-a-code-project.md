---
id: GIT-US-0098
type: story
title: Register the repository with Pando as a code project
status: done
priority: medium
parent: GIT-EP-0020
milestone: GIT-M-0013
author: mcp
labels: [server, cli, docs]
created: 2026-09-16T12:46:50Z
updated: 2026-09-16T15:56:02Z
started: 2026-09-16T15:44:29Z
closed: 2026-09-16T15:56:02Z
---

## Description

As someone asking about this project, I want the source and the Markdown that lives outside the knowledge base to be searchable too, so that a question about the code or about a README is answered from the repository rather than from nothing.

Pando's code indexation is the other half of the split. `code_index_project` walks a project root, skips hidden directories, `node_modules`, `vendor`, `dist`, `build`, `__pycache__` and `.git` (`internal/rag/code/indexer.go:234-240`), never writes outside its own tests, and indexes Markdown through a tree-sitter grammar that turns each heading into a symbol. Its exclusions are hardcoded, not configurable; the only filter `code_index_project` exposes is `languages`.

**Registration happens in `gintrack serve --agent`**, which already talks to Pando over REST and is what runs after a fresh clone: whoever clones the repository and starts the server gets an indexed project without running anything else. `agent init` writes configuration and is run once by one person, so it is the wrong hook for something a clone needs. Registration must be idempotent — an already-registered project is neither duplicated nor fully reindexed unless asked — and must never block startup: a Pando that is down or slow degrades to no code search, with the reason visible in the search settings card.

**Code results appear in the web search alongside items and KB pages**, labelled with the index they came from so a code hit is never mistaken for a backlog item. Searching "where is the exporter" from the interface should stop returning nothing.

**The overlap on `docs/*.md` is resolved by deduplicating at presentation.** Those 60 files are KB documents through `KBPath` and indexable Markdown for the code indexer, so one paragraph can arrive twice with different scores. The search backend merges results by file path and keeps the best-scoring one, rather than filtering the documentation directory out of the code side — Pando's exclusions are fixed, so filtering would mean a path filter on every query, and the two indexes answer different shapes of question (a paragraph versus a heading symbol) that are worth keeping both of.

## Acceptance Criteria

- [ ] `gintrack serve --agent` registers the repository root as a Pando code project, with the project name derived from the repository id so two repositories never collide.
- [ ] Registration is idempotent: a second start neither duplicates the project nor triggers a full reindex unless explicitly asked.
- [ ] Registration never blocks server startup, and a Pando that is unreachable or slow leaves the server fully functional with no code search and a stated reason in the search settings card.
- [ ] A code hit reaches the web search results labelled with its origin index, visually distinct from an item and from a KB page.
- [ ] A result matched in both indexes appears once, carrying the better score, with the merge keyed on the resolved file path.
- [ ] The merge is covered by a test in which the same `docs/` file is returned by both indexes with different scores.
- [ ] `docs/21` documents the split: what the KB indexation covers, what the code indexation covers, why the backlog is unreachable from the code side, and how duplicates are merged.

## Notes

Maintainer decisions, 2026-09-16: register from `serve --agent` rather than `agent init` or a separate command; surface code hits in the web search labelled by origin; deduplicate at presentation rather than filtering the documentation directory or accepting duplicates.

Relevant Pando tools: `code_index_project`, `code_index_status`, `code_list_projects`, `code_hybrid_search`, `code_reindex_file`, `code_delete_project`. A hit from `code_hybrid_search` carries symbol names and a file path, not the front matter a KB hit carries — so a merged result has to survive one side having no front matter.

The code indexer cannot see `docs/.pmngr/` because it is a dot-directory, which is exactly the half the KB indexation covers. The two do not compete for the backlog.

PANDO-US-0031 does not block this story: project registration goes over REST, not MCP. It does leave the `[AGUI] Tools` allow-list in the sibling story provisional, since with the gateway on tools are reached through `mcp_call_tool` rather than by name.

Not established: the wall-clock cost of a first code index of this repository. Measure it before deciding whether the first index runs eagerly on start or on the first search.
