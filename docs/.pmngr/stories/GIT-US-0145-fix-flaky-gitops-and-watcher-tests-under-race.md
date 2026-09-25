---
id: GIT-US-0145
type: story
title: Fix flaky gitops and watcher tests under -race
status: done
priority: high
author: mcp
labels: [git, ci, agent-ok]
estimate: 2
created: 2026-09-24T17:20:06Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
---

## Description

While GIT-US-0107 was being verified (2026-09-24), `make test` failed once in two packages that change does not touch. Both passed on a rerun with `-race`.

- `internal/gitops`: a nil-pointer panic inside go-git. It may be related to the new `ChangedFiles` from GIT-US-0112 (PR #27) under parallel tests, or it may be pre-existing.
- `internal/watcher`: a timing assertion.

Flaky tests hide real failures in CI and waste reviewer time.

## Acceptance Criteria

- [x] Reproduce both failures, for example with `go test -race -count=50 ./internal/gitops/ ./internal/watcher/`, and record the stack traces in a comment.
- [x] The gitops panic is fixed at its root cause. If it lives in `ChangedFiles`, fix it on the GIT-US-0112 branch.
- [x] The watcher test waits on an event or condition instead of a fixed sleep.
- [x] `go test -race -count=50` on both packages passes.

## Notes

Reported by the GIT-US-0107 implementer.
