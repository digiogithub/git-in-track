---
id: GIT-T-0080
type: task
title: Add the YouTrack query composition helper
status: done
priority: medium
parent: GIT-US-0054
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:17:15Z
updated: 2026-09-13T16:03:39Z
started: 2026-09-13T16:03:23Z
closed: 2026-09-13T16:03:39Z
---

## Description

Add `ComposeQuery(projectKey, userQuery, preset string) (string, error)` to `internal/youtrack`. It emits `project: {<key>}`, then the preset clause, then the trimmed user query, and always ends with `order by: created asc` so `$skip` paging stays stable. Presets are `epics` (`Type: Epic`), `stories` (`Type: {User Story}`), `tasks` (`Type: Task`), `unresolved` (`#Unresolved`) and `versions` (handled by the caller through the version bundle endpoint). Values containing spaces are brace-wrapped; an unknown preset is an error.

## Acceptance Criteria

- [ ] The composed query always brace-wraps the project key and ends with the ordering clause.
- [ ] Each supported preset emits its documented clause and an unknown preset returns an error.
- [ ] Values with spaces are brace-wrapped.
- [ ] Table-driven tests pin the exact composed strings; `go test -race ./internal/youtrack/...` passes.
