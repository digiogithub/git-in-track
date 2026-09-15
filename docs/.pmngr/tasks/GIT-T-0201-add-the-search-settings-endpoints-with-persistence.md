---
id: GIT-T-0201
type: task
title: Add the search settings endpoints with persistence
status: done
priority: medium
parent: GIT-US-0091
milestone: GIT-M-0013
author: mcp
labels: [server]
estimate: 3
created: 2026-09-13T13:20:22Z
updated: 2026-09-15T16:44:24Z
started: 2026-09-15T16:04:14Z
closed: 2026-09-15T16:44:24Z
---

## Description

Add `GET|PATCH /api/v1/search/settings` in `internal/server`, following `internal/server/git.go:374-393` and `gitState.persist` :336 exactly: the PATCH handler reloads the config file, replaces the search section, saves it and returns `persisted bool`, so the UI can say whether the change outlives the process. The GET response carries the Pando URL, the corpus directory, the code project id, the last full export time, the exported document count, a reachability probe result and the selected backend.

## Acceptance Criteria

- [ ] GET returns all seven fields and PATCH persists and returns `persisted`.
- [ ] With no `ConfigPath` the change applies to the process only and `persisted` is false.
- [ ] Invalid values are rejected with an `invalid_request` problem document.
- [ ] `go test -race ./internal/server/...` covers the round trip, the no-config case and validation.
