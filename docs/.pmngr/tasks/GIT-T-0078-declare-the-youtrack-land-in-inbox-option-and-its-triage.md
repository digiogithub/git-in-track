---
id: GIT-T-0078
type: task
title: Declare the YouTrack land_in_inbox option and its triage landing target
status: todo
priority: medium
parent: GIT-US-0066
milestone: GIT-M-0012
author: mcp
labels: [core, docs]
estimate: 2
created: 2026-09-13T13:17:12Z
updated: 2026-09-13T13:17:12Z
---

## Description

Add `LandInInbox bool` to the `integrations.youtrack` block of `core.ProjectConfig` (`internal/core/project.go:23-40`) and expose a single helper the importer calls to decide its landing target, so that GIT-EP-0012 can consume it without this story depending on the importer. Document the key in `docs/03-data-model.md` §6. Remember that `ProjectConfig` has no `Extra` map, so an undeclared key is invisible to Go, and that `project.yaml` is never re-serialized wholesale — do not add a writer.

## Acceptance Criteria

- [ ] `integrations.youtrack.land_in_inbox` decodes from `project.yaml` and defaults to false.
- [ ] A helper returns the landing status (triage or the workflow initial) and errors when the project has no triage status but the option is on.
- [ ] `docs/03-data-model.md` §6 documents the key, cross-referencing GIT-EP-0012.
- [ ] `go test -race ./internal/core/...` covers both settings.
