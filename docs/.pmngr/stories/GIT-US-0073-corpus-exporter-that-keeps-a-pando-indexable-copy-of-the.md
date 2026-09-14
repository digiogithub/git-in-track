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
updated: 2026-09-13T21:17:27Z
---

## Description

As a companion user, I want every item and KB page mirrored into a corpus directory Pando imports, so that semantic search sees my current backlog without me running anything by hand.

Add `internal/server/pandosync.go` (or `internal/pandosync` if it grows past a single file) exporting one Markdown file per item to `<CacheDir>/pando-kb/<project>/items/<ID>.md` and per KB page to `<CacheDir>/pando-kb/<project>/kb/<path>.md`, where `CacheDir` is `config.Config.CacheDir(configPath)` (`internal/config/path.go:96`) — outside the repository, so nothing exported is ever committed.

Each file carries YAML front matter with `id`, `type`, `status`, `milestone`, `parent`, `project`, `updated` and a `tags` list that includes the item id and its labels. **Write it for human readability and for the tags, not as a data channel:** Pando parses front matter into a fixed struct and discards every key other than `tags`/`aliases`, and its watcher erases even those on the first edit (report A1/A2/A5/A7 — 523 of 539 documents in Pando's own KB have already lost their tags). Nothing downstream may depend on reading these fields back out of a Pando search result; GIT-US-0082 resolves a hit to an item id and re-reads the authoritative fields from git-in-track's own index. **Put the item id in the body text as well as the front matter**, so it survives into the indexed chunk regardless of what the parser keeps. Body text is copied through unchanged so `[[GIT-US-0024]]` wikilinks build Pando's link graph.

**Sync model:** `KBAutoImport = true`, **`KBWatch = false`**. The exporter writes to disk and Pando imports on its own schedule; the companion never relies on fsnotify. Forcing a re-sync means waiting for the next auto-import pass today, and calling Pando's REST reindex route (`POST /api/v1/remembrances/kb/reindex`) once PANDO-EP-0005 lands.

Run a full export when the server starts with the feature enabled, then keep it incremental by subscribing to the hub on `item.changed` and `file.changed` (`internal/server/events.go:293`, `:359`; the `IsKb` discriminator is real, `events.go:195`), rewriting only the affected files and unlinking removed ones. Writes are debounced and atomic (temp file plus rename) so no importer ever reads a half-written document. Path safety matters: every exported path is derived from validated ids or vault-relative page paths and cleaned before use, the way `internal/mcp/paths.go` does it.

**The hub drops slow subscribers permanently** (`internal/server/hub.go:113-141`: `deliver` marks overflow, `markOverflow` closes `overflowed`). An exporter fed by the hub that overflows would silently stop exporting and desync forever, so overflow must trigger a full re-export. Better still, drive the exporter from the same publishers `events.go:293` and `:359` call rather than from an SSE-shaped `hubClient`.

## Acceptance Criteria

- [ ] A full export on start writes one file per item and per KB page under `<CacheDir>/pando-kb/<project>/`, and nothing inside the repository.
- [ ] Front matter carries id, type, status, milestone, parent, project, updated and a `tags` list containing the id and the labels; the item id also appears in the exported body text.
- [ ] An `item.changed` event rewrites exactly that item's file; a deleted item's file is removed.
- [ ] A `file.changed` event with `IsKb` rewrites the corresponding KB page file.
- [ ] **A hub subscriber overflow triggers a full re-export rather than a silent desync**, and the test drives an overflow to prove it.
- [ ] Writes are atomic and debounced; no reader ever observes a partial file.
- [ ] A second full export over an unchanged corpus rewrites nothing (mtime or content-hash comparison).
- [ ] Exported paths cannot escape the corpus root, whatever the page path contains.
- [ ] A full export of 10 000 items stays within the performance budget of `docs/02-architecture.md` §9 and does not block server start.
- [ ] `go test -race ./internal/server/...` covers full export, incremental update, deletion pruning, overflow re-export and path escape.

## Notes

**Metadata round-trip contract (a Note, not a gate).** Pando discards unrecognised front-matter keys and its watcher erases `tags` on edit. This story is deliberately designed not to depend on that round trip: the exporter must not rely on metadata fidelity, and consumers resolve ids and re-read fields locally. PANDO-EP-0005 would restore fidelity and turn this into an optimisation — track it there, do not block this story on it.

**Forcing a re-sync.** Today: write the files and wait for the next auto-import pass. Later: `POST /api/v1/remembrances/kb/reindex` (PANDO-EP-0005), which returns real `SyncStats{scanned, added, updated, unchanged, deleted}` and makes GIT-US-0091's "reindex now" an awaitable operation.

Event shapes and publishers: `internal/server/events.go` — the two publishers are at `:293` and `:359`, the payload structs at `:178` and `:188`, `IsKb` at `:195`. The hub subscription API is `internal/server/hub.go:79-104` and `:212`; the overflow policy is `:113-141`. The watcher that feeds them is `internal/watcher/watcher.go` via `internal/server/watch.go:64-240`.

Pando side: `[Remembrances] KBPath` + `KBAutoImport` is the corpus-sync path (`internal/app/remembrances.go:75-160`); it skips by mtime (`kb/sync.go:148-156`) and prunes vanished sources (`:312-339`). Its directory walk has **no** hidden-directory or `node_modules` exclusion (`sync.go:124-131`), so the corpus directory must contain nothing but the exported Markdown — never point `KBPath` at the repository root. `KBWatch` stays **false**: the watcher never parses front matter, so every edit it processes strips the tags.

`kb_add_document` per item is not the chosen path — it is chattier and mirrors into `KBPath` anyway — but the reason the pull path wins is the write pattern, not fidelity; both lose metadata today. Do not use `code_index_project` for the docs corpus: `code_hybrid_search` excludes Markdown by default.

**Pando dependencies (plain ids):** PANDO-EP-0005 — metadata fidelity and the REST reindex route. Everything else here is buildable today.
