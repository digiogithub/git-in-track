---
id: GIT-EP-0023
type: epic
title: Spec authoring, grammar lint and Spec Delta
status: in_progress
priority: high
milestone: GIT-M-0015
author: claude
labels: [core, wasm, server]
created: 2026-09-24T12:07:58Z
updated: 2026-09-25T11:15:00Z
started: 2026-09-25T11:15:00Z
links:
  - { kind: relates_to, target: GIT-T-0238 }
---

## Description

Make specs pleasant and safe to write. Address one requirement at a time through the vault (get/update with its block `rev`, allocate the next `R<n>`), lint the grammar (EARS / SHALL / scenarios / vague words) as a warning configurable in `project.yaml`, parse a story's `## Spec Delta` (ADDED / MODIFIED / REMOVED) and apply it when the story is done, and offer templates plus duplicate detection on create.

Everything that parses or lints lives in `internal/core` so it runs in the browser through WASM.

## Acceptance Criteria

- [ ] One requirement can be read and patched on its own through the vault with block-rev conflict detection.
- [ ] The linter reports warnings by default and errors when `project.yaml` raises it.
- [ ] A story's Spec Delta is applied to the target specs when the story moves to a done status.
- [ ] Every story of this epic is done.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]].
