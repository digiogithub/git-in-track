---
id: GIT-US-0146
type: story
title: Serialize go-git access in the server and fix the dead-letter retry flake
status: done
priority: high
author: mcp
labels: [server, git, agent-ok]
estimate: 3
created: 2026-09-24T20:23:59Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
---

## Description

GIT-US-0145 (PR #47) found that the gitops Committer drove one go-git Repository from several goroutines at once, and fixed it with a lock per repository. The same risk remains in `internal/server`: HTTP handlers share one backend per repository, so concurrent requests can still use go-git at the same time, and go-git is not safe for concurrent use.

Separately, `internal/server` `TestSyncJobRetryFromTheDeadLetter` fails intermittently under load ("the retry published sync.job.started, want sync.job.queued"). This is an event-ordering flake.

## Acceptance Criteria

- [x] Every go-git use of a repository in `internal/server` is serialized, either with the Committer's lock or with one shared per-repository guard. A test that sends concurrent requests passes under `-race`.
- [x] `TestSyncJobRetryFromTheDeadLetter` waits for events rather than relying on their order, or the ordering bug in the code is fixed.
- [x] `go test -race -count=50 ./internal/server/ -run 'Sync|Git'` passes under concurrent load.

## Notes

Found by the GIT-US-0145 implementer. See `docs/.pmngr/comments/GIT-US-0145/`.
