---
id: GIT-T-0164
type: task
title: Add the push_comments setting and the enqueue decision
status: todo
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:16Z
updated: 2026-09-13T13:19:16Z
---

## Description

Read `integrations.youtrack.push_comments` (`manual` by default, or `auto`) from the project config and add `shouldPushComment(project, item, comment) bool` in `internal/server/youtrack_comments.go`: true only when the mode is `auto`, the project is linked and the parent item carries a YouTrack `external` reference. Log the decision once per item, not once per comment.

## Acceptance Criteria

- [ ] The setting is read from `project.yaml` and defaults to `manual`.
- [ ] `shouldPushComment` is true only for auto mode on a linked project with a linked item.
- [ ] Logging is per item, not per comment.
- [ ] `go test -race ./internal/server/...` covers each combination.
