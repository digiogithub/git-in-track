---
id: GIT-US-0194
type: story
title: gintrack update command
status: backlog
priority: high
parent: GIT-EP-0032
author: mcp
labels: [cli, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T19:16:15Z
---

## Description

The user-facing command, built on the release lookup and binary-swap stories of GIT-EP-0032.

## Acceptance Criteria

- [ ] `gintrack update [version] [--check] [--prerelease] [--force] [--yes] [--json]`. Without `--yes` it asks for confirmation on a TTY, and it never prompts off a TTY: there it needs `--yes`, or `--check` which only reports.
- [ ] Distinct exit codes in `cmd/gintrack/exit.go`: up to date, update available (`--check`), updated, refused (channel), verification failed. `--json` gives current, latest, url, action and reason.
- [ ] After a successful update it reports the new version, and it detects a running `gintrack serve` or a managed Pando (supervisor state files and locks under the cache dir). If it finds one, it tells the user to restart; it never restarts anything itself.
- [ ] docs/07 gets a new `gintrack update` section, and `gintrack version` is cross-referenced; CHANGELOG Unreleased is updated.
- [ ] Command tests use the fake GitHub from the lookup story and a temp install dir.
