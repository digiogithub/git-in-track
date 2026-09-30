---
id: GIT-US-0197
type: story
title: Detect a running gintrack serve after an update
status: done
priority: low
parent: GIT-EP-0032
assignees: [claude]
author: mcp
labels: [cli, server, agent-ok]
created: 2026-09-30T20:06:37Z
updated: 2026-09-30T20:34:12Z
started: 2026-09-30T20:06:58Z
closed: 2026-09-30T20:34:12Z
---

## Description

`gintrack update` (GIT-US-0194, #128) prints a restart hint when it finds a managed Pando through the supervisor state files. A plain `gintrack serve` without a managed Pando leaves no state file, so the user is not told to restart it and keeps running the old version.

## Acceptance Criteria

- [x] `gintrack serve` records a small runtime file under the cache dir (pid, port, version, start time), removes it on clean shutdown, and tolerates a stale file.
- [x] `gintrack update` and `gintrack doctor` read it and report a running serve with an older version, and suggest restarting it.
- [x] Tests cover a live serve, a stale file (dead pid) and no file.

## Notes

Done in PR #129. `serve` writes `<cacheDir>/serve/<port>.json` once the listener is bound (`server.Options.OnListen`) and removes it on shutdown; stale files are deleted. `update` lists the live serves that are not at the new version, both in its hint and in `--json` `running[]`. `doctor` warns when a serve is running a different version.
