---
id: GIT-T-0220
type: task
title: Add the KB sync MCP tools
status: done
priority: medium
parent: GIT-US-0094
milestone: GIT-M-0011
author: mcp
labels: [mcp, agent-ok]
estimate: 3
created: 2026-09-13T13:21:46Z
updated: 2026-09-13T15:39:29Z
started: 2026-09-13T15:39:04Z
closed: 2026-09-13T15:39:29Z
---

## Description

Register `publish_kb_page_to_youtrack` and `sync_kb_page_from_youtrack` in `internal/mcp/tools_youtrack.go` as write tools with `In` `{project, path, recursive}`, returning `{jobId, pages: [{path, action, articleId, url, error?}]}`. Every path argument goes through `s.guard.Check` (`internal/mcp/paths.go:122`) before reaching the vault, since KB paths are user-supplied.

## Acceptance Criteria

- [x] Both tools are registered as write tools and absent on a read-only server.
- [x] Path arguments are checked by the path guard before dispatch, including a traversal attempt test.
- [x] The result shape carries the job id and the per-page list.
- [x] `go test -race ./internal/mcp/...` passes with behaviour tests through the client harness.

## Notes

Both tools queue a job rather than doing the work, so the per-page entries carry
`path` and `action` (`publish` / `pull`) at call time; `articleId`, `url` and
`error` are filled by the job's own report, which is what the shape's optional
fields are for.

`TestKBSyncToolsGuardThePath` proves the traversal attempt is refused with
`forbidden_path` **and** that nothing was queued — the guard runs before the
core is asked anything.
