---
id: GIT-EP-0014
type: epic
title: Knowledge base sync with YouTrack articles
status: backlog
priority: medium
milestone: GIT-M-0011
author: mcp
labels: [core, server, web, docs]
created: 2026-09-13T13:07:45Z
updated: 2026-09-13T13:07:45Z
---

## Description

Publish a KB page (or a folder of pages) as YouTrack articles and keep them in sync. A page carries `external: [{system: youtrack, id: PRJ-A-12, url}]` in its front matter; folders map to parent articles; wikilinks are rewritten to article links on the way up; the `## Feedback` block never leaves the repository. Sync is per page or per folder, on demand from the KB view and optionally on every page write, always through the sync engine. Pulling an article back into a page is supported when the remote is newer, with a simple last-writer-wins plus a conflict marker page when both changed.

## Acceptance Criteria

- [ ] KB page toolbar: "Publish to YouTrack", "Sync now", status badge (linked, pending, out of date, conflict).
- [ ] Folder publish creates the parent article tree (`parentArticle`), preserving order.
- [ ] Content transform: strip front matter and feedback block; H1 vs `summary` decided once (title lives in `summary`, body without H1); wikilinks → article links; local attachments uploaded.
- [ ] Pull: fetch `content`, write through `WritePage`, keep the local feedback block; conflicts produce `<page>.conflict.md` and an event.
- [ ] Project setting `kb_sync: manual | on_write`; direction `push | pull | both`.
- [ ] MCP tools `publish_kb_page_to_youtrack`, `sync_kb_page_from_youtrack`; CLI `gintrack youtrack kb push|pull`.

## Notes

YouTrack: `GET|POST /api/articles`, `content` field, `project` settable only at creation, `childArticles` for walking down. A KB edit UI does not exist yet; the sync surface is toolbar actions, not an editor.
