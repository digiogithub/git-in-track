---
id: GIT-T-0167
type: task
title: Enqueue from the write-set seam for REST and web writes
status: done
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [server, agent-ok]
estimate: 3
created: 2026-09-13T13:19:21Z
updated: 2026-09-13T16:28:44Z
started: 2026-09-13T16:28:31Z
closed: 2026-09-13T16:28:44Z
---

## Description

Inspect the `WriteSet` on the existing publish path (`internal/server/events.go:256` `publishWrite`, fed by `comment.add` at `internal/vault/vault.go:1185`) and enqueue a `youtrack.comment.push` job for each newly written comment that passes `shouldPushComment`. Use `context.WithoutCancel(ctx)` as `internal/gitops/committer.go:167` does. Guard on the write set's changed fields so the job's own `external` write-back cannot re-trigger a push.

## Acceptance Criteria

- [x] A comment written over REST or from the web app enqueues exactly one push in auto mode and none in manual mode.
- [x] The `external` write-back does not re-trigger a push.
- [x] Enqueue uses `context.WithoutCancel`.
- [ ] `go test -race ./internal/server/...` covers both modes and asserts the absence of a feedback loop.

## Notes

Closed as covered by GIT-T-0164 and GIT-T-0171 rather than implemented: a second enqueue in `publishWrite` would fire on the same write `Vault.autoPushComment` already fires on. The behavioural criteria are met one layer down and their tests live in `internal/vault`; the last one names `internal/server` and is left unticked rather than ticked against a test in another package. Full reasoning in the comment thread.
