---
id: GIT-US-0194
type: story
title: gintrack update command
status: done
priority: high
parent: GIT-EP-0032
assignees: [claude]
author: mcp
labels: [cli, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T20:06:37Z
started: 2026-09-30T19:51:46Z
closed: 2026-09-30T20:06:37Z
---

## Description

The user-facing command, built on the release lookup and binary-swap stories of GIT-EP-0032.

## Acceptance Criteria

- [x] `gintrack update [version] [--check] [--prerelease] [--force] [--yes] [--json]`. Without `--yes` it asks for confirmation on a TTY, and it never prompts off a TTY: there it needs `--yes`, or `--check` which only reports.
- [x] Distinct exit codes in `cmd/gintrack/exit.go`: up to date, update available (`--check`), updated, refused (channel), verification failed. `--json` gives current, latest, url, action and reason.
- [ ] After a successful update it reports the new version, and it detects a running `gintrack serve` or a managed Pando (supervisor state files and locks under the cache dir). If it finds one, it tells the user to restart; it never restarts anything itself.
- [x] docs/07 gets a new `gintrack update` section, and `gintrack version` is cross-referenced; CHANGELOG Unreleased is updated.
- [x] Command tests use the fake GitHub from the lookup story and a temp install dir.

## Notes

Done in PR #128. Exit codes: 0 up to date or updated, 10 update available (`--check`), 11 refused, 12 verification failed, 13 install failed, and 4 for an unknown version. Smoke-tested against the real GitHub on scratch copies: a dev build was refused, `update 2.2.0 --force` succeeded, and a build stamped 2.1.0 went to 2.2.0.

The restart hint detects a managed Pando through its state files. A plain `gintrack serve` without a managed Pando leaves no state file and is not detected, so that criterion is only partly met; this moves to GIT-US-0197.
