---
id: GIT-US-0109
type: story
title: Parse the Spec Delta section of a story
status: backlog
priority: high
parent: GIT-EP-0023
milestone: GIT-M-0015
author: claude
labels: [core, wasm, agent-ok]
estimate: 5
created: 2026-09-24T12:09:24Z
updated: 2026-09-24T12:09:24Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
  - { kind: blocked_by, target: GIT-US-0106 }
  - { kind: blocked_by, target: GIT-US-0108 }
---

## Description

As an agent changing behaviour, I want to describe the change to the living spec inside my story as a `## Spec Delta` (ADDED / MODIFIED / REMOVED), so the proposal is reviewed with the story and the spec itself only changes when the work is done.

## Acceptance Criteria

- [ ] `internal/core` parses `## Spec Delta` into operations: ADDED (target spec, new block without a number), MODIFIED (existing ref, replacement block), REMOVED (existing ref, reason).
- [ ] Validation reports unknown targets, MODIFIED/REMOVED of a missing ref, and blocks failing the grammar linter.
- [ ] MODIFIED and REMOVED targets are reflected as `modifies` links in the index so impact and coverage see pending changes.
- [ ] Golden tests under `internal/core/testdata/`; `make wasm` passes.
- [ ] The format is documented in docs/03 as ADR-037 defined it.

## Notes

Borrowed from OpenSpec change proposals.
