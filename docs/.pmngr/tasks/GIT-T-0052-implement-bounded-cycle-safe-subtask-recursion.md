---
id: GIT-T-0052
type: task
title: Implement bounded, cycle-safe subtask recursion
status: done
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 2
created: 2026-09-13T13:16:36Z
updated: 2026-09-13T15:08:51Z
started: 2026-09-13T15:08:16Z
closed: 2026-09-13T15:08:51Z
---

## Description

Add the expansion step that walks Subtask links outward up to `depth` levels, keeping a visited set keyed on `idReadable` so a cycle cannot loop, and resolving the parent of each imported issue. Parents and `links[]` targets outside the resulting set are recorded as warnings on the issue result instead of being written as dangling references. Depth 0 means the selected issues only.

## Acceptance Criteria

- [x] `depth` bounds the recursion and 0 imports only the selected issues.
- [x] A cyclic Subtask graph terminates and is reported.
- [x] Out-of-set parents and link targets become warnings, never dangling references.
- [x] `go test -race ./internal/vault/...` covers depth 0, 1 and 2 and a cyclic fixture.

## Notes

`youtrackExpand` is a breadth-first walk over the children `mapping.MapLinks` reports, with a visited set keyed on `idReadable`, so an issue is read once and a cycle simply ends; the walk is additionally bounded by `YouTrackMaxIssues` (500), reported as a set-level warning. Parent, milestone and `links[]` targets are resolved in `youtrackResolveRelations` through the batch's target map and `core.Index.ItemByExternal`; anything that does not resolve becomes a `mapping.Warning` on the issue result and is left off the file. Covered by `TestYouTrackImportRecursion`, `TestYouTrackImportCycleTerminates` and `TestYouTrackImportOutOfSetReferences`.
