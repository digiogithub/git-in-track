---
id: GIT-SP-0003
type: spec
title: Link validation
status: in_review
labels: [core]
created: 2026-09-24T22:59:00Z
updated: 2026-09-24T22:59:36Z
started: 2026-09-24T22:59:00Z
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
---

## Purpose

Keep the typed relations between items and requirements trustworthy: a link names a known kind
and a well-formed target, is stored on one side only, and a target that the repository does not
hold is reported without being lost.

## Scope

Covers item-level `links` (docs/03 §12.1, R-LINK-1 to R-LINK-8), requirement-level
`requirements.R<n>.links` (R-REQ-13) and the dangling-target check of the index. The per-item
checks live in `internal/core/validate.go` and `internal/core/requirements.go`; the inverse edges
and dangling targets are derived by the index. Wikilinks and external references are out of
scope.

## Glossary

- **computed-only kind** — `implemented_by` and `modified_by`: the index derives them from `implements` and `modifies`, and no file may write them.
- **qualified target** — a target prefixed with `<KEY>/`, naming another project.

## Requirements

### GIT-SP-0003.R1 — Only known link kinds

IF an item's link has a kind other than `blocks`, `blocked_by`, `relates_to`, `duplicates`, `duplicated_by`, `implements`, `implemented_by`, `modifies`, `modified_by`, `supersedes` or `superseded_by`, THEN the validator SHALL report `E-ENUM` on that link's `kind`.

#### Scenario: an invented kind
- **WHEN** a story declares `{kind: replaces, target: GIT-US-0002}`
- **THEN** the validator reports `E-ENUM`

#### Scenario: kinds are case-sensitive
- **WHEN** a story declares a link of kind `Implements`
- **THEN** the kind is not a known kind

### GIT-SP-0003.R2 — Inverses are computed, never stored twice

The index SHALL derive the inverse of every declared link as a computed edge on its target, so a relation is stored on its source only and a relation declared on both sides appears once.

#### Scenario: blocks seen from the other side
- **GIVEN** `GIT-US-0001` declares `blocks GIT-US-0002`
- **WHEN** the index builds the link graph
- **THEN** `GIT-US-0002` has a computed `blocked_by GIT-US-0001` edge

#### Scenario: work that implements a requirement
- **GIVEN** a story declares `implements GIT-SP-0001.R2`
- **WHEN** the index answers which work implements `GIT-SP-0001.R2`
- **THEN** it returns that story through a computed `implemented_by` edge

### GIT-SP-0003.R3 — Computed-only kinds are refused

IF an item's links write the kind `implemented_by` or `modified_by`, THEN the validator SHALL refuse the link with `E-LINK-COMPUTED-ONLY` and report no target-type finding on top.

#### Scenario: a spec claims its implementer
- **WHEN** a spec declares `implemented_by GIT-US-0002`
- **THEN** the validator reports `E-LINK-COMPUTED-ONLY` only

#### Scenario: a write through the store
- **WHEN** a story update adds `modified_by GIT-SP-0004.R2`
- **THEN** the write is refused with `E-LINK-COMPUTED-ONLY`
- **AND** `project.yaml` keeps its schema

### GIT-SP-0003.R4 — Link target grammar

The validator SHALL accept as a link target only an item ID or a requirement ref `<SPEC-ID>.R<n>` with an unpadded number of at least 1, either bare in the current project or qualified as `<KEY>/`, reporting a malformed target as `E-ID-GRAMMAR` and a foreign or mismatched key as `E-ID-KEY`.

#### Scenario: a padded requirement number
- **WHEN** a story links `implements TEST-SP-0003.R02`
- **THEN** the validator reports `E-ID-GRAMMAR`

#### Scenario: an unqualified foreign requirement
- **WHEN** a story of project TEST links `implements WEB-SP-0003.R2`
- **THEN** the validator reports `E-ID-KEY`

#### Scenario: a qualified requirement of another project
- **WHEN** a story links `implements WEB/WEB-SP-0001.R4`
- **THEN** the validator reports nothing

### GIT-SP-0003.R5 — Spec link kinds need spec targets

WHEN an item-level link of kind `implements` or `modifies` targets neither a spec nor a requirement ref, or a `supersedes` or `superseded_by` link does not link a spec to a spec, the validator SHALL report `E-LINK-TARGET-TYPE`.

#### Scenario: implements a story
- **WHEN** a story declares `implements TEST-US-0002`
- **THEN** the validator reports `E-LINK-TARGET-TYPE`

#### Scenario: the older kinds may target specs
- **WHEN** a task declares `blocked_by TEST-SP-0003`
- **THEN** the validator reports nothing

### GIT-SP-0003.R6 — Requirement links allow three kinds

IF a `requirements.R<n>.links` entry has a kind other than `supersedes`, `superseded_by` or `relates_to`, THEN the validator SHALL report `E-REQ-FIELD` on that entry.

#### Scenario: implements inside a requirement
- **WHEN** a spec's `R2` entry declares `implements TEST-SP-0002.R1`
- **THEN** the validator reports `E-REQ-FIELD`

#### Scenario: supersedes needs a requirement target
- **WHEN** a spec's `R2` entry declares `supersedes TEST-SP-0002`
- **THEN** the validator reports `E-LINK-TARGET-TYPE`

### GIT-SP-0003.R7 — Dangling targets are warnings

WHEN a parent, milestone or link target names an item, spec or requirement block that the index does not hold, the index SHALL report `W-REF-DANGLING` as a warning and keep the link, while a qualified target of a project the repository does not hold is a remote reference and is not reported.

#### Scenario: a requirement with no block
- **GIVEN** `GIT-SP-0001` has no block `R9`
- **WHEN** a story links `implements GIT-SP-0001.R9`
- **THEN** the index reports `W-REF-DANGLING` for an unknown requirement on `links.implements`

#### Scenario: an unknown spec
- **WHEN** a story links `implements GIT-SP-0099.R1`
- **THEN** the index reports `W-REF-DANGLING` for an unknown spec

#### Scenario: a remote requirement
- **WHEN** a story links `implements WEB/WEB-SP-0001.R1` and project WEB is not in the repository
- **THEN** no `W-REF-DANGLING` is reported

## Notes

Dogfood spec of GIT-US-0136. Implementation and tests carry `Implements:` and `Verifies:` markers
(docs/03 §21.7).
