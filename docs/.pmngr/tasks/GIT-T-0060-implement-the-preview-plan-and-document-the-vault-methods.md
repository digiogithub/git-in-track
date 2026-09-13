---
id: GIT-T-0060
type: task
title: Implement the preview plan and document the vault methods
status: done
priority: medium
parent: GIT-US-0047
milestone: GIT-M-0011
author: mcp
labels: [core, server, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:16:46Z
updated: 2026-09-13T17:22:01Z
started: 2026-09-13T15:09:11Z
closed: 2026-09-13T17:22:01Z
---

## Description

Implement `youtrack.import.preview` as the resolution half of `run` with the writes replaced by a plan: per issue `{youtrackId, title, mappedType, action, targetId, parent, milestone, warnings}`. It must share the resolution code with `run` so the two can never disagree. Then document both methods in `docs/07-cli-and-api.md` §4 alongside the other core API methods and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] `preview` writes nothing and returns the per-issue plan with the same action decision `run` would take.
- [x] Resolution code is shared between `preview` and `run`.
- [x] `docs/07-cli-and-api.md` documents both methods and `CHANGELOG.md` has an entry.
- [x] `go test -race ./internal/vault/...` asserts that preview and run agree on actions for the same input.

## Notes

`YouTrackImportPreview` and `YouTrackImportRun` both call `youtrackResolve` (the network half) and then `resolveTargets` + `youtrackDecide` (the decision half); the preview stops there and the run writes, so the two cannot disagree about what an issue becomes. The plan carries `{youtrackId, title, mappedType, action, targetId, parent, milestone, depth, comments, warnings}` and `comments` already excludes the ones a previous import wrote.

The documentation criterion is now satisfied and ticked. `docs/07-cli-and-api.md` §4.15 names both core methods (`youtrack.import.preview` / `youtrack.import.run`) as the two the CLI dispatches, and §5.5 documents both HTTP halves with their request and response shapes — the preview synchronous and writing nothing, the run queuing a job. `CHANGELOG.md` carries the entry under Unreleased.
