---
id: GIT-T-0131
type: task
title: Add the custom field discovery endpoint
status: done
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:32Z
updated: 2026-09-13T16:22:11Z
started: 2026-09-13T16:21:56Z
closed: 2026-09-13T16:22:11Z
---

## Description

Add `GET /api/v1/youtrack/fields` in `internal/server/youtrack.go`. It calls `GET /api/admin/projects/{id}/customFieldSettings?fields=field(name,fieldType(id)),bundle(id,$type),canBeEmpty` and then the matching enum, state or version bundle value endpoint for each bundle-backed field, returning `{fields: [{name, type, values: [{name, isResolved?, archived?}]}]}`. The route is gated on `features.youtrack` and a linked project and goes through the shared rate limiter.

## Acceptance Criteria

- [x] The endpoint returns each custom field with its type and, for bundle-backed fields, its values.
- [x] Enum, state and version bundles are all resolved; state values carry `isResolved`.
- [x] The route is gated and returns 404 for an unlinked project.
- [x] `go test -race ./internal/server/...` covers the projection with an httptest upstream.

## Notes

The route already existed with names only; it now projects `youtrack.ProjectFieldValues`, which resolves every bundle kind the client knows (enum, state, version, ownedField, build, user, group) in one call. The answer gained, per field, `bundled`, `emptyFieldText`, `values[]` and `warnings[]`, and, per value, `name` (the stable mapping key), `label` (what to display), `ordinal`, `archived`, `background`/`foreground`, `isResolved`, `released`, `login`/`fullName`. `isResolved` is omitted rather than `false` when the instance did not say, so an unknown flag never reads as "not done". The top level gained `valueMappableFields`, the three git-in-track fields whose values can be mapped.

Nothing is dropped: a text field, or a bundle kind this build cannot read, comes back in place with `bundled: false` and a warning saying why.
