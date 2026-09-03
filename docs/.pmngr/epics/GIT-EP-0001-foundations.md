---
id: GIT-EP-0001
type: epic
title: Foundations
status: in_progress
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T20:42:39Z
started: 2026-09-03T20:42:39Z
author: team
priority: critical
milestone: GIT-M-0001
estimate: 21
labels: [core, ci, wasm]
links: []
---

## Description

Phase 0. Establish the monorepo, the shared Go core and the build/release pipeline that
every later phase stands on. Nothing user-facing ships in this epic; everything
user-facing depends on it.

The core (`internal/core`) is the heart of the product: the item model, the YAML
front-matter parser and serialiser, schema validation against configurable workflows, ID
allocation, and the in-memory index and query API. It must compile unchanged both natively
and to `GOOS=js GOARCH=wasm`, so it accesses files only through the `core.FS` interface.

## Acceptance Criteria

- [ ] The repository layout matches the architecture brief exactly.
- [ ] `make build` produces a `gintrack` binary on Linux, macOS and Windows.
- [ ] `make test` is green and `internal/core` coverage is at or above 85 %.
- [ ] The parser round-trips every fixture file, verified by golden tests.
- [ ] The same core compiles to WASM and answers a smoke query from a browser page.
- [ ] Tagging `v0.1.0` produces six archives plus `checksums.txt` through GoReleaser.

## Notes

Stories: GIT-US-0001, GIT-US-0002, GIT-US-0003, GIT-US-0004, GIT-US-0005.
See docs/11-roadmap.md, milestone 1.
