---
id: GIT-US-0022
type: story
title: Resolve text conflicts in the UI
status: backlog
created: 2026-09-03T00:00:00Z
updated: 2026-09-03T00:00:00Z
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

- [ ] Conflicted files are listed with the conflict type and the affected fields.
- [ ] Markdown bodies get a three-way diff with per-hunk selection and manual editing.
- [ ] Front matter is merged per field on parsed values, never on raw text.
- [ ] Board `order:` conflicts preserve additions from both sides.
- [ ] Auto-merged fields are shown explicitly and can be overridden.
- [ ] Keep-mine, keep-theirs and manual edit are available for every conflict.
- [ ] Resolving writes a canonical file that validates and completes the rebase or merge.
- [ ] Aborting restores the pre-sync state exactly.
- [ ] Conflict scenarios are covered by scripted integration tests.
