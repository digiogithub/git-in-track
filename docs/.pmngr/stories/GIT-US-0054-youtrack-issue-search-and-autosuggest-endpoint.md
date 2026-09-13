---
id: GIT-US-0054
type: story
title: YouTrack issue search and autosuggest endpoint
status: backlog
priority: high
parent: GIT-EP-0012
milestone: GIT-M-0011
author: mcp
labels: [server]
estimate: 5
created: 2026-09-13T13:12:06Z
updated: 2026-09-13T13:12:06Z
---

## Description

As a user picking issues to import, I want to type a YouTrack query or choose a preset and see matching issues as I type, so that I can build the import selection without leaving git-in-track or memorising the query language.

Add `GET /api/v1/youtrack/issues?q=&preset=&limit=&cursor=` to the companion, mounted next to the other YouTrack routes in a new `internal/server/youtrack.go` and registered in `internal/server/api.go`. The handler composes the effective query as `project: {<shortName>}` plus the user's `q` plus the preset clause, and always appends `order by: created asc` so `$skip` paging is stable — the composition helper belongs in `internal/youtrack`, not in the handler. Presets: `epics` (`Type: Epic`), `stories` (`Type: {User Story}`), `tasks` (`Type: Task`), `versions` (a `Fix versions` bundle listing via the version bundle endpoint), `unresolved` (`#Unresolved`). Values containing spaces are wrapped in braces.

The response is a projection — `{items: [{id, idReadable, summary, type, state, assignee, updated, url, linked: {itemId}|null}], nextCursor}` — where `linked` is resolved locally from the index by `(external.system, external.id)` so the picker can show "already imported" without a second round trip. The route is gated on `features.youtrack` and on the project being linked, returns 404 otherwise, and never echoes the token. Requests are subject to the shared rate limiter so a fast typist cannot exhaust the instance's budget; the handler is read-only and therefore needs no `If-Match`.

## Acceptance Criteria

- [ ] `GET /api/v1/youtrack/issues` exists with `q`, `preset`, `limit` and `cursor`, gated on `features.youtrack` and a linked project.
- [ ] Query composition wraps the project key in braces, appends the preset clause and always ends with `order by: created asc`; it lives in `internal/youtrack` and is unit tested.
- [ ] The five presets `epics`, `stories`, `tasks`, `versions`, `unresolved` are supported and an unknown preset is a field-level 400.
- [ ] Paging uses `$top`/`$skip` behind an opaque cursor and is stable across pages.
- [ ] Each result carries `linked` resolved from the local index by `(external.system, external.id)`.
- [ ] Upstream 4xx/5xx and timeouts map to a documented error body without leaking the token or the Authorization header.
- [ ] Endpoint documented in `docs/07-cli-and-api.md`; `go test -race ./internal/server/...` covers composition, paging and the gating.

## Notes

Depends on GIT-EP-0011 for the client, the token resolution and the `features.youtrack` capability, and on GIT-EP-0015 for the shared rate limiter.

Query patterns and the brace-quoting rule are in the scratchpad YouTrack report §3.3; the endpoint is `GET /api/issues?query=&$top=&$skip=&fields=` (§8 row 8). Version bundles come from `GET /api/admin/customFieldSettings/bundles/version/{bid}/values` (§8 row 7). Follow `internal/server/items.go` for handler shape and `internal/server/api.go:134-156` for the `s.call` pattern.

Do NOT route YouTrack through the CORS proxy (ADR-025) — it is companion-only. Do NOT cache results in the index; the remote is the source of truth for search.
