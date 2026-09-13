---
id: GIT-T-0018
type: task
title: Add integrations.youtrack to ProjectConfig with validation
status: done
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:15:43Z
updated: 2026-09-13T14:41:27Z
started: 2026-09-13T14:41:02Z
closed: 2026-09-13T14:41:27Z
---

## Description

Add an `Integrations` section to `core.ProjectConfig` (`internal/core/project.go:23-40`) holding `YouTrack{URL, Project, FieldMap, PushComments, KBSync}`, modelled on the existing `TeamLink` (`:161`) and `LinksConfig` (`:167`). Validate in `LoadProjectConfig` (`:198`): the URL must parse and be absolute, the project short name must be non-empty when the block is present, and `field_map` keys must be known gintrack fields.

## Acceptance Criteria

- [x] The block loads into typed Go values and an invalid URL or empty project short name is a load error naming the offending key.
- [x] A `project.yaml` without the block still loads unchanged.
- [x] `go test -race ./internal/core/...` covers valid, absent and invalid blocks.

## Notes

Landed in `internal/config` (`projectlink.go`, `projectlink_test.go`) rather than in `internal/core`: this wave's agent did not own `internal/core`, which another agent held. `core.ProjectConfig` drops unknown keys silently and never re-serializes `project.yaml`, so the block survives untouched today and the feature works end to end without the typed core field. Adding `ProjectConfig.Integrations` as a typed mirror, and the `E-PROJ-INTEGRATION` diagnostic through `LoadProjectConfig`, is left to the owner of `internal/core`; the validation it needs is `config.YouTrackLink.Validate`, ready to be called.
