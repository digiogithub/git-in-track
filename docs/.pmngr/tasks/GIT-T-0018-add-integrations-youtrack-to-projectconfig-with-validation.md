---
id: GIT-T-0018
type: task
title: Add integrations.youtrack to ProjectConfig with validation
status: todo
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:15:43Z
updated: 2026-09-13T13:15:43Z
---

## Description

Add an `Integrations` section to `core.ProjectConfig` (`internal/core/project.go:23-40`) holding `YouTrack{URL, Project, FieldMap, PushComments, KBSync}`, modelled on the existing `TeamLink` (`:161`) and `LinksConfig` (`:167`). Validate in `LoadProjectConfig` (`:198`): the URL must parse and be absolute, the project short name must be non-empty when the block is present, and `field_map` keys must be known gintrack fields.

## Acceptance Criteria

- [ ] The block loads into typed Go values and an invalid URL or empty project short name is a load error naming the offending key.
- [ ] A `project.yaml` without the block still loads unchanged.
- [ ] `go test -race ./internal/core/...` covers valid, absent and invalid blocks.
