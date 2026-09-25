---
id: GIT-US-0108
type: story
title: Lint requirement grammar with configurable severity
status: done
priority: medium
parent: GIT-EP-0023
milestone: GIT-M-0015
author: claude
labels: [core, wasm, agent-ok]
estimate: 5
created: 2026-09-24T12:09:24Z
updated: 2026-09-25T11:15:00Z
closed: 2026-09-25T11:15:00Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

As a spec author, I want each requirement checked against a constrained grammar, so specs stay testable and unambiguous without a human reviewer catching every vague word.

## Acceptance Criteria

- [x] A linter in `internal/core` (e.g. `speclint.go`) checks each block: one EARS pattern or a SHALL statement, at least one `#### Scenario:` with WHEN and THEN, no vague words from a configurable list ("fast", "user-friendly", "as appropriate", ...).
- [x] Findings carry the requirement ref, a rule ID and a line; severity is `warning` by default and follows the `project.yaml` key defined in ADR-037 (raisable to `error`).
- [x] `error` severity makes item validation fail; `warning` does not.
- [x] Table-driven tests plus golden files under `internal/core/testdata/`; `make wasm` passes.
- [x] docs/03 (or the spec section of the data model doc) lists the rules.

## Notes

Decision 4 of GIT-T-0238. The web editor consumes it in the live-lint story.
