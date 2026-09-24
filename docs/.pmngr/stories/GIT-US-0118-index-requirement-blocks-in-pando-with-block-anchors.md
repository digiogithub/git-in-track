---
id: GIT-US-0118
type: story
title: Index requirement blocks in Pando with block anchors
status: in_review
priority: high
parent: GIT-EP-0025
milestone: GIT-M-0015
author: claude
labels: [server, agent-ok]
estimate: 5
created: 2026-09-24T12:10:32Z
updated: 2026-09-24T18:41:30Z
links:
  - { kind: blocked_by, target: GIT-US-0105 }
---

## Description

As an agent searching by meaning, I want each requirement indexed on its own, so a semantic hit resolves to `GIT-SP-NNNN.R<n>` and its block anchor rather than to the whole spec file.

Pando's code index skips dot-directories, so it never sees `docs/.pmngr/specs/`; the reindex job must feed requirement blocks explicitly. Pando must never write specs (`kb_add_document` strips front matter), so it only receives derived documents.

## Acceptance Criteria

- [ ] The reindex job (per repository and global) sends one derived document per requirement block, keyed by its ref, carrying spec title, statement and scenarios; stale documents are deleted when a block is removed or its `rev` changes.
- [x] `search_semantic` and workspace search return requirement hits as their own rows with `ref`, spec ID and anchor (`#git-sp-nnnn-r<n>` or the anchor ADR-037 defines).
- [x] Nothing is ever written back to `docs/.pmngr/specs/` from Pando.
- [x] Tests with a fake Pando client; docs for semantic search and ADR-036 follow-up note updated.

## Notes

Decision 1 of GIT-T-0238: every requirement has its own Pando entry.
