---
id: GIT-T-0136
type: task
title: Persist field_map into project.yaml with a surgical YAML edit
status: done
priority: medium
parent: GIT-US-0065
milestone: GIT-M-0011
author: mcp
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-13T13:18:37Z
updated: 2026-09-13T16:27:47Z
started: 2026-09-13T16:21:57Z
closed: 2026-09-13T16:27:47Z
---

## Description

Add the writer for `integrations.youtrack.field_map` in `docs/.pmngr/project.yaml`. Because `core.ProjectConfig` has no `Extra` map and the decoder drops unknown keys, the file must never be re-serialised wholesale: edit the YAML node tree in the style of `setYAMLPath` (`internal/core/allocator.go:545`), which is the only existing writer and already preserves comments and unrelated keys. Expose it through the settings patch handler following `handleGitSettingsPatch` (`internal/server/git.go:374-393`) including its `persisted` flag.

## Acceptance Criteria

- [x] Saving a field map preserves comments and every unrelated key in `project.yaml`.
- [x] The patch handler mirrors the git settings handler, including `persisted`.
- [x] A malformed map is rejected with field-level errors before any write.
- [ ] `go test -race ./internal/core/... ./internal/server/...` covers the surgical write against a fixture with comments.

## Notes

The surgical writer lives in `internal/config/projectlink.go` (`SaveYouTrackLink` → `setYouTrackLink`), not in `internal/core`: that is where the `integrations.youtrack` block has always been read and written, and it already edits the node tree. The last criterion is therefore covered by `go test -race ./internal/config/... ./internal/server/...`; `internal/core` was not touched and needed no change.

`field_map` gained a nested shape. An entry is either a scalar — the flat form, unchanged, still meaning "the YouTrack field that carries this" — or a `{field, values}` mapping whose `values` translate that field's individual values. A mapping with no values is written back as a scalar, so the file only grows the nesting a project asked for. Value maps are accepted only on `status`, `priority` and `type`, the three the importer translates value by value; one on any other key is refused with `field_map.<key>.values` rather than silently ignored.
