---
id: GIT-EP-0030
type: epic
title: Spec importers for Spec Kit, OpenSpec and Kiro
status: backlog
priority: low
author: claude
labels: [cli, core]
created: 2026-09-24T12:08:40Z
updated: 2026-09-24T12:08:40Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Deferred interop: import existing specs from Spec Kit (`spec.md` with `FR-NNN`), OpenSpec (`openspec/specs/*/spec.md` with `### Requirement:` blocks) and Kiro (`.kiro/specs/*/requirements.md`, EARS) into `spec` items with requirement blocks, keeping a mapping from the source identifiers.

Not part of Phase 11: no milestone until a human schedules it.

## Acceptance Criteria

- [ ] Each importer produces lint-clean specs and is idempotent on re-import.
- [ ] Every story of this epic is done.

## Notes

Deferred by the reviewer on 2026-09-24 (GIT-T-0238, decision 6). Design: [[research/2026-09-24-spec-driven-development-overview]].
