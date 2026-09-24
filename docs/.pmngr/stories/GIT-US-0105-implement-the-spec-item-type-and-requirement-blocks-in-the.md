---
id: GIT-US-0105
type: story
title: Implement the spec item type and requirement blocks in the core
status: backlog
priority: critical
parent: GIT-EP-0022
milestone: GIT-M-0015
author: claude
labels: [core, wasm]
estimate: 8
created: 2026-09-24T12:09:05Z
updated: 2026-09-24T12:09:05Z
links:
  - { kind: blocked_by, target: GIT-US-0104 }
---

## Description

As a user, I want `spec` items with requirement blocks to be first-class in the core, so every other part of Phase 11 can read, validate and index them exactly as ADR-037 describes.

Code implementation of the approved ADR-037, in its own PR, under human supervision.

## Acceptance Criteria

- [ ] `internal/core/model.go` adds `spec` to the closed item-type enum and models the `requirements:` map (`status`, `trace`, `verified`).
- [ ] `internal/core/ids.go` accepts `GIT-SP-NNNN` and parses requirement refs `GIT-SP-NNNN.R<n>`; `allocator.go` allocates spec IDs and the next `R<n>` as max+1 within a file, never reusing a number.
- [ ] `specs/` is known to `allocator.go`, `index.go`, `teamdiscover.go` and `scaffold.go`.
- [ ] A requirement-block parser extracts each `### <ref> — <title>` block (statement, scenarios, byte range) and computes the block `rev`; editing one block leaves every other block's `rev` unchanged.
- [ ] Validation: duplicate or malformed requirement IDs, map keys without a block and blocks without a map entry are reported; requirement statuses are validated against the project workflow.
- [ ] The JSON Schema for items includes `spec` and the `requirements:` map.
- [ ] Golden tests under `internal/core/testdata/` cover parsing and block `rev`; `make test`, `make lint` and `make wasm` pass.

## Notes

Blocked by the ADR-037 story. Not `agent-ok`: human-only area (on-disk format), agent works under supervision.
