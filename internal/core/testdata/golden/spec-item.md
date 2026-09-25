---
id: ACME-SP-0003
type: spec
title: Item ID allocation
status: in_progress
assignees: [jose]
author: jose
labels: [backend]
created: 2026-09-24T12:00:00Z
updated: 2026-10-01T09:12:00Z
requirements:
  R1:
    status: done
  R2:
    status: in_progress
    trace:
      code: [internal/core/allocator.go#NextID]
      tests: [internal/core/allocator_test.go#TestNextID/stale_counter]
    verified: {rev: "sha256:4e1b9c0d7a3f2e61", commit: 9c1f0a2e5b7d4c3e8f1a6b2d9e0c7f4a3b5d8e21, at: 2026-10-01T09:12:00Z, by: claude}
    x-owner: marta
  R3:
    status: cancelled
    links:
      - { kind: supersedes, target: ACME-SP-0001.R7 }
  R10:
    status: todo
x-review: pending
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
