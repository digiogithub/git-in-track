# ADR-026 — The web app soft-deletes an item, after showing what points at it

- **Status:** Accepted
- **Date:** 2026-09-06
- **Phase:** 1 (web app), closing a gap found by the backlog audit
- **Related:** [ADR-007](ADR-007-team-repo-references.md), [ADR-008](ADR-008-id-scheme.md)
- **Implements:** `GIT-US-0010` — Create and edit items in the Markdown editor

## Context

Until now there was no way to delete an item from the web app at all. The store
had both deletes — soft by default, hard on request (`core.DeleteOptions`) — and
the API and the CLI exposed them, but the UI offered neither, so the acceptance
criterion "deleting an item warns about inbound references and never leaves a
dangling parent" could not hold either way.

Adding the affordance forces two decisions.

**Which delete does a button mean?** A hard delete removes the file. That is
what a user who thinks of a tracker row expects, and it is exactly what breaks
this product's guarantees:

- The id disappears from the working tree, and the allocator can only avoid
  reusing it because it also scans `id_allocation.reserved` — a second source of
  truth for something the file used to say by existing (R-ID-3).
- Every `parent`, `milestone`, `links[]` entry, board card, sprint scope and
  promoted retro action that named the item becomes a dangling reference, in
  files the deleting user may not even have open — the team repository is a
  different clone.
- Two people deleting and editing the same item on two branches produce a
  delete/modify conflict that git cannot resolve on its own, and the resolution
  a merge picks by default is "keep the file", which resurrects it.

A soft delete has none of those properties. The file stays, flagged
`deleted: true` (docs/03 §7.1); it is excluded from every list, board, search
and metric, so it is gone as far as the user is concerned; the id keeps existing
where the allocator can see it; and every reference still resolves — to an item
marked deleted, which is a fact a reader can act on, rather than to nothing.

**What does "warns about inbound references" mean when the delete is soft?**
If nothing dangles, a refusal would be theatre. But the references are still
what the user needs to see: a story in a running sprint, a card on someone
else's board and a task whose parent is about to disappear from every list are
consequences worth reading before clicking, not after.

The project already has this shape of answer one level up:
`core.TeamProjectReferences` collects what a team's boards, sprints and retros
point at before a project is disconnected (ADR-007), and `team.project.remove`
refuses unless forced.

## Decision

1. **The web app always soft-deletes.** `provider.deleteItem` takes an optional
   `{ hard }`, the API keeps `?hard=true`, and the UI never sets either. A hard
   delete stays a deliberate act at the CLI and the API, where the user is
   asking for the file itself to go.

2. **The confirmation shows the inbound references first**, from a new core
   function `core.ItemReferences(id, sources)` — the item-level sibling of
   `TeamProjectReferences`, and a pure function over items and team artifacts
   the caller already loaded, so the browser and the companion warn about the
   same things. It covers `parent`, `milestone` and every `links[]` entry of
   every item of the project, plus the column orders and `filters.milestone` of
   every board, the `items` and `committed` of every sprint, and the `task` of
   every promoted retro action, across every open team repository.

3. **The reference list warns; it never refuses.** Nothing is left dangling by a
   soft delete, so there is nothing to force past. Children are called out
   separately, because a child is the one reference whose parent is about to
   read as deleted.

4. The analysis is a workspace-level call (`item.references`) because its two
   halves live in different repositories, and it is a read: it writes nothing
   and it is fetched when the dialog opens, not on every render of the item.

## Consequences

- Deleted items accumulate in the repository. That is the cost of a git-native
  model, and it is the same cost `git rm` avoids by keeping history: the files
  are small, they are excluded from every view, and `gintrack doctor` remains
  the place to reason about them in bulk.
- A user who genuinely wants the file gone has to reach for the CLI. The
  confirmation dialog says what the delete does, so this is a discoverable
  limit rather than a surprise.
- A future "delete permanently" affordance can be added on top without changing
  the model: the provider argument and the API parameter already exist, and it
  would be the same dialog with a second, more explicit button.
- The reference analysis is now available to anything else that needs it —
  re-parenting warnings, `doctor`, a future merge preview — because it lives in
  the core rather than in the dialog.
