---
id: GIT-M-0005
type: milestone
title: Phase 4 — Git sync
status: in_progress
author: team
labels: [git, security]
created: 2026-09-03T00:00:00Z
updated: 2026-09-04T00:00:00Z
links:
  - { kind: relates_to, target: GIT-EP-0005 }
custom:
  phase: 4
  version: v0.5.0
---

## Description

Phase 4 of the roadmap. Git operations from inside the product in both modes: commit on
save, explicit fetch/rebase/push, conflict resolution UI, and credential handling that
never persists a secret.

## Acceptance Criteria

- [ ] Two concurrent clones reconciled through the UI, conflict included.
- [ ] No credential written to disk or `localStorage` (test-enforced).
- [ ] Commit on save batches edits into one commit per logical change.
- [ ] Push failures are recoverable and explained.
- [ ] Browser CORS limitation documented with a working proxy recipe.

## Notes

Epic: GIT-EP-0005. Stories: GIT-US-0020 … GIT-US-0023. Risks: R3, R5.
