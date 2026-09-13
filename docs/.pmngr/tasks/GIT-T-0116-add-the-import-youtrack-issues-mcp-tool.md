---
id: GIT-T-0116
type: task
title: Add the import_youtrack_issues MCP tool
status: todo
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 3
created: 2026-09-13T13:18:10Z
updated: 2026-09-13T13:18:10Z
---

## Description

Create `internal/mcp/tools_youtrack.go` with `registerYouTrackTools(s)` called from `registerTools` (`internal/mcp/tools.go:24`), and register `import_youtrack_issues` as a write tool with `In` `{project, query?, ids?, depth, includeLinks, includeComments, includeAttachments, dryRun}` carrying `jsonschema` tags. Validate with `invalidField`, dispatch `youtrack.import.preview` when `dryRun` is true and `youtrack.import.run` otherwise, project the result through `internal/mcp/wire.go` so every item carries `id` and `rev`, and call `s.announce` on a write.

## Acceptance Criteria

- [ ] The tool is registered as a write tool and is absent from `tools/list` on a read-only server.
- [ ] `dryRun` selects preview versus run and the result projection carries `id` and `rev`.
- [ ] Invalid arguments produce a field-level `{"error":{…}}` payload.
- [ ] A behaviour test through the in-memory MCP client harness passes under `go test -race ./internal/mcp/...`.
