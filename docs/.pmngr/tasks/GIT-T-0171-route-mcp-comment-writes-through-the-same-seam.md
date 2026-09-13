---
id: GIT-T-0171
type: task
title: Route MCP comment writes through the same seam
status: done
priority: medium
parent: GIT-US-0072
milestone: GIT-M-0011
author: mcp
labels: [mcp, server, agent-ok]
estimate: 2
created: 2026-09-13T13:19:26Z
updated: 2026-09-13T15:39:55Z
started: 2026-09-13T15:39:41Z
closed: 2026-09-13T15:39:55Z
---

## Description

Make an agent-written comment reach the same enqueue seam through `Options.AfterWrite` and `s.publishAgentWrite` (`internal/server/mcp.go:87`, `:260`), with no second implementation of the decision or the enqueue. Verify through the in-memory MCP client harness that `add_comment` in auto mode produces one push job.

## Acceptance Criteria

- [x] An MCP `add_comment` in auto mode enqueues exactly one push through the shared seam.
- [x] No push-decision logic is duplicated in `internal/mcp`.
- [x] `go test -race ./internal/mcp/...` covers it through the client harness.

## Notes

The seam turned out to be one level lower than the task assumed, and better for
it: it is the vault's own `comment.add`, not `AfterWrite`. Every surface —
REST, the web app, the MCP `add_comment` tool and the CLI — dispatches
`comment.add`, so the decision is made once in `Vault.autoPushComment`
(`internal/vault/youtrackpush.go`) and no surface can forget it or spell it
differently. `internal/mcp` contains no push decision at all: the tool file only
validates, guards, dispatches and projects.

`TestAddCommentReachesTheSharedPushSeam` drives it through the real client
session in both modes: `auto` queues exactly one `youtrack.comment.push` job,
`manual` queues none.

Two details the seam gets right by construction: the enqueue runs **after** the
vault mutex is released (`Vault.lockedCall`), so saving a comment never waits on
anything but the file system, and it uses `context.WithoutCancel` so a closing
response cannot cancel the queued job. A comment that already carries a YouTrack
reference — the write-back of the push itself — is never queued again, which
closes the feedback loop on the reference rather than on a flag.
