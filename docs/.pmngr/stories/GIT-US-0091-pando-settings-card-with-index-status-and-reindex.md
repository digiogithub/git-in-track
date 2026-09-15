---
id: GIT-US-0091
type: story
title: Pando settings card with index status and reindex
status: done
priority: medium
parent: GIT-EP-0019
milestone: GIT-M-0013
author: mcp
labels: [web, server]
estimate: 3
created: 2026-09-13T13:15:13Z
updated: 2026-09-15T16:45:38Z
started: 2026-09-15T16:05:57Z
closed: 2026-09-15T16:45:38Z
---

## Description

As an operator, I want a settings card showing where Pando is, where the corpus lives and whether it is current, with a button to rebuild it, so that I can diagnose "semantic search returns nothing" without reading logs.

**The backend half comes forward.** The settings and reindex endpoints are what make GIT-US-0073's corpus trustworthy, so they are sequenced next to GIT-US-0077 rather than last; only the card itself stays at the end of the epic.

Add `GET|PATCH /api/v1/search/settings` in `internal/server`, following the precedent of `PATCH /api/v1/git/settings` exactly (`internal/server/git.go:374-393`, `gitState.persist` `:336`): reload the config file, replace the section, save, and return `persisted bool` so the UI can say whether only the running process took the change. Expose the Pando URL, the corpus directory, the code project id, the last full export time, the exported document count, the Pando reachability probe and the currently selected search backend.

`POST /api/v1/search/reindex` does two things today:

1. **Corpus (KB) half** — re-export the corpus (GIT-US-0073) and wait for Pando's next auto-import pass. `KBWatch` is off by design, so there is no watcher to trigger. Report this honestly on the card as "re-exported, Pando will pick it up", not as a completed index operation. When PANDO-EP-0005 lands, call `POST /api/v1/remembrances/kb/reindex` instead and surface the real `SyncStats{scanned, added, updated, unchanged, deleted}` it returns — that is the awaitable operation the card actually wants.
2. **Code half** — trigger `code_index_project` for the repository source tree now, through `internal/pando.Client`. Note that `POST /api/v1/remembrances/projects/index` already exists over plain REST with `X-Pando-Token`, which is simpler than going through the MCP client if the token is at hand; either is acceptable, and `code_index_status(job_id)` gives progress.

Progress is reported on the hub the way `sync.progress` does (`internal/server/sync.go:239`).

On the web side add `PandoSearchCard.tsx` to `web/src/features/settings/` and compose it into `SettingsPage.tsx:40-136` alongside `GitSettingsCard` and `McpToolsCard`, with the same plain `useState` plus imperative-save form pattern the other cards use — no react-hook-form, no zod in forms. Include a standing warning that the embedding model is pinned configuration: Pando silently skips chunks whose vector length differs from the query's, **and the model is global to the Pando instance**, so changing it degrades recall invisibly for every consumer of that instance until a full reindex.

## Acceptance Criteria

- [ ] `GET /api/v1/search/settings` returns the Pando URL, corpus directory, project id, last export time, document count, reachability and selected backend.
- [ ] `PATCH /api/v1/search/settings` persists to the config file and returns `persisted`; with no `ConfigPath` it applies to the process only and says so.
- [ ] `POST /api/v1/search/reindex` re-exports the corpus and triggers `code_index_project`, publishing progress events, and refuses to run twice concurrently.
- [ ] The KB half is reported as "re-exported, awaiting Pando's next import" rather than as a completed reindex, until the Pando reindex route exists.
- [ ] When the Pando REST reindex route is available, the KB half calls it and the card shows the returned scanned/added/updated/deleted counts.
- [ ] The settings card renders the status, saves changes, shows the reindex button with a progress state, and surfaces a clear error when Pando is unreachable.
- [ ] The card is hidden in browser-only mode and when the agent/search feature is not enabled.
- [ ] The embedding-model pin warning is visible on the card and repeated in the docs, and says the model is instance-global.
- [ ] `go test -race ./internal/server/...` covers the settings round trip, persistence and the concurrent-reindex refusal; Vitest covers the card.
- [ ] `docs/07-cli-and-api.md` documents the three endpoints and `CHANGELOG.md` records the feature.

## Notes

Existing cards to imitate: `GitSettingsCard.tsx:47,62-66`, `SyncProxyCard.tsx:44-72`, `McpToolsCard.tsx:34,55`. `TeamProjectsCard.tsx` is the only one using TanStack Query.

Pando has **no** REST or MCP trigger for a KB reindex today — with `KBWatch` off, re-export plus the next auto-import pass is the path. `POST /api/v1/remembrances/kb/reindex` is a small addition upstream (`SyncDirectoryWithStats` + `FilesystemMirrorPath` are both exported) and is tracked as PANDO-EP-0005.

The code-index half needs no upstream change: `POST /api/v1/remembrances/projects/index` (`routes.go:146`) already exists over plain REST, and `code_index_status(job_id)` (`remembrances_code.go:248`) reports progress.

Embedding-model caveat: `kb.go:894-896` skips mismatched vector lengths with no error and no dimension guard; the model is configured per Pando instance, not per corpus.

Settings persistence is per workspace; there are no per-repository overrides (`CHANGELOG.md:374`). Do not invent one here.

**Pando dependencies (plain ids):** PANDO-EP-0005 (REST reindex route) — turns the KB half from "re-export and hope" into a real awaitable operation with counts. Nothing else blocks; the backend half is buildable today.
