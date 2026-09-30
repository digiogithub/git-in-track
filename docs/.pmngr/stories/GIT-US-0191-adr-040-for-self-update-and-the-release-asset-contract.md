---
id: GIT-US-0191
type: story
title: ADR-040 for self-update and the release asset contract
status: done
priority: high
parent: GIT-EP-0032
assignees: [claude]
author: mcp
labels: [docs, agent-ok]
created: 2026-09-30T19:16:15Z
updated: 2026-09-30T19:38:18Z
started: 2026-09-30T19:25:29Z
closed: 2026-09-30T19:38:18Z
---

## Description

Record the GIT-EP-0032 decisions (2026-09-30) as ADR-040. Document the release assets the updater depends on as a contract. Fix the docs that are already stale about releases.

## Acceptance Criteria

- [x] `docs/adr/ADR-040-*.md` covers the context (Pando's updater and its weaknesses), the four maintainer decisions, the options considered (a library like go-selfupdate/go-update vs the stdlib; auto-restart vs a hint), security (TLS + sha256 + size, no signature check on update, pre-release policy, rate limits and optional `GITHUB_TOKEN`) and consequences; the README index is updated.
- [x] docs/09 states the asset naming contract (`gintrack_<ver>_<os>_<arch>.tar.gz` for linux, `.zip` for darwin and windows, flat layout, `checksums.txt` sha256) and that changing it requires updating `internal/selfupdate`.
- [x] Stale text is fixed: AGENTS.md "unsigned by design" (ADR-029 supersedes it) and docs/07 §2.1/§2.3 (GoReleaser, darwin tar.gz, paused channels). `.goreleaser.yaml` drift is only reported as a human-only follow-up, not edited.
- [x] `make lint` passes.

## Notes

Done in PR #124: ADR-040 (Accepted, 2026-09-30), the docs/09 §3.1 contract, and fixes to AGENTS.md and docs/07. The goreleaser drift is tracked in GIT-US-0196.
