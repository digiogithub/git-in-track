---
id: TEST-SP-0007
type: spec
title: Requirement diagnostics on file lines
status: in_progress
priority: medium
author: jose
labels:
  - core
  - specs
created: 2026-09-24T12:00:00Z
updated: 2026-09-25T09:00:00Z
requirements:
  R1:
    status: done
  R2:
    status: shipped
    trace:
      code: [/abs/path.go]
    links:
      - {kind: supersedes, target: TEST-SP-0001}
      - {kind: blocks, target: TEST-SP-0001.R1}
  R4: {}
  R9:
    status: todo
---

## Purpose

Every requirement diagnostic of this spec points at a line of this file, not of its body.

## Requirements

### TEST-SP-0007.R1 — Well formed

The system SHALL keep ids.

#### Scenario: kept
- **WHEN** an item is deleted
- **THEN** its id stays reserved

### TEST-SP-0007.R2 - Hyphen separator and a vague word

The system SHALL answer fast.

#### Scenario: answered
- **WHEN** a request arrives
- **THEN** it is answered

### TEST-SP-0007.R1 — Duplicated number

The system SHALL not be linted twice.

### Design notes

Prose, not a requirement.

### TEST-SP-0007.R02 — Padded number

### OTHER-SP-0001.R1 — Foreign spec

### TEST-SP-0007.R3 — No entry and no scenario

The system SHALL have no scenario.

### TEST-SP-0007.R4 — Entry without status

The system SHALL read as initial.

#### Scenario: initial
- **WHEN** the spec is read
- **THEN** the status is the initial one
