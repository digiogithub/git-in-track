---
id: GIT-EP-0012
type: epic
title: Import YouTrack issues into the backlog
status: backlog
priority: high
milestone: GIT-M-0011
author: mcp
labels: [core, server, web, mcp]
created: 2026-09-13T13:07:26Z
updated: 2026-09-13T13:07:26Z
---

## Description

A new "Import from YouTrack" entry in the Backlog. The user searches issues of the linked project with a query box (YouTrack query language, with presets: epics, user stories, tasks, versions, unresolved) and an autosuggest list, picks one or many, chooses what to pull along — subtasks recursively, linked issues, comments, attachments — previews the mapping and runs the import as a sync-engine job with progress.

Every imported issue becomes a normal git-in-track item written through the vault (commits, WebSocket events, rev), carrying `external: [{system: youtrack, id, url}]`. YouTrack Epic → epic, User Story → story, Task/Bug → task, Version (Fix versions bundle) → milestone; Subtask links → `parent`; Relates/Depend/Duplicate links → `links[]`; comments → comment files (ADR-012) with the original author and timestamp. Re-importing an already linked issue updates the item instead of duplicating it: `(external.system, external.id)` is the idempotency key.

## Acceptance Criteria

- [ ] Backlog toolbar shows "Import from YouTrack" when `features.youtrack` and the project is linked.
- [ ] Search dialog with query presets, debounced autosuggest and multi-select; shows type, state, id.
- [ ] Options: include subtasks (depth), include linked issues, include comments, include attachments.
- [ ] Preview lists what will be created vs updated, with type and parent mapping, before running.
- [ ] Import runs as a job; progress and result over WebSocket; failures listed per issue.
- [ ] Idempotent re-import; items keep their gintrack id.
- [ ] Vault methods `youtrack.import.preview` / `youtrack.import.run` reachable from REST, MCP (`import_youtrack_issues`) and CLI (`gintrack youtrack import`).
- [ ] Field map (State → status, Priority → priority, Estimation → estimate, Assignee → assignees) configurable per project with sensible defaults.

## Notes

Read links from the issue payload (`links(direction,linkType(...),issues(...))`); Subtask + OUTWARD = children, INWARD = parent. Filter empty link entries. Page comments and attachments explicitly (`$top`).
