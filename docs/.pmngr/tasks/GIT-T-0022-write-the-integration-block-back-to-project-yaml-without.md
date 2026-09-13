---
id: GIT-T-0022
type: task
title: Write the integration block back to project.yaml without losing keys
status: done
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:15:48Z
updated: 2026-09-13T14:41:29Z
started: 2026-09-13T14:41:09Z
closed: 2026-09-13T14:41:29Z
---

## Description

Add a writer for the `integrations.youtrack` block that edits `project.yaml` surgically through the YAML-node path helper already used by the ID allocator (`setYAMLPath`, `internal/core/allocator.go:545`, called from `writeCounter` `:414`). A wholesale re-serialization would silently delete every key `ProjectConfig` does not model, since it has no `Extra` map. Write atomically, tmp file plus rename, as `writeFileAtomic` (`internal/core/store.go:1051`) does.

## Acceptance Criteria

- [x] Writing the block preserves comments, key order and unrelated sections, proved by a round-trip test on a realistic file.
- [x] Creating the block when it is absent and updating it when present both work.
- [x] `go test -race ./internal/core/...` includes a fixture containing keys the Go struct does not model.

## Notes

`config.SaveYouTrackLink` (`internal/config/projectlink.go`) edits the YAML node tree in place and renames a temporary file over the original, keeping the file's mode. It returns `changed bool` and writes nothing when the file already says exactly this, so a no-op settings save does not dirty a tracked file. `setYAMLPath` in `internal/core` is unexported and that package was owned by another agent this wave, so the node helpers are a small, local reimplementation; folding the two together is a worthwhile follow-up for whoever owns `internal/core`.

Tests are in `internal/config/projectlink_test.go` rather than `internal/core`: the fixture carries comments, an unmodelled `house_rules:` section and a hand-written workflow, and asserts all three survive.
