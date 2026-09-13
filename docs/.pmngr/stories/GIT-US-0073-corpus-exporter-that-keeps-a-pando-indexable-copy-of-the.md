---
id: GIT-US-0073
type: story
title: Corpus exporter that keeps a Pando-indexable copy of the backlog
status: backlog
priority: high
parent: GIT-EP-0019
milestone: GIT-M-0013
author: mcp
labels: [server, performance]
estimate: 8
created: 2026-09-13T13:13:42Z
updated: 2026-09-13T13:13:42Z
---

## Description

As a companion user, I want every item and KB page mirrored into a corpus directory that Pando watches, so that semantic search sees my current backlog without me running anything by hand.

Add `internal/server/pandosync.go` (or `internal/pandosync` if it grows past a single file) exporting one Markdown file per item to `<CacheDir>/pando-kb/<project>/items/<ID>.md` and per KB page to `<CacheDir>/pando-kb/<project>/kb/<path>.md`, where `CacheDir` is `config.Config.CacheDir(configPath)` (`internal/config/path.go:96`) — outside the repository, so nothing exported is ever committed. Each file carries YAML front matter with `id`, `type`, `status`, `milestone`, `parent`, `project`, `updated` and a `tags` list that includes the item id and its labels, because `tags` and `aliases` are the only keys Pando reads specially (`kb/frontmatter.go:16-31`); everything else lands in the searchable metadata. Body text is copied through unchanged so `[[GIT-US-0024]]` wikilinks build Pando's link graph.

Run a full export when the server starts with the feature enabled, then keep it incremental by subscribing to the hub (`internal/server/hub.go:141`) on `item.changed` and `file.changed` (`internal/server/events.go:256`, `:282`), rewriting only the affected files and unlinking removed ones. Writes are debounced and atomic (temp file plus rename) so Pando's 250 ms-debounced fsnotify watcher never reads a half-written document. Path safety matters: every exported path is derived from validated ids or vault-relative page paths and cleaned before use, the way `internal/mcp/paths.go` does it.

## Acceptance Criteria

- [ ] A full export on start writes one file per item and per KB page under `<CacheDir>/pando-kb/<project>/`, and nothing inside the repository.
- [ ] Front matter carries id, type, status, milestone, parent, project, updated and a `tags` list containing the id and the labels.
- [ ] An `item.changed` event rewrites exactly that item's file; a deleted item's file is removed.
- [ ] A `file.changed` event with `IsKb` rewrites the corresponding KB page file.
- [ ] Writes are atomic and debounced; no reader ever observes a partial file.
- [ ] A second full export over an unchanged corpus rewrites nothing (mtime or content-hash comparison).
- [ ] Exported paths cannot escape the corpus root, whatever the page path contains.
- [ ] A full export of 10 000 items stays within the performance budget of `docs/02-architecture.md` §9 and does not block server start.
- [ ] `go test -race ./internal/server/...` covers full export, incremental update, deletion pruning and path escape.

## Notes

Event shapes and publishers: `internal/server/events.go:229-372`; the hub subscription API is `internal/server/hub.go:72-104`. The watcher that feeds them is `internal/watcher/watcher.go` via `internal/server/watch.go:64-240`.

Pando side: `[Remembrances] KBPath` + `KBAutoImport` + `KBWatch` is the only first-class corpus-sync path (`internal/app/remembrances.go:75-160`); it skips by mtime (`kb/sync.go:148-156`) and prunes vanished sources (`:312-339`). Its directory walk and watcher have **no** hidden-directory or `node_modules` exclusion (`sync.go:124-131`, `watcher.go:194-225`), so the corpus directory must contain nothing but the exported Markdown — never point `KBPath` at the repository root.

There is no MCP or REST trigger for a KB reindex in Pando; the filesystem watcher is the only path. Do not use `kb_add_document` per item instead: it is chattier and mirrors into `KBPath` anyway. Do not use `code_index_project` for the docs corpus — `code_hybrid_search` excludes Markdown by default.
