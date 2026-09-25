---
id: GIT-US-0107
type: story
title: Read and patch one requirement through the vault with its block rev
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
---

## Description

As an agent or the web app, I want to get, create, update and list single requirements, so that two people editing different requirements of the same spec never conflict, and every requirement behaves as its own unit.

## Acceptance Criteria

- [ ] `internal/vault` gains `ListRequirements`, `GetRequirement`, `CreateRequirement` and `UpdateRequirement` on the CoreApi contract; each read returns the block `rev`, each write requires it.
- [ ] `UpdateRequirement` patches the block text and/or its `requirements:` map entry only; a stale block `rev` fails with `stale_revision` and per-field `conflicts[]`, while a concurrent edit to another block of the same spec succeeds.
- [ ] `CreateRequirement` allocates `R<n>` as max+1 in the file, never reusing a removed number.
- [ ] Requirements appear as their own rows in list/search results (ref, spec, title, status).
- [ ] The same methods are exported through `wasm/main_js.go` for browser-only mode.
- [ ] Tests in `internal/vault` cover the conflict matrix; docs/07 describes the contract.

## Notes

Design: [[research/2026-09-24-spec-driven-development-overview]] §3.
