---
id: GIT-US-0091
type: story
title: Pando settings card with index status and reindex
status: backlog
priority: medium
parent: GIT-EP-0019
milestone: GIT-M-0013
author: mcp
labels: [web, server]
estimate: 3
created: 2026-09-13T13:15:13Z
updated: 2026-09-13T13:15:13Z
---

## Description

As an operator, I want a settings card showing where Pando is, where the corpus lives and whether it is current, with a button to rebuild it, so that I can diagnose "semantic search returns nothing" without reading logs.

Add `GET|PATCH /api/v1/search/settings` in `internal/server`, following the precedent of `PATCH /api/v1/git/settings` exactly (`internal/server/git.go:374-393`, `gitState.persist` :336): reload the config file, replace the section, save, and return `persisted bool` so the UI can say whether only the running process took the change. Expose the Pando URL, the corpus directory, the code project id, the last full export time, the exported document count, the Pando reachability probe and the currently selected search backend. `POST /api/v1/search/reindex` re-exports the corpus and calls `code_index_project` for the repository source tree through `internal/pando.Client`, reporting progress on the hub the way `sync.progress` does (`internal/server/sync.go:239`).

On the web side add `PandoSearchCard.tsx` to `web/src/features/settings/` and compose it into `SettingsPage.tsx:40-136` alongside `GitSettingsCard` and `McpToolsCard`, with the same plain `useState` plus imperative-save form pattern the other cards use — no react-hook-form, no zod in forms. Include a standing warning that the embedding model is pinned configuration: Pando silently skips chunks whose vector length differs from the query's, so changing the model degrades recall invisibly until a full reindex.

## Acceptance Criteria

- [ ] `GET /api/v1/search/settings` returns the Pando URL, corpus directory, project id, last export time, document count, reachability and selected backend.
- [ ] `PATCH /api/v1/search/settings` persists to the config file and returns `persisted`; with no `ConfigPath` it applies to the process only and says so.
- [ ] `POST /api/v1/search/reindex` re-exports the corpus and triggers `code_index_project`, publishing progress events, and refuses to run twice concurrently.
- [ ] The settings card renders the status, saves changes, shows the reindex button with a progress state, and surfaces a clear error when Pando is unreachable.
- [ ] The card is hidden in browser-only mode and when the agent/search feature is not enabled.
- [ ] The embedding-model pin warning is visible on the card and repeated in the docs.
- [ ] `go test -race ./internal/server/...` covers the settings round trip, persistence and the concurrent-reindex refusal; Vitest covers the card.
- [ ] `docs/07-cli-and-api.md` documents the three endpoints and `CHANGELOG.md` records the feature.

## Notes

Existing cards to imitate: `GitSettingsCard.tsx:47,62-66`, `SyncProxyCard.tsx:44-72`, `McpToolsCard.tsx:34,55`. `TeamProjectsCard.tsx` is the only one using TanStack Query.

Pando has **no** REST or MCP trigger for a KB reindex — the filesystem watcher is the only path, so "reindex now" means re-export the corpus and let Pando's watcher pick it up, plus an explicit `code_index_project` for the source tree. Say that on the card rather than implying a direct index call.

Embedding-model caveat: `kb.go:894-896` skips mismatched vector lengths with no error and no dimension guard. `code_index_status(job_id)` exists for progress on the code side.

Settings persistence is per workspace; there are no per-repository overrides (`CHANGELOG.md:374`). Do not invent one here.
