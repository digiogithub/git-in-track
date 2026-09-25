---
id: GIT-US-0104
type: story
title: Draft ADR-037 and the docs/03 changes for specs and requirement blocks
status: todo
priority: critical
parent: GIT-EP-0022
milestone: GIT-M-0015
author: claude
labels: [docs, core]
estimate: 3
created: 2026-09-24T12:08:51Z
updated: 2026-09-24T12:08:51Z
---

## Description

As a maintainer, I want the spec data model written down and approved before any code changes the on-disk format, because data-model paths, ID formats and front-matter fields are a human-only area until 1.0.

Documentation only, no code. The PR is reviewed and approved by a human; the implementation story is blocked by this one.

## Acceptance Criteria

- [ ] `docs/adr/ADR-037-specs-with-requirement-blocks.md` records the decisions of GIT-T-0238: the `spec` type (`GIT-SP-NNNN`, type code `SP`, folder `docs/.pmngr/specs/`); requirement blocks `### GIT-SP-NNNN.R<n> — <title>` with EARS/SHALL statement and `#### Scenario:` WHEN/THEN; scoped IDs (permanent, next = max+1 in the file, moving = new ID + `supersedes`); the block `rev` definition (exact byte range hashed); the `requirements:` map (`status`, `trace: {code, tests}`, `verified: {rev, commit, at, by}`); statuses reuse the project workflow; `suspect` is computed, never stored.
- [ ] The ADR defines the link kinds `implements`/`implemented_by`, `modifies`/`modified_by`, `supersedes`/`superseded_by` and the link-target grammar extension accepting `GIT-SP-NNNN.R<n>`.
- [ ] The ADR defines the marker grammar `Implements:` / `Verifies:` (comment styles recognised) and the `## Spec Delta` section format (ADDED / MODIFIED / REMOVED).
- [ ] The ADR defines the `project.yaml` key for grammar-lint severity (warning by default, raisable to error).
- [ ] `docs/03-data-model.md` is updated (folder layout, ID table, front-matter fields, link kinds) and `docs/adr/README.md` lists ADR-037.
- [ ] A human has approved the PR.

## Notes

Source: [[research/2026-09-24-spec-driven-development-overview]] §3 and §7. Not `agent-ok`: an agent may draft it only under direct human supervision.
