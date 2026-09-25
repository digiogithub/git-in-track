---
id: GIT-EP-0022
type: epic
title: Spec data model
status: done
priority: critical
milestone: GIT-M-0015
author: claude
labels: [core, docs]
created: 2026-09-24T12:07:52Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Introduce the `spec` item type and requirement blocks on disk. A spec (`GIT-SP-NNNN`, type code `SP`, folder `docs/.pmngr/specs/`) holds purpose, scope and glossary, followed by requirement blocks `### GIT-SP-NNNN.R<n> — <title>` (EARS/SHALL statement plus `#### Scenario:` WHEN/THEN blocks). Per-requirement metadata lives in a front-matter `requirements:` map keyed by `R<n>` (`status`, `trace`, `verified`). New link kinds `implements`/`implemented_by`, `modifies`/`modified_by`, `supersedes`/`superseded_by`, with link targets accepting requirement refs.

Human-only area until 1.0 (on-disk formats): done by an agent under human supervision, in its own PR. ADR-037 and docs/03 are approved before any code.

## Acceptance Criteria

- [x] ADR-037 is accepted and docs/03-data-model.md describes specs, requirement blocks, the `requirements:` map and the new link kinds.
- [x] The core parses, validates, indexes and allocates specs and requirement IDs; `make wasm` passes.
- [x] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §3 and §7. Blocks almost every other Phase 11 story.
