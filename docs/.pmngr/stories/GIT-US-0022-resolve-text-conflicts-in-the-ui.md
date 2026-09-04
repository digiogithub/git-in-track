---
id: GIT-US-0022
type: story
title: Resolve text conflicts in the UI
status: in_review
created: 2026-09-03T00:00:00Z
updated: 2026-09-04
author: team
priority: high
parent: GIT-EP-0005
milestone: GIT-M-0005
estimate: 8
labels: [git, web, core]
links:
  - kind: blocked_by
    target: GIT-US-0021
---

## Description

As a user hitting a merge conflict, I want to resolve it inside git-in-track, so that a
conflict does not force me into a terminal and does not risk me silently dropping a
teammate's field.

Conflicts in Markdown bodies get a three-way view (mine, theirs, base) with per-hunk
selection and free editing. Conflicts in YAML front matter are resolved **field by field on
parsed values**, not on text, because a textual merge of front matter is exactly how an
assignee or a label gets silently lost (risk R5). Board `order:` lists get a dedicated
merge that preserves both sides' additions.

Nothing is auto-resolved silently: every field the tool merges automatically is shown, and
the user can always fall back to keep-mine, keep-theirs or manual editing.

## Acceptance Criteria

- [x] Conflicted files are listed with the conflict type and the affected fields.
- [x] Markdown bodies get a three-way diff with per-hunk selection and manual editing.
- [x] Front matter is merged per field on parsed values, never on raw text.
- [x] Board `order:` conflicts preserve additions from both sides.
- [x] Auto-merged fields are shown explicitly and can be overridden.
- [x] Keep-mine, keep-theirs and manual edit are available for every conflict.
- [x] Resolving writes a canonical file that validates and completes the rebase or merge.
- [x] Aborting restores the pre-sync state exactly.
- [x] Conflict scenarios are covered by scripted integration tests.

## Notes

Implemented on `feat/phase-4-git-sync`:

- `internal/core/merge.go` + `merge_text.go` — the three-way merge, front matter
  field by field on parsed values and the body hunk by hunk (diff3), with the
  result round-tripped through the emitter the editor writes with. WASM-safe, so
  browser mode runs the same rules through the `conflict.merge` core method.
- `internal/gitops` — `Backend.ConflictFile` (index stages 1/2/3, sides swapped
  back into the user's frame during a rebase) and `Backend.ResolvePath` (write,
  stage, continue). Reading works on both backends; applying is system-git only,
  as `Abort` and `Continue` already were.
- `internal/server/conflicts.go` — `GET /api/v1/sync/conflicts/file` and
  `POST /api/v1/sync/conflicts/resolve` (replacing the `not_implemented` stub),
  plus the `conflict.resolved` event.
- Web — `ConflictResolver`, wired into the sync panel, over new provider methods
  `readConflict` and `resolveConflict` implemented in all three providers. Browser
  mode plugs a merge driver into `isomorphic-git`, keeps the conflict in memory
  (nothing is written while it is open) and replays the merge with the accepted
  resolution to complete it.

Docs updated: 06 §5.7 (as built), §6.2, §7.1, §13; 07 (routes, events, the
`Backend` surface); 05 (SyncPanel, ConflictResolver, the provider contract); 03
(R-FMT-7); 02 (the `internal/gitops` row). No ADR: every decision here follows
the rules docs/06 §5 already fixed.
