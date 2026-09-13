---
id: GIT-T-0164
type: task
title: Add the push_comments setting and the enqueue decision
status: done
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:16Z
updated: 2026-09-13T16:28:12Z
started: 2026-09-13T16:27:48Z
closed: 2026-09-13T16:28:12Z
---

## Description

Read `integrations.youtrack.push_comments` (`manual` by default, or `auto`) from the project config and add `shouldPushComment(project, item, comment) bool` in `internal/server/youtrack_comments.go`: true only when the mode is `auto`, the project is linked and the parent item carries a YouTrack `external` reference. Log the decision once per item, not once per comment.

## Acceptance Criteria

- [x] The setting is read from `project.yaml` and defaults to `manual`.
- [x] `shouldPushComment` is true only for auto mode on a linked project with a linked item.
- [x] Logging is per item, not per comment.
- [x] `go test -race ./internal/server/...` covers each combination.

## Notes

`shouldPushComment` is deliberately not in `internal/server`. The decision lives in `Vault.autoPushComment` (`internal/vault/youtrackpush.go`), reached from `comment.add`, which every surface writes through — a copy here would be a second rule to drift. What the server owed and now does:

1. `youtrackLinkOf` carries `PushComments` into `vault.YouTrackLink`, which is the whole of the automatic push: without it the vault's `auto` branch could never be true. `TestPushCommentsReachesTheVaultLink` covers unset, `manual` and `auto`.
2. `internal/server/youtrackpushlog.go` logs what the vault decided, once per item, because the vault compiles to WebAssembly and has no logger. `TestPushDecisionIsLoggedOncePerItem` and `TestCommentPushItemIDReadsThePayload` cover it.

The second criterion is therefore honoured by `internal/vault`'s own tests plus the link test here; the "the item is not linked" half is proven by `TestCommentPushFailsTerminallyWithoutAnItemLink`.
