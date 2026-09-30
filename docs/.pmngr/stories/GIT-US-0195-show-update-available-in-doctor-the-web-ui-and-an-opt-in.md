---
id: GIT-US-0195
type: story
title: Show update available in doctor, the web UI and an opt-in CLI notice
status: done
priority: medium
parent: GIT-EP-0032
assignees: [claude]
author: mcp
labels: [cli, server, web, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T20:20:03Z
started: 2026-09-30T19:39:27Z
closed: 2026-09-30T20:20:03Z
---

## Description

Passive notices from ADR-040. Nothing downloads without `gintrack update`.

## Acceptance Criteria

- [x] The latest-version answer is cached as derived data under the cache dir: about 6 h on success, 15 min on failure. Lookups have a short timeout and never block startup.
- [x] `gintrack doctor` has an "update" check (current vs latest stable, and "not applicable" for package-manager or dev installs).
- [x] A companion endpoint (for example `GET /api/v1/version`) returns current, latest and updateAvailable, and the web UI shows a small, dismissible "update available" notice with the command to run. Browser-only mode shows nothing.
- [x] New config key `update.checkOnStart` (default false). When true, interactive CLI commands print one stderr line when an update exists; never for `--json`, `gintrack mcp`, `serve` logs or non-TTY stderr. docs/07 §3.2 is updated.
- [x] Tests: cache TTLs, doctor output, endpoint, the notice suppression rules, and Vitest for the web notice.

## Notes

Done in PR #127. The cache lives in `<cacheDir>/update-check.json` (`selfupdate.Checker`). "Not applicable" comes from `DetectChannel`. The endpoint is `GET /api/v1/version`, behind the bearer token, and the web notice is `UpdateNotice`, dismissed per version. The notice is also suppressed for `--quiet`, `update`, `version`, `doctor`, `completion` and dev builds, and exit waits at most 300 ms.
