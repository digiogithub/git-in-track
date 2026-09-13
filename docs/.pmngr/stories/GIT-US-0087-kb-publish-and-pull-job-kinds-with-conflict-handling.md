---
id: GIT-US-0087
type: story
title: KB publish and pull job kinds with conflict handling
status: done
priority: medium
parent: GIT-EP-0014
milestone: GIT-M-0011
assignees: [claude]
author: mcp
labels: [server, performance]
estimate: 8
created: 2026-09-13T13:14:45Z
updated: 2026-09-13T16:02:23Z
started: 2026-09-13T16:02:06Z
closed: 2026-09-13T16:02:23Z
---

## Description

As a documentation owner, I want publishing a page or a whole folder, and pulling changes back, to run as background jobs, so that a large handbook syncs without blocking and a simultaneous edit on both sides is reported instead of silently lost.

Register `youtrack.kb.publish` and `youtrack.kb.pull` job kinds with the sync engine. Publish takes `{project, path, recursive}`: for a single page it creates the article when the page has no `external` YouTrack reference (`POST /api/articles` with `project` in the body, since `project` is only settable at creation) and updates it otherwise (`POST /api/articles/{id}`), then writes the article id and url into the page's front matter through `kb.write`. For a folder it walks the tree depth-first, creating a parent article per directory so the remote hierarchy mirrors the local one through `parentArticle`, publishing children after their parent and preserving order.

Pull takes `{project, path, recursive}` and reads the article (and `GET /api/articles/{id}/childArticles` when recursive), transforms it back and writes through `WritePage`. Conflict detection compares the page's `external.synced_at` and rev against the local rev and the article's `updated`: if only one side changed, last writer wins in that direction; if both changed, the incoming content is written to `<page>.conflict.md`, the original page is left untouched, and a `youtrack.kb.conflict` event is published so the UI can surface it. Every remote listing that pages must carry an explicit ordering clause — the reference CLI's article listing omits it and is a latent paging bug worth not copying.

## Acceptance Criteria

- [x] `youtrack.kb.publish` and `youtrack.kb.pull` job kinds are registered with `internal/syncengine`.
- [x] Publishing an unlinked page creates the article with `project` in the creation body; a linked page updates it.
- [x] Folder publish builds the parent article tree via `parentArticle`, parents before children, preserving order.
- [x] The article id and url are written back into the page front matter through the vault, rev-guarded.
- [x] Pull writes through `WritePage` and keeps the local feedback block intact.
- [x] Both-changed produces `<page>.conflict.md` plus a `youtrack.kb.conflict` event and leaves the original page untouched.
- [x] Article listings that page always send an ordering clause.
- [x] `go test -race ./internal/server/...` covers create, update, folder tree, pull and the conflict branch with a fake client.

## Notes

Depends on the transform story of this epic, on GIT-EP-0015 for the engine, rate limiter and retry, and on GIT-EP-0011 for the client and the `external` field on pages.

Endpoints: `GET|POST /api/articles`, `GET|POST /api/articles/{id}`, `GET /api/articles/{id}/childArticles?fields=id,idReadable,summary,ordinal,hasChildren` (scratchpad YouTrack report §4 and §8 rows 23 to 30). Article `idReadable` carries an `-A-` infix (`ACME-A-3`). `ordinal` is documented read-only and is silently ignored if sent, so an article cannot be moved between projects.

Do NOT attempt a three-way merge; the agreed semantics are last writer wins plus a conflict page. Do NOT treat the pruning of the local feedback block as a local change when deciding who changed.
