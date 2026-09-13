---
id: GIT-T-0209
type: task
title: Publish a folder as a parent article tree
status: done
priority: medium
parent: GIT-US-0087
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:20:49Z
updated: 2026-09-13T16:01:37Z
started: 2026-09-13T16:01:05Z
closed: 2026-09-13T16:01:37Z
---

## Description

Extend the publish handler with the recursive mode: walk the KB tree depth-first, create or reuse one parent article per directory so the remote hierarchy mirrors the local one through `parentArticle`, publish parents before their children and keep sibling order. Note that `ordinal` is documented read-only and is silently ignored if sent, so ordering must come from creation order.

## Acceptance Criteria

- [x] A folder publish creates the parent article tree and reuses existing parents on re-publish.
- [x] Parents are always published before their children and sibling order is preserved.
- [x] `ordinal` is not relied on for ordering.
- [x] `go test -race ./internal/server/...` covers a two-level tree on first publish and re-publish.

## Notes

`ensureKBParent` walks upwards and memoises per run, so a directory's article is
created once however many pages hang under it, and the chain down to it exists
before the first of them is published.

Reuse on a re-publish is the part with no state to lean on: a directory has no
front matter and therefore no `external` entry of its own. A nested directory is
found among the `childArticles` of its own parent by summary; a top-level one is
found with a project-scoped article search. **That search carries an ordering
clause**, through `youtrack.ProjectQuery` — the reference CLI's article listing
omits one, which is a latent paging bug worth not copying.

`ordinal` is never sent. It is documented read-only and is silently ignored, so
relying on it would have been a no-op that looked like ordering; the order pages
are created in is the order they appear in.
