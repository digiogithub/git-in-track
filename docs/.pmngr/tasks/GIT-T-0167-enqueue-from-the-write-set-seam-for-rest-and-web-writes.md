---
id: GIT-T-0167
type: task
title: Enqueue from the write-set seam for REST and web writes
status: todo
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:21Z
updated: 2026-09-13T13:19:21Z
---

## Description

Inspect the `WriteSet` on the existing publish path (`internal/server/events.go:256` `publishWrite`, fed by `comment.add` at `internal/vault/vault.go:1185`) and enqueue a `youtrack.comment.push` job for each newly written comment that passes `shouldPushComment`. Use `context.WithoutCancel(ctx)` as `internal/gitops/committer.go:167` does. Guard on the write set's changed fields so the job's own `external` write-back cannot re-trigger a push.

## Acceptance Criteria

- [ ] A comment written over REST or from the web app enqueues exactly one push in auto mode and none in manual mode.
- [ ] The `external` write-back does not re-trigger a push.
- [ ] Enqueue uses `context.WithoutCancel`.
- [ ] `go test -race ./internal/server/...` covers both modes and asserts the absence of a feedback loop.
