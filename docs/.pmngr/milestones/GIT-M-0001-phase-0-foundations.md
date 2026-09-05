---
id: GIT-M-0001
type: milestone
title: Phase 0 — Foundations
status: done
author: team
labels: [core, ci, wasm]
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T21:47:05Z
started: 2026-09-03T20:42:39Z
closed: 2026-09-03T21:47:05Z
links:
  - { kind: relates_to, target: GIT-EP-0001 }
custom:
  phase: 0
  version: v0.1.0
---

## Description

Phase 0 of the roadmap. Establish the repository scaffold, the shared Go core (model,
front-matter parser, validation, ID allocation, index and query) and the CI/release
pipeline. No user-facing surface ships in this milestone; everything else depends on it.

## Acceptance Criteria

- [ ] Repository layout matches the architecture brief.
- [ ] `make build` produces a `gintrack` binary on Linux, macOS and Windows.
- [ ] `make test` green; `internal/core` coverage at or above 85 %.
- [ ] Parser round-trips every fixture file (golden tests).
- [ ] The core compiles to WASM and answers a smoke query in a browser.
- [ ] `v0.1.0` releases six archives plus `checksums.txt` via GoReleaser.

## Notes

Epic: GIT-EP-0001. Stories: GIT-US-0001 … GIT-US-0005.
