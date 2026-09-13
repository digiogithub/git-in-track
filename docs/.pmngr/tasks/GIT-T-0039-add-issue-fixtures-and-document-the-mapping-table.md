---
id: GIT-T-0039
type: task
title: Add issue fixtures and document the mapping table
status: in_review
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:16:16Z
updated: 2026-09-13T14:35:15Z
started: 2026-09-13T14:34:38Z
---

## Description

Add realistic fixture JSON under `internal/youtrack/mapping/testdata/` — an epic, a story with subtasks, a bug with attachments and comments, and an issue whose custom fields are unmapped — with a `-update` flag to regenerate the golden outputs, and a full-issue end-to-end test that exercises `IssueToDraft` over each one. Then document the mapping table (YouTrack field to git-in-track field, with the defaults) in `docs/03-data-model.md` next to the `external` field section and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Four fixtures plus golden outputs exist and the `-update` flag regenerates them.
- [x] An end-to-end test maps each fixture and asserts the golden output; `go test -race ./internal/youtrack/...` passes.
- [ ] The mapping table and its defaults are documented in `docs/03-data-model.md` and `CHANGELOG.md` has an entry.

## Notes

Fixtures landed: `epic.json`, `story.json`, `bug.json`, `unmapped.json`, `comments.json` plus their `*.golden.json`, regenerated with `go test ./internal/youtrack/mapping/ -run Golden -update`. The golden document holds the draft, the relations, the warning lines and the patch, so any change to any of them shows up in review. `story.json` carries the real multi-entry link shape with empty `issues` arrays.

**The documentation criterion is left unticked on purpose.** This wave assigned `internal/youtrack/mapping/` to this agent and `docs/` to another; editing `docs/03-data-model.md` or `CHANGELOG.md` here would have collided with the doc agent's working tree. The mapping table is fully specified in the package doc comments (`fieldmap.go` `DefaultFieldMap`, `item.go`, `links.go`) and can be lifted from there verbatim. Reported to the coordinator.
