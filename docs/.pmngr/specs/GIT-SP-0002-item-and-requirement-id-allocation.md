---
id: GIT-SP-0002
type: spec
title: Item and requirement ID allocation
status: in_review
labels: [core]
created: 2026-09-24T22:53:55Z
updated: 2026-09-24T22:54:17Z
started: 2026-09-24T22:53:55Z
requirements:
  R1:
    status: in_review
  R2:
    status: in_review
  R3:
    status: in_review
  R4:
    status: in_review
  R5:
    status: in_review
  R6:
    status: in_review
  R7:
    status: in_review
  R8:
    status: in_review
---

## Purpose

Give every item and every requirement a short, permanent, human-speakable ID without a
coordination service: IDs are allocated by scanning what is already in the repository, never
reused, and never renumbered.

## Scope

Covers item IDs `<KEY>-<CODE>-<NNNN>` (docs/03 §4.1–§4.5) and requirement numbers `R<n>`
(docs/03 §21.3, R-REQ-4 and R-REQ-5), as allocated by `internal/core` and reached through the
vault and MCP create tools. The per-user `ranges` strategy and the renumber planner are out of
scope.

## Glossary

- **scan** — reading every item file of the backlog folders, including soft-deleted and unparseable ones.
- **counter hint** — `id_allocation.counters.<type>` in `project.yaml`; a hint, never authoritative.
- **inbound ref** — a requirement ref to a spec found elsewhere in the project index: a link target or a Spec Delta heading.

## Requirements

### GIT-SP-0002.R1 — Allocate the next item ID by scan

WHEN an item of a type is created, the allocator SHALL assign one more than the highest number of that type found by scanning the backlog folders.

#### Scenario: the scan decides
- **GIVEN** the stories folder holds `ACME-US-0001` and `ACME-US-0007`
- **WHEN** a story is created
- **THEN** it is allocated `ACME-US-0008`

#### Scenario: an empty type starts at one
- **WHEN** the first epic of a project is created
- **THEN** it is allocated `ACME-EP-0001`

### GIT-SP-0002.R2 — Gaps are never filled

WHEN numbers below the highest number of a type are free, the allocator SHALL still allocate above the highest number and leave the gap unfilled.

#### Scenario: a gap stays a gap
- **GIVEN** the stories folder holds `ACME-US-0001` and `ACME-US-0007` only
- **WHEN** a story is created
- **THEN** no story is allocated a number from 2 to 6

### GIT-SP-0002.R3 — Numbers are permanent

The allocator SHALL NOT allocate a number that a soft-deleted item, an item whose front matter does not parse, a renumbering redirect or a reserved range of the same type holds.

#### Scenario: a soft-deleted item keeps its number
- **GIVEN** `ACME-US-0009` is marked `deleted: true` and is the highest story
- **WHEN** a story is created
- **THEN** it is allocated `ACME-US-0010`

#### Scenario: a broken file keeps its number
- **GIVEN** `ACME-US-0012` has front matter that does not parse
- **WHEN** a story is created
- **THEN** it is allocated `ACME-US-0013`

#### Scenario: a reserved range is skipped
- **GIVEN** tasks 200 to 249 are reserved and no task exists
- **WHEN** a task is created
- **THEN** it is allocated `ACME-T-0250`

### GIT-SP-0002.R4 — Counters are hints

The allocator SHALL treat `id_allocation.counters` in `project.yaml` as a hint, allocating one more than the larger of the hint and the scanned maximum.

#### Scenario: a stale hint is ignored
- **GIVEN** the story counter is 2 and the scan finds `ACME-US-0005`
- **WHEN** a story is created
- **THEN** it is allocated `ACME-US-0006`

#### Scenario: a hint above the scan wins
- **GIVEN** the story counter is 40 and the scan finds `ACME-US-0005`
- **WHEN** a story is created
- **THEN** it is allocated `ACME-US-0041`

### GIT-SP-0002.R5 — Concurrent allocation never repeats a number

WHILE several writers of one process allocate IDs of one type at once, the allocator SHALL hand out each number at most once, even before any of their files is written.

#### Scenario: eight concurrent writers
- **WHEN** eight goroutines each allocate six stories from an empty project
- **THEN** 48 distinct IDs are handed out
- **AND** the sequence runs from `ACME-US-0001` to `ACME-US-0048` without a gap

### GIT-SP-0002.R6 — Item ID format

The core SHALL format an item ID as `<KEY>-<CODE>-<NNNN>`, zero-padding the number to at least four digits and keeping every digit of a larger number.

#### Scenario: padding and overflow
- **WHEN** story 42 and task 10234 of project ACME are formatted
- **THEN** the IDs are `ACME-US-0042` and `ACME-T-10234`

### GIT-SP-0002.R7 — Allocate the next requirement number

WHEN a requirement is created in a spec, the core SHALL number it one more than the highest number among the spec's block headings, its `requirements:` keys and every inbound ref to that spec in the project index.

#### Scenario: an orphan entry keeps its number
- **GIVEN** a spec has a block `R1` and `requirements:` keys `R1` and `R5`
- **WHEN** a requirement is created
- **THEN** it is numbered `R6`

#### Scenario: an inbound link keeps a deleted number
- **GIVEN** a task links `implements` to `R7` of a spec whose highest block is `R3`
- **WHEN** a requirement is created in that spec
- **THEN** it is numbered `R8`

### GIT-SP-0002.R8 — A Spec Delta reserves the numbers it names

WHEN a Spec Delta names a requirement ref as a `MODIFIED` or `REMOVED` target or on a `Supersedes:` line, the index SHALL count that ref as an inbound ref for allocation whatever the item's status, while an unapplied `ADDED` holds no number.

#### Scenario: delta refs are reserved
- **GIVEN** a story's delta removes `R12` and supersedes `R14` of a spec whose highest block is `R3`
- **WHEN** the index computes the next requirement number of that spec
- **THEN** it is 15

## Notes

Dogfood spec of GIT-US-0135. Implementation and tests carry `Implements:` and `Verifies:` markers
(docs/03 §21.7); `gintrack spec coverage` reports each requirement's state.
