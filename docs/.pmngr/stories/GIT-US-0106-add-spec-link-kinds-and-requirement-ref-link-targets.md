---
id: GIT-US-0106
type: story
title: Add spec link kinds and requirement-ref link targets
status: in_review
priority: high
parent: GIT-EP-0022
milestone: GIT-M-0015
author: claude
labels: [core]
estimate: 5
created: 2026-09-24T12:09:05Z
updated: 2026-09-24T17:23:41Z
links:
  - { kind: blocked_by, target: GIT-US-0104 }
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

As an agent, I want to link a story to the requirement it implements or modifies, so the trace and impact engines can follow `links[]` down to a single requirement block.

## Acceptance Criteria

- [x] `implements`/`implemented_by`, `modifies`/`modified_by` and `supersedes`/`superseded_by` are valid link kinds with their inverses computed by the index.
- [x] `validateLinkTarget` in `internal/core/validate.go` accepts `GIT-SP-NNNN.R<n>` and reports a dangling target when the spec or the block does not exist.
- [x] Backlinks resolve to the requirement block: the index answers "which stories implement GIT-SP-NNNN.R<n>".
- [x] Table-driven tests in `internal/core` cover each kind and the target grammar; `make wasm` passes.
- [x] docs/03 link-kind table matches the implementation.

## Notes

Blocked by the ADR-037 story and the spec type story. Not `agent-ok` (data model).

Maintainer decision (2026-09-24, ADR-037 third review round): spec links are one-sided. For the first criterion, `implemented_by` and `modified_by` are known kinds only as computed inverses; writing one is `E-LINK-COMPUTED-ONLY` (docs/03 R-LINK-8).
