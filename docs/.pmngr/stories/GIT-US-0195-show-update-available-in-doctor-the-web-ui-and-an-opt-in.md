---
id: GIT-US-0195
type: story
title: Show update available in doctor, the web UI and an opt-in CLI notice
status: backlog
priority: medium
parent: GIT-EP-0032
author: mcp
labels: [cli, server, web, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T19:16:15Z
---

## Description

Passive notices from ADR-040. Nothing downloads without `gintrack update`.

## Acceptance Criteria

- [ ] The latest-version answer is cached as derived data under the cache dir: about 6 h on success, 15 min on failure. Lookups have a short timeout and never block startup.
- [ ] `gintrack doctor` has an "update" check (current vs latest stable, and "not applicable" for package-manager or dev installs).
- [ ] A companion endpoint (for example `GET /api/v1/version`) returns current, latest and updateAvailable, and the web UI shows a small, dismissible "update available" notice with the command to run. Browser-only mode shows nothing.
- [ ] New config key `update.checkOnStart` (default false). When true, interactive CLI commands print one stderr line when an update exists; never for `--json`, `gintrack mcp`, `serve` logs or non-TTY stderr. docs/07 §3.2 is updated.
- [ ] Tests: cache TTLs, doctor output, endpoint, the notice suppression rules, and Vitest for the web notice.
