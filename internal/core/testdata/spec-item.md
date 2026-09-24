---
title: Item ID allocation
type: spec
id: ACME-SP-0003
status: in_progress
labels: [backend]
assignees: [jose]
author: jose
created: 2026-09-24T12:00:00Z
updated: 2026-10-01T09:12:00Z
x-review: pending
requirements:
  R10:
    status: todo
  R2:
    x-owner: marta
    verified: {by: claude, at: 2026-10-01T09:12:00Z, commit: 9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21, rev: "sha256:4e1b9c0d7a3f2e61"}
    trace:
      tests: [internal/core/allocator_test.go#TestNextID/stale_counter]
      code: internal/core/allocator.go#NextID
    status: in_progress
  R1:
    status: done
  R3:
    status: cancelled
    links:
      - {target: ACME-SP-0001.R7, kind: supersedes}
---

## Purpose

Give every item a short, permanent, human-speakable id without a coordination service.

## Requirements

### ACME-SP-0003.R1 — IDs are never reused

The allocator SHALL NOT assign a number that any existing, deleted or reserved
item of the same type holds.

#### Scenario: a deleted item keeps its number
- **WHEN** `ACME-T-0107` is marked `deleted: true`
- **THEN** no later task is allocated `ACME-T-0107`


### ACME-SP-0003.R2 - Allocate the next ID by index scan

WHEN an item of type T is created, the allocator SHALL assign `max(existing numbers of T) + 1`.

```markdown
### ACME-SP-0003.R9 — not a heading, it is inside a fence
```

#### Scenario: a stale counter hint is ignored
- **WHEN** `id_allocation.counters.task` is 12 and the scan finds 108
- **THEN** the next task is `ACME-T-0109`

### Design notes

Prose, not a requirement.

### ACME-SP-0003.R10 — Counters are hints

The allocator SHALL treat `id_allocation.counters` as a hint only.

## Notes

Nothing else.
