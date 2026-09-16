---
id: GIT-US-0096
type: story
title: Resolve Pando search hits against real repository paths
status: done
priority: high
parent: GIT-EP-0020
milestone: GIT-M-0013
author: mcp
labels: [server, core]
created: 2026-09-16T12:46:14Z
updated: 2026-09-16T15:56:00Z
started: 2026-09-16T15:07:27Z
closed: 2026-09-16T15:56:00Z
---

## Description

As someone searching semantically, I want a hit in a repository file to resolve to the item or KB page it is, so that results carry a real status, title and link rather than a path I have to interpret.

`internal/server/search_pando.go` builds and parses the retired corpus layout: `corpusPrefix` (`:141-160`) and `parseCorpusPath` (`:222-257`) assume `<PROJECT>/items/<ID>.md` and `<PROJECT>/kb/<path>.md`. With Pando indexing the repository, a hit's `file_path` is relative to `KBPath` — the documentation directory — so an item is `.pmngr/<type>/<ID>-<slug>.md` and a KB page is `<path>.md`. The id is still in the filename, but suffixed with a slug and nested under a type directory.

Resolve items with `core.IDFromFileName`, which `ParseItem` already uses and which the search path has never been wired to. Resolve KB pages by their path relative to the documentation root, which is what `KBPage` is keyed by.

**A hit under `.pmngr/comments/<ID>/` resolves to the item the comment belongs to**, presented as that item and marked as having matched in a comment rather than in the body. Comment threads hold the reasoning that never makes it into an item body — the decision, the evidence, the rejected alternative — so dropping them would index that knowledge and leave it unreachable. Comments were never exported, so nothing about this is a regression; it is new reach. Several comments on the same item can match one query, so the resolver deduplicates to one result per item, keeping the best-scoring fragment and counting the rest.

**A hit that is neither a backlog file nor a KB page is returned as a plain file result**, carrying its path and fragment but no status, title or item link. With `KBPath` at the documentation directory this is close to impossible, but a hand-edited `KBPath` must not make results vanish silently.

Every hit still re-reads its fields from git-in-track's own index, as today; only the path parsing changes.

## Acceptance Criteria

- [ ] A hit at `.pmngr/stories/GIT-US-0073-corpus-exporter-that-keeps-a-pando-indexable-copy-of-the.md` resolves to `GIT-US-0073` with its current status, title and labels read from the index.
- [ ] A hit at `20-agent-interface.md` resolves to that KB page.
- [ ] A hit at `.pmngr/comments/GIT-US-0073/<timestamp>-<author>.md` resolves to `GIT-US-0073`, is marked as a comment match, and carries the matching fragment.
- [ ] Several comment hits on one item collapse to a single result holding the best-scoring fragment, with the number of other matching comments reported.
- [ ] A hit that is neither a backlog file nor a KB page is returned as a plain file result with its path and fragment, distinguishable in the response from an item and from a page.
- [ ] A hit at a path whose id does not exist in the index, or whose file was deleted since the last Pando pass, is dropped without failing the whole search, and the drop is counted in the response so a stale index is visible rather than silent.
- [ ] Table-driven tests cover each path shape, including a project key that contains a digit, an item whose slug contains a dot, and a comment directory whose item was since deleted.
- [ ] Absolute paths from `metadata.source_path` are handled as well as `file_path` relative to `KBPath`, since Pando returns both.

## Notes

Maintainer decisions, 2026-09-16: comment hits resolve to the parent item rather than being dropped or given a result type of their own; foreign paths come back as plain file results rather than being dropped silently.

`core.IDFromFileName` lives in `internal/core`; `ParseItem` already requires the id in the filename to match the front matter, so the filename is a reliable key. A comment path carries its item id as the directory name, which is the same guarantee.

`internal/server/search_pando_test.go:141` uses `/var/cache/pando-kb/demo/DEMO/items/DEMO-T-0001.md` as its fixture and has to be replaced wholesale.

Pando chunks by character offset, not by line, and surfaces no line range or chunk index — only `file_path`, `chunk_content`, `score`, `tags`, timestamps and `metadata`. Do not try to build a line anchor out of a hit.
