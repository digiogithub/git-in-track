---
id: GIT-T-0116
type: task
title: Add the import_youtrack_issues MCP tool
status: done
priority: medium
parent: GIT-US-0062
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 3
created: 2026-09-13T13:18:10Z
updated: 2026-09-13T15:38:36Z
started: 2026-09-13T15:38:06Z
closed: 2026-09-13T15:38:36Z
---

## Description

Add `internal/mcp/tools_youtrack.go` with a `registerYouTrackTools(s)` called from `registerTools` (`internal/mcp/tools.go:24`). The tool is a write tool, so it is absent from `tools/list` on a read-only server; its `In` struct carries `{project, query?, ids?, depth, includeLinks, includeComments, includeAttachments, dryRun}` with `jsonschema` tags, and `dryRun: true` dispatches `youtrack.import.preview` while `false` dispatches `youtrack.import.run`. The handler validates with `invalidField`, dispatches through `dispatch[T]`, projects the result through `internal/mcp/wire.go` so every returned item carries `id` and `rev`, and calls `s.announce` on a write.

## Acceptance Criteria

- [x] `import_youtrack_issues` is registered as a write tool, hidden on a read-only server, with `dryRun` selecting preview vs run.
- [x] The MCP surface-pinning tests are updated and pass.
- [x] Errors carry the machine-readable `{"error":{…}}` payload over MCP.
- [x] `go test -race ./internal/mcp/...` passes, including a behaviour test through the in-memory MCP client harness.

## Notes

`internal/mcp/tools_youtrack.go` holds all four YouTrack tools, as the story of
GIT-EP-0013 and GIT-EP-0014 asked — created once, extended, not forked.

The file carries no business logic: it validates arguments, guards paths,
dispatches and projects. Every returned item carries the `rev` a later write has
to quote, read back with `s.itemRev`, so a caller never has to re-read the item
it just imported.
