## Purpose

Lint fixture: every requirement below is either clean or breaks the grammar in
one documented way. Prose here is never linted, even when it is fast and easy.

## Requirements

### ACME-SP-0004.R1 — Ubiquitous and clean

The allocator SHALL assign the next free number of a type.

#### Scenario: a new task
- **GIVEN** the highest task is `ACME-T-0108`
- **WHEN** a task is created
- **AND** nothing else is written
- **THEN** it is `ACME-T-0109`

### ACME-SP-0004.R2 — Event-driven and clean

WHEN an item is saved, the store SHALL write it atomically.

#### Scenario: a save
- **WHEN** an item is saved
- **THEN** the file holds either the old or the new bytes
  - never a mixture of both

### ACME-SP-0004.R3 — Unwanted behaviour and clean

IF the file changed since it was read, THEN the store SHALL refuse the write.

#### Scenario: a stale rev
- **WHEN** the rev no longer matches
- **THEN** the write fails with `stale_revision`

### ACME-SP-0004.R4 — No SHALL at all

The store writes items quickly.

#### Scenario: a save
- **WHEN** an item is saved
- **THEN** it is on disk

### ACME-SP-0004.R5 — Lower-case keywords

when an item is saved, the store shall be robust.

### ACME-SP-0004.R6 — Two requirements in one

WHILE the vault is read-only, the store SHALL refuse writes and
SHALL report `read_only` as appropriate.

#### Scenario: THEN first
- **THEN** the write fails
- **WHEN** a write arrives

#### Scenario: no steps

### ACME-SP-0004.R7 — Malformed IF and steps

IF the index is stale the store SHALL rebuild it.

#### Scenario: odd steps
- **when** the index is stale
- **GIVEN** a stale index
- **SO** the index is rebuilt
- the index is fresh again

### ACME-SP-0004.R8 — Code spans are not prose

The linter SHALL NOT read `fast` or `SHALL` inside a code span.

#### Scenario: a code span
- **WHEN** a statement quotes `user-friendly`
- **THEN** no vague word is reported etc.

```text
### ACME-SP-0004.R9 — inside a fence, not a block
```
