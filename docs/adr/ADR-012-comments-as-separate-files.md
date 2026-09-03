# ADR-012 — Comments are separate files, not inline in the item

- **Status:** Accepted
- **Date:** 2026-09-03
- **Phase:** 1 (Browser-only MVP)
- **Related:** [ADR-001](ADR-001-markdown-yaml-storage.md), [ADR-002](ADR-002-git-as-only-sync.md), [ADR-008](ADR-008-id-scheme.md)

## Context

Discussion is a core part of project management: a story accumulates questions,
decisions, status notes, and — increasingly — reports from AI agents about what they
did. In a file-based system there are two natural places to put it: appended to the
item's own Markdown file, or stored as separate files.

The forces are all about concurrency and diff quality, because git is the only sync
mechanism ([ADR-002](ADR-002-git-as-only-sync.md)):

- Comments are **append-heavy**, produced by several actors, often at the same time.
  Two people commenting on the same story while offline is a routine event, not an
  edge case.
- Appending to the same region of the same file from two branches is the classic
  case git *cannot* merge automatically. Inline comments would make the most common
  collaborative action the most conflict-prone one.
- Item files are read on every index scan. Bodies that grow without bound make every
  scan slower and every item diff noisier.
- An agent that adds a progress note should not have to rewrite a file a human is
  currently editing.

## Decision

**Each comment is its own Markdown file, under a per-item directory.**

```
.pmngr/comments/<ITEM-ID>/<timestamp>-<author>.md
```

For example `.pmngr/comments/ACME-US-0042/20260901T102211Z-dana.md`:

```markdown
---
item: ACME-US-0042
author: dana
created: 2026-09-01T10:22:11Z
---

SSO metadata endpoint is live in staging. Group claim mapping still open —
see ACME-TSK-0117.
```

- The filename is `<UTC timestamp, basic ISO-8601>-<author>.md`, which sorts
  chronologically in any file listing and makes collisions between two authors
  impossible. Two comments by the same author within the same second get a short
  suffix.
- Front matter carries `item`, `author`, `created`; the body is free Markdown.
- Comments are never edited by the app on someone else's behalf; editing your own
  comment rewrites your own file. Deleting removes the file, and git keeps the
  history.
- The index maps `itemID -> []CommentRef`, sorted by timestamp, so the UI renders a
  thread without reading bodies until they are displayed.
- Threading is flat in v1. An optional `replyTo` front-matter field is reserved for a
  future threaded view.

## Consequences

**Positive**

- **Concurrent comments never conflict.** Two people commenting offline produce two
  new files with different names; git merges them without a decision. This is the
  single largest reason for the decision.
- Item files stay small and their diffs stay about the item — a status change is one
  changed line, not one line lost in a growing discussion log.
- Agents can append notes safely while a human edits the item body, with no `rev`
  contention on the item.
- Per-comment authorship and timestamps are explicit in front matter *and*
  corroborated by git history.
- Comment bodies are excluded from item scans, so indexing cost scales with items,
  not with discussion volume.

**Negative**

- **File count grows quickly.** A busy story can produce dozens of small files; a
  large project reaches thousands. Directory-per-item keeps listings manageable, and
  the indexer reads comment front matter lazily, but `git status` and file explorers
  do get busier.
- **Reading a full thread means many small reads.** Mitigated by front-matter-only
  indexing and by reading bodies only for the thread actually on screen.
- **The item file alone is no longer the whole story.** Someone reading
  `ACME-US-0042-login-with-sso.md` in a plain editor sees no discussion. Mitigated by
  a stable, discoverable path convention documented in the format specification.
- **Deletion is a real file deletion**, which some users expect to be a soft delete.
  Git history is the recovery path and must be explained.
- **Author identity is a plain string.** There is no identity system
  ([ADR-002](ADR-002-git-as-only-sync.md)); `author` is conventional and can be
  cross-checked against the commit author, not enforced.

## Alternatives considered

- **Inline comments appended to the item body under a `## Comments` heading.** One
  file to read, trivially visible in any editor and in GitHub's preview — and every
  concurrent comment is a merge conflict in the same region of the same file, plus an
  item file that grows without bound and pollutes every item diff. Rejected on the
  conflict behaviour alone.
- **A single `comments.md` per item, next to the item file.** Better than inline
  (item diffs stay clean) and it still conflicts on every concurrent append.
  Rejected.
- **One JSON/YAML file per item containing an array of comments.** Same append
  conflict, plus escaped Markdown bodies that are unreadable in a diff. Rejected.
- **A per-project comments log (one file, all items).** Maximum conflict surface and
  no locality. Rejected.
- **Using git notes or commit messages as the discussion channel.** Elegant for
  developers, invisible to everyone else, and poorly supported by hosting UIs.
  Rejected.
