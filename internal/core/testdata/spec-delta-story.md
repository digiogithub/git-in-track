---
id: ACME-US-0042
type: story
title: Harden reserved ranges
status: in_progress
author: jose
created: 2026-09-24T12:00:00Z
updated: 2026-09-24T12:00:00Z
---

## Description

Refuse inverted reserved ranges. The ### headings of this section are prose.

### ADDED ACME-SP-0003 — Not an operation: outside the Spec Delta

## Spec Delta

Prose before the first operation is ignored.

### ADDED ACME-SP-0003 — Reject a malformed reserved range

The allocator SHALL refuse a `reserved` range whose start is greater than its end.

#### Scenario: inverted range
- **WHEN** `reserved.task` is `[[249, 200]]`
- **THEN** project validation reports an error naming the range

### ADDED ACME-SP-0005 — Allocate spec numbers by index scan
Supersedes: ACME-SP-0003.R7

WHEN a spec is created, the allocator SHALL assign the next free number.

#### Scenario: a gap is kept
- **WHEN** the highest spec is `ACME-SP-0004`
- **THEN** the next spec is `ACME-SP-0005`

### MODIFIED ACME-SP-0003.R2 — Allocate the next ID by index scan

WHEN an item is created, the allocator shall assign the next number fast.

```markdown
### MODIFIED ACME-SP-0003.R8 — inside a fence, not an operation
```

#### Scenario: a stale counter hint is ignored
- **WHEN** `id_allocation.counters.task` is 12 and the scan finds 108
- **THEN** the next task is `ACME-T-0109`

### REMOVED ACME-SP-0003.R4 — Counters are authoritative

Reason: superseded by R2; counters are hints only.

### REMOVED ACME-SP-0003.R5 — Counters are cached

Nothing says why.

### MODIFIED ACME-SP-0003.R6 - Hyphen separator

The allocator SHALL keep numbers.

#### Scenario: kept
- **WHEN** an item is deleted
- **THEN** its number is not reused

### ADDED ACME-SP-0003.R9 — Numbered before it is done

The allocator SHALL log every allocation.

#### Scenario: logged
- **WHEN** an id is allocated
- **THEN** a log line names it

### ADDED ACME-SP-0003 — Bad supersedes line
Supersedes: the old rule

The allocator SHALL refuse a duplicate range.

#### Scenario: duplicate
- **WHEN** two ranges overlap
- **THEN** validation reports both

### CHANGED ACME-SP-0003.R2 — Unknown operation

### MODIFIED ACME-SP-0003 — Names the spec

### REMOVED ACME-SP-0003.R02 — Padded number

### ADDED ACME-US-0001 — Names a story

### MODIFIED ACME-SP-0003.R3

## Notes

### REMOVED ACME-SP-0003.R1 — Not an operation: the section has ended
