---
id: GIT-SP-0001
type: spec
title: The rev write protocol
status: in_review
labels: [core]
created: 2026-09-24T22:53:07Z
updated: 2026-09-24T22:53:44Z
started: 2026-09-24T22:53:07Z
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
  R9:
    status: in_review
  R10:
    status: in_review
---

## Purpose

Let many writers — the web app, the CLI, the companion's REST API and MCP agents — edit the same
Markdown files without a lock server and without losing anyone's change: every read returns a
`rev`, every write quotes the `rev` it is based on, and a write based on an older `rev` is refused
with enough information to decide what to do next.

## Scope

Covers the file rev (docs/03 §5), the `stale_revision` refusal and its `conflicts[]`, the
precondition that an MCP write carries a `rev`, the `*` waiver, and the per-requirement rev of
ADR-037 §6 (docs/03 §21.5). Board, sprint and retro files use the same file rev; their stores are
out of scope here. Comment writes quote the rev of their item.

## Glossary

- **file rev** — `sha256:` plus the first 16 hex characters of the SHA-256 of a file's canonical bytes.
- **block rev** — the same hash over one requirement block alone.
- **requirement rev** — the same hash over the block followed by the canonical JSON of its `requirements:` entry; the write token of one requirement.

## Requirements

### GIT-SP-0001.R1 — Compute the file rev from canonical bytes

The core SHALL compute a file's `rev` as `sha256:` followed by the first 16 lowercase hex characters of the SHA-256 of the file's canonical bytes: no UTF-8 BOM, LF line endings and exactly one trailing LF.

#### Scenario: line endings do not change the rev
- **WHEN** one file is read with CRLF line endings and a copy with LF line endings
- **THEN** both reads return the same `rev`

#### Scenario: the rev is never stored
- **WHEN** an item is written
- **THEN** its file holds no `rev` key
- **AND** the next read recomputes the `rev` from the bytes

### GIT-SP-0001.R2 — Refuse a write based on a stale rev

WHEN a conditional write quotes a `rev` that differs from the `rev` of the file on disk, the store SHALL refuse the write with `stale_revision`, carrying the current `rev`, and leave the file unchanged.

#### Scenario: two agents claim one story
- **GIVEN** two agents read `GIT-US-0042` at the same `rev`
- **WHEN** the first agent's `update_item` lands and the second quotes the same `rev`
- **THEN** the second write is refused with `stale_revision`
- **AND** `currentRev` is the `rev` the first write produced
- **AND** the first agent's assignee is still on disk

### GIT-SP-0001.R3 — Name the fields still in conflict

WHEN a write is refused with `stale_revision`, the store SHALL list in `conflicts[]` each field the write would still change against the content on disk now, as `{field, current, proposed}`, naming a body or block text without quoting it.

#### Scenario: only the loser's field is named
- **GIVEN** a write changed `priority` after the caller's read
- **WHEN** the caller's stale write changes `assignees`
- **THEN** `conflicts[]` holds one entry for `assignees`

#### Scenario: the body is named but never quoted
- **WHEN** a stale write replaces the body
- **THEN** `conflicts[]` holds `{field: body}` with no current or proposed text

### GIT-SP-0001.R4 — An empty conflicts list means the change already happened

WHEN every field of a stale write already holds the value the write proposes, the store SHALL refuse it with `stale_revision` and an empty `conflicts[]`, so the caller stops without writing.

#### Scenario: the winner made the same change
- **GIVEN** another writer already set `status: todo` on a requirement
- **WHEN** a stale `update_requirement` sets `status: todo`
- **THEN** the refusal carries `currentRev` and no `conflicts`

### GIT-SP-0001.R5 — Every MCP write carries a rev

IF an MCP write tool that takes a `rev` is called with an empty or blank `rev`, THEN the MCP server SHALL refuse the call with `precondition_required`, naming the missing field and what a correct value looks like.

#### Scenario: update_item without a rev
- **WHEN** `update_item` is called with `rev: ""`
- **THEN** the error code is `precondition_required`
- **AND** its `field` is `rev` and it carries `expected` and `retry`

#### Scenario: move_on_board without an item rev
- **WHEN** `move_on_board` is called with a board `rev` but an empty `itemRev`
- **THEN** the call is refused with `precondition_required` on `itemRev`

### GIT-SP-0001.R6 — Teach the retry in the stale refusal

WHEN the MCP server reports `stale_revision`, the error SHALL carry `currentRev`, the file path and a one-line `retry` that tells the agent to re-read with `get_item`, decide from `conflicts`, quote `currentRev` and never retry with `rev: "*"`.

#### Scenario: a stale update_item
- **WHEN** an agent's `update_item` quotes a superseded `rev`
- **THEN** the error carries `currentRev` and `path`
- **AND** its `retry` names `get_item`

### GIT-SP-0001.R7 — The wildcard rev is an explicit unsafe waiver

WHERE a write quotes `rev: "*"`, the MCP server SHALL hand the core an unconditional write, so a missing `rev` can never reach that path by omission.

#### Scenario: a wildcard requirement write
- **WHEN** `update_requirement` is called with `rev: "*"` and `status: todo`
- **THEN** the requirement's status is `todo` whatever its current `rev`

### GIT-SP-0001.R8 — Block rev and requirement rev

The core SHALL compute a requirement's block rev over its block text alone and its requirement rev over the block text followed by the canonical JSON of its `requirements:` entry, and the vault returns both on every requirement read as `blockRev` and `rev`.

#### Scenario: a status change moves only the requirement rev
- **WHEN** `update_requirement` changes only a requirement's `status`
- **THEN** its `rev` changes
- **AND** its `blockRev` stays the same

#### Scenario: another block is isolated
- **WHEN** a sibling block of the same spec is edited
- **THEN** neither hash of this requirement changes

### GIT-SP-0001.R9 — Only the requirement rev is a write token

IF a requirement write quotes the block rev or the spec's file rev instead of the requirement rev, THEN the vault SHALL refuse it with `stale_revision` carrying the current requirement rev.

#### Scenario: quoting blockRev
- **WHEN** `update_requirement` quotes the `blockRev` a read returned
- **THEN** the write is refused with `stale_revision`
- **AND** `currentRev` is the requirement rev

### GIT-SP-0001.R10 — Writes to sibling requirements never conflict

WHEN two writers update different requirements of one spec, each quoting the requirement rev it read, the vault SHALL apply both writes and keep each one's change.

#### Scenario: concurrent edits of R1 and R2
- **GIVEN** two writers read `R1` and `R2` of one spec
- **WHEN** the first rewrites the text of `R2` and the second then changes the status of `R1`
- **THEN** both writes succeed
- **AND** `R2` still holds the first writer's text

## Notes

Dogfood spec of GIT-US-0135. Implementation and tests carry `Implements:` and `Verifies:` markers
(docs/03 §21.7); `gintrack spec coverage` reports each requirement's state.
