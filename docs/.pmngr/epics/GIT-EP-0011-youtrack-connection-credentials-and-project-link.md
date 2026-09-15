---
id: GIT-EP-0011
type: epic
title: YouTrack connection, credentials and project link
status: done
priority: high
milestone: GIT-M-0011
author: mcp
labels: [server, core, web, security, docs]
created: 2026-09-13T13:07:15Z
updated: 2026-09-15T19:37:26Z
started: 2026-09-15T19:37:06Z
closed: 2026-09-15T19:37:26Z
---

## Description

The foundation every other YouTrack feature stands on: a native Go client for the YouTrack REST API (`internal/youtrack/`, outside the WASM-compiled core), a place to keep the permanent token on the machine running the companion (never in the repository), a committed per-project block in `project.yaml` naming the YouTrack instance and project, the Settings card where the user pastes the token, validates it and picks the YouTrack project with an autosuggest combobox, and the `external` front-matter field that records where an item came from.

Two ADRs are part of this epic: one for the first-class `external` reference on items and KB pages, one revising the "git-in-track stores no credentials" promise in `docs/10-development-guidelines.md`.

## Acceptance Criteria

- [ ] `internal/youtrack` client: Bearer auth, base URL with context path, `GET /api/users/me` probe, project search, issue search/get, links, comments, articles; rate limit 5 req/s, retry on 429/5xx with `Retry-After`; `$skip` paging always paired with `order by:`.
- [ ] Token stored in the companion config file (0600) keyed by project, `GINTRACK_YOUTRACK_TOKEN` override; never returned by any API, never logged, never committed.
- [ ] `project.yaml` gains `integrations.youtrack: {url, project, field_map}`; documented in `docs/03-data-model.md` §6.
- [ ] Items and KB pages accept `external: [{system, id, url, key, synced_at}]`; serializer, key order, JSON schema and docs updated; ADR written.
- [ ] `GET /api/v1/capabilities` reports `features.youtrack`; UI hides the feature in browser-only mode.
- [ ] Settings card: URL + token, "Test connection" showing the YouTrack user, project combobox with autosuggest, save; generic `Combobox` component extracted from `ItemPicker`.

## Notes

References: scratchpad reports on youtrack-cli endpoints (`/api/admin/projects`, `/api/users/me`) and on git-in-track config (`internal/config/config.go`, `internal/server/git.go` persist pattern).
