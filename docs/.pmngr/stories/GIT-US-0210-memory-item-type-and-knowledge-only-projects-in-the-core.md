---
id: GIT-US-0210
type: story
title: Memory item type and knowledge-only projects in the core
status: todo
priority: high
parent: GIT-EP-0034
author: claude-code
labels: [core, wasm, agent-ok]
estimate: 8
created: 2026-11-08T17:59:20Z
updated: 2026-11-08T17:59:20Z
---

## Description

Implement ADR-044 in `internal/core`: the `memory` item type with id code `MEM`, folders `.pmngr/memory/` by scope, the front-matter fields (`key`, `version`, `content_hash`, `local_id`, `valid_from`, `valid_to`, `superseded_by`), the fixed lifecycle with `proposed` in the triage category, the `supersedes`/`superseded_by` link kinds, and `kind: knowledge` in `project.yaml`. Human-only area (data model): design approved before code; docs/03 updated in the same change.

## Acceptance Criteria

- [ ] Design approved by the supervisor before implementation.
- [ ] `ACME-MEM-0001` parses, validates and is allocated by the existing allocator; other id types are unaffected.
- [ ] `proposed` memory appears in inbox queries and nowhere in boards, sprints or metrics.
- [ ] A `kind: knowledge` project indexes pages and memory, has no backlog, and keeps its key; the team pseudo-project uses the same path.
- [ ] Golden files under `internal/core/testdata/` cover the new front matter; `make wasm` builds.
- [ ] docs/03-data-model.md and AGENTS.md updated; ADR-044 moved to Accepted by the maintainer.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.
