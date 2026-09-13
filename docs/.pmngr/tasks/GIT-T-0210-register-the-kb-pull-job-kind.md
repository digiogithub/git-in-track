---
id: GIT-T-0210
type: task
title: Register the KB pull job kind
status: done
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:53Z
updated: 2026-09-13T16:01:49Z
started: 2026-09-13T16:01:06Z
closed: 2026-09-13T16:01:49Z
---

## Description

Register a `youtrack.kb.pull` `Kind` and `Handler` that reads the article, and its descendants through `GET /api/articles/{id}/childArticles` when recursive, transforms it back with `ArticleToPage` and writes through `kb.write` so `WritePage` and its feedback pruning apply normally. Every listing that pages must send an explicit ordering clause — the reference CLI's article listing omits one and is a latent paging bug.

## Acceptance Criteria

- [x] A single page and a recursive subtree both pull and write through `kb.write`.
- [x] Article listings that page always send an ordering clause.
- [x] The local feedback block survives the pull.
- [x] `go test -race ./internal/server/...` covers single and recursive pull with a fake client.

## Notes

The pull is page-driven rather than article-driven: the selection is the local
tree, and each selected page pulls the article its `external` entry names. A page
that mirrors no article is a no-op, not a failure — a folder pull walks pages
that were never published and must not abandon the rest of the tree over them.

**The body handed to `ArticleToPage` is read from disk, not from the index.**
The indexed `KBPage.Body` has the feedback block removed, because the block is
local-only and never leaves the repository (ADR-030) — so transforming against
the indexed body would drop it on every pull. `rawPageBody` reads the file and
splits the front matter off, and `ArticleToPage` then re-appends the block
verbatim. The write still goes through `kb.write`, so `WritePage` and its
pruning apply normally: a note whose anchored text the incoming article no
longer contains is dropped, which is the documented behaviour and not a loss.

The reference is rewritten after a pull with the fingerprint of the article that
was just applied and a fresh `synced_at`; `ArticleToPage` reads no clock and
computes no fingerprint, so leaving its `external` value in place would have left
the next run guessing from timestamps.
