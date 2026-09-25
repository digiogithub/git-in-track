---
id: GIT-US-0127
type: story
title: Serve specs, requirements, coverage and impact over HTTP
status: done
priority: high
parent: GIT-EP-0027
milestone: GIT-M-0015
author: claude
labels: [server, wasm, agent-ok]
estimate: 5
created: 2026-09-24T12:11:34Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0107 }
  - { kind: blocked_by, target: GIT-US-0116 }
  - { kind: blocked_by, target: GIT-US-0120 }
---

## Description

As the web app, I need spec data in both operating modes: the companion serves everything, browser-only serves authoring and lint through the WASM CoreApi and reports coverage and impact as `unavailable`.

## Acceptance Criteria

- [x] `internal/server` adds routes under `/api/v1/projects/{key}/specs` for list/get specs, list/get/create/update requirements (block `rev` via `If-Match`), `.../coverage` and `.../impact?base=&head=`.
- [x] The web API client exposes the same operations over both the HTTP and WASM transports; the WASM side returns `unavailable` for coverage and impact.
- [x] Changes to spec files emit the existing watcher events so open views refresh.
- [x] Route tests in `internal/server`; docs/07 API reference updated.

## Notes

Foundation for the other web stories.
