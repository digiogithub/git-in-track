---
id: GIT-T-0039
type: task
title: Add issue fixtures and document the mapping table
status: done
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:16:16Z
updated: 2026-09-13T16:18:59Z
started: 2026-09-13T14:34:38Z
closed: 2026-09-13T16:18:59Z
---

## Description

Add realistic fixture JSON under `internal/youtrack/mapping/testdata/` — an epic, a story with subtasks, a bug with attachments and comments, and an issue whose custom fields are unmapped — with a `-update` flag to regenerate the golden outputs, and a full-issue end-to-end test that exercises `IssueToDraft` over each one. Then document the mapping table (YouTrack field to git-in-track field, with the defaults) in `docs/03-data-model.md` next to the `external` field section and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [x] Four fixtures plus golden outputs exist and the `-update` flag regenerates them.
- [x] An end-to-end test maps each fixture and asserts the golden output; `go test -race ./internal/youtrack/...` passes.
- [ ] The mapping table and its defaults are documented in `docs/03-data-model.md` and `CHANGELOG.md` has an entry.

## Notes

Fixtures landed: `epic.json`, `story.json`, `bug.json`, `unmapped.json`, `comments.json` plus their `*.golden.json`, regenerated with `go test ./internal/youtrack/mapping/ -run Golden -update`. The golden document holds the draft, the relations, the warning lines and the patch, so any change to any of them shows up in review. `story.json` carries the real multi-entry link shape with empty `issues` arrays.

**The documentation half landed in the docs pass**: `docs/03-data-model.md` §12.6 "What a YouTrack issue becomes" now carries the field-to-field table, the three built-in value maps (types, states, priorities), the link-type table and seven rules (R-YT-1 … R-YT-7). The third criterion stays unticked only because of its `CHANGELOG.md` half: the changelog belongs to another agent in this wave and was not touched.

Verified against the code rather than against the earlier note: the value tables were read out of `DefaultFieldMap`, the estimate arithmetic out of `mapEstimate`, the link directions out of `linkKind`, and two divergences from the story's own description were found and documented instead of being repeated — an imported item's `external` entry carries no `key`, and `attachments[]` holds full vault-relative paths rather than the bare filenames R-ATT-4 specifies.
