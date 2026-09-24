---
id: GIT-US-0119
type: story
title: Resolve requirement impact of a diff in three tiers
status: backlog
priority: high
parent: GIT-EP-0025
milestone: GIT-M-0015
author: claude
labels: [server, agent-ok]
estimate: 8
created: 2026-09-24T12:10:32Z
updated: 2026-09-24T12:10:32Z
links:
  - { kind: blocked_by, target: GIT-US-0112 }
  - { kind: blocked_by, target: GIT-US-0114 }
  - { kind: blocked_by, target: GIT-US-0116 }
  - { kind: blocked_by, target: GIT-US-0117 }
  - { kind: blocked_by, target: GIT-US-0118 }
---

## Description

As an agent or CI, I want the set of requirements a diff affects, each with the reason it was hit, its coverage status and whether it is suspect, so I do not re-read every spec.

## Acceptance Criteria

- [ ] A native impact package (e.g. `internal/impact`) takes `base..head` (or worktree), uses `gitops.ChangedFiles` to get files and line ranges, and maps them to changed symbols.
- [ ] Tier 1 (direct): requirements whose markers or `trace:` entries sit in changed files/symbols. Tier 2 (transitive): Pando `ImpactAnalysis` over changed symbols reaching marked callers, with depth. Tier 3 (semantic): requirement-block search from changed symbol names and the story title, flagged `candidate` with score. Requirements with pending `modifies` links are included.
- [ ] Each hit carries `ref`, `tier`, `reason` (file/symbol/path of calls), coverage status and `suspect`; suspect now also covers transitive changes.
- [ ] Tiers 1–2 are deterministic: the same diff and index give byte-identical results (golden test). Without Pando, tiers 2–3 report `unavailable` and tier 1 still answers.
- [ ] Installed into the vault through a host seam; browser-only answers `unavailable`; `make wasm` passes.
- [ ] Tests with a fixture repository and a fake Pando client; docs updated.

## Notes

The core differentiator of Phase 11.
