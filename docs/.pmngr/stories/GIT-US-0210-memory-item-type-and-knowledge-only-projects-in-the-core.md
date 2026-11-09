---
id: GIT-US-0210
type: story
title: Memory item type and knowledge-only projects in the core
status: done
priority: high
parent: GIT-EP-0034
author: claude-code
assignees: [claude-code]
labels: [core, wasm, agent-ok]
estimate: 8
created: 2026-11-08T17:59:20Z
updated: 2026-11-09T13:00:00Z
---

## Description

Implement ADR-044 in `internal/core`: the `memory` item type with id code `MEM`, folders `.pmngr/memory/` by scope, the front-matter fields (`key`, `version`, `content_hash`, `local_id`, `valid_from`, `valid_to`, `superseded_by`), the fixed lifecycle with `proposed` in the triage category, the `supersedes`/`superseded_by` link kinds, and `kind: knowledge` in `project.yaml`. Human-only area (data model): design approved before code; docs/03 updated in the same change.

## Acceptance Criteria

- [x] Design approved by the supervisor before implementation.
- [x] `ACME-MEM-0001` parses, validates and is allocated by the existing allocator; other id types are unaffected.
- [x] `proposed` memory appears in inbox queries and nowhere in boards, sprints or metrics.
- [x] A `kind: knowledge` project indexes pages and memory, has no backlog, and keeps its key; the team pseudo-project uses the same path.
- [x] Golden files under `internal/core/testdata/` cover the new front matter; `make wasm` builds.
- [x] docs/03-data-model.md and AGENTS.md updated; ADR-044 moved to Accepted by the maintainer.

## Notes

Part of the gintrack hub PoC; see ADR-043 and ADR-044.

## Spec Delta

### MODIFIED GIT-SP-0003.R5 — Spec and memory link kinds need matching targets

WHEN an item-level link of kind `implements` or `modifies` targets neither a spec nor a requirement ref, or a `supersedes` or `superseded_by` link does not link a spec to a spec, or a memory writes `supersedes` to anything but a memory or writes `superseded_by` at all, the validator SHALL report `E-LINK-TARGET-TYPE`.

#### Scenario: implements a story
- **WHEN** a story declares `implements TEST-US-0002`
- **THEN** the validator reports `E-LINK-TARGET-TYPE`

#### Scenario: the older kinds may target specs
- **WHEN** a task declares `blocked_by TEST-SP-0003`
- **THEN** the validator reports nothing

#### Scenario: a memory supersedes a memory
- **WHEN** a memory declares `supersedes TEST-MEM-0002`
- **THEN** the validator reports nothing

#### Scenario: a memory supersedes a story
- **WHEN** a memory declares `supersedes TEST-US-0002`
- **THEN** the validator reports `E-LINK-TARGET-TYPE`

