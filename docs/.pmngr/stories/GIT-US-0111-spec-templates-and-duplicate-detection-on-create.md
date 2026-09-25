---
id: GIT-US-0111
type: story
title: Spec templates and duplicate detection on create
status: backlog
priority: low
parent: GIT-EP-0023
milestone: GIT-M-0015
author: claude
labels: [core, server, agent-ok]
estimate: 3
created: 2026-09-24T12:09:37Z
updated: 2026-09-24T12:09:37Z
links:
  - { kind: blocked_by, target: GIT-US-0107 }
  - { kind: blocked_by, target: GIT-US-0108 }
---

## Description

As an author, I want a spec and a requirement to start from a template, and to be warned when a new requirement looks like one that already exists, so the spec set does not fill up with near-duplicates.

## Acceptance Criteria

- [ ] `gintrack init`/scaffold ships a spec template (purpose, scope, glossary, one example requirement block) and a requirement-block template; both pass the grammar linter.
- [ ] `CreateRequirement` asks the host-installed semantic searcher (`vault.SemanticSearcher` seam) for similar requirement blocks and returns them as non-blocking `similar[]` with scores; without Pando it returns no suggestions and no error.
- [ ] No Pando or network code enters `internal/core` or `internal/vault`; `make wasm` passes.
- [ ] Tests with a fake searcher; docs/03 mentions the templates.

## Notes

Requirement blocks must be in the Pando index for good suggestions (impact epic).
