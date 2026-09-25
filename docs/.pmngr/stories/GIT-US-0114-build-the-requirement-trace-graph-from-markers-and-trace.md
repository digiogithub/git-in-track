---
id: GIT-US-0114
type: story
title: Build the requirement trace graph from markers and trace entries
status: backlog
priority: high
parent: GIT-EP-0024
milestone: GIT-M-0015
author: claude
labels: [server, agent-ok]
estimate: 3
created: 2026-09-24T12:10:07Z
updated: 2026-09-24T12:10:07Z
links:
  - { kind: blocked_by, target: GIT-US-0106 }
  - { kind: blocked_by, target: GIT-US-0113 }
---

## Description

As the coverage and impact engines, I want one deterministic trace per requirement — code symbols and tests, from markers and from the optional `trace:` entry — plus the stories linked through `implements`/`modifies`.

## Acceptance Criteria

- [ ] `internal/trace` merges scanned markers with `requirements.R<n>.trace.code[]` / `.tests[]` (`path#Symbol`) and `links[]` into a graph queryable both ways (requirement → code/tests/stories, file/symbol → requirements).
- [ ] `trace:` entries whose path or symbol no longer exists are reported as broken (warning).
- [ ] The graph is installed into the vault through a host seam (like `vault.SemanticSearcher`), so browser-only mode answers `unavailable`.
- [ ] Output is stable-sorted so the same repository state yields identical results.
- [ ] Table-driven tests; docs/03 explains marker vs `trace:` precedence.

## Notes

Depends on the marker scanner and the spec link kinds.
