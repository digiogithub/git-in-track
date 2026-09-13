---
id: GIT-T-0039
type: task
title: Add issue fixtures and document the mapping table
status: todo
priority: medium
parent: GIT-US-0045
milestone: GIT-M-0011
author: mcp
labels: [core, docs, agent-ok]
estimate: 2
created: 2026-09-13T13:16:16Z
updated: 2026-09-13T13:16:16Z
---

## Description

Add realistic fixture JSON under `internal/youtrack/mapping/testdata/` — an epic, a story with subtasks, a bug with attachments and comments, and an issue whose custom fields are unmapped — with a `-update` flag to regenerate the golden outputs, and a full-issue end-to-end test that exercises `IssueToDraft` over each one. Then document the mapping table (YouTrack field to git-in-track field, with the defaults) in `docs/03-data-model.md` next to the `external` field section and add a `CHANGELOG.md` entry.

## Acceptance Criteria

- [ ] Four fixtures plus golden outputs exist and the `-update` flag regenerates them.
- [ ] An end-to-end test maps each fixture and asserts the golden output; `go test -race ./internal/youtrack/...` passes.
- [ ] The mapping table and its defaults are documented in `docs/03-data-model.md` and `CHANGELOG.md` has an entry.
