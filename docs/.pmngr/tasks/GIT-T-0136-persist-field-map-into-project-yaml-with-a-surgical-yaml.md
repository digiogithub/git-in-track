---
id: GIT-T-0136
type: task
title: Persist field_map into project.yaml with a surgical YAML edit
status: todo
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:37Z
updated: 2026-09-13T13:18:37Z
---

## Description

Add the writer for `integrations.youtrack.field_map` in `docs/.pmngr/project.yaml`. Because `core.ProjectConfig` has no `Extra` map and the decoder drops unknown keys, the file must never be re-serialised wholesale: edit the YAML node tree in the style of `setYAMLPath` (`internal/core/allocator.go:545`), which is the only existing writer and already preserves comments and unrelated keys. Expose it through the settings patch handler following `handleGitSettingsPatch` (`internal/server/git.go:374-393`) including its `persisted` flag.

## Acceptance Criteria

- [ ] Saving a field map preserves comments and every unrelated key in `project.yaml`.
- [ ] The patch handler mirrors the git settings handler, including `persisted`.
- [ ] A malformed map is rejected with field-level errors before any write.
- [ ] `go test -race ./internal/core/... ./internal/server/...` covers the surgical write against a fixture with comments.
