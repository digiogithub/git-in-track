---
id: GIT-EP-0024
type: epic
title: Requirement trace engine
status: done
priority: high
milestone: GIT-M-0015
author: claude
labels: [git, server, cli]
created: 2026-09-24T12:08:04Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Link every requirement to the code and tests that implement and verify it, and know whether it still holds. Parts: `gitops.ChangedFiles(from, to)` for the go-git, system-git and jj backends; a native scanner for `// Implements:` / `// Verifies:` markers; a trace graph merging markers with the optional `trace:` entry; ingest of test results (`go test -json`, JUnit XML, Vitest JSON) into a derived cache; the `verified` stamp and the computed status `untested` / `passing` / `failing` / `suspect`.

All of this is native: it stays out of `internal/core` and `internal/vault` and is installed by the host behind a seam, like `vault.SemanticSearcher`.

## Acceptance Criteria

- [x] Changed files between two refs are available from all three git backends.
- [x] Every requirement has a deterministic trace (code and tests) and a coverage status.
- [x] Suspect is computed, never stored, from the block `rev` and changes since `verified.commit`.
- [x] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §3. Runs in parallel with the Pando impact epic after the data model lands.
