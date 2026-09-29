---
id: GIT-US-0178
type: story
title: Make the YouTrack server tests independent of the network
status: done
priority: low
assignees: [claude]
author: mcp
labels: [server, agent-ok]
created: 2026-09-29T22:21:36Z
updated: 2026-09-29T22:47:53Z
started: 2026-09-29T22:30:55Z
closed: 2026-09-29T22:47:53Z
---

## Description

While implementing GIT-US-0169 (2026-09-29), `make test` failed once in `internal/server` on a YouTrack bridge test that depends on the network, then passed on rerun. Tests must be hermetic: a flaky network test erodes trust in `make test` and in CI.

## Acceptance Criteria

- [x] Identify the YouTrack test(s) in `internal/server` that reach the network or depend on timing/DNS.
- [x] Replace real network access with an `httptest` server or an injected client; no test touches the internet.
- [x] `go test -race -count=10 ./internal/server/...` passes with networking disabled (for example, run inside `unshare -n` or with a proxy set to an invalid address).

## Notes

Observed as a one-off failure; exact test name not recorded. Fixed in PR #106: no test reached a real host (all used `httptest`), but `TestYouTrackTest/server_error` and `TestIssueSearchNeverEchoesTheToken` slept ~8.5 s each through the client's real retry backoff, making the package slow and timing-sensitive under `-race`. Tests now install an instant sleep; `-race -count=10` passes, also under `unshare -rn`. The original failure was never reproduced, so this is the only wall-clock dependency found, not a confirmed match.
