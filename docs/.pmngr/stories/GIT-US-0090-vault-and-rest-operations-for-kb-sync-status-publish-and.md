---
id: GIT-US-0090
type: story
title: Vault and REST operations for KB sync status, publish and pull
status: in_review
priority: medium
parent: GIT-EP-0014
milestone: GIT-M-0011
author: mcp
labels: [core, server]
estimate: 5
created: 2026-09-13T13:15:01Z
updated: 2026-09-13T15:41:38Z
started: 2026-09-13T15:41:28Z
---

## Description

As a client of the companion, I want KB sync to be reachable as core API methods and REST routes, so that the web app, MCP and the CLI all ask the same questions and get the same answers about a page's sync state.

Add `youtrack.kb.status`, `youtrack.kb.publish` and `youtrack.kb.pull` to the vault dispatch table (`internal/vault/vault.go:320-404`, and `internal/vault/dispatch.go:73` for workspace routing). `status` takes `{project, path, recursive}` and returns, per page, `{path, linked, articleId, url, state: unlinked|in_sync|local_ahead|remote_ahead|conflict, syncedAt}`; it compares the page's rev and `external.synced_at` with the article's `updated`, reading the remote only when asked so a tree view does not fan out into hundreds of calls. `publish` and `pull` validate their arguments, check the project link and enqueue the corresponding job, returning the job id rather than blocking.

Expose them over REST next to the existing KB routes (`internal/server/kb.go:19-23`): `GET /api/v1/youtrack/kb/status?path=&recursive=`, `POST /api/v1/youtrack/kb/publish` and `POST /api/v1/youtrack/kb/pull`, mounted through the same per-project and per-team mounts as `/kb` (`internal/server/api.go:53`, `:58`, `:70`) and gated on `features.youtrack`. Publishing and pulling emit the engine's `sync.job.*` events plus `youtrack.kb.conflict`, and the page write itself already produces `file.changed` through `publishPageWrite` (`internal/server/events.go:282`), so the UI refreshes with no extra plumbing. All three new event and route contracts are documented in `docs/07-cli-and-api.md`.

## Acceptance Criteria

- [ ] `youtrack.kb.status`, `youtrack.kb.publish` and `youtrack.kb.pull` exist in the vault dispatch table and are routable from the workspace layer.
- [ ] `status` returns the five states per page and only touches the network when asked to.
- [ ] `publish` and `pull` enqueue a job and return its id instead of blocking the request.
- [ ] REST routes are mounted beside the KB routes, gated on `features.youtrack`, and return 404 for an unlinked project.
- [ ] `youtrack.kb.conflict` is published on the hub and documented in `docs/07-cli-and-api.md` §5.6 alongside the `sync.job.*` events.
- [ ] The routes and the status shape are documented in `docs/07-cli-and-api.md`.
- [ ] `go test -race ./internal/vault/... ./internal/server/...` covers each state, the gating and the enqueue behaviour.

## Notes

Depends on the job kinds of this epic, on GIT-EP-0015 for the engine and its events, and on GIT-EP-0011 for the capability and the project link.

Existing code: `internal/vault/vault.go:1227` (`kb.tree`), `:1290` (`kb.page`), `:1356` (`kb.write`), `:1391` (`kb.feedback.add`); `internal/server/kb.go` for the route shape; `internal/server/hub.go:167` for `Publish`, which never blocks and drops on overflow.

Do NOT read the remote for every page of a tree by default — that is how a status call becomes a rate-limit incident. Do NOT bypass `kb.write` when stamping the article reference into front matter; the rev guard is the point.
