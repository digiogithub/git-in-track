---
id: GIT-US-0193
type: story
title: Replace the running gintrack binary safely and detect install channels
status: done
priority: high
parent: GIT-EP-0032
assignees: [claude]
author: mcp
labels: [cli, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T19:51:22Z
started: 2026-09-30T19:38:38Z
closed: 2026-09-30T19:51:22Z
---

## Description

In `internal/selfupdate`: swap the running executable for the verified binary, and decide whether this install may self-update at all (ADR-040).

## Acceptance Criteria

- [x] The target is `os.Executable` resolved through symlinks. The new file is written next to it and keeps the existing mode (and owner where possible), then renamed atomically. The old binary is kept as `.old` until success, with rollback on failure. On Windows the running `.exe` is renamed aside first and cleaned up on the next run. A permission error says to re-run with enough rights; it never escalates privileges.
- [x] Install-channel detection: a Homebrew Caskroom/Cellar path, a Scoop `apps` path and a container (`/.dockerenv` or cgroup) are refused, with the right upgrade command. A source or dev build (`version=dev` or `builtBy=source`) only accepts an explicit version. `--force` overrides each case with a warning.
- [x] Does not collide with the hidden `__pando-watch` pre-dispatch in `cmd/gintrack/main.go`.
- [x] Tests cover the swap and rollback in a temp dir, mode preservation, the Windows rename path (build-tagged or simulated), and each channel rule.

## Notes

Done in PR #126. API: `Apply`, `ApplyOptions`, `ApplyError` (with `BinaryMayBeMissing` and `IsPermission`), `CurrentExecutable`, `CleanupOld`, `DetectChannel`, `CheckChannel` and `ChannelError`. `main.go` runs the Windows-only `CleanupOld` before `Execute`. The Windows path was simulated in tests, not run on Windows.
