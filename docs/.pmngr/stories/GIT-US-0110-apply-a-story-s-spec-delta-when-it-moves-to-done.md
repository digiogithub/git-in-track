---
id: GIT-US-0110
type: story
title: Apply a story's Spec Delta when it moves to done
status: in_review
priority: medium
parent: GIT-EP-0023
milestone: GIT-M-0015
author: claude
labels: [core, server, agent-ok]
estimate: 5
created: 2026-09-24T12:09:37Z
updated: 2026-09-24T18:55:57Z
links:
  - { kind: blocked_by, target: GIT-US-0107 }
  - { kind: blocked_by, target: GIT-US-0109 }
---

## Description

As a maintainer, I want the living spec updated automatically from the reviewed delta when the story is done, so specs never drift from what was merged and nobody copies text by hand.

## Acceptance Criteria

- [x] When a story transitions to a `done`-category status, the vault applies its delta: ADDED allocates new `R<n>` refs, MODIFIED replaces the block (its `verified` stamp becomes stale because the block `rev` changes), REMOVED sets the requirement to a `cancelled`-category status and keeps the block and ID.
- [x] A requirement moved to another spec gets a new ref with `supersedes` pointing at the old one.
- [x] Application is atomic per story: a conflict (stale block `rev`, missing target) refuses the transition with a clear error and changes nothing.
- [x] The story body records the refs it created (ADDED blocks rewritten with their numbers) and gains `implements`/`modifies` links.
- [x] Tests in `internal/vault` cover each operation and the failure path; docs/03 and docs/07 describe the behaviour.

## Notes

Depends on the delta parser and vault requirement addressing.
