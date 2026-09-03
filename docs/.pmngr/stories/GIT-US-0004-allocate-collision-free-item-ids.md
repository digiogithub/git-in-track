---
id: GIT-US-0004
type: story
title: Allocate collision-free item IDs
status: done
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T21:47:05Z
closed: 2026-09-03T21:47:05Z
started: 2026-09-03T21:17:39Z
author: team
priority: high
parent: GIT-EP-0001
milestone: GIT-M-0001
estimate: 5
labels: [core]
links:
  - kind: blocked_by
    target: GIT-US-0002
---

## Description

As a team working on parallel branches, I want new items to get IDs that do not collide, so
that a merge never produces two `GIT-US-0042` files or a reference that points at both.

IDs are `<KEY>-<TYPE>-<NNNN>`, allocated from the highest existing number for that type
across the **whole repository**, not the current working branch. Duplicates are a hard
validation error at load, naming both files. `gintrack renumber <id> <new-id>` rewrites the
file and every inbound reference (parent, milestone, links, board refs, comments) in one
atomic operation.

`project.yaml` offers `idStrategy: sequential | ulid` for teams with heavy parallel work;
ULIDs trade readability for guaranteed uniqueness. This story addresses risk R4.

## Acceptance Criteria

- [ ] `NextID(type)` returns the next free number, padded per `idPadding`.
- [ ] Allocation scans all item directories, not just the one being written to.
- [ ] A duplicate ID at load produces `ErrDuplicateID` naming both file paths.
- [ ] `gintrack renumber` updates the file name, the `id` field and all inbound references.
- [ ] `renumber` is atomic: a failure leaves the vault unchanged.
- [ ] `idStrategy: ulid` produces valid, sortable ULIDs and is honoured everywhere.
- [ ] A concurrency test allocates from two goroutines and never returns the same ID twice.
- [ ] The slug in the file name is derived from the title and is stable and URL-safe.
