---
id: GIT-T-0022
type: task
title: Write the integration block back to project.yaml without losing keys
status: todo
priority: medium
parent: GIT-US-0048
milestone: GIT-M-0011
author: mcp
labels: [core, agent-ok]
estimate: 3
created: 2026-09-13T13:15:48Z
updated: 2026-09-13T13:15:48Z
---

## Description

Add a writer for the `integrations.youtrack` block that edits `project.yaml` surgically through the YAML-node path helper already used by the ID allocator (`setYAMLPath`, `internal/core/allocator.go:545`, called from `writeCounter` `:414`). A wholesale re-serialization would silently delete every key `ProjectConfig` does not model, since it has no `Extra` map. Write atomically, tmp file plus rename, as `writeFileAtomic` (`internal/core/store.go:1051`) does.

## Acceptance Criteria

- [ ] Writing the block preserves comments, key order and unrelated sections, proved by a round-trip test on a realistic file.
- [ ] Creating the block when it is absent and updating it when present both work.
- [ ] `go test -race ./internal/core/...` includes a fixture containing keys the Go struct does not model.
