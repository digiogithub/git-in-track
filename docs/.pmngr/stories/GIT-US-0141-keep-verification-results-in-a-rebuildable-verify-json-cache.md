---
id: GIT-US-0141
type: story
title: Keep verification results in a rebuildable verify.json cache
status: done
priority: high
parent: GIT-EP-0024
milestone: GIT-M-0015
author: mcp
labels: [core, cli, wasm, agent-ok]
estimate: 3
created: 2026-09-24T15:29:49Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

ADR-037 §4 and §7 say that `verify` runs only fill a local, rebuildable cache next to the index: `<docs>/.pmngr/verify.json` natively, and IndexedDB in the browser. The `verified:` stamp is written into the spec only when the implementing story is done, or by `gintrack spec verify --commit`. R-LOC-5 already names `verify.json`, but no story owned the cache itself or its ignore entry. This came up during the GIT-US-0105 review.

Blocked by GIT-US-0105. Feeds GIT-US-0116.

## Acceptance Criteria

- [x] The cache format stores, per requirement: block rev, commit, test ids, result and time. It is versioned and fully rebuildable. It is never read as the source of truth.
- [x] Native store at `<docs>/.pmngr/verify.json` and a browser store in IndexedDB. Both sit behind a core interface, so `internal/core` stays WASM-clean.
- [x] Coverage reads the cache first and falls back to the `verified:` stamp when the cache is empty, as ADR-037 §7 requires.
- [x] The `.gitignore` snippet from `gintrack init` ignores `verify.json` (and `index.json`, if missing). This repository's `.gitignore` is updated to match.
- [x] Tests cover a missing cache, a corrupt cache (rebuilt, never fatal) and precedence over the stamp. `make test`, `make lint` and `make wasm` pass.
- [x] docs/03 R-LOC-5 and §21 are updated.

## Notes

Flagged as unowned by the GIT-US-0105 implementer.
