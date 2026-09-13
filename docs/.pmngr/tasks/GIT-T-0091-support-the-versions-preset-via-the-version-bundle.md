---
id: GIT-T-0091
type: task
title: Support the versions preset via the version bundle
status: done
priority: medium
parent: GIT-US-0054
milestone: GIT-M-0011
author: mcp
labels: [server, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:17:34Z
updated: 2026-09-13T16:03:44Z
started: 2026-09-13T16:03:28Z
closed: 2026-09-13T16:03:44Z
---

## Description

Implement the `versions` preset, which is not an issue query: resolve the project's version bundle through `GET /api/admin/projects/{id}/customFieldSettings` and list its values with `GET /api/admin/customFieldSettings/bundles/version/{bid}/values?fields=id,name,releaseDate,released,archived`, returning them in the same result envelope with `type: "version"` so the picker renders one list. Then document the endpoint, its presets and the result shape in `docs/07-cli-and-api.md` and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] The `versions` preset returns bundle values in the standard result envelope with `type: "version"`.
- [ ] Archived versions are excluded unless explicitly requested.
- [ ] `docs/07-cli-and-api.md` documents the endpoint, the presets and the result shape; `CHANGELOG.md` has an entry.
- [ ] `go test -race ./internal/server/...` covers the bundle resolution with an httptest upstream.
