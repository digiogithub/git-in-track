---
id: GIT-US-0154
type: story
title: Write the integrations block of project.yaml without re-encoding the file
status: in_review
priority: medium
author: mcp
labels: [core, agent-ok]
estimate: 2
created: 2026-09-24T22:32:35Z
updated: 2026-09-24T22:43:24Z
---

## Description

GIT-US-0153 (PR #67) fixed the counter write so that it edits `project.yaml` in place instead of re-encoding the whole yaml.v3 node tree. The re-encode had turned an unquoted flow-mapping value into visible extra keys. `internal/config/projectlink.go`, which writes the integrations block (for example the YouTrack project link), still re-encodes the file the same way.

## Acceptance Criteria

- [x] A failing test: linking a project rewrites unrelated lines of `project.yaml`, such as comments, quoting or flow mappings.
- [x] The integrations write uses the in-place splice from `internal/core/yamlsplice.go`, or an equivalent that preserves the file. The result is byte-identical apart from the changed block.
- [x] `make test` and `make lint` pass.
